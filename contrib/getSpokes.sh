#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"

hubs=$(find "${DEMO_DIR}"/hubs -name "hub*.kubeconfig" | wc -l)

for((i=0;i<$hubs;i++));
do
    echo ">> hub$i"
    export KUBECONFIG="${DEMO_DIR}"/hubs/hub$i.kubeconfig
    kubectl get managedclusters
    kubectl get managedclusteraddons --all-namespaces
    kubectl get policies --all-namespaces
    #kubectl get ConfigurationPolicy --all-namespaces
    unset KUBECONFIG
done