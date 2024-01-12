#!/bin/bash

set -eu -o pipefail

CONTAINER=test-container
TIMEOUT=80m

DOCKER_BUILDKIT=1 docker build --progress=plain -f Dockerfile.test -t $CONTAINER .
docker run --rm -d --privileged -v ${CONTAINER}-cache:/var/lib/docker --name $CONTAINER $CONTAINER
until docker exec $CONTAINER docker version
do
    echo "retrying..."
    sleep 3
done
docker exec -w /test $CONTAINER docker run --rm -d --name testregistry -p 127.0.0.1:5000:5000 registry:2
docker exec -w /test $CONTAINER docker buildx create --name container --driver=docker-container
docker exec -w /test $CONTAINER go test ${GO_TEST_FLAGS:-} -timeout $TIMEOUT -v ./tests/integration
docker kill $CONTAINER
