#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"

export KUBECONFIG=${DEMO_DIR}/kubeconfig/hub.kubeconfig
kubectl delete clustermanagers --all --wait=false
kubectl get clustermanagers | grep -v NAME | awk '{print $1}' | xargs kubectl delete ns --wait=false
kubectl get clustermanagers | grep -v NAME | awk '{print $1}' | xargs kubectl patch clustermanagers -p '{"metadata":{"finalizers": []}}' --type=merge
unset KUBECONFIG

export KUBECONFIG=${DEMO_DIR}/kubeconfig/cluster1.kubeconfig
kubectl delete klusterlets --all
unset KUBECONFIG

rm -rf kubeconfig
rm -rf .kcp
rm -f *.log
rm -f kcp-started
