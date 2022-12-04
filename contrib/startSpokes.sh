#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"
HUB_DIR="${DEMO_DIR}"/hubs

clear

rm -rf "${DEMO_DIR}"/hub-kubeconfigs
rm -f "${DEMO_DIR}"/*.log

#export KUBECONFIG="${DEMO_DIR}"/spoke.kubeconfig
kubectl delete namespace open-cluster-management-agent --ignore-not-found
kubectl create namespace open-cluster-management-agent
#unset KUBECONFIG

function create_cluster() {
  hubname=$1
  listenPort=$2
  clusters=1
  for((i=0;i<$clusters;i++));
  do
    clustername="cluster$i"
    spokename="${hubname}-${clustername}"
    echo "create cluster ${clustername} in hub ${hubname} port=$listenPort"
    #continue

    kubectl delete namespace ${spokename} --ignore-not-found
    kubectl create namespace ${spokename}

    bootstrap_kubeconfig="${HUB_DIR}/${hubname}.kubeconfig"

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

    hub_kubeconfig_dir="${DEMO_DIR}"/hub-kubeconfigs/${spokename}
    mkdir -p ${hub_kubeconfig_dir}

    args="--disable-leader-election"
    args="${args} --cluster-name=${clustername}"
    args="${args} --listen=0.0.0.0:$listenPort"
    args="${args} --namespace=${spokename}"
    args="${args} --kubeconfig=${HOME}/.kube/config"
    args="${args} --bootstrap-kubeconfig=${bootstrap_kubeconfig}"
    args="${args} --hub-kubeconfig-dir=${hub_kubeconfig_dir}"
    args="${args} --hub-kubeconfig-secret=${spokename}-hub-kubeconfig"
    args="${args} --feature-gates=ClusterClaim=false"

    #"${DEMO_DIR}"/bin/registration agent ${args}
    (cd "${DEMO_DIR}" && exec "${DEMO_DIR}"/registration/bin/registration agent $args) &> ${spokename}.log &
    agent_id=$!
    echo "Agent started: $agent_id"
  done
}

hubs=2
port=8443
for((h=0;h<$hubs;h++));
do
  create_cluster "hub$h" "$port"
  port=$(($port + 1))
done
