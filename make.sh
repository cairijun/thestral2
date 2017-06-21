#!/bin/sh

function warn() {
    echo "\033[1;33mWARN: $*\033[0m"
}

function error() {
    echo "\033[0;31mERROR: $*\033[0m"
}

function info() {
    echo "\033[0;37mINFO: $*\033[0m"
}

function success() {
    echo "\033[0;32m$*\033[0m"
}

VERSION=$(git describe --always --dirty)
if [[ $? != 0 ]]; then
    warn "failed to retrive version"
else
    info "version: $VERSION"
fi

if [[ "$VERSION" != "" ]]; then
    GO_ARGS="-ldflags '-X \"main.ThestralVersion=$VERSION\"'"
fi

if [[ $# == 0 ]]; then
    cmd="build"
else
    cmd="$1"
    shift
fi

case $cmd in
    "help")
        echo "$0 [(help | build | test | install) [extra_args...]]"
        exit 0 ;;
    "build")
        CMD="go build $GO_ARGS $*" ;;
    "test")
        CMD="go test $GO_ARGS $*" ;;
    "install")
        CMD="go install $GO_ARGS $*" ;;
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
