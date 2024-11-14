package main

import (
	"fmt"
	"log"

	"github.com/i2534/ngamm/mgr"
	"github.com/jessevdk/go-flags"
)

type Option struct {
	Port int `short:"p" long:"port" description:"端口号" default:"5842"`
}

func main() {
	var opts Option
	parser := flags.NewParser(&opts, flags.Default & ^flags.HelpFlag)
	args, err := parser.Parse()
	if err != nil {
		log.Fatalln("参数解析出现问题:", err.Error())
	}
	println(args)
	// if len(args) == 0 {
	// 	log.Fatalln("请提供tid")
	// }

	client, err := mgr.InitNGA()
	if err != nil {
		log.Fatalln("初始化NGA客户端出现问题:", err.Error())
	}

	addr := fmt.Sprintf(":%d", opts.Port)
	srv, err := mgr.NewServer(&mgr.Config{Addr: addr}, client)
	if err != nil {
		log.Fatalln("初始化服务器出现问题:", err.Error())
	}
	srv.Run()
}
