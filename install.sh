#!/usr/bin/env bash
set -euo pipefail

REPO="TheRealMal/3x-ui-subs-aggregator"
APP_NAME="subs-aggregator"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/${APP_NAME}"

info()  { printf "\033[1;34m[INFO]\033[0m  %s\n" "$1"; }
error() { printf "\033[1;31m[ERROR]\033[0m %s\n" "$1" >&2; exit 1; }

need_cmd() {
    command -v "$1" >/dev/null 2>&1 || error "required command not found: $1"
}

detect_platform() {
    local os arch
    os="$(uname -s)"
    arch="$(uname -m)"

    case "${os}" in
        Linux)   OS="linux" ;;
        Darwin)  OS="darwin" ;;
        FreeBSD) OS="freebsd" ;;
        *)       error "unsupported OS: ${os}" ;;
    esac

    case "${arch}" in
        x86_64|amd64)       ARCH="amd64" ;;
        aarch64|arm64)      ARCH="arm64" ;;
        i386|i686)          ARCH="386" ;;
        armv7l|armv6l|arm)  ARCH="arm" ;;
        *)                  error "unsupported architecture: ${arch}" ;;
    esac
}

get_latest_version() {
    need_cmd curl

    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep '"tag_name"' \
        | cut -d '"' -f 4) || true

    [ -n "${VERSION}" ] || error "could not determine latest version (check https://github.com/${REPO}/releases)"
    info "latest version: ${VERSION}"
}

install_binary() {
    local binary_name url tmp_dir
    binary_name="${APP_NAME}-${OS}-${ARCH}"
    url="https://github.com/${REPO}/releases/download/${VERSION}/${binary_name}"
    tmp_dir="$(mktemp -d)"

    info "downloading ${binary_name}..."
    curl -fsSL -o "${tmp_dir}/${APP_NAME}" "${url}" \
        || error "download failed — no release for ${OS}/${ARCH} at ${VERSION}"

    chmod +x "${tmp_dir}/${APP_NAME}"

    info "installing to ${INSTALL_DIR}/${APP_NAME}..."
    if [ -w "${INSTALL_DIR}" ]; then
        mv "${tmp_dir}/${APP_NAME}" "${INSTALL_DIR}/${APP_NAME}"
    else
        sudo mv "${tmp_dir}/${APP_NAME}" "${INSTALL_DIR}/${APP_NAME}"
    fi

    rm -rf "${tmp_dir}"
}

setup_config() {
    if [ -f "${CONFIG_DIR}/config.yaml" ]; then
        info "config already exists at ${CONFIG_DIR}/config.yaml, skipping"
        return
    fi

    info "creating config directory ${CONFIG_DIR}..."
    if [ -w "$(dirname "${CONFIG_DIR}")" ]; then
        mkdir -p "${CONFIG_DIR}"
    else
        sudo mkdir -p "${CONFIG_DIR}"
    fi

    local config_url="https://raw.githubusercontent.com/${REPO}/main/configs/config.example.yaml"
    info "downloading example config..."
    if [ -w "${CONFIG_DIR}" ]; then
        curl -fsSL -o "${CONFIG_DIR}/config.yaml" "${config_url}"
    else
        sudo curl -fsSL -o "${CONFIG_DIR}/config.yaml" "${config_url}"
    fi
}

setup_logging() {
    local log_file="/var/log/${APP_NAME}.log"
    if [ ! -f "${log_file}" ]; then
        if [ -w "/var/log" ]; then
            touch "${log_file}"
        else
            sudo touch "${log_file}"
            sudo chmod 666 "${log_file}"
        fi
    fi
}

print_success() {
    cat <<DONE

============================================
  ${APP_NAME} installed successfully!
============================================

  Binary:  ${INSTALL_DIR}/${APP_NAME}
  Config:  ${CONFIG_DIR}/config.yaml
  Logs:    /var/log/${APP_NAME}.log

  Next steps:
    1. Edit the config:
       sudo nano ${CONFIG_DIR}/config.yaml

    2. Start the service:
       ${APP_NAME} start

  Commands:
    ${APP_NAME} start     Start in background
    ${APP_NAME} stop      Graceful stop
    ${APP_NAME} restart   Restart
    ${APP_NAME} status    Check if running
    ${APP_NAME} run       Run in foreground
    ${APP_NAME} version   Show version
    ${APP_NAME} update    Check and install updates

DONE
}

main() {
    info "installing ${APP_NAME}..."
    detect_platform
    info "detected platform: ${OS}/${ARCH}"
    get_latest_version
    install_binary
    setup_config
    setup_logging
    print_success
}

main
