#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"
KCP_DATA_DIR="${DEMO_DIR}"/.kcp

source "${DEMO_DIR}"/demo_magic
source "${DEMO_DIR}"/utils

clear

mkdir -p "${DEMO_DIR}"/hubs

hubs=${1:-1}
current_hubs=$(find "${DEMO_DIR}"/hubs -name "hub*.kubeconfig" | wc -l)

echo ">>> Prepare $hubs hubs, $current_hubs exists"
export KUBECONFIG="${DEMO_DIR}"/root.kubeconfig
kubectl apply -f workspace/type.yaml
for((i=$current_hubs;i<$hubs;i++));
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

echo ">>> wating hub ready... (10s)"
sleep 5

for((i=$current_hubs;i<$hubs;i++));
do
    echo ">> enable addons in hub$i"
    export KUBECONFIG="${DEMO_DIR}"/hubs/hub$i.kubeconfig
    kubectl apply -f "${DEMO_DIR}/addons/clustermanagementaddons"
    unset KUBECONFIG
done
