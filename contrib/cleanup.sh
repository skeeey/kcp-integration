#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"

if [ "$1"x = "save"x ]; then
    ps ax | pgrep "registration" | xargs kill

    kubectl delete namespace open-cluster-management-agent --ignore-not-found

    for((i=0;i<20;i++));
    do
        for((j=0;j<5;j++));
        do
            kubectl delete namespace "hub$i-cluster$j" --ignore-not-found
        done
    done

    namespace="control-plane"

    kubectl -n $namespace delete -f $DEMO_DIR/deploy/kcp --ignore-not-found

    kubectl delete -f $DEMO_DIR/deploy/controller/clusterrole_binding.yaml --ignore-not-found
    kubectl -n $namespace delete -f $DEMO_DIR/deploy/controller/deployment.yaml --ignore-not-found
    kubectl -n $namespace delete -f $DEMO_DIR/deploy/controller/service_account.yaml --ignore-not-found

    kubectl delete ns $namespace --ignore-not-found
else
    kind delete clusters --all
    rm -rf "${DEMO_DIR}"/spokes
fi

rm -rf "${DEMO_DIR}"/hub-kubeconfigs
rm -rf "${DEMO_DIR}"/*.kubeconfig
rm -rf "${DEMO_DIR}"/.kcp
rm -f "${DEMO_DIR}"/kcp-started
rm -rf "${DEMO_DIR}"/rootca.*
rm -f "${DEMO_DIR}"/*.log
rm -rf "${DEMO_DIR}"/hubs
