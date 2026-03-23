#!/usr/bin/env bash
set -euo pipefail

APP_NAME="3x-ui-sub-unifier"
APP_DIR=$(cd "$(dirname "$0")/.." && pwd)
BINARY="${APP_DIR}/${APP_NAME}"
CONFIG="${APP_DIR}/configs/config.yaml"
PID_FILE="${APP_DIR}/${APP_NAME}.pid"

build() {
    cd "${APP_DIR}"
    echo "Building ${APP_NAME}..."
    go build -o "${BINARY}" ./cmd/unifier/
    echo "Build done."
}

start() {
    if [ -f "${PID_FILE}" ]; then
        PID=$(cat "${PID_FILE}")
        if kill -0 "${PID}" 2>/dev/null; then
            echo "${APP_NAME} is already running (PID: ${PID})."
            exit 1
        else
            rm -f "${PID_FILE}"
        fi
    fi

    if [ ! -f "${BINARY}" ]; then
        build
    fi

    echo "Starting ${APP_NAME}..."
    nohup "${BINARY}" -config "${CONFIG}" > /dev/null 2>&1 &
    echo $! > "${PID_FILE}"
    echo "${APP_NAME} started (PID: $!)."
}

stop() {
    if [ ! -f "${PID_FILE}" ]; then
        echo "${APP_NAME} is not running (no PID file found)."
        exit 1
    fi

    PID=$(cat "${PID_FILE}")
    echo "Stopping ${APP_NAME} (PID: ${PID})..."
    kill "${PID}"

    for i in $(seq 1 10); do
        if ! kill -0 "${PID}" 2>/dev/null; then
            echo "${APP_NAME} stopped."
            rm -f "${PID_FILE}"
            return
        fi
        sleep 1
    done

    echo "${APP_NAME} did not stop gracefully, forcing..."
    kill -9 "${PID}"
    rm -f "${PID_FILE}"
    echo "${APP_NAME} killed."
}

status() {
    if [ -f "${PID_FILE}" ]; then
        PID=$(cat "${PID_FILE}")
        if kill -0 "${PID}" 2>/dev/null; then
            echo "${APP_NAME} is running (PID: ${PID})."
        else
            echo "${APP_NAME} is not running (stale PID file)."
        fi
    else
        echo "${APP_NAME} is not running."
    fi
}

restart() {
    stop || true
    start
}

case "${1:-}" in
    start)
        start
        ;;
    stop)
        stop
        ;;
    status)
        status
        ;;
    restart)
        restart
        ;;
    build)
        build
        ;;
    *)
        echo "Usage: $0 {start|stop|status|restart|build}"
        exit 1
        ;;
esac
