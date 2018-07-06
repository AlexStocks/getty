package main

import (
	"github.com/AlexStocks/getty/rpc"
	"github.com/AlexStocks/getty/rpc/example/data"
	log "github.com/AlexStocks/log4go"
)

func main() {
	log.LoadConfiguration("server_log.xml")
	srv := rpc.NewServer("server_config.toml")
	srv.Register(new(data.TestRpc))
	srv.Run()
}
