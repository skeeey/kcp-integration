FROM golang:1.19 AS builder
ARG OS=linux
ARG ARCH=amd64
WORKDIR /go/src/github.com/skeeey/kcp-integration
COPY . .

ENV GO_PACKAGE github.com/skeeey/kcp-integration

RUN GOOS=${OS} \
    GOARCH=${ARCH} \
    make build --warn-undefined-variables

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
ENV USER_UID=10001

COPY --from=builder /go/src/github.com/skeeey/kcp-integration/xcm-connector /

USER ${USER_UID}
