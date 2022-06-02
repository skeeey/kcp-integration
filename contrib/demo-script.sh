#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"

source "${DEMO_DIR}"/demo_magic
source "${DEMO_DIR}"/utils

clear

export KUBECONFIG=${DEMO_DIR}/.kcp/demo.kubeconfig

comment "Create an organization workspace in the KCP server"
pe "kubectl kcp workspace create myorg --type organization"
pe "kubectl kcp workspace use myorg"

comment "Create a hub workspace type in myorg workspace"
pe "cat workspace/type.yaml"
pe "kubectl apply -f workspace/type.yaml"

comment "Create a hub workspace in myorg workspace"
pe "kubectl kcp workspace create hub --type ocmhub"

comment "Wait for the hub is deployed"
pe "kubectl get clusterworkspace hub -w -ojsonpath='{range .status.conditions[*]}{.type}{\"\\t\"}{.status}{\"\\n\"}{end}'"

clear

comment "Create a managed cluster in hub workspace"
pe "kubectl kcp workspace use hub"
pe "kubectl apply -f clusters/cluster1.yaml"

kubectl config view --kubeconfig "${DEMO_DIR}"/.kcp/demo.kubeconfig --minify --flatten  > "${DEMO_DIR}"/clusters/cluster1/hub.kubeconfig

comment "Import the managed cluster"
pe "kubectl apply --kubeconfig kubeconfig/cluster1.kubeconfig -k clusters/cluster1"

comment "Approve the managed cluter CSR"
pe "kubectl get csr -w"
csr_name=$(kubectl get csr -l open-cluster-management.io/cluster-name=cluster1 | grep Pending | awk '{print $1}')
pe "kubectl certificate approve ${csr_name}"
pe "kubectl get managedclusters -w"

unset KUBECONFIG
