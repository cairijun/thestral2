#!/bin/bash

function warn() {
    >&2 echo -e "\033[1;33mWARN: $*\033[0m"
}

function error() {
    >&2 echo -e "\033[0;31mERROR: $*\033[0m"
}

function info() {
    >&2 echo -e "\033[0;37mINFO: $*\033[0m"
}

function success() {
    >&2 echo -e "\033[0;32m$*\033[0m"
}

COMM_PKG="github.com/richardtsai/thestral2/lib"
VERSION=$(git describe --always --dirty)
if [[ $? != 0 ]]; then
    warn "failed to retrive version"
else
    info "version: $VERSION"
    EXT_VARS="-X \"$COMM_PKG.ThestralVersion=$VERSION\""
fi

BUILT_TIME=$(date -R)
if [[ $? != 0 ]]; then
    warn "failed to get built time"
else
    EXT_VARS="$EXT_VARS -X \"$COMM_PKG.ThestralBuiltTime=$BUILT_TIME\""
fi

if [[ "$EXT_VARS" != "" ]]; then
    GO_ARGS="-ldflags '$EXT_VARS'"
fi

if [[ $# == 0 ]] || [[ $1 == \-* ]]; then
    cmd="build"
else
    cmd="$1"
    shift
fi

if [[ $# > 0 ]]; then
    EXTRA_ARGS=$(printf "'%s' " "$@")
fi

case $cmd in
    "help")
        echo "$0 [(help | build | test | install) [extra_args...]]"
        exit 0 ;;
    "build")
        CMD="go build $GO_ARGS $EXTRA_ARGS" ;;
    "test")
        CMD="go test $GO_ARGS $EXTRA_ARGS -p 1 ./..." ;;
    "install")
        CMD="go install $GO_ARGS $EXTRA_ARGS" ;;
    *)
        error "unknown command '$cmd'"
        exit 1 ;;
esac

eval "$CMD"
EXIT_CODE=$?
if [[ $EXIT_CODE == 0 ]]; then
    success "Done: $CMD"
else
    error "failed to run $CMD"
fi
exit $EXIT_CODE
