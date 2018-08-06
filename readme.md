# getty examples ##
---
*[getty](https://github.com/alexstocks/getty) code examples*

> getty-examples alse shows a java like compile package.

## license ##
---
Apache License 2.0


## getty example1: tcp-echo ##
---

This example shows a simple tcp client and server.

The server sends back messages from client. The client sends messages to the echo server and prints all messages received.

To run the example, start the server:

    $ cd echo/tcp-echo/server/ && sh assembly/linux/test.sh && cd target/linux/echo_server-0.3.07-20161009-1632-test/ && sh bin/load.sh start

Next, start the client:

    $ cd echo/tcp-echo/client/ && sh assembly/linux/test.sh && cd target/linux/echo_client-0.3.07-20161009-1634-test/ && sh bin/load.sh start

## getty example2: ws-echo ##
---

This example shows a simple websocket client(go client & javascript client) and server.

The server sends back messages from client. The client sends messages to the echo server and prints all messages received.

To run the example, start the server:

    $ cd echo/ws-echo/server/ && sh assembly/linux/test.sh && cd target/linux/echo_server-0.3.07-20161009-1632-test/ && sh bin/load.sh start

Next, start the go client:

    $ cd echo/ws-echo/client/ && sh assembly/linux/test.sh && cd target/linux/echo_client-0.3.07-20161009-1634-test/ && sh bin/load.sh start

Or start the js client:

    $ cd echo/ws-echo/js-client/ && open index.html in a internet browser(like chrome or ie or firefox etc).


## getty example3: rpc ##
---

This example shows a simple rpc client and server.

The server sends back rpc response to client. The client sends rpc requests to the rpc server and prints all messages received.

To run the example on linux, start the server:

    $ cd rpc/server/ && sh assembly/linux/test.sh && cd target/linux/rpc_server-0.9.2-20180806-1559-test/ && sh bin/load.sh start

Next, start the go client:

    $ cd rpc/client/ && sh assembly/linux/test.sh && cd target/linux/rpc_client-0.9.2-20180806-1559-test/ && sh bin/load.sh start

What's more, if you run this example on mac, the server compile command should be:

    $ cd rpc/server/ && sh assembly/mac/test.sh && cd target/darwin/rpc_server-0.9.2-20180806-1559-test/ && sh bin/load.sh start
    $ cd rpc/client/ && sh assembly/mac/test.sh && cd target/darwin/rpc_client-0.9.2-20180806-1559-test/ && sh bin/load.sh start
