#!/bin/bash
set -e

APP_NAME="opencode-openai-proxy"
PID_FILE="/tmp/${APP_NAME}.pid"
LOG_DIR="./logs"
GO_BIN="/Users/xinglongliu/go/go1.25.8/bin/go"

build() {
    echo "==> 编译 $APP_NAME ..."
    GOROOT=/Users/xinglongliu/go/go1.25.8 $GO_BIN build -o "$APP_NAME" .
    echo "    完成: ./$APP_NAME"
}

start() {
    if [ -f "$PID_FILE" ]; then
        pid=$(cat "$PID_FILE")
        if kill -0 "$pid" 2>/dev/null; then
            echo "==> $APP_NAME 已在运行 (PID: $pid)"
            return 0
        fi
        rm -f "$PID_FILE"
    fi

    mkdir -p "$LOG_DIR"
    echo "==> 启动 $APP_NAME ..."
    nohup ./"$APP_NAME" > /dev/null 2>&1 &
    echo $! > "$PID_FILE"
    sleep 1
    echo "    已启动 (PID: $(cat $PID_FILE))"
}

stop() {
    if [ ! -f "$PID_FILE" ]; then
        echo "==> $APP_NAME 未在运行"
        return 0
    fi

    pid=$(cat "$PID_FILE")
    echo "==> 停止 $APP_NAME (PID: $pid) ..."
    kill "$pid" 2>/dev/null || true
    rm -f "$PID_FILE"
    echo "    已停止"
}

restart() {
    stop
    sleep 1
    start
}

case "${1:-}" in
    build)
        build
        ;;
    start)
        if [ ! -f "./$APP_NAME" ]; then
            build
        fi
        start
        ;;
    stop)
        stop
        ;;
    restart)
        stop
        sleep 1
        start
        ;;
    reopen)
        if [ ! -f "$PID_FILE" ]; then
            echo "==> $APP_NAME 未在运行"
            exit 1
        fi
        pid=$(cat "$PID_FILE")
        echo "==> 重新打开日志文件 (PID: $pid) ..."
        kill -HUP "$pid" 2>/dev/null
        echo "    完成"
        ;;
    *)
        echo "用法: $0 {build|start|stop|restart|reopen}"
        exit 1
        ;;
esac
