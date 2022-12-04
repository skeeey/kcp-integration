#!/usr/bin/env bash

CURRENT_DIR="$(dirname "${BASH_SOURCE[0]}")"
DEMO_DIR="$(cd ${CURRENT_DIR} && pwd)"

kcp_pid=$1

kcp_mem=$(cat /proc/${kcp_pid}/status | grep VmRSS | awk '{print $2}')
kcp_mem=$((${kcp_mem}/1024))

control_plane_mem=$(kubectl -n control-plane top pods --use-protocol-buffers | awk '{print $3}' | tail -1)

log_time=$(date '+%Y-%m-%dT%H:%M:%SZ')
echo "====== ${log_time} ======" | tee -a metrics
echo "kcp mem(bytes): ${kcp_mem}Mi" | tee -a metrics
echo "control-plane mem(bytes): ${control_plane_mem}" | tee -a metrics
echo "==================================" | tee -a metrics
