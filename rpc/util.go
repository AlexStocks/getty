package rpc

import (
	"net/http"

	"github.com/AlexStocks/goext/net"
	log "github.com/AlexStocks/log4go"
)

const (
	pprofPath = "/debug/pprof/"
)

func initProfiling() {
	var (
		addr string
	)

	// addr = *host + ":" + "10000"
	addr = gxnet.HostAddress(conf.Host, conf.ProfilePort)
	log.Info("App Profiling startup on address{%v}", addr+pprofPath)
	go func() {
		log.Info(http.ListenAndServe(addr, nil))
	}()
}
