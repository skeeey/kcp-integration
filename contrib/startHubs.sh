#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"
KCP_DATA_DIR="${DEMO_DIR}"/.kcp

source "${DEMO_DIR}"/demo_magic
source "${DEMO_DIR}"/utils

clear

mkdir -p "${DEMO_DIR}"/hubs

hubs=${1:-1}

echo ">>> Prepare $hubs hubs"
export KUBECONFIG="${DEMO_DIR}"/root.kubeconfig
kubectl apply -f workspace/type.yaml
for((i=0;i<$hubs;i++));
do
    hubname="hub$i"
    echo ">>> hub ${hubname} is created"
    kubectl kcp workspace create "$hubname" --type hub
    
    cp root.kubeconfig "${DEMO_DIR}/hubs/${hubname}.kubeconfig"

    server=$(kubectl --kubeconfig "${DEMO_DIR}/hubs/${hubname}.kubeconfig" config view -o jsonpath='{.clusters[?(@.name == "root")].cluster.server}')
    server="${server}:${hubname}"
    kubectl --kubeconfig "${DEMO_DIR}/hubs/${hubname}.kubeconfig" config set-cluster root --server=${server}

    echo ">>> hub ${hubname} kubeconfig: ${DEMO_DIR}/hubs/${hubname}.kubeconfig"
done
unset KUBECONFIG
