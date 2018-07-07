package main

import (
	"github.com/AlexStocks/getty/rpc"
	"github.com/AlexStocks/getty/rpc/example/data"
	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
)

func main() {
	log.LoadConfiguration("/Users/alex/test/golang/lib/src/github.com/AlexStocks/getty/rpc/example/server/server_log.xml")
	srv, err := rpc.NewServer("/Users/alex/test/golang/lib/src/github.com/AlexStocks/getty/rpc/example/server/server_config.toml")
	if err != nil {
		panic(jerrors.ErrorStack(err))
	}
	err = srv.Register(new(data.TestRpc))
	srv.Run()
}
