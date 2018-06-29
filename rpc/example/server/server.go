package main

import (
	"github.com/AlexStocks/getty/rpc"
	"github.com/AlexStocks/getty/rpc/example/data"
)

func main() {
	srv := rpc.NewServer()
	srv.Register(new(data.TestRpc))
	srv.Run()
}
