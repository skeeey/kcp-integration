#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"

hubs=${1:-1}

for((i=0;i<$hubs;i++));
do
    echo ">> managedclusters in hub$i"
    export KUBECONFIG="${DEMO_DIR}"/hubs/hub$i.kubeconfig
    kubectl get managedclusters
    unset KUBECONFIG
done