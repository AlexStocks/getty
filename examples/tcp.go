package main

import (
	"sync"
	"github.com/AlexStocks/getty"
	"sync/atomic"
	"github.com/labstack/gommon/log"
	"time"
)

func main() {
	tcp()
}

func tcp() {

	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		defer func() {
			wg.Done()
		}()
		sessionHandler := EchoSessionHandler{}
		eventListener := newEchoMessageHandler()
		rw := EchoPackageHandler{}

		server := getty.NewTCPServer(getty.WithLocalAddress(":8089"), getty.WithServerEventListener(eventListener),
			getty.WithServerPkgReader(&rw), getty.WithServerPkgWriter(&rw), getty.WithServerSessionHander(&sessionHandler))

		server.RunEventLoop()
	}()

	go func() {
		defer func() {
			wg.Done()
		}()
		sessionHandler := EchoSessionHandler{}
		eventListener := newEchoMessageHandler()
		rw := EchoPackageHandler{}

		client := getty.NewTCPClient(getty.WithServerAddress("localhost:8089"), getty.WithClientEventListener(eventListener),
			getty.WithClientPkgReader(&rw), getty.WithClientPkgWriter(&rw), getty.WithClientSessionHander(&sessionHandler),
			getty.WithConnectionNumber(10))

		client.RunEventLoop()

		var reqID uint32 = 1
		for i := 0; i < 10; i++ {
			var pkg EchoPackage
			pkg.H.Magic = echoPkgMagic
			pkg.H.LogID = (uint32)(100)
			pkg.H.Sequence = atomic.AddUint32(&reqID, 1)
			// pkg.H.ServiceID = 0
			pkg.H.Command = echoCmd
			pkg.B = "11111"
			pkg.H.Len = (uint16)(len(pkg.B)) + 1

			if session := client.SelectSession(); session != nil {
				err := session.WritePkg(&pkg, WritePkgTimeout)
				if err != nil {
					log.Warn("session.WritePkg(session{%s}, pkg{%s}) = error{%v}", session.Stat(), pkg, err)
					session.Close()
					client.RemoveSession(session)
				}
			}

			time.Sleep(1 * time.Second)
		}

	}()

	wg.Wait()

}
