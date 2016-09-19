# getty #
---
 *a netty like asynchronous network I/O library*

## introdction ##
---
> DESC       : a asynchronous network I/O library in golang. In getty there are two goroutines in one connection(session), one handle network read buffer tcp stream, the other handle logic process and write response into network write buffer. If your logic process may take a long time, you should start a new logic process goroutine by yourself in (Codec):OnMessage. Getty is based on "ngo" whose author is sanbit(https://github.com/sanbit).
>
> LICENCE    : Apache License 2.0

