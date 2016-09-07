# getty #
---
*a netty like asynchronous network I/O library*

## license ##
---
Apache License 2.0   

## getty examples ##
---
*getty examples是基于getty的实现的代码示例*

> dubbogo-examples借鉴java的编译思路，提供了区别于一般的go程序的而类似于java的独特的编译脚本系统。

### dubogo example1: echo ###
---
*这个程序是为了给出代码示例以及执行压力测试*

> 1 部署zookeeper服务；
>
> 2 请部署 https://github.com/QianmiOpen/dubbo-rpc-jsonrpc 服务端，如果你不想编译，可以使用我编译好的 dubbogo-examples/user-info/java-server/dubbo_jsonrpc_example.bz2，注意修改zk地址；
>
> 3 修改dubbogo-examples/user-info/client/profiles/test/client.toml:line 33，写入正确的zk地址；
>
> 4 dubbogo-examples/user-info/client/下执行 sh assembly/windows/test.sh命令(linux下请执行sh assembly/linux/test.sh)，然后target/windows下即放置好了编译好的程序以及打包结果，在dubbogo-examples\user-info\client\target\windows\user_info_client-0.1.0-20160818-1346-test下执行sh bin/load.sh start命令即可客户端程序；
>
> 5 修改dubbogo-examples/user-info/server/profiles/test/server.toml:line 21，写入正确的zk地址；
>
> 6 dubbogo-examples/user-info/server/下执行 sh assembly/windows/test.sh命令(linux下请执行sh assembly/linux/test.sh)，然后target/windows下即放置好了编译好的程序以及打包结果，在dubbogo-examples\user-info\server\target\windows\user_info_server-0.1.0-xxxx下执行sh bin/load.sh start命令即可服务端程序；
>
