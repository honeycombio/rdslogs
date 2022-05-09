#!/bin/bash

# Build deb or rpm packages for rdslogs.
set -e

function usage() {
    echo "Usage: build-pkg.sh -v <version> -t <package_type> -m arch"
    exit 2
}

while getopts "v:t:m:" opt; do
    case "$opt" in
    v)
        version=$OPTARG
        ;;
    t)
        pkg_type=$OPTARG
        ;;
    m)
        arch=$OPTARG
        ;;
    esac
done

if [ -z "$version" ] || [ -z "$pkg_type" ] || [ -z "$arch" ]; then
    usage
fi

fpm -s dir -n rdslogs \
    -m "Honeycomb <solutions@honeycomb.io>" \
    -p ~/artifacts \
    -v $version \
    -t $pkg_type \
    -a $arch \
    --pre-install=./preinstall \
    ~/artifacts/rdslogs-linux-${arch}=/usr/bin/rdslogs \
    ./rdslogs.upstart=/etc/init/rdslogs.conf \
    ./rdslogs.service=/lib/systemd/system/rdslogs.service \
    ./rdslogs.conf=/etc/rdslogs/rdslogs.conf \
