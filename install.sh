#!/bin/bash


APP_NAME="xymusic"
DOWNLOAD_URL="https://github.com/muchenspace/XyMusic/releases/download/v0.1/XyMusic_0.1_linux_amd64.zip" 
INSTALL_DIR="/opt/xymusic"
SERVICE_FILE="/etc/systemd/system/${APP_NAME}.service"
TMP_DIR=$(mktemp -d)

# ==========================================
# 颜色与工具函数
# ==========================================
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info() { echo -e "${GREEN}[INFO]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

cleanup() {
    info "清理临时文件..."
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

# ==========================================
# 核心逻辑函数
# ==========================================

# 1. 检查 Root 权限
check_root() {
    if [[ $EUID -ne 0 ]]; then
        error "此脚本需要 root 权限来写入 /opt 和 systemd 目录，请使用 sudo 执行: sudo ./install.sh"
    fi
}

# 2. 下载并解压
download_and_extract() {
    local ARCHIVE="${TMP_DIR}/${APP_NAME}.zip"
    
    info "正在从 ${DOWNLOAD_URL} 拉取 release 包..."
    # 使用 -f 在 HTTP 错误时返回失败状态，-L 跟随重定向
    curl -fSL -o "$ARCHIVE" "$DOWNLOAD_URL" || error "下载失败，请检查 URL 是否正确。"
    
    info "正在解压到 ${INSTALL_DIR}..."
    mkdir -p "$INSTALL_DIR"
    # 清空旧目录内容（防止旧文件干扰）
    rm -rf "${INSTALL_DIR:?}/"*
    
    # 解压文件
    unzip "$ARCHIVE" -d "$INSTALL_DIR" || error "解压失败。请确认下载的是 zip 格式。"
}

# 3. 赋予权限并定位二进制文件
setup_permissions() {
    # 查找解压出来的二进制文件（可能在根目录，也可能在子目录中）
    local BINARY_PATH=$(find "$INSTALL_DIR" -name "${APP_NAME}" -type f | head -n 1)
    
    if [ -z "$BINARY_PATH" ]; then
        error "在解压后的文件中未找到二进制文件: ${APP_NAME}"
    fi
    
    # 如果二进制文件在子目录中，将其移动到 /opt/xymusic 根目录方便管理
    if [ "$BINARY_PATH" != "${INSTALL_DIR}/${APP_NAME}" ]; then
        mv "$BINARY_PATH" "${INSTALL_DIR}/${APP_NAME}"
    fi

    info "为 ${INSTALL_DIR}/${APP_NAME} 赋予 777 权限..."
    chmod 777 "${INSTALL_DIR}/${APP_NAME}"
}

# 4. 配置 Systemd 服务
setup_systemd() {
    info "配置 systemd 服务..."
    cat <<EOF > "$SERVICE_FILE"
[Unit]
Description=${APP_NAME} Service
After=network.target

[Service]
Type=simple
# 工作目录
WorkingDirectory=${INSTALL_DIR}
# 启动命令
ExecStart=${INSTALL_DIR}/${APP_NAME}
# 崩溃后自动重启
Restart=on-failure
RestartSec=5s
# 安全限制 (按需开启或注释)
# User=nobody
# Group=nogroup

[Install]
WantedBy=multi-user.target
EOF

    info "重载 systemd 配置..."
    systemctl daemon-reload

    info "设置开机自启并启动服务..."
    systemctl enable "${APP_NAME}"
    systemctl start "${APP_NAME}"

    info "当前服务状态："
    systemctl status "${APP_NAME}" --no-pager || true
}


main() {
    set -euo pipefail
    info "===== 开始安装 ${APP_NAME} ====="
    check_root
    download_and_extract
    setup_permissions
    setup_systemd
    info "===== ${APP_NAME} 安装完成 ====="
}

main "$@"
