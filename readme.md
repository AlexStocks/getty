# getty #
---
*a netty like asynchronous network I/O library*

## license ##
---
Apache License 2.0

## getty examples ##
---
*getty examples是基于getty的实现的代码示例*

> getty-examples借鉴java的编译思路，提供了区别于一般的go程序的而类似于java的独特的编译脚本系统。

### getty example1: tcp-echo ###
---

This example shows a simple tcp client and server.

The server echoes messages sent to it. The client sends messages to the echo server
and prints all messages received.

To run the example, start the server:

    $ cd tcp-echo/server/ && sh assembly/linux/test.sh && cd target/linux/echo_server-0.3.07-20161009-1632-test/ && sh bin/load.sh start

Next, start the client:

    $ cd tcp-echo/client/ && sh assembly/linux/test.sh && cd target/linux/echo_client-0.3.07-20161009-1634-test/ && sh bin/load.sh start

