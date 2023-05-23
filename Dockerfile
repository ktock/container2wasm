# syntax = docker/dockerfile:1.5

ARG BINFMT_VERSION=qemu-v6.1.0
ARG WASI_SDK_VERSION=19
ARG WASI_SDK_VERSION_FULL=${WASI_SDK_VERSION}.0
ARG WASI_VFS_VERSION=v0.3.0
ARG WIZER_VERSION=04e49c989542f2bf3a112d60fbf88a62cce2d0d0
ARG EMSDK_VERSION=3.1.35
ARG BUSYBOX_VERSION=1_36_0
ARG RUNC_VERSION=v1.1.7

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
ARG SOURCE_REPO_VERSION=v0.2.3

FROM scratch AS oci-image-src
COPY . .

FROM ubuntu:22.04 AS assets-base
ARG SOURCE_REPO
ARG SOURCE_REPO_VERSION
RUN apt-get update && apt-get install -y git
RUN git clone -b ${SOURCE_REPO_VERSION} ${SOURCE_REPO} /assets
FROM scratch AS assets
COPY --link --from=assets-base /assets /

FROM golang:1.20-bullseye AS golang-base

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

FROM gcc-riscv64-linux-gnu-base AS linux-dev-common
RUN apt-get update && apt-get install -y gperf flex bison bc
RUN mkdir /work-buildlinux
WORKDIR /work-buildlinux
RUN git clone -b v6.1 --depth 1 https://github.com/torvalds/linux

FROM linux-dev-common AS linux-dev
WORKDIR /work-buildlinux/linux
COPY --link --from=assets /patches/linux_rv64_config ./.config
RUN make ARCH=riscv CROSS_COMPILE=riscv64-linux-gnu- -j$(nproc) all && \
    mkdir /out && \
    mv /work-buildlinux/linux/arch/riscv/boot/Image /out/Image && \
    make clean

FROM linux-dev-common AS linux-config-dev
WORKDIR /work-buildlinux/linux
COPY --link --from=assets /patches/linux_rv64_config ./.config
RUN make ARCH=riscv CROSS_COMPILE=riscv64-linux-gnu- olddefconfig

FROM scratch AS linux-config
COPY --link --from=linux-config-dev /work-buildlinux/linux/.config /

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
# <vm-rootfs>/etc/initconfig.json : configuration file for init
RUN mkdir -p /out/oci/rootfs /out/oci/bundle /out/etc && \
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
    mv image.json spec.json /out/oci/ && mv initconfig.json /out/etc/

FROM golang-base AS init-dev
COPY --link --from=assets / /work
WORKDIR /work
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    GOARCH=riscv64 go build -ldflags "-s -w -extldflags '-static'" -tags "osusergo netgo static_build" -o /out/init ./cmd/init

FROM golang-base AS runc-dev
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

FROM gcc-riscv64-linux-gnu-base AS vmtouch-dev
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

FROM gcc-riscv64-linux-gnu-base AS busybox-dev
ARG BUSYBOX_VERSION
RUN apt-get update -y && apt-get install -y gcc bzip2
WORKDIR /work
RUN git clone -b $BUSYBOX_VERSION --depth 1 https://git.busybox.net/busybox
WORKDIR /work/busybox
RUN make CROSS_COMPILE=riscv64-linux-gnu- LDFLAGS=--static defconfig
RUN make CROSS_COMPILE=riscv64-linux-gnu- LDFLAGS=--static -j$(nproc)
RUN mkdir -p /out && mv busybox /out/busybox
RUN make LDFLAGS=--static defconfig
RUN make LDFLAGS=--static -j$(nproc)
RUN for i in $(./busybox --list) ; do ln -s busybox /out/$i ; done

FROM gcc-riscv64-linux-gnu-base AS tini-dev
# https://github.com/krallin/tini#building-tini
RUN apt-get update -y && apt-get install -y cmake
ENV CFLAGS="-DPR_SET_CHILD_SUBREAPER=36 -DPR_GET_CHILD_SUBREAPER=37"
WORKDIR /work
RUN git clone -b v0.19.0 https://github.com/krallin/tini
WORKDIR /work/tini
ENV CC="riscv64-linux-gnu-gcc -static"
RUN cmake . && make && mkdir /out/ && mv tini /out/

FROM ubuntu:22.04 AS rootfs-dev
RUN apt-get update -y && apt-get install -y mkisofs
COPY --link --from=busybox-dev /out/ /rootfs/bin/
COPY --link --from=binfmt-dev / /rootfs/
COPY --link --from=runc-dev /out/runc /rootfs/sbin/runc
COPY --link --from=bundle-dev /out/ /rootfs/
COPY --link --from=init-dev /out/init /rootfs/sbin/init
COPY --link --from=vmtouch-dev /out/vmtouch /rootfs/bin/
COPY --link --from=tini-dev /out/tini /rootfs/sbin/tini
RUN mkdir -p /rootfs/proc /rootfs/sys /rootfs/mnt /rootfs/run /rootfs/tmp /rootfs/dev /rootfs/var && mknod /rootfs/dev/null c 1 3 && chmod 666 /rootfs/dev/null
RUN touch /rootfs/etc/resolv.conf /rootfs/etc/hosts
RUN mkdir /out/ && mkisofs -l -J -r -o /out/rootfs.bin /rootfs/
# RUN isoinfo -i /out/rootfs.bin -l

FROM ubuntu:22.04 AS tinyemu-config-dev
ARG LINUX_LOGLEVEL
ARG VM_MEMORY_SIZE_MB
COPY --link --from=assets /patches/tinyemu.config.template /
RUN apt-get update && apt-get install -y gettext-base && mkdir /out
RUN cat /tinyemu.config.template | LOGLEVEL=$LINUX_LOGLEVEL MEMORY_SIZE=$VM_MEMORY_SIZE_MB envsubst > /out/tinyemu.config

FROM scratch AS vm-dev
COPY --link --from=bbl-dev /out/bbl.bin /pack/bbl.bin
COPY --link --from=linux-dev /out/Image /pack/Image
COPY --link --from=rootfs-dev /out/rootfs.bin /pack/rootfs.bin
COPY --link --from=tinyemu-config-dev /out/tinyemu.config /pack/config

FROM rust:1-buster AS tinyemu-dev-common
ARG WASI_VFS_VERSION
ARG WASI_SDK_VERSION
ARG WASI_SDK_VERSION_FULL
ARG WIZER_VERSION
ARG OUTPUT_NAME
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

COPY --link --from=assets /patches/tinyemu /tinyemu
WORKDIR /tinyemu
RUN make -j $(nproc) -f Makefile \
    CONFIG_FS_NET= CONFIG_SDL= CONFIG_INT128= CONFIG_X86EMU= CONFIG_SLIRP= \
    CC="${WASI_SDK_PATH}/bin/clang --sysroot=${WASI_SDK_PATH}/share/wasi-sysroot -D_WASI_EMULATED_SIGNAL -DWASI -I/tools/wizer/include/" \
    EMU_LIBS="/tools/wasi-vfs/libwasi_vfs.a -lrt" \
    EMU_OBJS="virtio.o pci.o fs.o cutils.o iomem.o simplefb.o json.o machine.o temu.o wasi.o riscv_machine.o softfp.o riscv_cpu32.o riscv_cpu64.o fs_disk.o"

FROM tinyemu-dev-common AS tinyemu-dev-native
COPY --link --from=vm-dev /pack /minpack

FROM tinyemu-dev-common AS tinyemu-dev-wizer
COPY --link --from=vm-dev /pack /pack
RUN mv temu temu-org && /tools/wizer/wizer --allow-wasi --wasm-bulk-memory=true -r _start=wizer.resume --mapdir /pack::/pack -o temu temu-org
RUN mkdir /minpack && cp /pack/rootfs.bin /minpack/

FROM tinyemu-dev-${OPTIMIZATION_MODE} AS tinyemu-dev-packed
RUN /tools/wasi-vfs/wasi-vfs pack /tinyemu/temu --mapdir /pack::/minpack -o packed && mkdir /out && mv packed /out/$OUTPUT_NAME

FROM emscripten/emsdk:$EMSDK_VERSION AS tinyemu-emscripten
ARG JS_OUTPUT_NAME
COPY --link --from=vm-dev /pack /pack
COPY --link --from=assets /patches/tinyemu /tinyemu
WORKDIR /tinyemu
RUN make -j $(nproc) -f Makefile \
    CONFIG_FS_NET= CONFIG_SDL= CONFIG_INT128= CONFIG_X86EMU= CONFIG_SLIRP= OUTPUT_NAME=$JS_OUTPUT_NAME \
    CC="emcc --preload-file /pack -s WASM=1 -s ASYNCIFY=1 -s ALLOW_MEMORY_GROWTH=1 -UEMSCRIPTEN -DON_BROWSER -sNO_EXIT_RUNTIME=1 -sFORCE_FILESYSTEM=1" && \
    mkdir -p /out/ && mv ${JS_OUTPUT_NAME} /out/${JS_OUTPUT_NAME}.js && mv ${JS_OUTPUT_NAME}.wasm /out/ && mv ${JS_OUTPUT_NAME}.data /out/

FROM scratch AS js
COPY --link --from=tinyemu-emscripten /out/ /

FROM scratch
COPY --link --from=tinyemu-dev-packed /out/ /
