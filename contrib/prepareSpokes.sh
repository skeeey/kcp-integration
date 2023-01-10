#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"
SPOKE_DIR="${DEMO_DIR}"/spokes

clear

mkdir -p $SPOKE_DIR

clusters=${2:-5}

function create_cluster() {
    hub_index=$1
    hub_name="hub${hub_index}"

    for((i=0;i<$clusters;i++));
    do
        clustername="cluster$i"
        spokename="${hub_name}-${clustername}"
        echo ">> create cluster $spokename"
        export KUBECONFIG="${DEMO_DIR}/spokes/${spokename}.kubeconfig"
        kind create cluster --name "$spokename"
        unset KUBECONFIG
    done
}

hubs=$(find "${DEMO_DIR}"/hubs -name "hub*.kubeconfig" | wc -l)
start_hub=${1:-0}
for((h=${start_hub};h<$hubs;h++));
do
  create_cluster "$h"
done