# getty

 *一个类似 Netty 的异步网络 I/O 库*

[![Build Status](https://travis-ci.org/AlexStocks/getty.svg?branch=master)](https://travis-ci.org/AlexStocks/getty)
[![codecov](https://codecov.io/gh/AlexStocks/getty/branch/master/graph/badge.svg)](https://codecov.io/gh/AlexStocks/getty)
[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/AlexStocks/getty?tab=doc)
[![Go Report Card](https://goreportcard.com/badge/github.com/AlexStocks/getty)](https://goreportcard.com/report/github.com/AlexStocks/getty)
![license](https://img.shields.io/badge/license-Apache--2.0-green.svg)

## 简介

Getty 是一个使用 Golang 开发的异步网络 I/O 库。它适用于 TCP、UDP 和 WebSocket 网络协议，并提供了一致的接口 EventListener。

在 Getty 中，每个连接（会话）涉及两个独立的 Goroutine。一个负责读取 TCP 流、UDP 数据包或 WebSocket 数据包，而另一个负责处理逻辑并将响应写入网络写缓冲区。如果您的逻辑处理可能需要较长时间，建议您在 codec.go 的 (Codec)OnMessage 方法内自行启动一个新的逻辑处理 Goroutine。

此外，您可以在 codec.go 的 (Codec)OnCron 方法内管理心跳逻辑。如果您使用 TCP 或 UDP，应该自行发送心跳包，然后调用 session.go 的 (Session)UpdateActive 方法来更新会话的活动时间戳。您可以通过 codec.go 的 (Codec)OnCron 方法内使用 session.go 的 (Session)GetActive 方法来验证 TCP 会话是否已超时。

如果您使用 WebSocket，您无需担心心跳请求/响应，因为 Getty 在 session.go 的 (Session)handleLoop 方法内通过发送和接收 WebSocket ping/pong 帧来处理此任务。您只需在 codec.go 的 (Codec)OnCron 方法内使用 session.go 的 (Session)GetActive 方法检查 WebSocket 会话是否已超时。

有关代码示例，请参阅 [AlexStocks/getty-examples](https://github.com/AlexStocks/getty-examples)。

## 回调系统

Getty 提供了一个强大的回调系统，允许您为会话生命周期事件注册和管理回调函数。这对于清理操作、资源管理和自定义事件处理特别有用。

### 主要特性

- **线程安全操作**：所有回调操作都受到互斥锁保护
- **替换语义**：使用相同的 (handler, key) 添加会替换现有回调并保持位置不变
- **Panic 安全性**：在会话关闭期间，回调在专用 goroutine 中运行，带有 defer/recover；panic 会被记录堆栈跟踪且不会逃逸出关闭路径
- **有序执行**：回调按照添加的顺序执行

### 使用示例

```go
// 添加关闭回调
session.AddCloseCallback("cleanup", "resources", func() {
    // 当会话关闭时清理资源
    cleanupResources()
})

// 移除特定回调
// 即使从未添加过该对也可以安全调用（无操作）
session.RemoveCloseCallback("cleanup", "resources")

// 当会话关闭时，回调会自动执行
```

**注意**：在会话关闭期间，回调在专用 goroutine 中顺序执行以保持添加顺序，带有 defer/recover 来记录 panic 而不让它们逃逸出关闭路径。

### 回调管理

- **AddCloseCallback**：注册一个在会话关闭时执行的回调
- **RemoveCloseCallback**：移除之前注册的回调（未找到时无操作；可安全多次调用）
- **线程安全**：所有操作都是线程安全的，可以并发调用

### 类型要求

`handler` 和 `key` 参数必须是**可比较的类型**，支持 `==` 操作符：

**✅ 支持的类型：**
- **基本类型**：`string`、`int`、`int8`、`int16`、`int32`、`int64`、`uint`、`uint8`、`uint16`、`uint32`、`uint64`、`uintptr`、`float32`、`float64`、`bool`、`complex64`、`complex128`
  - ⚠️ 避免使用 `float*`/`complex*` 作为键，因为 NaN 和精度语义问题；建议使用字符串/整数
- **指针类型**：指向任何类型的指针（如 `*int`、`*string`、`*MyStruct`）
- **接口类型**：仅当其动态值为可比较类型时可比较；若动态值不可比较，使用"=="将被安全忽略并记录错误日志
- **通道类型**：通道类型（按通道标识比较）
- **数组类型**：可比较元素的数组（如 `[3]int`、`[2]string`）
- **结构体类型**：所有字段都是可比较类型的结构体

**⚠️ 不可比较类型（将被安全忽略并记录错误日志）：**
- `map` 类型（如 `map[string]int`）
- `slice` 类型（如 `[]int`、`[]string`）
- `func` 类型（如 `func()`、`func(int) string`）
- 包含不可比较字段的结构体（maps、slices、functions）

**示例：**
```go
// ✅ 有效用法
session.AddCloseCallback("user", "cleanup", callback)
session.AddCloseCallback(123, "cleanup", callback)
session.AddCloseCallback(true, false, callback)

// ⚠️ 不可比较类型（安全忽略并记录错误日志）
session.AddCloseCallback(map[string]int{"a": 1}, "key", callback)  // 记录日志并忽略
session.AddCloseCallback([]int{1, 2, 3}, "key", callback)          // 记录日志并忽略
```

## 关于 Getty 中的网络传输

在网络通信中，Getty 的数据传输接口并不保证数据一定会成功发送，它缺乏内部的重试机制。相反，Getty 将数据传输的结果委托给底层操作系统机制处理。在这种机制下，如果数据成功传输，将被视为成功；如果传输失败，则被视为失败。这些结果随后会传递给上层调用者。

上层调用者需要根据这些结果决定是否加入重试机制。这意味着当数据传输失败时，上层调用者必须根据情况采取不同的处理方式。例如，如果失败是由于连接断开导致的，上层调用者可以尝试基于 Getty 的自动重新连接结果重新发送数据。另外，如果失败是因为底层操作系统的发送缓冲区已满，发送者可以自行实现重试机制，在再次尝试传输之前等待发送缓冲区可用。

总之，Getty 的数据传输接口并不自带内部的重试机制；相反，是否在特定情况下实现重试逻辑由上层调用者决定。这种设计方法为开发者在控制数据传输行为方面提供了更大的灵活性。

## Session 会话管理

Session 是 Getty 框架的核心组件，负责管理客户端与服务器之间的连接会话。每个连接对应一个 Session 实例，提供完整的连接生命周期管理。

### Session 接口

```go
type Session interface {
    Connection
    Reset()
    Conn() net.Conn
    Stat() string
    IsClosed() bool
    EndPoint() EndPoint
    SetMaxMsgLen(int)
    SetName(string)
    SetEventListener(EventListener)
    SetPkgHandler(ReadWriter)
    SetReader(Reader)
    SetWriter(Writer)
    SetCronPeriod(int)
    SetWaitTime(time.Duration)
    GetAttribute(any) any
    SetAttribute(any, any)
    RemoveAttribute(any)
    WritePkg(pkg any, timeout time.Duration) (totalBytesLength int, sendBytesLength int, err error)
    WriteBytes([]byte) (int, error)
    WriteBytesArray(...[]byte) (int, error)
    Close()
    AddCloseCallback(handler, key any, callback CallBackFunc)
    RemoveCloseCallback(handler, key any)
}
```

### 主要方法说明

#### 连接管理
- **`Conn()`**: 获取底层网络连接对象
- **`IsClosed()`**: 检查会话是否已关闭
- **`Close()`**: 关闭会话连接
- **`Reset()`**: 重置会话状态

#### 配置设置
- **`SetName(string)`**: 设置会话名称
- **`SetMaxMsgLen(int)`**: 设置最大消息长度
- **`SetCronPeriod(int)`**: 设置心跳检测周期（毫秒）
- **`SetWaitTime(time.Duration)`**: 设置等待超时时间

#### 处理器设置
- **`SetEventListener(EventListener)`**: 设置事件监听器，处理连接生命周期事件
- **`SetPkgHandler(ReadWriter)`**: 设置数据包处理器，负责解析和序列化网络数据
- **`SetReader(Reader)`**: 设置数据读取器，用于自定义数据解析
- **`SetWriter(Writer)`**: 设置数据写入器，用于自定义数据序列化

#### 详细方法使用说明

**SetPkgHandler - 数据包处理器**
```go
// SetPkgHandler 负责解析和序列化网络数据包
session.SetPkgHandler(&EchoPackageHandler{})

// 处理器必须实现 ReadWriter 接口
type EchoPackageHandler struct{}

func (h *EchoPackageHandler) Read(session transport.Session, data []byte) (interface{}, int, error) {
    // 解析接收到的数据为应用层数据包
    // 返回: (解析出的数据包, 消费的字节数, 错误)
    if len(data) < 4 {
        return nil, 0, nil // 数据不足，等待更多数据
    }
    length := int(data[0])<<24 | int(data[1])<<16 | int(data[2])<<8 | int(data[3])
    if len(data) < 4+length {
        return nil, 0, nil // 数据包不完整
    }
    return data[4:4+length], 4 + length, nil
}

func (h *EchoPackageHandler) Write(session transport.Session, pkg interface{}) ([]byte, error) {
    // 将应用层数据包序列化为网络字节
    data := []byte(fmt.Sprintf("%v", pkg))
    length := len(data)
    header := []byte{
        byte(length >> 24), byte(length >> 16), 
        byte(length >> 8), byte(length),
    }
    return append(header, data...), nil
}
```

**SetEventListener - 事件处理器**
```go
// SetEventListener 处理连接生命周期事件
session.SetEventListener(&EchoMessageHandler{})

type EchoMessageHandler struct{}

func (h *EchoMessageHandler) OnOpen(session transport.Session) error {
    log.Printf("连接建立: %s", session.RemoteAddr())
    return nil
}

func (h *EchoMessageHandler) OnClose(session transport.Session) {
    log.Printf("连接关闭: %s", session.RemoteAddr())
}

func (h *EchoMessageHandler) OnError(session transport.Session, err error) {
    log.Printf("连接错误: %s, 错误: %v", session.RemoteAddr(), err)
}

func (h *EchoMessageHandler) OnCron(session transport.Session) {
    // 心跳检测 - 定期调用
    activeTime := session.GetActive()
    if time.Since(activeTime) > 30*time.Second {
        log.Printf("连接超时: %s", session.RemoteAddr())
        session.Close()
    }
}

func (h *EchoMessageHandler) OnMessage(session transport.Session, pkg interface{}) {
    // 处理接收到的消息
    messageData := pkg.([]byte)
    log.Printf("收到消息: %s", string(messageData))
    
    // 处理业务逻辑并发送响应
    response := fmt.Sprintf("Echo: %s", string(messageData))
    session.WritePkg(response, time.Second*5)
}
```

**SetCronPeriod - 心跳配置**
```go
// SetCronPeriod 设置心跳检测周期，单位是毫秒
// conf.heartbeatPeriod 是 time.Duration 类型（例如 5 * time.Second）
cronPeriod := int(conf.heartbeatPeriod.Nanoseconds() / 1e6) // 转换为毫秒
session.SetCronPeriod(cronPeriod) // 设置 5000ms = 5 秒

// OnCron 方法将每 5 秒被调用一次进行心跳检测
func (h *EchoMessageHandler) OnCron(session transport.Session) {
    // 检查连接是否仍然活跃
    activeTime := session.GetActive()
    if time.Since(activeTime) > 30*time.Second {
        // 连接超时，关闭它
        session.Close()
    }
    
    // 可选：发送心跳包
    // session.WritePkg("ping", time.Second)
}
```

#### 活跃时间更新机制

**自动活跃时间更新**
```go
// Getty 在以下情况下自动更新会话活跃时间：
// 1. 从网络接收数据时
func (t *gettyTCPConn) recv(p []byte) (int, error) {
    // ... 接收数据逻辑
    t.UpdateActive() // 自动调用 - 更新 GetActive() 值
    return length, err
}

// 2. WebSocket ping/pong 帧（仅 WebSocket）
func (w *gettyWSConn) handlePing(message string) error {
    w.UpdateActive() // 收到 ping 时更新
    return w.writePong([]byte(message))
}

func (w *gettyWSConn) handlePong(string) error {
    w.UpdateActive() // 收到 pong 时更新
    return nil
}

// 注意：TCP/UDP 的 Send() 方法不会自动调用 UpdateActive()
// 只有数据接收和 WebSocket ping/pong 会更新活跃时间
```

**服务端心跳检测**
```go
// 服务端定期为每个会话自动调用 OnCron
func (h *ServerMessageHandler) OnCron(session transport.Session) {
    // 获取最后活跃时间（在数据接收/发送时自动更新）
    activeTime := session.GetActive()
    idleTime := time.Since(activeTime)
    
    log.Printf("心跳检测: %s, 最后活跃: %v, 空闲: %v", 
        session.RemoteAddr(), activeTime, idleTime)
    
    // 检查是否超时
    if idleTime > 30*time.Second {
        log.Printf("客户端超时，关闭连接: %s", session.RemoteAddr())
        session.Close()
    }
}
```

**活跃时间更新时间线**
```go
// 示例时间线，显示 GetActive() 值何时变化：
// 00:00:00 - 连接建立，GetActive() = 2024-01-01 10:00:00
// 00:00:05 - 客户端发送数据，GetActive() = 2024-01-01 10:00:05  
// 00:00:10 - 服务端发送响应，GetActive() = 2024-01-01 10:00:10
// 00:00:15 - OnCron 被调用，检查空闲时间：5 秒
// 00:00:20 - OnCron 被调用，检查空闲时间：10 秒
// 00:00:30 - OnCron 被调用，检测到超时，关闭连接
```

**关键要点：**
- **自动更新**：活跃时间在数据接收/发送时自动更新
- **服务端检测**：服务端定期调用 OnCron 检查客户端活动
- **无需客户端请求**：心跳检测是服务端发起的，不需要客户端请求
- **实时监控**：GetActive() 反映真实的网络活动

#### 数据流处理

**接收数据流**
```go
// 1. 网络数据接收
网络数据 → Getty 框架 → PkgHandler.Read() → EventListener.OnMessage() → 业务逻辑

// 2. 详细流程：
// 步骤 1: 接收原始网络数据
func (t *gettyTCPConn) recv(p []byte) (int, error) {
    // 来自网络的原始字节
    t.UpdateActive() // 更新活跃时间
    return length, err
}

// 步骤 2: PkgHandler 解析数据为应用层数据包
func (h *EchoPackageHandler) Read(session transport.Session, data []byte) (interface{}, int, error) {
    // 将原始字节解析为应用层数据包
    // 返回: (解析出的数据包, 消费的字节数, 错误)
    return parsedPacket, bytesConsumed, nil
}

// 步骤 3: EventListener.OnMessage() 处理解析后的数据包
func (h *EchoMessageHandler) OnMessage(session transport.Session, pkg interface{}) {
    // 使用解析后的数据包处理业务逻辑
    // 发送响应回客户端
}
```

**发送数据流**
```go
// 1. 业务逻辑生成响应
业务逻辑 → PkgHandler.Write() → Getty 框架 → 网络

// 2. 详细流程：
// 步骤 1: 业务逻辑调用 WritePkg
session.WritePkg(response, timeout)

// 步骤 2: PkgHandler 序列化应用数据
func (h *EchoPackageHandler) Write(session transport.Session, pkg interface{}) ([]byte, error) {
    // 将应用层数据包序列化为网络字节
    return serializedBytes, nil
}

// 步骤 3: Getty 发送到网络
func (t *gettyTCPConn) Send(pkg any) (int, error) {
    // 发送序列化字节到网络
    return length, err
}
```

**完整数据流图**
```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            接收数据流                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│  网络 → Getty → PkgHandler.Read() → EventListener.OnMessage() → 业务逻辑      │
└─────────────────────────────────────────────────────────────────────────────┘
┌─────────────────────────────────────────────────────────────────────────────┐
│                            发送数据流                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│  业务逻辑 → WritePkg() → PkgHandler.Write() → Getty → 网络                   │
└─────────────────────────────────────────────────────────────────────────────┘
```

**处理顺序：**
1. **PkgHandler 优先**：处理协议层解析/序列化
2. **EventListener 其次**：处理业务逻辑和事件
3. **两个独立 goroutine**：一个负责读取，一个负责处理

**关键组件：**
- **PkgHandler**：实现 `ReadWriter` 接口，负责数据解析/序列化
- **EventListener**：实现 `EventListener` 接口，负责业务逻辑
- **OnMessage()**：`EventListener` 接口的方法，用于处理解析后的数据包

#### 数据发送
- **`WritePkg(pkg any, timeout time.Duration)`**: 发送数据包，返回总字节数和成功发送字节数
- **`WriteBytes([]byte)`**: 发送字节数据
- **`WriteBytesArray(...[]byte)`**: 发送多个字节数组

#### 属性管理
- **`GetAttribute(key any)`**: 获取会话属性
- **`SetAttribute(key any, value any)`**: 设置会话属性
- **`RemoveAttribute(key any)`**: 删除会话属性

#### 统计信息
- **`Stat()`**: 获取会话统计信息（连接状态、读写字节数、包数量等）

## Server 服务器管理

Getty 提供了多种类型的服务器实现，支持 TCP、UDP、WebSocket 和 WSS 协议。

### 服务器类型

#### TCP 服务器
```go
// 创建 TCP 服务器
server := getty.NewTCPServer(
    getty.WithLocalAddress(":8080"),
    getty.WithServerTaskPool(taskPool),
)
```

#### UDP 端点
```go
// 创建 UDP 端点
server := getty.NewUDPEndPoint(
    getty.WithLocalAddress(":8080"),
    getty.WithServerTaskPool(taskPool),
)
```

#### WebSocket 服务器
```go
// 创建 WebSocket 服务器
server := getty.NewWSServer(
    getty.WithLocalAddress(":8080"),
    getty.WithWebsocketServerPath("/ws"),
    getty.WithServerTaskPool(taskPool),
)
```

#### 安全 WebSocket 服务器
```go
// 创建 WSS 服务器
server := getty.NewWSSServer(
    getty.WithLocalAddress(":8443"),
    getty.WithWebsocketServerPath("/wss"),
    getty.WithWebsocketServerCert("cert.pem"),
    getty.WithWebsocketServerPrivateKey("key.pem"),
    getty.WithServerTaskPool(taskPool),
)
```

### 服务器接口

```go
type Server interface {
    EndPoint
}

type StreamServer interface {
    Server
    Listener() net.Listener
}

type PacketServer interface {
    Server
    PacketConn() net.PacketConn
}
```

### 主要方法

- **`RunEventLoop(newSession NewSessionCallback)`**: 启动事件循环，处理客户端连接
- **`Close()`**: 关闭服务器
- **`IsClosed()`**: 检查服务器是否已关闭
- **`ID()`**: 获取服务器ID
- **`EndPointType()`**: 获取端点类型

### 事件循环

服务器通过 `RunEventLoop` 方法启动事件循环：

```go
func (s *server) RunEventLoop(newSession NewSessionCallback) {
    if err := s.listen(); err != nil {
        panic(fmt.Errorf("server.listen() = error:%+v", perrors.WithStack(err)))
    }

    switch s.endPointType {
    case TCP_SERVER:
        s.runTCPEventLoop(newSession)
    case UDP_ENDPOINT:
        s.runUDPEventLoop(newSession)
    case WS_SERVER:
        s.runWSEventLoop(newSession)
    case WSS_SERVER:
        s.runWSSEventLoop(newSession)
    default:
        panic(fmt.Sprintf("illegal server type %s", s.endPointType.String()))
    }
}
```

## Options 配置系统

Getty 使用函数式选项模式来配置服务器和客户端，提供了灵活的配置方式。

### Server Options 服务器选项

#### 基础配置
- **`WithLocalAddress(addr string)`**: 设置服务器监听地址
- **`WithServerTaskPool(pool GenericTaskPool)`**: 设置服务器任务池

#### WebSocket 配置
- **`WithWebsocketServerPath(path string)`**: 设置 WebSocket 请求路径
- **`WithWebsocketServerCert(cert string)`**: 设置服务器证书文件
- **`WithWebsocketServerPrivateKey(key string)`**: 设置服务器私钥文件
- **`WithWebsocketServerRootCert(cert string)`**: 设置根证书文件

#### TLS 配置
- **`WithServerSslEnabled(sslEnabled bool)`**: 启用/禁用 SSL
- **`WithServerTlsConfigBuilder(builder TlsConfigBuilder)`**: 设置 TLS 配置构建器

### Client Options 客户端选项

#### 基础配置
- **`WithServerAddress(addr string)`**: 设置服务器地址
- **`WithConnectionNumber(num int)`**: 设置连接数量
- **`WithClientTaskPool(pool GenericTaskPool)`**: 设置客户端任务池

#### 重连配置
- **`WithReconnectInterval(interval int)`**: 设置重连间隔（纳秒）
- **`WithReconnectAttempts(maxAttempts int)`**: 设置最大重连次数

#### 证书配置
- **`WithRootCertificateFile(cert string)`**: 设置根证书文件
- **`WithClientSslEnabled(sslEnabled bool)`**: 启用/禁用客户端 SSL
- **`WithClientTlsConfigBuilder(builder TlsConfigBuilder)`**: 设置客户端 TLS 配置

### 配置示例

#### 服务器配置示例
```go
// TCP 服务器配置
server := getty.NewTCPServer(
    getty.WithLocalAddress(":8080"),
    getty.WithServerTaskPool(taskPool),
)

// WebSocket 服务器配置
wsServer := getty.NewWSServer(
    getty.WithLocalAddress(":8080"),
    getty.WithWebsocketServerPath("/ws"),
    getty.WithServerTaskPool(taskPool),
)

// WSS 服务器配置
wssServer := getty.NewWSSServer(
    getty.WithLocalAddress(":8443"),
    getty.WithWebsocketServerPath("/wss"),
    getty.WithWebsocketServerCert("server.crt"),
    getty.WithWebsocketServerPrivateKey("server.key"),
    getty.WithServerTaskPool(taskPool),
)
```

#### 客户端配置示例
```go
// TCP 客户端配置
client := getty.NewTCPClient(
    getty.WithServerAddress("127.0.0.1:8080"),
    getty.WithConnectionNumber(1),
    getty.WithReconnectInterval(5e8), // 500ms
    getty.WithReconnectAttempts(3),
    getty.WithClientTaskPool(taskPool),
)

// WebSocket 客户端配置
wsClient := getty.NewWSClient(
    getty.WithServerAddress("127.0.0.1:8080"),
    getty.WithConnectionNumber(1),
    getty.WithReconnectInterval(5e8),
    getty.WithClientTaskPool(taskPool),
)
```

## TCP 服务器示例

以下是一个完整的 TCP 服务器示例，展示了如何使用 Getty 框架：

```go
package main

import (
    "fmt"
    "log"
    "time"
    "github.com/AlexStocks/getty/transport"
    gxsync "github.com/dubbogo/gost/sync"
)

// EchoPackageHandler 实现数据包处理
type EchoPackageHandler struct{}

func (h *EchoPackageHandler) Read(session transport.Session, data []byte) (interface{}, int, error) {
    // 简单的长度前缀协议
    if len(data) < 4 {
        return nil, 0, nil
    }
    
    length := int(data[0])<<24 | int(data[1])<<16 | int(data[2])<<8 | int(data[3])
    if len(data) < 4+length {
        return nil, 0, nil
    }
    
    return data[4:4+length], 4 + length, nil
}

func (h *EchoPackageHandler) Write(session transport.Session, pkg interface{}) ([]byte, error) {
    data := []byte(fmt.Sprintf("%v", pkg))
    length := len(data)
    header := []byte{
        byte(length >> 24),
        byte(length >> 16),
        byte(length >> 8),
        byte(length),
    }
    return append(header, data...), nil
}

// EchoMessageHandler 实现事件处理
type EchoMessageHandler struct{}

func (h *EchoMessageHandler) OnOpen(session transport.Session) error {
    log.Printf("新连接: %s", session.RemoteAddr())
    return nil
}

func (h *EchoMessageHandler) OnClose(session transport.Session) {
    log.Printf("连接关闭: %s", session.RemoteAddr())
}

func (h *EchoMessageHandler) OnError(session transport.Session, err error) {
    log.Printf("连接错误: %s, 错误: %v", session.RemoteAddr(), err)
}

func (h *EchoMessageHandler) OnCron(session transport.Session) {
    // 心跳检测
    activeTime := session.GetActive()
    if time.Since(activeTime) > 30*time.Second {
        session.Close()
    }
}

func (h *EchoMessageHandler) OnMessage(session transport.Session, pkg interface{}) {
    messageData := pkg.([]byte)
    log.Printf("收到消息: %s", string(messageData))
    
    // 回显消息
    response := fmt.Sprintf("Echo: %s", string(messageData))
    session.WritePkg(response, time.Second*5)
}

// 新连接回调
func newSession(session transport.Session) error {
    session.SetName("tcp-echo-session")
    session.SetMaxMsgLen(4096)
    session.SetReadTimeout(time.Second * 10)
    session.SetWriteTimeout(time.Second * 10)
    session.SetCronPeriod(5)
    session.SetWaitTime(time.Second * 3)
    
    session.SetPkgHandler(&EchoPackageHandler{})
    session.SetEventListener(&EchoMessageHandler{})
    
    session.AddCloseCallback("cleanup", "resources", func() {
        log.Printf("清理资源: %s", session.RemoteAddr())
    })
    
    return nil
}

func main() {
    // 创建任务池
    taskPool := gxsync.NewTaskPoolSimple(0)
    defer taskPool.Close()

    // 创建 TCP 服务器
    server := transport.NewTCPServer(
        transport.WithLocalAddress(":8080"),
        transport.WithServerTaskPool(taskPool),
    )

    // 启动服务器
    log.Println("TCP 服务器启动在 :8080")
    server.RunEventLoop(newSession)
}
```

## 框架架构图

Getty 框架采用分层架构设计，从上到下分为应用层、Getty 核心层和网络层：

```
┌─────────────────────────────────────────────────────────────┐
│                    应用层 (Application Layer)                │
├─────────────────────────────────────────────────────────────┤
│  Application Code  │  Message Handler  │  Codec/ReadWriter  │
└─────────────────────────────────────────────────────────────┘
┌─────────────────────────────────────────────────────────────┐
│                   Getty 核心层 (Core Layer)                 │
├─────────────────────────────────────────────────────────────┤
│  Session Management │  Server Management │  Client Management │
│  Connection Mgmt    │  Event System     │  Options & Config  │
└─────────────────────────────────────────────────────────────┘
┌─────────────────────────────────────────────────────────────┐
│                    网络层 (Network Layer)                    │
├─────────────────────────────────────────────────────────────┤
│     TCP Protocol    │    UDP Protocol   │  WebSocket Protocol │
│     TLS/SSL         │                   │                   │
└─────────────────────────────────────────────────────────────┘
```

### 核心组件关系

1. **Session** 是核心组件，管理连接生命周期
2. **Server/Client** 提供不同协议的端点实现
3. **Connection** 封装底层网络连接
4. **EventListener** 处理各种事件
5. **Options** 提供灵活的配置方式

## 许可证

Apache 许可证 2.0
