#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"
SPOKE_DIR="${DEMO_DIR}"/spokes

clear

mkdir -p $SPOKE_DIR

function create_cluster() {
    hub_index=$1
    hub_name="hub${hub_index}"

    clusters=1
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
for((h=0;h<$hubs;h++));
do
  create_cluster "$h"
done