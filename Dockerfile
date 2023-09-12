# syntax = docker/dockerfile:1.5

ARG BINFMT_VERSION=qemu-v6.1.0
ARG WASI_SDK_VERSION=19
ARG WASI_SDK_VERSION_FULL=${WASI_SDK_VERSION}.0
ARG WASI_VFS_VERSION=v0.3.0
ARG WIZER_VERSION=04e49c989542f2bf3a112d60fbf88a62cce2d0d0
ARG EMSDK_VERSION=3.1.40 # TODO: support recent version
ARG BINARYEN_VERSION=114
ARG BUSYBOX_VERSION=1_36_1
ARG RUNC_VERSION=v1.1.9

# ARG LINUX_LOGLEVEL=0
# ARG INIT_DEBUG=false
ARG LINUX_LOGLEVEL=7
ARG INIT_DEBUG=true
ARG VM_MEMORY_SIZE_MB=128
ARG NO_VMTOUCH=

ARG OUTPUT_NAME=out.wasm # for wasi
ARG JS_OUTPUT_NAME=out # for emscripten; must not include "."
ARG OPTIMIZATION_MODE=wizer # "wizer" or "native"
# ARG OPTIMIZATION_MODE=native

ARG SOURCE_REPO=https://github.com/ktock/container2wasm
ARG SOURCE_REPO_VERSION=v0.5.0

FROM scratch AS oci-image-src
COPY . .

FROM ubuntu:22.04 AS assets-base
ARG SOURCE_REPO
ARG SOURCE_REPO_VERSION
RUN apt-get update && apt-get install -y git
RUN git clone -b ${SOURCE_REPO_VERSION} ${SOURCE_REPO} /assets
FROM scratch AS assets
COPY --link --from=assets-base /assets /

FROM golang:1.21-bullseye AS golang-base

FROM golang-base AS bundle-dev
ARG TARGETPLATFORM
ARG INIT_DEBUG
ARG OPTIMIZATION_MODE
ARG NO_VMTOUCH
COPY --link --from=assets / /work
WORKDIR /work
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -o /bin/create-spec ./cmd/create-spec
COPY --link --from=oci-image-src / /oci
# This step creates the following files
# <vm-rootfs>/oci/rootfs          : rootfs dir this Dockerfile creates container's rootfs and used by the container.
# <vm-rootfs>/oci/image.json      : container image config file used by init
# <vm-rootfs>/oci/spec.json       : container runtime spec file used by init
# <vm-rootfs>/oci/initconfig.json : configuration file for init
RUN mkdir -p /out/oci/rootfs /out/oci/bundle && \
    IS_WIZER=false && \
    if test "${OPTIMIZATION_MODE}" = "wizer" ; then IS_WIZER=true ; fi && \
    NO_VMTOUCH_F=false && \
    if test "${OPTIMIZATION_MODE}" = "native" ; then NO_VMTOUCH_F=true ; fi && \
    if test "${NO_VMTOUCH}" != "" ; then NO_VMTOUCH_F="${NO_VMTOUCH}" ; fi && \
    create-spec --debug=${INIT_DEBUG} --debug-init=${IS_WIZER} --no-vmtouch=${NO_VMTOUCH_F} \
                --image-config-path=/oci/image.json \
                --runtime-config-path=/oci/spec.json \
                --rootfs-path=/oci/rootfs \
                /oci "${TARGETPLATFORM}" /out/oci/rootfs && \
    mv image.json spec.json /out/oci/ && mv initconfig.json /out/oci/

FROM ubuntu:22.04 AS gcc-riscv64-linux-gnu-base
RUN apt-get update && apt-get install -y gcc-riscv64-linux-gnu libc-dev-riscv64-cross git make

FROM gcc-riscv64-linux-gnu-base AS bbl-dev
WORKDIR /work-buildroot/
RUN git clone https://github.com/riscv-software-src/riscv-pk
WORKDIR /work-buildroot/riscv-pk
RUN git checkout 7e9b671c0415dfd7b562ac934feb9380075d4aa2
RUN mkdir build
WORKDIR /work-buildroot/riscv-pk/build
RUN ../configure --host=riscv64-linux-gnu
RUN cat ../machine/htif.c ../bbl/bbl.lds
# HTIF address needs to be static on TinyEMU
RUN sed -i 's/volatile uint64_t tohost __attribute__((section(".htif")));/#define tohost *(uint64_t*)0x40008000/' ../machine/htif.c && \
    sed -i 's/volatile uint64_t fromhost __attribute__((section(".htif")));/#define fromhost *(uint64_t*)0x40008008/' ../machine/htif.c
RUN make bbl
RUN riscv64-linux-gnu-objcopy -O binary bbl bbl.bin && \
    mkdir /out/ && \
    mv bbl.bin /out/

FROM gcc-riscv64-linux-gnu-base AS linux-riscv64-dev-common
RUN apt-get update && apt-get install -y gperf flex bison bc
RUN mkdir /work-buildlinux
WORKDIR /work-buildlinux
RUN git clone -b v6.1 --depth 1 https://github.com/torvalds/linux

FROM linux-riscv64-dev-common AS linux-riscv64-dev
WORKDIR /work-buildlinux/linux
COPY --link --from=assets /patches/tinyemu/linux_rv64_config ./.config
RUN make ARCH=riscv CROSS_COMPILE=riscv64-linux-gnu- -j$(nproc) all && \
    mkdir /out && \
    mv /work-buildlinux/linux/arch/riscv/boot/Image /out/Image && \
    make clean

FROM linux-riscv64-dev-common AS linux-riscv64-config-dev
WORKDIR /work-buildlinux/linux
COPY --link --from=assets /patches/tinyemu/linux_rv64_config ./.config
RUN make ARCH=riscv CROSS_COMPILE=riscv64-linux-gnu- olddefconfig

FROM scratch AS linux-riscv64-config
COPY --link --from=linux-riscv64-config-dev /work-buildlinux/linux/.config /

FROM golang-base AS init-riscv64-dev
COPY --link --from=assets / /work
WORKDIR /work
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    GOARCH=riscv64 go build -ldflags "-s -w -extldflags '-static'" -tags "osusergo netgo static_build" -o /out/init ./cmd/init

FROM golang-base AS runc-riscv64-dev
ARG RUNC_VERSION
RUN apt-get update -y && apt-get install -y gcc-riscv64-linux-gnu libc-dev-riscv64-cross git make gperf
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    git clone https://github.com/opencontainers/runc.git /go/src/github.com/opencontainers/runc && \
    cd /go/src/github.com/opencontainers/runc && \
    git checkout "${RUNC_VERSION}" && \
    # mkdir -p /opt/libseccomp && ./script/seccomp.sh "2.5.4" /opt/libseccomp riscv64 && \
    make static GOARCH=riscv64 CC=riscv64-linux-gnu-gcc EXTRA_LDFLAGS='-s -w' BUILDTAGS="" && \
    mkdir -p /out/ && mv runc /out/runc

FROM gcc-riscv64-linux-gnu-base AS vmtouch-riscv64-dev
RUN git clone https://github.com/hoytech/vmtouch.git && \
    cd vmtouch && \
    CC="riscv64-linux-gnu-gcc -static" make && \
    mkdir /out && mv vmtouch /out/

FROM --platform=riscv64 tonistiigi/binfmt:${BINFMT_VERSION} AS binfmt
FROM scratch AS binfmt-riscv64
FROM scratch AS binfmt-base
COPY --link --from=binfmt /usr/bin/binfmt /usr/bin/
FROM binfmt-base AS binfmt-amd64
COPY --link --from=binfmt /usr/bin/qemu-x86_64 /usr/bin/
FROM binfmt-base AS binfmt-aarch64
COPY --link --from=binfmt /usr/bin/qemu-aarch64 /usr/bin/
FROM binfmt-base AS binfmt-arm
COPY --link --from=binfmt /usr/bin/qemu-arm /usr/bin/
FROM binfmt-base AS binfmt-i386
COPY --link --from=binfmt /usr/bin/qemu-i386 /usr/bin/
FROM binfmt-base AS binfmt-mips64
COPY --link --from=binfmt /usr/bin/qemu-mips64 /usr/bin/
FROM binfmt-base AS binfmt-ppc64le
COPY --link --from=binfmt /usr/bin/qemu-ppc64le /usr/bin/
FROM binfmt-base AS binfmt-s390
COPY --link --from=binfmt /usr/bin/qemu-s390 /usr/bin/
FROM binfmt-$TARGETARCH AS binfmt-dev

FROM gcc-riscv64-linux-gnu-base AS busybox-riscv64-dev
ARG BUSYBOX_VERSION
RUN apt-get update -y && apt-get install -y gcc bzip2
WORKDIR /work
RUN git clone -b $BUSYBOX_VERSION --depth 1 https://git.busybox.net/busybox
WORKDIR /work/busybox
RUN make CROSS_COMPILE=riscv64-linux-gnu- LDFLAGS=--static defconfig
RUN make CROSS_COMPILE=riscv64-linux-gnu- LDFLAGS=--static -j$(nproc)
RUN mkdir -p /out/bin && mv busybox /out/bin/busybox
RUN make LDFLAGS=--static defconfig
RUN make LDFLAGS=--static -j$(nproc)
RUN for i in $(./busybox --list) ; do ln -s busybox /out/bin/$i ; done
RUN mkdir -p /out/usr/share/udhcpc/ && cp ./examples/udhcp/simple.script /out/usr/share/udhcpc/default.script

FROM gcc-riscv64-linux-gnu-base AS tini-riscv64-dev
# https://github.com/krallin/tini#building-tini
RUN apt-get update -y && apt-get install -y cmake
ENV CFLAGS="-DPR_SET_CHILD_SUBREAPER=36 -DPR_GET_CHILD_SUBREAPER=37"
WORKDIR /work
RUN git clone -b v0.19.0 https://github.com/krallin/tini
WORKDIR /work/tini
ENV CC="riscv64-linux-gnu-gcc -static"
RUN cmake . && make && mkdir /out/ && mv tini /out/

FROM ubuntu:22.04 AS rootfs-riscv64-dev
RUN apt-get update -y && apt-get install -y mkisofs
COPY --link --from=busybox-riscv64-dev /out/ /rootfs/
COPY --link --from=binfmt-dev / /rootfs/
COPY --link --from=runc-riscv64-dev /out/runc /rootfs/sbin/runc
COPY --link --from=bundle-dev /out/ /rootfs/
COPY --link --from=init-riscv64-dev /out/init /rootfs/sbin/init
COPY --link --from=vmtouch-riscv64-dev /out/vmtouch /rootfs/bin/
COPY --link --from=tini-riscv64-dev /out/tini /rootfs/sbin/tini
RUN mkdir -p /rootfs/proc /rootfs/sys /rootfs/mnt /rootfs/run /rootfs/tmp /rootfs/dev /rootfs/var /rootfs/etc && mknod /rootfs/dev/null c 1 3 && chmod 666 /rootfs/dev/null
RUN mkdir /out/ && mkisofs -l -J -R -o /out/rootfs.bin /rootfs/
# RUN isoinfo -i /out/rootfs.bin -l

FROM ubuntu:22.04 AS tinyemu-config-dev
ARG LINUX_LOGLEVEL
ARG VM_MEMORY_SIZE_MB
RUN apt-get update && apt-get install -y gettext-base && mkdir /out
COPY --link --from=assets /patches/tinyemu/tinyemu.config.template /
RUN cat /tinyemu.config.template | LOGLEVEL=$LINUX_LOGLEVEL MEMORY_SIZE=$VM_MEMORY_SIZE_MB envsubst > /out/tinyemu.config

FROM scratch AS vm-riscv64-dev
COPY --link --from=bbl-dev /out/bbl.bin /pack/bbl.bin
COPY --link --from=linux-riscv64-dev /out/Image /pack/Image
COPY --link --from=rootfs-riscv64-dev /out/rootfs.bin /pack/rootfs.bin
COPY --link --from=tinyemu-config-dev /out/tinyemu.config /pack/config

FROM rust:1-buster AS tinyemu-dev-common
ARG WASI_VFS_VERSION
ARG WASI_SDK_VERSION
ARG WASI_SDK_VERSION_FULL
ARG WIZER_VERSION
RUN apt-get update -y && apt-get install -y make curl git gcc xz-utils

WORKDIR /wasi
RUN curl -o wasi-sdk.tar.gz -fSL https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-${WASI_SDK_VERSION}/wasi-sdk-${WASI_SDK_VERSION_FULL}-linux.tar.gz && \
    tar xvf wasi-sdk.tar.gz && rm wasi-sdk.tar.gz
ENV WASI_SDK_PATH=/wasi/wasi-sdk-${WASI_SDK_VERSION_FULL}

WORKDIR /work/
RUN git clone https://github.com/kateinoigakukun/wasi-vfs.git --recurse-submodules && \
    cd wasi-vfs && \
    git checkout "${WASI_VFS_VERSION}" && \
    cargo build --target wasm32-unknown-unknown && \
    cargo build --package wasi-vfs-cli && \
    mkdir -p /tools/wasi-vfs/ && \
    mv target/debug/wasi-vfs target/wasm32-unknown-unknown/debug/libwasi_vfs.a /tools/wasi-vfs/ && \
    cargo clean

WORKDIR /work/
RUN git clone https://github.com/bytecodealliance/wizer && \
    cd wizer && \
    git checkout "${WIZER_VERSION}" && \
    cargo build --bin wizer --all-features && \
    mkdir -p /tools/wizer/ && \
    mv include target/debug/wizer /tools/wizer/ && \
    cargo clean

COPY --link --from=assets /patches/tinyemu/tinyemu /tinyemu
WORKDIR /tinyemu
RUN make -j $(nproc) -f Makefile \
    CONFIG_FS_NET= CONFIG_SDL= CONFIG_INT128= CONFIG_X86EMU= CONFIG_SLIRP= \
    CC="${WASI_SDK_PATH}/bin/clang --sysroot=${WASI_SDK_PATH}/share/wasi-sysroot -D_WASI_EMULATED_SIGNAL -DWASI -I/tools/wizer/include/" \
    EMU_LIBS="/tools/wasi-vfs/libwasi_vfs.a -lrt" \
    EMU_OBJS="virtio.o pci.o fs.o cutils.o iomem.o simplefb.o json.o machine.o temu.o wasi.o riscv_machine.o softfp.o riscv_cpu32.o riscv_cpu64.o fs_disk.o"

FROM tinyemu-dev-common AS tinyemu-dev-native
COPY --link --from=vm-riscv64-dev /pack /minpack

FROM tinyemu-dev-common AS tinyemu-dev-wizer
COPY --link --from=vm-riscv64-dev /pack /pack
RUN mv temu temu-org && /tools/wizer/wizer --allow-wasi --wasm-bulk-memory=true -r _start=wizer.resume --mapdir /pack::/pack -o temu temu-org
RUN mkdir /minpack && cp /pack/rootfs.bin /minpack/

FROM tinyemu-dev-${OPTIMIZATION_MODE} AS tinyemu-dev-packed
RUN /tools/wasi-vfs/wasi-vfs pack /tinyemu/temu --mapdir /pack::/minpack -o packed && mkdir /out
ARG OUTPUT_NAME
RUN mv packed /out/$OUTPUT_NAME

FROM emscripten/emsdk:$EMSDK_VERSION AS tinyemu-emscripten
ARG JS_OUTPUT_NAME
COPY --link --from=vm-riscv64-dev /pack /pack
COPY --link --from=assets /patches/tinyemu/tinyemu /tinyemu
WORKDIR /tinyemu
RUN make -j $(nproc) -f Makefile \
    CONFIG_FS_NET= CONFIG_SDL= CONFIG_INT128= CONFIG_X86EMU= CONFIG_SLIRP= OUTPUT_NAME=$JS_OUTPUT_NAME \
    CC="emcc --preload-file /pack -s WASM=1 -s ASYNCIFY=1 -s ALLOW_MEMORY_GROWTH=1 -UEMSCRIPTEN -DON_BROWSER -sNO_EXIT_RUNTIME=1 -sFORCE_FILESYSTEM=1" && \
    mkdir -p /out/ && mv ${JS_OUTPUT_NAME} /out/${JS_OUTPUT_NAME}.js && mv ${JS_OUTPUT_NAME}.wasm /out/ && mv ${JS_OUTPUT_NAME}.data /out/

FROM scratch AS js-tinyemu
COPY --link --from=tinyemu-emscripten /out/ /
FROM js-tinyemu AS js-riscv64
FROM js-tinyemu AS js-aarch64
FROM js-tinyemu AS js-arm
FROM js-tinyemu AS js-i386
FROM js-tinyemu AS js-mips64
FROM js-tinyemu AS js-ppc64le
FROM js-tinyemu AS js-s390

FROM scratch AS wasi-tinyemu
COPY --link --from=tinyemu-dev-packed /out/ /
FROM wasi-tinyemu AS wasi-riscv64
FROM wasi-tinyemu AS wasi-aarch64
FROM wasi-tinyemu AS wasi-arm
FROM wasi-tinyemu AS wasi-i386
FROM wasi-tinyemu AS wasi-mips64
FROM wasi-tinyemu AS wasi-ppc64le
FROM wasi-tinyemu AS wasi-s390

FROM ubuntu:22.04 AS gcc-x86-64-linux-gnu-base
RUN apt-get update && apt-get install -y gcc-x86-64-linux-gnu linux-libc-dev-amd64-cross git make

FROM gcc-x86-64-linux-gnu-base AS linux-amd64-dev-common
RUN apt-get update && apt-get install -y gperf flex bison bc
RUN mkdir /work-buildlinux
WORKDIR /work-buildlinux
RUN git clone -b v6.1 --depth 1 https://github.com/torvalds/linux

FROM linux-amd64-dev-common AS linux-amd64-dev
RUN apt-get install -y libelf-dev
WORKDIR /work-buildlinux/linux
COPY --link --from=assets ./patches/bochs/linux_x86_config ./.config
RUN make ARCH=x86 CROSS_COMPILE=x86_64-linux-gnu- -j$(nproc) all && \
    mkdir /out && \
    mv /work-buildlinux/linux/arch/x86/boot/bzImage /out/bzImage && \
    make clean

FROM linux-amd64-dev-common AS linux-amd64-config-dev
WORKDIR /work-buildlinux/linux
COPY --link --from=assets ./patches/bochs/linux_x86_config ./.config
RUN make ARCH=x86 CROSS_COMPILE=x86_64-linux-gnu- olddefconfig

FROM scratch AS linux-amd64-config
COPY --link --from=linux-amd64-config-dev /work-buildlinux/linux/.config /

FROM gcc-x86-64-linux-gnu-base AS busybox-amd64-dev
ARG BUSYBOX_VERSION
RUN apt-get update -y && apt-get install -y gcc bzip2
WORKDIR /work
RUN git clone -b $BUSYBOX_VERSION --depth 1 https://git.busybox.net/busybox
WORKDIR /work/busybox
RUN make CROSS_COMPILE=x86_64-linux-gnu- LDFLAGS=--static defconfig
RUN make CROSS_COMPILE=x86_64-linux-gnu- LDFLAGS=--static -j$(nproc)
RUN mkdir -p /out/bin && mv busybox /out/bin/busybox
RUN make LDFLAGS=--static defconfig
RUN make LDFLAGS=--static -j$(nproc)
RUN for i in $(./busybox --list) ; do ln -s busybox /out/bin/$i ; done
RUN mkdir -p /out/usr/share/udhcpc/ && cp ./examples/udhcp/simple.script /out/usr/share/udhcpc/default.script

FROM golang-base AS runc-amd64-dev
ARG RUNC_VERSION
RUN apt-get update -y && apt-get install -y git make gperf
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    git clone https://github.com/opencontainers/runc.git /go/src/github.com/opencontainers/runc && \
    cd /go/src/github.com/opencontainers/runc && \
    git checkout "${RUNC_VERSION}" && \
    make static GOARCH=amd64 CC=gcc EXTRA_LDFLAGS='-s -w' BUILDTAGS="" EXTRA_LDFLAGS='-s -w' BUILDTAGS="" && \
    mkdir -p /out/ && mv runc /out/runc

FROM gcc-x86-64-linux-gnu-base AS tini-amd64-dev
# https://github.com/krallin/tini#building-tini
RUN apt-get update -y && apt-get install -y cmake
ENV CFLAGS="-DPR_SET_CHILD_SUBREAPER=36 -DPR_GET_CHILD_SUBREAPER=37"
WORKDIR /work
RUN git clone -b v0.19.0 https://github.com/krallin/tini
WORKDIR /work/tini
ENV CC="x86_64-linux-gnu-gcc -static"
RUN cmake . && make && mkdir /out/ && mv tini /out/

FROM gcc-x86-64-linux-gnu-base AS grub-amd64-dev
ARG LINUX_LOGLEVEL
RUN apt-get update && apt-get install -y mkisofs xorriso wget bison flex python-is-python3 gettext
WORKDIR /work/
RUN wget https://ftp.gnu.org/gnu/grub/grub-2.06.tar.gz
RUN tar zxvf grub-2.06.tar.gz
WORKDIR /work/grub-2.06
RUN ./configure --target=i386
RUN make -j$(nproc)
RUN make install
RUN mkdir -p /iso/boot/grub
COPY --link --from=linux-amd64-dev /out/bzImage /iso/boot/grub/
COPY --link --from=assets ./patches/bochs/grub.cfg.template /
RUN cat /grub.cfg.template | LOGLEVEL=$LINUX_LOGLEVEL envsubst > /iso/boot/grub/grub.cfg
RUN mkdir /out && grub-mkrescue --directory ./grub-core -o /out/boot.iso /iso

FROM ubuntu AS bios-amd64-dev
RUN apt-get update && apt-get install -y build-essential
COPY --link --from=assets ./patches/bochs/Bochs /Bochs
WORKDIR /Bochs/bochs
RUN CC="x86_64-linux-gnu-gcc" ./configure --enable-x86-64 --with-nogui
RUN make -j$(nproc) bios/BIOS-bochs-latest bios/VGABIOS-lgpl-latest
RUN mkdir /out/ && mv bios/BIOS-bochs-latest bios/VGABIOS-lgpl-latest /out/

FROM golang-base AS init-amd64-dev
COPY --link --from=assets / /work
WORKDIR /work
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    GOARCH=amd64 go build -ldflags "-s -w -extldflags '-static'" -tags "osusergo netgo static_build" -o /out/init ./cmd/init

FROM gcc-x86-64-linux-gnu-base AS vmtouch-amd64-dev
RUN git clone https://github.com/hoytech/vmtouch.git && \
    cd vmtouch && \
    CC="x86_64-linux-gnu-gcc -static" make && \
    mkdir /out && mv vmtouch /out/

FROM ubuntu:22.04 AS rootfs-amd64-dev
RUN apt-get update -y && apt-get install -y mkisofs
COPY --link --from=busybox-amd64-dev /out/ /rootfs/
COPY --link --from=runc-amd64-dev /out/runc /rootfs/sbin/runc
COPY --link --from=bundle-dev /out/ /rootfs/
COPY --link --from=init-amd64-dev /out/init /rootfs/sbin/init
COPY --link --from=vmtouch-amd64-dev /out/vmtouch /rootfs/bin/
COPY --link --from=tini-amd64-dev /out/tini /rootfs/sbin/tini
RUN mkdir -p /rootfs/proc /rootfs/sys /rootfs/mnt /rootfs/run /rootfs/tmp /rootfs/dev /rootfs/var /rootfs/etc && mknod /rootfs/dev/null c 1 3 && chmod 666 /rootfs/dev/null
RUN mkdir /out/ && mkisofs -l -J -R -o /out/rootfs.bin /rootfs/
# RUN isoinfo -i /out/rootfs.bin -l

FROM ubuntu:22.04 AS bochs-config-dev
ARG VM_MEMORY_SIZE_MB
RUN apt-get update && apt-get install -y gettext-base && mkdir /out
COPY --link --from=assets ./patches/bochs/bochsrc.template /
RUN cat /bochsrc.template | MEMORY_SIZE=$VM_MEMORY_SIZE_MB envsubst > /out/bochsrc

FROM scratch AS vm-amd64-dev
COPY --link --from=grub-amd64-dev /out/boot.iso /pack/
COPY --link --from=bios-amd64-dev /out/ /pack/
COPY --link --from=rootfs-amd64-dev /out/rootfs.bin /pack/
COPY --link --from=bochs-config-dev /out/bochsrc /pack/

FROM rust:1-buster AS bochs-dev-common
ARG WASI_VFS_VERSION
ARG WASI_SDK_VERSION
ARG WASI_SDK_VERSION_FULL
ARG BINARYEN_VERSION
ARG WIZER_VERSION
RUN apt-get update -y && apt-get install -y make curl git gcc xz-utils

WORKDIR /wasi
RUN curl -o wasi-sdk.tar.gz -fSL https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-${WASI_SDK_VERSION}/wasi-sdk-${WASI_SDK_VERSION_FULL}-linux.tar.gz && \
    tar xvf wasi-sdk.tar.gz && rm wasi-sdk.tar.gz
ENV WASI_SDK_PATH=/wasi/wasi-sdk-${WASI_SDK_VERSION_FULL}

WORKDIR /work/
RUN git clone https://github.com/kateinoigakukun/wasi-vfs.git --recurse-submodules && \
    cd wasi-vfs && \
    git checkout "${WASI_VFS_VERSION}" && \
    cargo build --target wasm32-unknown-unknown && \
    cargo build --package wasi-vfs-cli && \
    mkdir -p /tools/wasi-vfs/ && \
    mv target/debug/wasi-vfs target/wasm32-unknown-unknown/debug/libwasi_vfs.a /tools/wasi-vfs/ && \
    cargo clean

WORKDIR /work/
RUN git clone https://github.com/bytecodealliance/wizer && \
    cd wizer && \
    git checkout "${WIZER_VERSION}" && \
    cargo build --bin wizer --all-features && \
    mkdir -p /tools/wizer/ && \
    mv include target/debug/wizer /tools/wizer/ && \
    cargo clean

RUN wget -O /tmp/binaryen.tar.gz https://github.com/WebAssembly/binaryen/releases/download/version_${BINARYEN_VERSION}/binaryen-version_${BINARYEN_VERSION}-x86_64-linux.tar.gz
RUN mkdir -p /binaryen
RUN tar -C /binaryen -zxvf /tmp/binaryen.tar.gz

COPY --link --from=assets ./patches/bochs/jmp /jmp
WORKDIR /jmp
RUN ${WASI_SDK_PATH}/bin/clang --sysroot=${WASI_SDK_PATH}/share/wasi-sysroot -O2 --target=wasm32-unknown-wasi -c jmp.c -I . -o jmp.o
RUN ${WASI_SDK_PATH}/bin/clang --sysroot=${WASI_SDK_PATH}/share/wasi-sysroot -O2 --target=wasm32-unknown-wasi -Wl,--export=wasm_setjmp -c jmp.S -o jmp_wrapper.o
RUN ${WASI_SDK_PATH}/bin/wasm-ld jmp.o jmp_wrapper.o --export=wasm_setjmp --export=wasm_longjmp --export=handle_jmp --no-entry -r -o jmp

COPY --link --from=assets ./patches/bochs/vfs /vfs
WORKDIR /vfs
RUN ${WASI_SDK_PATH}/bin/clang --sysroot=${WASI_SDK_PATH}/share/wasi-sysroot -O2 --target=wasm32-unknown-wasi -c vfs.c -I . -o vfs.o

COPY --link --from=assets /patches/bochs/Bochs /Bochs
WORKDIR /Bochs/bochs
ARG INIT_DEBUG
RUN LOGGING_FLAG=--disable-logging && \
    if test "${INIT_DEBUG}" = "true" ; then LOGGING_FLAG=--enable-logging ; fi && \
    CC="${WASI_SDK_PATH}/bin/clang" CXX="${WASI_SDK_PATH}/bin/clang++" RANLIB="${WASI_SDK_PATH}/bin/ranlib" \
    CFLAGS="--sysroot=${WASI_SDK_PATH}/share/wasi-sysroot -D_WASI_EMULATED_SIGNAL -DWASI -D__GNU__ -O2 -I/jmp/ -I/tools/wizer/include/" \
    CXXFLAGS="${CFLAGS}" \
    ./configure --host wasm32-unknown-wasi --enable-x86-64 --with-nogui --enable-usb --enable-usb-ehci \
    --disable-large-ramfile --disable-show-ips --disable-stats ${LOGGING_FLAG} \
    --enable-repeat-speedups --enable-fast-function-calls --disable-trace-linking --enable-handlers-chaining # TODO: --enable-trace-linking causes "out of bounds memory access"
RUN make -j$(nproc) bochs EMU_DEPS="/tools/wasi-vfs/libwasi_vfs.a /jmp/jmp /vfs/vfs.o -lrt"
RUN /binaryen/binaryen-version_${BINARYEN_VERSION}/bin/wasm-opt bochs --asyncify -O2 -o bochs.async --pass-arg=asyncify-ignore-imports
RUN mv bochs.async bochs

FROM bochs-dev-common AS bochs-dev-native
COPY --link --from=vm-amd64-dev /pack /minpack

FROM bochs-dev-common AS bochs-dev-wizer
COPY --link --from=vm-amd64-dev /pack /pack
ENV WASMTIME_BACKTRACE_DETAILS=1
RUN mv bochs bochs-org && /tools/wizer/wizer --allow-wasi --wasm-bulk-memory=true -r _start=wizer.resume --mapdir /pack::/pack -o bochs bochs-org
RUN mkdir /minpack && cp /pack/rootfs.bin /minpack/ && cp /pack/boot.iso /minpack/

FROM bochs-dev-${OPTIMIZATION_MODE} AS bochs-dev-packed
RUN /tools/wasi-vfs/wasi-vfs pack /Bochs/bochs/bochs --mapdir /pack::/minpack -o packed && mkdir /out
ARG OUTPUT_NAME
RUN mv packed /out/$OUTPUT_NAME

FROM scratch AS wasi-amd64
COPY --link --from=bochs-dev-packed /out/ /

FROM emscripten/emsdk:$EMSDK_VERSION AS bochs-emscripten
RUN apt-get install -y wget
COPY --link --from=assets ./patches/bochs/Bochs /Bochs
WORKDIR /Bochs/bochs
COPY --link --from=vm-amd64-dev /pack /pack
ARG INIT_DEBUG
RUN LOGGING_FLAG=--disable-logging && \
    if test "${INIT_DEBUG}" = "true" ; then LOGGING_FLAG=--enable-logging ; fi && \
    CFLAGS="-O2 -s WASM=1 -s ASYNCIFY=1 -s ALLOW_MEMORY_GROWTH=1  -s TOTAL_MEMORY=$((20*1024*1024)) -sNO_EXIT_RUNTIME=1 -sFORCE_FILESYSTEM=1 -D__GNU__" \
    CXXFLAGS="${CFLAGS}" \
    emconfigure ./configure --host wasm32-unknown-emscripten --enable-x86-64 --with-nogui --enable-usb --enable-usb-ehci \
    --disable-large-ramfile --disable-show-ips --disable-stats ${LOGGING_FLAG} \
    --enable-repeat-speedups --enable-fast-function-calls --disable-trace-linking --enable-handlers-chaining # TODO: --enable-trace-linking causes "too much recursion"
RUN emmake make -j$(nproc) bochs EMU_DEPS="--preload-file /pack"
RUN mkdir -p /out/ && mv bochs /out/out.js && mv bochs.wasm /out/ && mv bochs.data /out/

FROM scratch AS js-amd64
COPY --link --from=bochs-emscripten /out/ /

FROM js-$TARGETARCH AS js

FROM wasi-$TARGETARCH
