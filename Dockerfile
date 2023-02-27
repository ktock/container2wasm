# syntax = docker/dockerfile:1.4

ARG BINFMT_VERSION=qemu-v6.1.0
ARG WASI_SDK_VERSION=19
ARG WASI_SDK_VERSION_FULL=${WASI_SDK_VERSION}.0
ARG WASI_VFS_VERSION=main
ARG WIZER_VERSION=main
ARG EMSDK_VERSION=3.1.31

# ARG LINUX_LOGLEVEL=0
# ARG INIT_DEBUG=false
ARG LINUX_LOGLEVEL=7
ARG INIT_DEBUG=true
ARG VM_MEMORY_SIZE_MB=128
ARG NO_VMTOUCH=

ARG OUTPUT_NAME=out.wasm
ARG OPTIMIZATION_MODE=wizer # "wizer" or "native"
# ARG OPTIMIZATION_MODE=native

ARG EMSCRIPTEN_INITIAL_MEMORY=200MB

ARG SOURCE_REPO=https://github.com/ktock/container2wasm

FROM scratch AS oci-image-src
COPY . .

FROM ghcr.io/stargz-containers/ubuntu:22.04 AS assets-base
RUN apt-get update && apt-get install -y git
RUN git clone ${SOURCE_REPO} /assets
FROM scratch AS assets
COPY --link --from=assets-base /assets /

FROM ghcr.io/stargz-containers/ubuntu:22.04 AS bbl-dev
RUN apt-get update && apt-get install -y gcc-riscv64-linux-gnu libc-dev-riscv64-cross git make
WORKDIR /work-buildroot/
RUN git clone https://github.com/riscv-software-src/riscv-pk
WORKDIR /work-buildroot/riscv-pk
RUN git checkout 7e9b671c0415dfd7b562ac934feb9380075d4aa2
# Apply patch for static HTIF address like done by TinyEMU
# We only support riscv64. You might need more patches from TinyEMU project.
COPY --link --from=assets /patches/bbl-static-htif-addr.diff /
RUN cat /bbl-static-htif-addr.diff | git apply
RUN mkdir build
WORKDIR /work-buildroot/riscv-pk/build
RUN ../configure --host=riscv64-linux-gnu
RUN make
RUN riscv64-linux-gnu-objcopy -O binary bbl bbl.bin && \
    mkdir /out/ && \
    mv bbl.bin /out/

FROM ghcr.io/stargz-containers/ubuntu:22.04 AS linux-dev-common
RUN apt-get update && apt-get install -y gcc-riscv64-linux-gnu libc-dev-riscv64-cross git make gperf gcc flex bison bc
RUN mkdir /work-buildlinux
WORKDIR /work-buildlinux
RUN git clone -b v6.1 --depth 1 https://github.com/torvalds/linux

FROM linux-dev-common AS linux-dev
WORKDIR /work-buildlinux/linux
COPY --link --from=assets /patches/linux_rv64_config ./.config
RUN make ARCH=riscv CROSS_COMPILE=riscv64-linux-gnu- -j$(nproc) all && \
    mkdir /out && \
    mv /work-buildlinux/linux/arch/riscv/boot/Image /out/Image

FROM linux-dev-common AS linux-config-dev
WORKDIR /work-buildlinux/linux
COPY --link --from=assets /patches/linux_rv64_config ./.config
RUN make ARCH=riscv CROSS_COMPILE=riscv64-linux-gnu- olddefconfig

FROM scratch AS linux-config
COPY --link --from=linux-config-dev /work-buildlinux/linux/.config /

FROM golang:1.19-bullseye AS bundle-dev
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

FROM golang:1.19-bullseye AS init-dev
COPY --link --from=assets / /work
WORKDIR /work
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    GOARCH=riscv64 go build -ldflags "-s -w -extldflags '-static'" -tags "osusergo netgo static_build" -o /out/init ./cmd/init

FROM golang:1.19-bullseye AS runc-dev
RUN apt-get update -y && \
    apt-get install -y gcc-riscv64-linux-gnu libc-dev-riscv64-cross git make gperf
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    git clone https://github.com/opencontainers/runc.git /go/src/github.com/opencontainers/runc && \
    cd /go/src/github.com/opencontainers/runc && \
    # mkdir -p /opt/libseccomp && ./script/seccomp.sh "2.5.4" /opt/libseccomp riscv64 && \
    make static GOARCH=riscv64 CC=riscv64-linux-gnu-gcc EXTRA_LDFLAGS='-s -w' BUILDTAGS="" && \
    mkdir -p /out/ && mv runc /out/runc

FROM ghcr.io/stargz-containers/ubuntu:22.04 AS vmtouch-dev
RUN apt-get update && apt-get install -y gcc-riscv64-linux-gnu libc-dev-riscv64-cross git make
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

FROM --platform=riscv64 riscv64/alpine:20221110 AS rootfs

FROM ghcr.io/stargz-containers/ubuntu:22.04 AS tini-dev
# https://github.com/krallin/tini#building-tini
RUN apt-get update -y && \
    apt-get install -y gcc-riscv64-linux-gnu libc-dev-riscv64-cross git make cmake
ENV CFLAGS="-DPR_SET_CHILD_SUBREAPER=36 -DPR_GET_CHILD_SUBREAPER=37"
WORKDIR /work
RUN git clone -b v0.19.0 https://github.com/krallin/tini
WORKDIR /work/tini
ENV CC="riscv64-linux-gnu-gcc -static"
RUN cmake . && make && mkdir /out/ && mv tini /out/

FROM ghcr.io/stargz-containers/ubuntu:22.04 AS rootfs-dev
RUN apt-get update -y && apt-get install -y mkisofs
COPY --link --from=rootfs / /rootfs/
COPY --link --from=binfmt-dev / /rootfs/
COPY --link --from=runc-dev /out/runc /rootfs/sbin/runc
COPY --link --from=bundle-dev /out/ /rootfs/
RUN rm /rootfs/sbin/init
COPY --link --from=init-dev /out/init /rootfs/sbin/init
COPY --link --from=vmtouch-dev /out/vmtouch /rootfs/bin/
COPY --link --from=tini-dev /out/tini /rootfs/sbin/tini
RUN touch /rootfs/etc/resolv.conf
RUN mkdir /out/ && mkisofs -l -J -r -o /out/rootfs.bin /rootfs/
# RUN isoinfo -i /out/rootfs.bin -l

FROM ghcr.io/stargz-containers/ubuntu:22.04 AS tinyemu-config-dev
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

FROM ghcr.io/ktock/rust:1-buster AS tinyemu-dev-common
ARG WASI_VFS_VERSION
ARG WASI_SDK_VERSION
ARG WASI_SDK_VERSION_FULL
ARG WIZER_VERSION
ARG OUTPUT_NAME
RUN apt-get update -y && apt-get install -y wget make curl git gcc xz-utils

WORKDIR /wasi
RUN wget https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-${WASI_SDK_VERSION}/wasi-sdk-${WASI_SDK_VERSION_FULL}-linux.tar.gz && \
    tar xvf wasi-sdk-${WASI_SDK_VERSION_FULL}-linux.tar.gz
ENV WASI_SDK_PATH=/wasi/wasi-sdk-${WASI_SDK_VERSION_FULL}

WORKDIR /work/
RUN git clone -b "${WASI_VFS_VERSION}" https://github.com/kateinoigakukun/wasi-vfs.git --recurse-submodules
WORKDIR /work/wasi-vfs
RUN cargo build --target wasm32-unknown-unknown && \
    cargo build --package wasi-vfs-cli

WORKDIR /work/
RUN git clone -b "${WIZER_VERSION}" https://github.com/bytecodealliance/wizer && \
    cd wizer && \
    cargo build --bin wizer --all-features

COPY --link --from=assets /patches/tinyemu /tinyemu
WORKDIR /tinyemu
RUN make -j $(nproc) -f Makefile \
    CONFIG_FS_NET= CONFIG_SDL= CONFIG_INT128= CONFIG_X86EMU= CONFIG_SLIRP= \
    CC="${WASI_SDK_PATH}/bin/clang --sysroot=${WASI_SDK_PATH}/share/wasi-sysroot -D_WASI_EMULATED_SIGNAL -DWASI -I/work/wizer/include/" \
    EMU_LIBS="/work/wasi-vfs/target/wasm32-unknown-unknown/debug/libwasi_vfs.a -lrt" \
    EMU_OBJS="virtio.o pci.o fs.o cutils.o iomem.o simplefb.o json.o machine.o temu.o wasi.o riscv_machine.o softfp.o riscv_cpu32.o riscv_cpu64.o fs_disk.o"

FROM tinyemu-dev-common AS tinyemu-dev-native
COPY --link --from=vm-dev /pack /minpack

FROM tinyemu-dev-common AS tinyemu-dev-wizer
COPY --link --from=vm-dev /pack /pack
RUN mv temu temu-org && /work/wizer/target/debug/wizer --allow-wasi --wasm-bulk-memory=true -r _start=wizer.resume --mapdir /pack::/pack -o temu temu-org
RUN mkdir /minpack && cp /pack/rootfs.bin /minpack/

FROM tinyemu-dev-${OPTIMIZATION_MODE} AS tinyemu-dev

FROM tinyemu-dev AS tinyemu-dev-packed
RUN /work/wasi-vfs/target/debug/wasi-vfs pack /tinyemu/temu --mapdir /pack::/minpack -o packed && mkdir /out && mv packed /out/$OUTPUT_NAME

FROM emscripten/emsdk:$EMSDK_VERSION AS tinyemu-emscripten
ARG EMSCRIPTEN_INITIAL_MEMORY
COPY --link --from=vm-dev /pack /pack
COPY --link --from=assets /patches/tinyemu /tinyemu
WORKDIR /tinyemu
RUN make -j $(nproc) -f Makefile \
    CONFIG_FS_NET= CONFIG_SDL= CONFIG_INT128= CONFIG_X86EMU= CONFIG_SLIRP= \
    CC="emcc --embed-file /pack -s WASM=1 -s ALLOW_MEMORY_GROWTH=1 -s INITIAL_MEMORY=${EMSCRIPTEN_INITIAL_MEMORY} -UEMSCRIPTEN -sNO_EXIT_RUNTIME=1 -sFORCE_FILESYSTEM=1" && \
    mkdir -p /out/ && mv temu /out/container.js && mv temu.wasm /out/

FROM scratch AS js
COPY --link --from=tinyemu-emscripten /out/ /

FROM scratch
COPY --link --from=tinyemu-dev-packed /out/ /
