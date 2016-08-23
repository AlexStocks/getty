# getty #
---
 *a netty like asynchronous network I/O library*

## introdction ##
---
> DESC       : a asynchronous network I/O library in golang
  LICENCE    : Apache License 2.0
  AUTHOR     : https://github.com/sanbit
  MAINTAINER : Alex Stocks
  EMAIL      : alexstocks@foxmail.com

## develop history ##
---

- 2016/08/22
	> rename (Session)OnIdle to (Session)OnCron
	  rewrite server.go: add Server{done, wg}
	  add utils.go
	  version: 0.2.03

- 2016/08/21
	> add name for Session
	  add OnError for Codec

- 2016/08/18
	> delete last clause of handleRead
	  add reqQ handle case in last clause of handleLoop
	  add conditon check in (*Session)RunEventLoop()
	  version: 0.2.02

- 2016/08/16
	> rename all structs
	  add getty connection
	  rewrite (Session)handleRead & (Session)handleEventLoop
	  version: 0.2.01
