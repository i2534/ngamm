#!/bin/bash

# check in docker
# docker run --rm -v $(pwd)/entrypoint.sh:/script.sh alpine:latest sh -n /script.sh

# 设置工作目录
setup_directories() {
    local input_dir=$1
    if [ -z "$input_dir" ]; then
        echo "使用默认工作目录 /app"
        dir_work=/app
    else
        echo "使用指定工作目录 $input_dir"
        dir_work=$input_dir
    fi
    dir_data=$dir_work/data
    dir_np2md=$dir_work/np2md
    dir_baidupcs=$dir_work/baidupcs
    dir_pan=$dir_data/pan
}

# 获取ngapost2md二进制文件
fetch_ngapost2md() {
    local bin_url="https://github.com/ludoux/ngapost2md/releases/download/1.7.1/ngapost2md-NEO_1.7.1-linux-amd64.tar.gz"
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

fetch_baidupcs() {
    # read tag_name from https://api.github.com/repos/qjfoidnh/BaiduPCS-Go/releases/latest
    local api_url="https://api.github.com/repos/qjfoidnh/BaiduPCS-Go/releases/latest"
    local tag_name=$(curl -s "$api_url" | sed -n 's/.*"tag_name": "\([^"]*\)".*/\1/p')
    if [ -z "$tag_name" ]; then
        tag_name="v3.9.7"
    fi
    local url="https://github.com/qjfoidnh/BaiduPCS-Go/releases/download/$tag_name/BaiduPCS-Go-$tag_name-linux-amd64.zip"
    local program="BaiduPCS-Go"
    local old=$(pwd)

    echo "正在从 $url 获取 $program ..."
    local tmp=baidupcs.zip
    local dir=$dir_baidupcs
    mkdir -p $dir && cd $dir
    wget -q -O $tmp $url
    echo "正在解压 $program ..."
    # 提取特定文件，-j 忽略目录结构
    unzip -j -q $tmp "*/$program" -d .
    # 如果没找到，可能在根目录，尝试直接提取
    if [ ! -f "$program" ]; then
        unzip -q $tmp "$program" -d .
    fi
    rm -f $tmp
    chmod +x "$program"
    echo "获取 $program 完成。"

    cd "$old"
}

# 启动应用
start_application() {
    echo "正在启动应用..."

    mkdir -p "$dir_data"
    cp -rn "$dir_np2md/"* "$dir_data/"

    cd "$dir_work"
    chmod +x ngamm

    export GIN_MODE=release
    local CMD="./ngamm"
    if [ -n "${TOKEN:-}" ]; then
        CMD="$CMD -t ${TOKEN}"
    fi
    CMD="$CMD -p 5842 -m $dir_data/ngapost2md"
    if [ "$NET_PAN" = "true" ]; then
        mkdir -p "$dir_pan"
        cp -rn "./pan-config.ini" "$dir_pan/config.ini"

        local dpb="$dir_pan/baidu"
        mkdir -p "$dpb"
        cp -rn "$dir_baidupcs/"* "$dpb/"

        local dpq="$dir_pan/quark"
        mkdir -p "$dpq"

        CMD="$CMD -n $dir_pan"
    fi
    eval "$CMD"
}

# 主函数
main() {
    local cmd=$1
    local dir=$2
    
    setup_directories "$dir"
    
    case "$cmd" in
        fetch)
            fetch_ngapost2md
            ;;
        start)
            start_application
            ;;
        baidupcs)
            fetch_baidupcs
            ;;
        prepare)
            fetch_ngapost2md
            if [ "$NET_PAN" = "true" ]; then
                fetch_baidupcs
            fi
            ;;
        *)
            echo "可用命令: start, prepare"
            exit 1
            ;;
    esac
}

# 执行主函数
main "$1" "$2"