#!/bin/bash

tmp=ngapost2md.bin
app=ngapost2md
mkdir -p $app
cd $app
wget -q -O $tmp https://github.com/ludoux/ngapost2md/releases/download/1.6.0/ngapost2md-NEO_1.6.0-linux-amd64.tar.gz
tar -zxvf $tmp $app
mv $app main
rm -f $tmp