# getty #
---
 *a netty like asynchronous network I/O library*

## introdction ##
---
> DESC       : a asynchronous network I/O library in golang. In getty there are two goroutines in one connection(session), one handle network read buffer tcp stream, the other handle logic process and write response into network write buffer. If your logic process may take a long time, you should start a new logic process goroutine by yourself in (Codec):OnMessage. Getty is based on "ngo" whose author is sanbit(https://github.com/sanbit).
>
> LICENCE    : Apache License 2.0

## develop history ##
---

- 2016/11/16
    > 1 add zip/snappy compress for tcp/ws connection
    >
    > 2 version: 0.6.01
 
- 2016/11/02
    > 1 add session.go:Session{ID(), LocalAddr(), RemoteAddr()}
    >
    > 2 add conn.go:iConn{id(), localAddr(), remoteAddr()}
    >
    > 3 version: 0.4.08

- 2016/11/01
    > 1 session.go:Session{maxPkgLen(int)} -> Session{maxPkgLen(int32)}
    >
    > 2 add remarks in session.go
    >
    > 2 version: 0.4.07(0.4.06 is a obsolete version)

- 2016/10/21
    > 1 session.go:(Session)RunEventLoop -> session.go:(Session)run
    >
    > 2 version: 0.4.05

- 2016/10/14
    > 1 add conn.go:(gettyWSConn)handlePing
    >
    > 2 add conn.go:(gettyWSConn)handlePong
    >
    > 3 set read/write timeout in session.go:(Session)stop to let read/write timeout asap
    >
    > 4 fix bug: websocket block on session.go:(Session)handleWSPkg when got error. set read/write timeout asap to solve this problem.
    >
    > 5 version: 0.4.04

- 2016/10/13
    > 1 add conn.go:(gettyWSConn)writePing which is invoked automatically in session.go:(Session)handleLoop
    >
    > 2 modify session.go:(Session)handleLoop:Websocket session will send ping frame automatically every peroid.
    >
    > 3 add conn.go:(gettyConn)UpdateActive, which can used as (Session)UpdateActive which is invoked by Session automatically.
    >
    > 4 add conn.go:(gettyConn)GetActive, which can used as (Session)GetActive
    >
    > 5 modify conn.go:gettyWSConn{websocket.Conn} ->  conn.go:gettyWSConn{*websocket.Conn}
    >
    > 6 version: 0.4.03

- 2016/10/11
    > 1 fix bug: use websocket.BinaryMessage in conn.go:(gettyWSConn)write
    >
    > 2 version: 0.4.02

- 2016/10/10
    > 1 delete session.go:Session{errFlag} to invoke codec.go:EventListener{OnClose&OnError} both when got a error
    >
    > 2 modify session.go:(Session)SetReader
    >
    > 3 modify session.go:(Session)SetWriter
    >
    > 4 add modify session.go:(Session)maxMsgLen for websocket session
    >
    > 5 version: 0.4.01

- 2016/10/09
    > 1 add client.go:NewWSSClient
    >
    > 2 add server.go:RunWSEventLoopWithTLS
    >
    > 3 add session.go:(Session*)gettyConn

- 2016/10/08
    > 1 add websocket connection & client & server
    >
    > 3 version: 0.4.0

- 2016/10/01
    > 1 remark SetReadDeadline & SetWriteDeadline in session.go (ref: https://github.com/golang/go/issues/15133)
    >
    > 3 version: 0.3.14

- 2016/09/30
    > 1 modify wheel time interval from 1 second to 100ms.
    >
    > 2 modify session.go:(Session)WritePkg timeout
    >
    > 3 version: 0.3.13

- 2016/09/27
    > 1 fix bug: getty panic when conn.RemoteAddr() is nil in session.go:(Session)sessionToken()
    >
    > 2 version: 0.3.12

- 2016/09/26
    > 1 move utils.go's function to github.com/AlexStocks/goext and delete it
    >
    > 2 use goext/time Wheel
    >
    > 3 version: 0.3.11

- 2016/09/20
    > 1 just invoke OnError when session got error
    >
    > 2 version: 0.3.10

- 2016/09/19
    > 1 move empty to from client.go to session.go
    >
    > 2 version: 0.3.09

- 2016/09/12
    > 1 add defeat self connection logic in client.go & server.go
    >
    > 2 version: 0.3.08

- 2016/09/09
    > 1 delete session.go:(Session)readerDone
    >
    > 2 delete session.go:(Session)handlePackage Last clause
    >
    > 3 set write timeout in session.go:(Session)WritePkg
    >
    > 4 version: 0.3.07

- 2016/09/08
    > 1 rewrite session.go:(Session)handlePackage() error handle logic
    >
    > 2 add utils.go:CountWatch
    >
    > 3 version: 0.3.06

- 2016/09/07
    > 1 session.go:(Session)Close() -> session.go:(Session)gc() to be invoked by session.go:(Session)handleLoop
    >
    > 2 add panic stack message for session.go:(Session)handleLoop & session.go:(Session)handlePackage
    >
    > 3 version: 0.3.05

- 2016/09/06
    > 1 codec.go:(Reader)Read(*Session, []byte) (interface{}, error)  -> codec.go:(Reader)Read(*Session, []byte) (interface{}, int, error)
    >
    > 2 codec.go:(EventListener)OnOpen(*Session) -> codec.go:(EventListener)OnOpen(*Session) error
    >
    > 3 version: 0.3.04

- 2016/09/05
    > 1 add 'errFlag = true' when got err in pkgHandler.Read loop clause in session.go:(Session)handlePackage
    >
    > 2 use '[]byte' instead of bytes.Buffer in codec.go:(Reader)Read
    >
    > 3 version: 0.3.03

- 2016/09/04
    > 1 add server.go:(Server)Listen
    >
    > 2 version: 0.3.02

- 2016/09/03
    > 1 modify return value of session.go:(Session)Close from void to error
    >
    > 2 add clause "this.attrs = nil" in session.go:(Session)Close
    >
    > 3 session.go:Session{*gettyConn, readDeadline, writeDeadline} -> session.go:Session{gettyConn, rDeadline, wDeadline}
    >
    > 4 add session.go:(Session)Reset
    >
    > 5 add session.go:(Session)SetConn
    >
    > 6 add elastic sleep time machanism in client.go:(Client)RunEventLoop
    >
    > 7 version: 0.3.01

- 2016/09/02
    > 1 add session.go:(gettyConn)close and session.go:(Session)dispose
    >
    > 2 modify return value of server.go:NewSessionCallback from void to err
    >
    > 3 add client.go:Client
    >
    > 4 version: 0.3.00

- 2016/08/29
    > 1 rename reconnect to errFlag in function session.go:(Session)handlePackage
    >
    > 2 session.go:(gettyConn)readCount is reconsidered as read in tcp stream bytes
    >
    > 3 session.go:(gettyConn)writeCount is reconsidered as write out tcp stream bytes
    >
    > 4 reconstruct session output token string session.go:(Session)sessionToken
    >
    > 5 use err instead of nerr in session.go:(Session)handlePackage:defer:OnError
    >
       > 6 version: 0.2.07

- 2016/08/25
    > 1 move close done to once clause in server.go:(Server)stop
    >
    > 2 rename reqQ to rQ which means read queue and its relative params
    >
    > 3 rename rspQ to wQ which means write queue and its relative params
    >
    > 4 rename reqPkg to inPkg in function session.go:(Session)handleLoop
    >
    > 5 rename rspPkg to outPkg in function session.go:(Session)handleLoop
    >
    > 6 version: 0.2.06

- 2016/08/24
    > 1 delete session.go:Session:wg(atomic.WaitGroup). Add session.go:Session:grNum instead to prevent from  (Session)Close() block on session.go:Session:wg.Wait()
    >
    > 2 add once for session.go:Session:done(chan struct{})
    >
    > 3 version: 0.2.05

- 2016/08/23
    > 1 do not consider empty package as a error in (Session)handlePackage
    >
    > 2 version: 0.2.04

- 2016/08/22
    > 1 rename (Session)OnIdle to (Session)OnCron
    >
    > 2 rewrite server.go: add Server{done, wg}
    >
    > 3 add utils.go
    >
    > 4 version: 0.2.03

- 2016/08/21
    > 1 add name for Session
    >
    > 2 add OnError for Codec

- 2016/08/18
    > 1 delete last clause of handleRead
    >
    > 2 add reqQ handle case in last clause of handleLoop
    >
    > 3 add conditon check in (*Session)RunEventLoop()
    >
    > 4 version: 0.2.02

- 2016/08/16
    > 1 rename all structs
    >
    > 2 add getty connection
    >
    > 3 rewrite (Session)handleRead & (Session)handleEventLoop
    >
    > 4 version: 0.2.01
