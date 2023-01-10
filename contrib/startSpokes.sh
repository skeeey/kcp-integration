#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"
HUB_DIR="${DEMO_DIR}"/hubs

clear

rm -rf "${DEMO_DIR}"/hub-kubeconfigs
clusters=${2:-5}

function create_cluster() {
  hub_index=$1
  hub_name="hub${hub_index}"

  clusters=1
  for((i=0;i<$clusters;i++));
  do
    clustername="cluster$i"
    spokename="${hub_name}-${clustername}"
    echo ">>> Register cluster ${clustername} in hub ${hub_name}"

    bootstrap_kubeconfig="${HUB_DIR}/${hub_name}.kubeconfig"

    export KUBECONFIG="${bootstrap_kubeconfig}"
    cat <<EOF | kubectl apply -f -
apiVersion: cluster.open-cluster-management.io/v1
kind: ManagedCluster
metadata:
  name: $clustername
spec:
  hubAcceptsClient: true
EOF
    unset KUBECONFIG

    kubectl config view --kubeconfig "${bootstrap_kubeconfig}" --minify --flatten > "${DEMO_DIR}/klusterlet/bootstrap/hub-kubeconfig"

    export KUBECONFIG="${DEMO_DIR}/spokes/${spokename}.kubeconfig"
    kubectl apply -k "${DEMO_DIR}/klusterlet"
    kubectl create ns open-cluster-management-agent
    kubectl apply -k "${DEMO_DIR}/klusterlet/bootstrap"
    cat <<EOF | kubectl apply -f -
apiVersion: operator.open-cluster-management.io/v1
kind: Klusterlet
metadata:
  name: klusterlet
spec:
  deployOption:
    mode: Default
  registrationImagePullSpec: quay.io/open-cluster-management/registration
  workImagePullSpec: quay.io/open-cluster-management/work
  clusterName: $clustername
  namespace: open-cluster-management-agent
  externalServerURLs:
  - url: https://localhost
  registrationConfiguration:
    featureGates:
    - feature: AddonManagement
      mode: Enable
EOF
    unset KUBECONFIG

    sleep 10
  done
}

hubs=$(find "${DEMO_DIR}"/hubs -name "hub*.kubeconfig" | wc -l)
start_hub=${1:-0}
for((h=$start_hub;h<$hubs;h++));
do
  create_cluster "$h"
done
