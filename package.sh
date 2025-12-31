#!/bin/bash

# 打包脚本
# 用法: ./package.sh <VERSION>

VERSION=$1
if [ -z "$VERSION" ]; then
    echo "用法: $0 <VERSION>"
    exit 1
fi

# 获取脚本所在目录的绝对路径
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$SCRIPT_DIR

# 读取 ngapost2md 版本
NP2MD_VERSION_FILE="$ROOT_DIR/ngapost2md.version"
if [ -f "$NP2MD_VERSION_FILE" ]; then
    NP2MD_VERSION=$(cat "$NP2MD_VERSION_FILE")
else
    NP2MD_VERSION="1.10.0"
    echo "版本文件不存在, 使用默认版本 $NP2MD_VERSION"
fi

# 下载 ngapost2md 到指定目录
# 参数: $1=目标目录, $2=平台(linux/windows), $3=架构(amd64/arm64)
fetch_ngapost2md() {
    local target_dir=$1
    local platform=$2
    local arch=$3
    
    local ext="tar.gz"
    local bin_name="ngapost2md"
    if [ "$platform" = "windows" ]; then
        ext="zip"
        bin_name="ngapost2md.exe"
    fi
    
    local bin_url="https://github.com/ludoux/ngapost2md/releases/download/${NP2MD_VERSION}/ngapost2md-NEO_${NP2MD_VERSION}-${platform}-${arch}.${ext}"
    local tmp_file="ngapost2md_tmp.${ext}"
    
    echo "正在下载 ngapost2md ${NP2MD_VERSION} (${platform}-${arch})..."
    
    if ! wget -q -O "$tmp_file" "$bin_url"; then
        echo "警告: 下载 ngapost2md 失败: $bin_url"
        rm -f "$tmp_file"
        return 1
    fi
    
    mkdir -p "$target_dir"
    
    if [ "$ext" = "tar.gz" ]; then
        tar -zxf "$tmp_file" -C "$target_dir"
    else
        unzip -q -o "$tmp_file" -d "$target_dir"
    fi
    
    rm -f "$tmp_file"
    echo "ngapost2md ${NP2MD_VERSION} 下载完成"
}

cd artifacts

for dir in */; do
    dir_name="${dir%/}"
    # 从 build-artifacts-os-arch 提取 os 和 arch, 并去掉 -latest
    os_arch="${dir_name#build-artifacts-}"
    os_arch="${os_arch//-latest/}"
    
    # 解析平台和架构
    if [[ "$os_arch" == *"windows"* ]]; then
        platform="windows"
    else
        platform="linux"
    fi
    if [[ "$os_arch" == *"arm64"* ]]; then
        arch="arm64"
    else
        arch="amd64"
    fi
    
    echo "正在打包 $dir_name -> ngamm-${VERSION}-${os_arch}.zip"
    
    # 重命名可执行文件
    if [ "$platform" = "windows" ]; then
        mv "$dir"/ngamm-*.exe "$dir/ngamm.exe" 2>/dev/null || true
    else
        mv "$dir"/ngamm-* "$dir/ngamm" 2>/dev/null || true
    fi
    
    dir_data="$dir/data"
    mkdir -p "$dir_data"
    
    # 下载对应平台的 ngapost2md
    fetch_ngapost2md "$dir_data" "$platform" "$arch"
    
    # 复制附件配置文件
    [ ! -f "$dir_data/attachment.ini" ] && cp "$ROOT_DIR/assets/attachment.ini" "$dir_data/"

    # 复制 LICENSE
    cp "$ROOT_DIR/LICENSE" "$dir"
    
    # 打包 (不包含外层文件夹)
    cd "$dir"
    zip -r "../../ngamm-${VERSION}-${os_arch}.zip" .
    cd ..
done

cd ..
echo "打包完成:"
ls -la *.zip
