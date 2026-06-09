#!/bin/bash

set -xeu -o pipefail

OUTPUT=${1:-$(pwd)/out/}

mkdir -p $OUTPUT

# script
cp ./src.js ${OUTPUT}/

# c2w
(
    cd ../../ && \
    make c2w && \
    ./out/c2w --dockerfile=Dockerfile --assets=. --external-bundle --to-js $OUTPUT
)

# runcontainer.js
(
    cd ../../ && \
    docker build --progress=plain --output type=local,dest=$OUTPUT \
           -f ./examples/librechat-hook/Dockerfile.runcontainerjs \
           ./extras/runcontainerjs
)

# imagemounter.wasm
(
    cd ../../ && \
    make imagemounter.wasm && \
    cat ./out/imagemounter.wasm | gzip > ${OUTPUT}/imagemounter.wasm.gzip
)
