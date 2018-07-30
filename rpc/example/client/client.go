package main

import (
	"time"
)

import (
	"github.com/AlexStocks/getty/rpc"
	"github.com/AlexStocks/getty/rpc/example/data"
	log "github.com/AlexStocks/log4go"
	jerrors "github.com/juju/errors"
)

func main() {
	log.LoadConfiguration("client_log.xml")
	client, err := rpc.NewClient("client_config.toml")
	if err != nil {
		panic(jerrors.ErrorStack(err))
	}
	// client.SetCodecType(rpc.ProtoBuffer)//默认是json序列化
	defer client.Close()

	for i := 0; i < 100; i++ {
		go func() {
			var res string
			err := client.Call("127.0.0.1:20000", "json", "TestRpc", "Test", data.TestABC{"aaa", "bbb", "ccc"}, &res)
			if err != nil {
				log.Error(err)
				return
			}
			log.Info(res)
		}()
	}

	for i := 0; i < 100; i++ {
		go func() {
			var result int
			err := client.Call("127.0.0.1:20000", "json", "TestRpc", "Add", 1, &result)
			if err != nil {
				log.Error(err)
				return
			}
			log.Info(result)
		}()
	}

	var errInt int
	err = client.Call("127.0.0.1:20000", "json", "TestRpc", "Err", 2, &errInt)
	if err != nil {
		log.Error(jerrors.ErrorStack(err))
	}

	time.Sleep(20 * time.Second)
}
