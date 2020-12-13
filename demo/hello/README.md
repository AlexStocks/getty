# Run Hello Demo

## 1. prepare
```bash

git clone https://github.com/apache/dubbo-getty.git

cd getty/demo/hello
```

## 2. run server

run server:
`go run tcp/server/server.go`

Or run server in task pool mode:
```bash
go run tcp/server/server.go -taskPool=true \
    -task_pool_size=2000 \
    -pprof_port=60000
```

## 3. run client

```bash
go run tcp/client/client.go
```

Or run client in task pool mode:
```bash
go run tcp/client/client.go -taskPool=true \
    -task_pool_size=50 \
    -pprof_port=60001
```

