#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"
KCP_DIR="${DEMO_DIR}"/kcp

BUILD_BINARY=${BUILD_BINARY:-"true"}

KCP_SERVER_ARGS=""

source "${DEMO_DIR}"/utils

rm -rf ${DEMO_DIR}/.kcp

# build binary if it is required
if [ "$BUILD_BINARY" = "true" ]; then
    echo "Building kcp ..."
    rm -rf kcp
    git clone --depth 1 https://github.com/skeeey/kcp.git
    pushd $KCP_DIR
    make build
    if [ ! -f "bin/kcp" ]; then
        echo "kcp does not exist. Compilation probably failed"
        exit 1
    fi
    popd
fi

generate_ca "${DEMO_DIR}"
KCP_SERVER_ARGS="${KCP_SERVER_ARGS} --client-ca-file ${DEMO_DIR}/rootca.crt"

echo "Starting KCP server ..."
(cd "${DEMO_DIR}" && exec "${KCP_DIR}"/bin/kcp start $KCP_SERVER_ARGS) &> kcp.log &
KCP_PID=$!
echo "KCP server started: $KCP_PID"

echo "Waiting for KCP server to be ready..."
wait_command "grep 'Serving securely' ${DEMO_DIR}/kcp.log"
wait_command "grep 'Ready to start controllers' ${DEMO_DIR}/kcp.log"

touch "${DEMO_DIR}/kcp-started"

kubectl config view --kubeconfig "${DEMO_DIR}"/.kcp/admin.kubeconfig --minify --flatten --context=root > "${DEMO_DIR}"/.kcp/root.kubeconfig
kubectl config view --kubeconfig "${DEMO_DIR}"/.kcp/admin.kubeconfig --minify --flatten --context=root > "${DEMO_DIR}"/.kcp/demo.kubeconfig

echo "KCP server is ready. Press <ctrl>-C to terminate."
wait
