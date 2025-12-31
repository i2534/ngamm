#!/bin/bash

# 打包脚本
# 用法: ./package.sh <VERSION>

VERSION=$1
if [ -z "$VERSION" ]; then
    echo "用法: $0 <VERSION>"
    exit 1
fi

ROOT_DIR=$(dirname "$0")

# 获取ngapost2md二进制文件
fetch_ngapost2md() {
    local bin_url="https://github.com/ludoux/ngapost2md/releases/download/1.8.2/ngapost2md-NEO_1.8.2-linux-amd64.tar.gz"
    echo "正在从 $bin_url 获取 ngapost2md ..."
    local tmp=ngapost2md.bin
    local dir=$dir_np2md
    local old=$(pwd)

    mkdir -p $dir && cd $dir
    wget -q -O $tmp $bin_url
    tar -zxf $tmp -C .
    rm -f win_*
    rm -f $tmp
    echo "获取 ngapost2md 完成。"

    cd "$old"
}

cd artifacts

for dir in */; do
    dir_name="${dir%/}"
    # 从 build-artifacts-os-arch 提取 os 和 arch, 并去掉 -latest
    os_arch="${dir_name#build-artifacts-}"
    os_arch="${os_arch//-latest/}"
    
    echo "正在打包 $dir_name -> ngamm-${VERSION}-${os_arch}.zip"
    
    dir_data="$dir/data"
    mkdir -p "$dir_data"
    # 复制附件配置文件
    cp -n "$ROOT_DIR/assets/attachment.ini" "$dir_data"

    # 复制 LICENSE
    cp "$ROOT_DIR/LICENSE" "$dir"
    
    # 打包
    zip -r "../ngamm-${VERSION}-${os_arch}.zip" "$dir"
done

cd ..
echo "打包完成:"
ls -la *.zip
