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

comment "Create a workspace type for OCM hub in myorg workspace"
pe "cat workspace/type.yaml"
pe "kubectl apply -f workspace/type.yaml"

comment "Create a hub workspace in myorg workspace for dev"
pe "kubectl kcp workspace create devhub --type ocmhub"

comment "Wait for the hosted cluster manager is available"
pe "kubectl get clusterworkspace devhub -w -ojsonpath='{range .status.conditions[*]}{.type}{\"\\t\"}{.status}{\"\\n\"}{end}'"

comment "Go to dev hub workspace"
pe "kubectl kcp workspace use devhub"
comment "The OCM resources in the dev hub workspace"
pe "kubectl get crds"
comment "Create a managed cluster for dev cluster in dev hub workspace"
pe "kubectl apply -f clusters/dev/managedcluster.yaml"

kubectl config view --kubeconfig "${DEMO_DIR}"/.kcp/demo.kubeconfig --minify --flatten  > "${DEMO_DIR}"/clusters/dev/klusterlet/hub.kubeconfig

comment "Import the dev cluster to the dev hub"
pe "kubectl apply --kubeconfig kubeconfig/dev.kubeconfig -k clusters/dev/klusterlet"

comment "Approve the managed cluter CSR"
pe "kubectl get csr -w"
csr_name=$(kubectl get csr -l open-cluster-management.io/cluster-name=dev | grep Pending | awk '{print $1}')
pe "kubectl certificate approve ${csr_name}"
pe "kubectl get managedclusters -w"

comment "Go back to myorg workspace"
pe "kubectl kcp workspace use root:myorg"

comment "Create a hub workspace in myorg workspace for qe"
pe "kubectl kcp workspace create qehub --type ocmhub"

comment "Wait for the hosted cluster manager is available"
pe "kubectl get clusterworkspace qehub -w -ojsonpath='{range .status.conditions[*]}{.type}{\"\\t\"}{.status}{\"\\n\"}{end}'"

comment "Go to qe hub workspace"
pe "kubectl kcp workspace use qehub"
comment "The OCM resources in the qe hub workspace"
pe "kubectl get crds"
comment "Create a managed cluster for qe cluster in qe hub workspace"
pe "kubectl apply -f clusters/qe/managedcluster.yaml"

kubectl config view --kubeconfig "${DEMO_DIR}"/.kcp/demo.kubeconfig --minify --flatten  > "${DEMO_DIR}"/clusters/qe/klusterlet/hub.kubeconfig

comment "Import the qe cluster to the qe hub"
pe "kubectl apply --kubeconfig kubeconfig/qe.kubeconfig -k clusters/qe/klusterlet"

comment "Approve the managed cluter CSR"
pe "kubectl get csr -w"
csr_name=$(kubectl get csr -l open-cluster-management.io/cluster-name=qe | grep Pending | awk '{print $1}')
pe "kubectl certificate approve ${csr_name}"
pe "kubectl get managedclusters -w"

comment "Now we have two independent ocm hub control planes in the myorg workspace"
pe "kubectl kcp workspace use root:myorg"
pe "kubectl get workspaces"

unset KUBECONFIG
