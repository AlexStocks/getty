# getty #
---
 *a netty like asynchronous network I/O library*

## introdction ##
---
> DESC       : a asynchronous network I/O library in golang
  LICENCE    : Apache License 2.0

## develop history ##
---
- 2016/08/29
	> 1 rename reconnect to errFlag in function session.go:(Session)handlePackage
	>
   	> 2 version: 0.2.07
    
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
