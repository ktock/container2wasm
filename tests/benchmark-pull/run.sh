#!/bin/bash

set -euo pipefail

SAMPLES_NUM_="${SAMPLES_NUM:-3}"
PERCENTILE_="${PERCENTILE:-90}"
BENCHMARK_RESULT_="${BENCHMARK_RESULT:-}"
TARGET_FILE_="${TARGET_FILE:-}"

CALCTEMP=$(mktemp)
function percentile {
    local DATAFILE="${1}"
    local SAMPLES="${2}"

    cat "${DATAFILE}" | sort -R | head -n "${SAMPLES}" | sort -n > "${CALCTEMP}"
    local PYTHON_BIN=
    if which python &> /dev/null ; then
        PYTHON_BIN=python
    elif which python3 &> /dev/null ; then
        # Try also with python3
        PYTHON_BIN=python3
    else
        echo "Python not found"
        exit 1
    fi
    cat <<EOF | "${PYTHON_BIN}"
import numpy as np
f = open('${CALCTEMP}', 'r')
arr = []
for line in f.readlines():
    arr.append(float(line))
f.close()
print(np.percentile(a=np.array(arr), q=${PERCENTILE_}, interpolation='linear'))
EOF
}

pushd ./../../
make c2w imagemounter.wasm
./out/c2w --external-bundle ./out/out.wasm
mkdir -p ./out/assets/
cat ./out/out.wasm | gzip > ./out/assets/out.wasm.gzip
cat ./out/imagemounter.wasm | gzip > ./out/assets/imagemounter.wasm.gzip
( cd extras/runcontainerjs/ ; npm install ; npx webpack && cp -R ./dist ../../out/assets/ )

popd
docker compose build

TMP_RES=$(mktemp)
TMP_RES_2=$(mktemp)
TMP_DATA_LOG=$(mktemp)
TMP_DATA_PULL=$(mktemp)
TMP_DATA_RUN=$(mktemp)
function cleanup {
    local ORG_EXIT_CODE="${1}"
    rm "${TMP_RES}" || true
    rm "${TMP_RES_2}" || true
    rm "${TMP_DATA_LOG}" || true
    rm "${TMP_DATA_PULL}" || true
    rm "${TMP_DATA_RUN}" || true
    exit "${ORG_EXIT_CODE}"
}
trap 'cleanup "$?"' EXIT SIGHUP SIGINT SIGQUIT SIGTERM

ELEMS=$(jq length "${TARGET_FILE_}")

for ((e = 0 ; e < ${ELEMS} ; e++ )); do
    TARGET_IMAGE=$(jq -r '.['"$e"'].image' "${TARGET_FILE_}")
    TARGET_STRING=$(jq -r '.['"$e"'].want' "${TARGET_FILE_}")
    TARGET_COMMAND=$(jq -r '.['"$e"'].command' "${TARGET_FILE_}")
    echo "Using image at ${TARGET_IMAGE} (${PERCENTILE} percentile, ${SAMPLES_NUM_} samples) (want=${TARGET_STRING}, command=${TARGET_COMMAND})"
    : > "${TMP_DATA_LOG}"
    : > "${TMP_DATA_PULL}"
    : > "${TMP_DATA_RUN}"
    for I in $(seq 1 ${SAMPLES_NUM_}); do 
        docker compose up -d --force-recreate
        sleep 5
        docker exec benchmark-pull-benchmark-1 python /run.py "image=${TARGET_IMAGE}&want=${TARGET_STRING}&command=$(echo -n ${TARGET_COMMAND} | base64)" | tee "${TMP_RES}"
        START_TIME=$(cat "${TMP_RES}" | grep "start timer" | yq -j eval . - | jq -r ".message" | sed -E 's/[^"]*(.*)/\1/' | sed -E 's/\\\\/\\/g'  | jq -r | jq -r ".time")
        PULL_TIME=$(cat "${TMP_RES}" | grep "detected pull completion" | yq -j eval . - | jq -r ".message" | sed -E 's/[^"]*(.*)/{\1/' | sed -E 's/\\\\/\\/g'  | jq -r | jq -r ".time")
        RUN_TIME=$(cat "${TMP_RES}" | grep "found wanted string" | yq -j eval . - | jq -r ".message" | sed -E 's/[^"]*(.*)/\1/' | sed -E 's/\\\\/\\/g'  | jq -r | jq -r ".time")

        PULL_DURATION=$(expr $(date -d"${PULL_TIME}" +%s) - $(date -d"${START_TIME}" +%s))
        RUN_DURATION=$(expr $(date -d"${RUN_TIME}" +%s) - $(date -d"${START_TIME}" +%s))

        echo "{\"pull\": \""${PULL_DURATION}"\", \"total\": \""${RUN_DURATION}"\"}" | tee -a "${TMP_DATA_LOG}"
        echo "${PULL_DURATION}" | tee -a "${TMP_DATA_PULL}"
        echo "${RUN_DURATION}" | tee -a "${TMP_DATA_RUN}"
    done
    echo "======== RESULT (${PERCENTILE} percentile, ${SAMPLES_NUM_} samples) ========="
    RES_PULL=$(percentile "${TMP_DATA_PULL}" "${SAMPLES_NUM_}")
    RES_RUN=$(percentile "${TMP_DATA_RUN}" "${SAMPLES_NUM_}")
    echo "pull: ${RES_PULL}, total: ${RES_RUN}"
    if [ "${BENCHMARK_RESULT_}" != "" ] ; then
        echo "======== RESULT ${TARGET_IMAGE} (want=${TARGET_STRING}, command=${TARGET_COMMAND}) (${PERCENTILE} percentile, ${SAMPLES_NUM_} samples) =========" >> "${BENCHMARK_RESULT_}"
        cat "${TMP_DATA_LOG}" >> "${BENCHMARK_RESULT_}"
        echo "RESULT: pull: ${RES_PULL}, total: ${RES_RUN}" >> "${BENCHMARK_RESULT_}"
    fi
done
