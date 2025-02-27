#!/bin/bash

cmd=$1
dir=$2
if [ -z "$dir" ]; then
    dir_work=/app
else
    dir_work=$dir
fi
dir_np2md=$dir_work/np2md
dir_data=$dir_work/data
bin_url=https://github.com/ludoux/ngapost2md/releases/download/1.7.1/ngapost2md-NEO_1.7.1-linux-amd64.tar.gz
if [ "$cmd" = "fetch" ]; then
    echo "Fetch ngapost2md from $bin_url ..."
    tmp=ngapost2md.bin
    dir=$dir_np2md
    mkdir -p $dir && cd $dir
    wget -q -O $tmp $bin_url
    tar -zxf $tmp -C .
    rm -f win_*
    rm -f $tmp
    echo "Fetch ngapost2md done."
elif [ "$cmd" = "start" ]; then
    echo "Starting ..."

    mkdir -p $dir_data
    cp -rn $dir_np2md/* $dir_data/
    cd $dir_work
    chmod +x main
    export GIN_MODE=release
    if [ -z "$TOKEN" ]; then
        export TOKEN=""
    fi
    ./main -t "$TOKEN" -p 5842 -m data/ngapost2md
fi