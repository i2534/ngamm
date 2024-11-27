package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/i2534/ngamm/mgr"
	"github.com/jessevdk/go-flags"
)

type Option struct {
	Port    int    `short:"p" long:"port" description:"端口号" default:"5842"`
	Program string `short:"m" long:"program" description:"ngapost2md 程序路径" default:"ngapost2md/main"`
}

func main() {
	var opts Option
	parser := flags.NewParser(&opts, flags.Default & ^flags.HelpFlag)
	args, err := parser.Parse()
	if err != nil {
		log.Fatalln("参数解析出现问题:", err.Error())
	}
	println(args)

	wd, err := os.Getwd()
	if err != nil {
		log.Fatalln("获取当前工作目录出现问题:", err.Error())
	}

	program := filepath.Join(wd, opts.Program)
	if _, err := os.Stat(program); os.IsNotExist(err) {
		log.Fatalln("ngapost2md 程序文件不存在:", program)
	}

	client, err := mgr.InitNGA(program)
	if err != nil {
		log.Fatalln("初始化 NGA 客户端出现问题:", err.Error())
	}

	addr := fmt.Sprintf(":%d", opts.Port)
	srv, err := mgr.NewServer(&mgr.Config{Addr: addr}, client)
	if err != nil {
		log.Fatalln("初始化服务器出现问题:", err.Error())
	}
	srv.Run()
}
