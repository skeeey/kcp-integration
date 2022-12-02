#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"

namespace="control-plane"

kubectl -n $namespace delete -f $DEMO_DIR/deploy/kcp
kubectl delete -f $DEMO_DIR/deploy/controller/clusterrole_binding.yaml
kubectl -n $namespace delete -f $DEMO_DIR/deploy/controller/deployment.yaml
kubectl -n $namespace delete -f $DEMO_DIR/deploy/controller/service_account.yaml
