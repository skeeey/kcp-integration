#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"
ROOT_DIR="$( cd ${CURRENT_DIR}/.. && pwd)"

BUILD_BINARY=${BUILD_BINARY:-"false"}
IN_CLUSTER=${IN_CLUSTER:-"false"}

source "${DEMO_DIR}"/utils

# build binary if it is required
if [ "$BUILD_BINARY" = "true" ]; then
    echo "Building kcp-integration controller ..."
    pushd $ROOT_DIR
    rm -f kcp-integration
    make build
    if [ ! -f "kcp-integration" ]; then
        echo "kcp-integration does not exist.Compilation probably failed"
        exit 1
    fi
    popd
fi

rm -rf "${DEMO_DIR}"/kubeconfig
mkdir -p "${DEMO_DIR}"/kubeconfig

kubectl config view --minify --flatten --context=kind-management > "${DEMO_DIR}"/kubeconfig/management.kubeconfig
kubectl config view --minify --flatten --context=kind-dev > "${DEMO_DIR}"/kubeconfig/dev.kubeconfig
kubectl config view --minify --flatten --context=kind-qe > "${DEMO_DIR}"/kubeconfig/qe.kubeconfig

HUB_KUBECONFIG=${DEMO_DIR}/kubeconfig/management.kubeconfig
KCP_KUBECONFIG="${DEMO_DIR}"/.kcp/root.kubeconfig

export KUBECONFIG=${DEMO_DIR}/kubeconfig/management.kubeconfig
echo "Deploy the cluster manager operator on the management cluster ...."
hub_advertise_addr=$(kubectl -n kube-system get cm kubeadm-config -o=jsonpath='{.data.ClusterStatus}' | grep advertiseAddress | awk '{print $2}')
registration_webhook_host="${hub_advertise_addr}"
work_webhook_host="${hub_advertise_addr}"
# TODO: work webhook need read kube-system/extension-apiserver-authentication
kubectl kustomize "${DEMO_DIR}"/clustermanager | sed "s/<webhook_public_host_placeholder>/${hub_advertise_addr}/g" | kubectl apply -f -
unset KUBECONFIG

export KUBECONFIG=${DEMO_DIR}/kubeconfig/dev.kubeconfig
echo "Deploy the klusterlet operator on the dev cluster ...."
kubectl apply -k "${DEMO_DIR}"/klusterlet
kubectl create ns open-cluster-management-agent
unset KUBECONFIG

export KUBECONFIG=${DEMO_DIR}/kubeconfig/qe.kubeconfig
echo "Deploy the klusterlet operator on the qe cluster ...."
kubectl apply -k "${DEMO_DIR}"/klusterlet
kubectl create ns open-cluster-management-agent
unset KUBECONFIG

echo "Waiting for KCP server to be started..."
wait_command "test -f ${DEMO_DIR}/kcp-started"

if [ "$IN_CLUSTER" = "true" ]; then
    echo "Deploy the kcp-integration controller in the management cluster with HUB_KUBECONFIG=${HUB_KUBECONFIG}, KCP_KUBECONFIG=${KCP_KUBECONFIG}"
    pushd $ROOT_DIR
    make deploy
    popd
    exit 0
fi

CTRL_ARGS="--disable-leader-election --namespace=default"
CTRL_ARGS="${CTRL_ARGS} --kcp-kubeconfig=${KCP_KUBECONFIG} --kubeconfig=${HUB_KUBECONFIG}"
CTRL_ARGS="${CTRL_ARGS} --kcp-signing-cert-file=${DEMO_DIR}/rootca.crt --kcp-signing-key-file=${DEMO_DIR}/rootca.key"
CTRL_ARGS="${CTRL_ARGS} --registration-webhook-host=${registration_webhook_host}"
CTRL_ARGS="${CTRL_ARGS} --work-webhook-host=${work_webhook_host}"

(cd "${ROOT_DIR}" && exec "${ROOT_DIR}"/kcp-integration controller ${CTRL_ARGS}) &> kcp-integration-controller.log &
KCP_OCM_PID=$!
echo "KCP integration controller started: ${KCP_OCM_PID}. Press <ctrl>-C to terminate."
wait

#${ROOT_DIR}/kcp-integration controller ${CTRL_ARGS}
