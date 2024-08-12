#!/bin/bash

set -e

get_latest_version() {
    curl --silent "https://api.github.com/repos/nguyenvanduocit/epubtrans/releases/latest" |
    grep '"tag_name":' |
    sed -E 's/.*"([^"]+)".*/\1/'
}

get_os_arch() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case $ARCH in
        x86_64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        i386|i686)
            ARCH="386"
            ;;
    esac

    echo "${OS}_${ARCH}"
}

VERSION=$(get_latest_version)
OS_ARCH=$(get_os_arch)

DOWNLOAD_URL="https://github.com/nguyenvanduocit/epubtrans/releases/download/${VERSION}/epubtrans_${VERSION#v}_${OS_ARCH}.tar.gz"

echo "Downloading epubtrans ${VERSION} for ${OS_ARCH}..."
curl -L -o epubtrans.tar.gz "$DOWNLOAD_URL"

echo "Extracting..."
tar -xzf epubtrans.tar.gz

echo "Installing..."
sudo mv epubtrans /usr/local/bin/

echo "Cleaning up..."
rm epubtrans.tar.gz

echo "epubtrans ${VERSION} has been installed successfully!"
