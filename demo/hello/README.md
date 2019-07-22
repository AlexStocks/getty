# Run Hello Demo

## 1. prepare 
```bash

git clone https://github.com/dubbogo/getty.git

cd getty/demo/hello
```

## 2. run server

run server: 
`go run tcp/server/server.go`

Or run server in task pool mode:
```bash
go run tcp/server/server.go -taskPool=true \
    -task_queue_length=100 \
    -task_queue_number=4 \
    -task_pool_size=2000
```

## 3. run client

```bash
go run tcp/client/client.go
```