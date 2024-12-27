#!/bin/bash

cmd=$1

dir_work=/app
dir_np2md=$dir_work/np2md
dir_data=$dir_work/data
if [ "$cmd" = "fetch" ]; then
    echo "Fetch ngapost2md ..."
    tmp=ngapost2md.bin
    dir=$dir_np2md
    mkdir -p $dir && cd $dir
    wget -q -O $tmp https://github.com/ludoux/ngapost2md/releases/download/1.6.0/ngapost2md-NEO_1.6.0-linux-amd64.tar.gz
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
    ./main -t "$TOKEN" -p 5842 -m data/ngapost2md
fi