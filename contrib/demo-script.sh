#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"

source "${DEMO_DIR}"/demo_magic
source "${DEMO_DIR}"/utils

clear

export KUBECONFIG=${DEMO_DIR}/external.kubeconfig
echo "prepare 20 hubs ..."
kubectl apply -f workspace/type.yaml

for((i=0;i<20;i++));
do
    echo "hub-$i is created at $(date '+%Y-%m-%dT%H:%M:%SZ')"
    kubectl kcp workspace create "hub-$i" --type hub
    sleep 2
done

unset KUBECONFIG
