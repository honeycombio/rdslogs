#!/bin/bash

# Build deb or rpm packages for rdslogs.
set -e

function usage() {
    echo "Usage: build-pkg.sh -v <version> -t <package_type>"
    exit 2
}

while getopts "v:t:" opt; do
    case "$opt" in
    v)
        version=$OPTARG
        ;;
    t)
        pkg_type=$OPTARG
        ;;
    esac
done

if [ -z "$version" ] || [ -z "$pkg_type" ]; then
    usage
fi

fpm -s dir -n rdslogs \
    -m "Honeycomb <team@honeycomb.io>" \
    -p $GOPATH/bin \
    -v $version \
    -t $pkg_type \
    --pre-install=./preinstall \
    $GOPATH/bin/rdslogs=/usr/bin/rdslogs \
    ./rdslogs.upstart=/etc/init/rdslogs.conf \
    ./rdslogs.service=/lib/systemd/system/rdslogs.service \
    ./rdslogs.conf=/etc/rdslogs/rdslogs.conf \
