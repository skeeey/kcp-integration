#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"
HUB_DIR="${DEMO_DIR}"/hubs

clear

function create_policy() {
  hub_index=$1
  hub_name="hub${hub_index}"

  echo ">>> Apply policy in hub ${hub_name}"
  export KUBECONFIG="${HUB_DIR}/${hub_name}.kubeconfig"
  kubectl apply -f "${DEMO_DIR}/policy"
  unset KUBECONFIG
}


hubs=$(find "${DEMO_DIR}"/hubs -name "hub*.kubeconfig" | wc -l)
for((h=0;h<$hubs;h++));
do
  create_policy "$h"
done
