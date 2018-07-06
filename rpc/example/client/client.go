package main

import (
	"time"
)

import (
	"github.com/AlexStocks/getty/rpc"
	"github.com/AlexStocks/getty/rpc/example/data"
	log "github.com/AlexStocks/log4go"
)

func main() {
	log.LoadConfiguration("client_log.xml")
	client := rpc.NewClient("client_config.toml")
	// client.SetCodecType(rpc.ProtoBuffer)//默认是json序列化
	defer client.Close()

	for i := 0; i < 100; i++ {
		go func() {
			var res string
			err := client.Call("TestRpc", "Test", data.TestABC{"aaa", "bbb", "ccc"}, &res)
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
			err := client.Call("TestRpc", "Add", 1, &result)
			if err != nil {
				log.Error(err)
				return
			}
			log.Info(result)
		}()
	}

	var errInt int
	err := client.Call("TestRpc", "Err", 2, &errInt)
	if err != nil {
		log.Error(err)
	}

	time.Sleep(20 * time.Second)
}
