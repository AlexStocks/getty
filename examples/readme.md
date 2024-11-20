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

```bash
$ cd echo/tcp-echo/server/ && sh assembly/linux/test.sh
// your_version is the version set in version.go, current-time is the time when you compile the code.
// please replace your_version and current-time with the real version and time.
$ cd target/linux/echo_server-${your-version}-${current-time}-test/ && sh bin/load_echo_server.sh start
```

Next, start the client:

```bash
$ cd echo/tcp-echo/client/ && sh assembly/linux/test.sh
// your_version is the version set in version.go, current-time is the time when you compile the code.
// please replace your_version and current-time with the real version and time.
$ cd target/linux/echo_client-${your-version}-${current-time}-test/ && sh bin/load_echo_client.sh start
```

## getty example2: ws-echo ##
---

This example shows a simple websocket client(go client & javascript client) and server.

The server sends back messages from client. The client sends messages to the echo server and prints all messages received.

To run the example, start the server:

```bash
$ cd echo/ws-echo/server/ && sh assembly/linux/test.sh
// your_version is the version set in version.go, current-time is the time when you compile the code.
// please replace your_version and current-time with the real version and time.
$ cd target/linux/echo_server-${your-version}-${current-time}-test/ && sh bin/load_echo_server.sh start

```

Next, start the go client:

```bash
$ cd echo/ws-echo/client/ && sh assembly/linux/test.sh
// your_version is the version set in version.go, current-time is the time when you compile the code.
// please replace your_version and current-time with the real version and time.
$ cd target/linux/echo_client-${your-version}-${current-time}-test/ && sh bin/load_echo_client.sh start
```

Or start the js client:

```bash
$ cd echo/ws-echo/js-client/ && open index.html in a internet browser(like chrome or ie or firefox etc).
```


## getty example3: rpc ##
---

This example shows how to build rpc client and server.

The server sends back rpc response to client. The client sends rpc requests to the rpc server and prints all messages received.

To run the example on linux, start the server:

```bash
$ cd rpc/server/ && sh assembly/linux/test.sh
// your_version is the version set in version.go, current-time is the time when you compile the code.
// please replace your_version and current-time with the real version and time.
$ cd target/linux/rpc_server-${your-version}-${current-time}-test/ && sh bin/load_rpc_server.sh start
```

Next, start the go client:

```bash
$ cd rpc/client/ && sh assembly/linux/test.sh
// your_version is the version set in version.go, current-time is the time when you compile the code.
// please replace your_version and current-time with the real version and time.
$ cd target/linux/rpc_client-${your-version}-${current-time}-test/ && sh bin/load_rpc_client.sh start
```

What's more, if you run this example on mac, the server compile command should be:

```bash
$ cd rpc/server/ && sh assembly/mac/test.sh
// your_version is the version set in version.go, current-time is the time when you compile the code.
// please replace your_version and current-time with the real version and time.
$ cd target/darwin/rpc_server-${your-version}-${current-time}-test/ && sh bin/load_rpc_server.sh start

$ cd rpc/client/ && sh assembly/mac/test.sh
// your_version is the version set in version.go, current-time is the time when you compile the code.
// please replace your_version and current-time with the real version and time.
$ cd target/darwin/rpc_client-${your-version}-${current-time}-test/ && sh bin/load_rpc_client.sh start
```

## getty example4: micro ##
---

This example shows how to build micro client and server to do service registration and service discovery based on rpc.

To run the example on linux, start the server:

```bash
$ cd micro/server/ && sh assembly/linux/test.sh
// your_version is the version set in version.go, current-time is the time when you compile the code.
// please replace your_version and current-time with the real version and time.
$ cd target/linux/micro_server-${your-version}-${current-time}-test/ && sh bin/load_micro_server.sh start
```

Next, start the go client:

```bash
$ cd micro/client/ && sh assembly/linux/test.sh
// your_version is the version set in version.go, current-time is the time when you compile the code.
// please replace your_version and current-time with the real version and time.
$ cd target/linux/micro_client-${your-version}-${current-time}-test/ && sh bin/load_micro_client.sh start
```

What's more, if you run this example on mac, the server compile command should be:

```bash
$ cd micro/server/ && sh assembly/mac/test.sh
// your_version is the version set in version.go, current-time is the time when you compile the code.
// please replace your_version and current-time with the real version and time.
$ cd target/darwin/micro_server-${your-version}-${current-time}-test/ && sh bin/load_micro_server.sh start

$ cd micro/client/ && sh assembly/mac/test.sh
// your_version is the version set in version.go, current-time is the time when you compile the code.
// please replace your_version and current-time with the real version and time.
$ cd target/darwin/micro_client-${your-version}-${current-time}-test/ && sh bin/load_micro_client.sh start
```
