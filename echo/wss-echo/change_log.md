# wss-echo #
---
*getty secure websocket code examples of Echo Example*

## LICENSE ##
---

> LICENCE    : Apache License 2.0

## develop history ##
---

- 2018/03/17
    > improvement
    * use getty 0.8.2

- 2018/03/10
    > improvement
    * using getty 0.8.1

- 2017/04/27
    > improvement
    * enable wss client just using cert file；
    * js-client can connect wss server by allow-insecure-localhost (chrome://flags/#allow-insecure-localhost)

- 2016/11/19
    > 1 add client/app/config.go:GettySessionParam{CompressEncoding} to test compress websocket compression extension.
    >
    > 2 add server/app/config.go:GettySessionParam{CompressEncoding} to test compress websocket compression extension.
	>
	> 3 Pls attention that ie does not support weboscket compression extension while the latest chrome support it while echo/ws-echo/server enable websocket compress function. I have not test firefox and edge.
	>   As of version 19, Chrome will apparently compress WebSocket traffic automatically when the server supports it.
