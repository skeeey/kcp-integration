#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"
KCP_DATA_DIR="${DEMO_DIR}"/.kcp

clear

namespace="control-plane"
kubectl delete namespace $namespace --ignore-not-found
kubectl create namespace $namespace

kubectl -n $namespace delete secret kcp-client-ca --ignore-not-found
kubectl -n $namespace create secret generic kcp-client-ca --from-file=rootca.crt=rootca.crt --from-file=rootca.key=rootca.key

# kubectl -n $namespace apply -f $DEMO_DIR/deploy/kcp

# echo "waiting kcp server start with 10s"
# sleep 10

# pod_name=$(kubectl -n $namespace get pods | awk '{print $1}' | tail -1)
# kubectl -n $namespace exec ${pod_name} -- bash -c "cat /var/kcp-data/admin.kubeconfig" > admin.kubeconfig

kubectl config view --minify --flatten --kubeconfig ${KCP_DATA_DIR}/admin.kubeconfig > root.kubeconfig

#rm -f external.kubeconfig
#cp root.kubeconfig external.kubeconfig
#kubectl config --kubeconfig external.kubeconfig set-cluster root --server=https://127.0.0.1:6443/clusters/root
#kubectl config --kubeconfig external.kubeconfig set-cluster root --insecure-skip-tls-verify=true

kubectl -n $namespace delete secret kcp-admin-kubeconfig --ignore-not-found
kubectl -n $namespace create secret generic kcp-admin-kubeconfig --from-file=admin.kubeconfig=root.kubeconfig

kubectl apply -f $DEMO_DIR/deploy/controller/clusterrole_binding.yaml
kubectl -n $namespace apply -f $DEMO_DIR/deploy/controller/deployment.yaml
kubectl -n $namespace apply -f $DEMO_DIR/deploy/controller/service_account.yaml

kubectl -n control-plane get pods -w
