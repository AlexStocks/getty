# getty [中文](./README_CN.md)

 *a netty like asynchronous network I/O library*

[![CI](https://github.com/AlexStocks/getty/actions/workflows/github-actions.yml/badge.svg?branch=master)](https://github.com/AlexStocks/getty/actions/workflows/github-actions.yml)
[![codecov](https://codecov.io/gh/AlexStocks/getty/branch/master/graph/badge.svg)](https://codecov.io/gh/AlexStocks/getty)
[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/AlexStocks/getty?tab=doc)
[![Go Report Card](https://goreportcard.com/badge/github.com/AlexStocks/getty)](https://goreportcard.com/report/github.com/AlexStocks/getty)
![license](https://img.shields.io/badge/license-Apache--2.0-green.svg)

## INTRO

Getty is an asynchronous network I/O library developed in Golang. It operates on TCP, UDP, and WebSocket network protocols, providing a consistent interface [EventListener](https://github.com/AlexStocks/getty/blob/01184614ef72d0cf2dd11894ab31e0dace066b6c/transport/getty.go#L68).

Within Getty, each connection (session) involves two separate goroutines. One handles the reading of TCP streams, UDP packets, or WebSocket packages, while the other manages the logic processing and writes responses into the network write buffer. If your logic processing might take a considerable amount of time, it's recommended to start a new logic process goroutine yourself within codec.go's (Codec)OnMessage method.

Additionally, you can manage heartbeat logic within the (Codec)OnCron method in codec.go. If you're using TCP or UDP, you should send heartbeat packages yourself and then call session.go's (Session)UpdateActive method to update the session's activity timestamp. You can verify if a TCP session has timed out or not in codec.go's (Codec)OnCron method using session.go's (Session)GetActive method.

If you're using WebSocket, you don't need to worry about heartbeat request/response, as Getty handles this task within session.go's (Session)handleLoop method by sending and receiving WebSocket ping/pong frames. Your responsibility is to check whether the WebSocket session has timed out or not within codec.go's (Codec)OnCron method using session.go's (Session)GetActive method.

For code examples, you can refer to [getty-examples](https://github.com/AlexStocks/getty-examples).

## Network Transmission

In network communication, the data transmission interface of getty does not guarantee that data will be sent successfully; it lacks an internal retry mechanism. Instead, getty delegates the outcome of data transmission to the underlying operating system mechanism. Under this mechanism, if data is successfully transmitted, it is considered a success; if transmission fails, it is regarded as a failure. These outcomes are then communicated back to the upper-layer caller.

Upper-layer callers need to determine whether to incorporate a retry mechanism based on these outcomes. This implies that when data transmission fails, upper-layer callers must handle the situation differently depending on the circumstances. For instance, if the failure is due to a disconnect in the connection, upper-layer callers can attempt to resend the data based on the result of getty's automatic reconnection. Alternatively, if the failure is caused by the sending buffer of the underlying operating system being full, the sender can implement its own retry mechanism to wait for the sending buffer to become available before attempting another transmission.

In summary, the data transmission interface of getty does not come with an inherent retry mechanism; instead, it is up to upper-layer callers to decide whether to implement retry logic based on specific situations. This design approach provides developers with greater flexibility in controlling the behavior of data transmission.

## Framework Architecture

Getty framework adopts a layered architecture design, from top to bottom: Application Layer, Getty Core Layer, and Network Layer:

```text
┌─────────────────────────────────────────────────────────────┐
│                 Application Layer                           │
├─────────────────────────────────────────────────────────────┤
│  Application Code  │  Message Handler  │  Codec/ReadWriter  │
└─────────────────────────────────────────────────────────────┘
┌─────────────────────────────────────────────────────────────┐
│                 Getty Core Layer                            │
├─────────────────────────────────────────────────────────────┤
│  Session Management │  Server Management │  Client Management │
│  Connection Mgmt    │  Event System     │  Options & Config  │
└─────────────────────────────────────────────────────────────┘
┌─────────────────────────────────────────────────────────────┐
│                 Network Layer                               │
├─────────────────────────────────────────────────────────────┤
│     TCP Protocol    │    UDP Protocol   │  WebSocket Protocol │
│     TLS/SSL         │                   │                   │
└─────────────────────────────────────────────────────────────┘
```

### Core Component Relationships

1. **Session** is the core component, managing connection lifecycle
2. **Server/Client** provides endpoint implementations for different protocols
3. **Connection** encapsulates underlying network connections
4. **EventListener** handles various events
5. **Options** provides flexible configuration

## Data Flow Processing

### Complete Data Flow Diagram

```text
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Incoming Data Flow                               │
├─────────────────────────────────────────────────────────────────────────────┤
│  Network → Getty → PkgHandler.Read() → EventListener.OnMessage() → Logic    │
└─────────────────────────────────────────────────────────────────────────────┘
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Outgoing Data Flow                               │
├─────────────────────────────────────────────────────────────────────────────┤
│  Logic → WritePkg() → PkgHandler.Write() → Getty → Network                  │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Processing Order
1. **PkgHandler first**: Handles protocol-level parsing/serialization
2. **EventListener second**: Handles business logic and events
3. **Two separate goroutines**: One for reading, one for processing

### Key Components
- **PkgHandler**: Implements `ReadWriter` interface for data parsing/serialization
- **EventListener**: Implements `EventListener` interface for business logic
- **OnMessage()**: Method of `EventListener` interface for processing parsed packets

## Quick Start

### TCP Server Example

Here's a simplified TCP server example demonstrating Getty framework's core usage:

```go
package main

import (
    "fmt"
    "log"
    "time"
    getty "github.com/AlexStocks/getty/transport"
    gxsync "github.com/dubbogo/gost/sync"
)

// Packet handler - responsible for packet serialization/deserialization
type EchoPackageHandler struct{}

// Deserialize: parse network byte stream into application packets
func (h *EchoPackageHandler) Read(session getty.Session, data []byte) (interface{}, int, error) {
    // Pseudo code: implement length-prefixed protocol
    // 1. Check if there's enough data to read length header (4 bytes)
    if len(data) < 4 {
        return nil, 0, nil // Need more data
    }
    
    // 2. Parse packet length
    length := int(data[0])<<24 | int(data[1])<<16 | int(data[2])<<8 | int(data[3])
    
    // 3. Check if we have complete packet
    if len(data) < 4+length {
        return nil, 0, nil // Incomplete packet, wait for more data
    }
    
    // 4. Return parsed packet and bytes consumed
    return data[4:4+length], 4 + length, nil
}

// Serialize: convert application packets to network byte stream
func (h *EchoPackageHandler) Write(session getty.Session, pkg interface{}) ([]byte, error) {
    // Pseudo code: implement length-prefixed protocol
    // 1. Convert application data to bytes
    data := []byte(fmt.Sprintf("%v", pkg))
    
    // 2. Build length header (4 bytes)
    length := len(data)
    header := []byte{
        byte(length >> 24), byte(length >> 16), 
        byte(length >> 8), byte(length),
    }
    
    // 3. Return complete network packet
    return append(header, data...), nil
}

// Event handler - responsible for business logic
type EchoMessageHandler struct{}

// Called when connection is established
func (h *EchoMessageHandler) OnOpen(session getty.Session) error {
    log.Printf("New connection: %s", session.RemoteAddr())
    return nil
}

// Called when connection is closed
func (h *EchoMessageHandler) OnClose(session getty.Session) {
    log.Printf("Connection closed: %s", session.RemoteAddr())
}

// Called when error occurs
func (h *EchoMessageHandler) OnError(session getty.Session, err error) {
    log.Printf("Connection error: %s, error: %v", session.RemoteAddr(), err)
}

// Heartbeat detection - called periodically
func (h *EchoMessageHandler) OnCron(session getty.Session) {
    activeTime := session.GetActive()
    if time.Since(activeTime) > 30*time.Second {
        log.Printf("Connection timeout, closing: %s", session.RemoteAddr())
        session.Close()
    }
}

// Called when message is received - core business logic
func (h *EchoMessageHandler) OnMessage(session getty.Session, pkg interface{}) {
    messageData, ok := pkg.([]byte)
    if !ok {
        log.Printf("invalid packet type: %T", pkg)
        return
    }
    log.Printf("Received message: %s", string(messageData))
    
    // Business logic: echo message
    response := fmt.Sprintf("Echo: %s", string(messageData))
    if _, _, err := session.WritePkg(response, 5*time.Second); err != nil {
        log.Printf("send failed: %v", err)
    }
}

// New connection callback - configure session
func newSession(session getty.Session) error {
    // Basic configuration
    session.SetName("tcp-echo-session")
    session.SetMaxMsgLen(4096)
    session.SetReadTimeout(time.Second * 10)
    session.SetWriteTimeout(time.Second * 10)
    session.SetCronPeriod(5000) // 5 second heartbeat detection
    session.SetWaitTime(time.Second * 3)
    
    // Set handlers
    session.SetPkgHandler(&EchoPackageHandler{})    // Packet handler
    session.SetEventListener(&EchoMessageHandler{}) // Event handler
    
    // Add close callback
    session.AddCloseCallback("cleanup", "resources", func() {
        log.Printf("Cleaning up resources: %s", session.RemoteAddr())
    })
    
    return nil
}

func main() {
    // Create task pool (for concurrent message processing)
    taskPool := gxsync.NewTaskPoolSimple(0)
    defer taskPool.Close()

    // Create TCP server
    server := getty.NewTCPServer(
        getty.WithLocalAddress(":8080"),        // Listen address
        getty.WithServerTaskPool(taskPool),    // Task pool
    )

    // Start server
    log.Println("TCP server starting on :8080")
    server.RunEventLoop(newSession) // Start event loop
}
```

## Core Concepts

### Session Management

Session is the core component of the Getty framework, responsible for managing connection sessions between clients and servers. Each connection corresponds to a Session instance, providing complete connection lifecycle management.

#### Session Interface

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
    GetActive() time.Time
    UpdateActive()
    SetCronPeriod(int)
    SetWaitTime(time.Duration)
    SetReadTimeout(time.Duration)
    SetWriteTimeout(time.Duration)
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

#### Key Methods

##### Connection Management
- **`Conn()`**: Get the underlying network connection object
- **`IsClosed()`**: Check if the session is closed
- **`Close()`**: Close the session connection
- **`Reset()`**: Reset session state

##### Configuration Settings
- **`SetName(string)`**: Set session name
- **`SetMaxMsgLen(int)`**: Set maximum message length
- **`SetCronPeriod(int)`**: Set heartbeat detection period (milliseconds)
- **`SetWaitTime(time.Duration)`**: Set wait timeout
- **`SetReadTimeout(time.Duration)`**: Set read timeout
- **`SetWriteTimeout(time.Duration)`**: Set write timeout

##### Handler Settings
- **`SetEventListener(EventListener)`**: Set event listener for handling connection lifecycle events
- **`SetPkgHandler(ReadWriter)`**: Set packet handler for parsing and serializing network data
- **`SetReader(Reader)`**: Set data reader for custom data parsing
- **`SetWriter(Writer)`**: Set data writer for custom data serialization

##### Data Transmission
- **`WritePkg(pkg any, timeout time.Duration)`**: Send data packet, returns total bytes and successfully sent bytes
- **`WriteBytes([]byte)`**: Send byte data
- **`WriteBytesArray(...[]byte)`**: Send multiple byte arrays

##### Attribute Management
- **`GetAttribute(key any)`**: Get session attribute
- **`SetAttribute(key any, value any)`**: Set session attribute
- **`RemoveAttribute(key any)`**: Remove session attribute

##### Statistics
- **`Stat()`**: Get session statistics (connection status, read/write bytes, packet count, etc.)

#### Active Time Update Mechanism

**Automatic Active Time Updates**
```go
// Getty automatically updates session active time when:
// 1. Receiving data from network
func (t *gettyTCPConn) recv(p []byte) (int, error) {
    // ... receive data logic
    t.UpdateActive() // Automatically called - updates GetActive() value
    return length, err
}

// 2. WebSocket ping/pong frames (WebSocket only)
func (w *gettyWSConn) handlePing(message string) error {
    w.UpdateActive() // Updates when receiving ping
    return w.writePong([]byte(message))
}

func (w *gettyWSConn) handlePong(string) error {
    w.UpdateActive() // Updates when receiving pong
    return nil
}

// Note: TCP/UDP send does NOT automatically call UpdateActive()
// Only "data reception" and WebSocket ping/pong update active time
```

**Server-Side Heartbeat Detection**
```go
// Server automatically calls OnCron periodically for each session
func (h *ServerMessageHandler) OnCron(session getty.Session) {
    // Get last active time (automatically updated on data reception or WS ping/pong)
    activeTime := session.GetActive()
    idleTime := time.Since(activeTime)
    
    log.Printf("Heartbeat check: %s, last active: %v, idle: %v", 
        session.RemoteAddr(), activeTime, idleTime)
    
    // Check for timeout
    if idleTime > 30*time.Second {
        log.Printf("Client timeout, closing connection: %s", session.RemoteAddr())
        session.Close()
    }
}
```

**Active Time Update Timeline**
```go
// Example timeline showing when GetActive() values change:
// 00:00:00 - Connection established, GetActive() = 2024-01-01 10:00:00
// 00:00:05 - Server receives client data, GetActive() = 2024-01-01 10:00:05  
// 00:00:10 - Server receives client data, GetActive() = 2024-01-01 10:00:10
// 00:00:15 - OnCron called, checks idle time: 5 seconds
// 00:00:20 - OnCron called, checks idle time: 10 seconds
// 00:00:30 - OnCron called, detects timeout, closes connection
```

**Key Points:**
- **Automatic Updates**: Active time updates only on data reception or WebSocket ping/pong
- **Server-Side Detection**: Server calls OnCron periodically to check client activity
- **No Client Request Needed**: Heartbeat detection is server-initiated, not client-requested
- **Real-Time Monitoring**: GetActive() reflects actual network activity

### Server Management

Getty provides multiple types of server implementations, supporting TCP, UDP, WebSocket, and WSS protocols.

#### TCP Server

```go
// Create TCP server
server := getty.NewTCPServer(
    getty.WithLocalAddress(":8080"),       // Listen address
    getty.WithServerTaskPool(taskPool),    // Task pool
)
```

#### Server Interface

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

#### Key Methods

- **`RunEventLoop(newSession NewSessionCallback)`**: Start event loop to handle client connections
- **`Close()`**: Close the server
- **`IsClosed()`**: Check if the server is closed
- **`ID()`**: Get server ID
- **`EndPointType()`**: Get endpoint type

#### Event Loop

The server starts the event loop through the `RunEventLoop` method:

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

### Options Configuration System

Getty uses functional options pattern to configure servers and clients, providing flexible configuration.

#### Server Options

**Basic Configuration**
- **`WithLocalAddress(addr string)`**: Set server listen address
- **`WithServerTaskPool(pool GenericTaskPool)`**: Set server task pool

**WebSocket Configuration**
- **`WithWebsocketServerPath(path string)`**: Set WebSocket request path
- **`WithWebsocketServerCert(cert string)`**: Set server certificate file
- **`WithWebsocketServerPrivateKey(key string)`**: Set server private key file
- **`WithWebsocketServerRootCert(cert string)`**: Set root certificate file

**TLS Configuration**
- **`WithServerSslEnabled(sslEnabled bool)`**: Enable/disable SSL
- **`WithServerTlsConfigBuilder(builder TlsConfigBuilder)`**: Set TLS config builder

#### Client Options

**Basic Configuration**
- **`WithServerAddress(addr string)`**: Set server address
- **`WithConnectionNumber(num int)`**: Set connection number
- **`WithClientTaskPool(pool GenericTaskPool)`**: Set client task pool

**Reconnection Configuration**
- **`WithReconnectInterval(interval int)`**: Set reconnection interval (nanoseconds)
- **`WithReconnectAttempts(maxAttempts int)`**: Set maximum reconnection attempts

**Certificate Configuration**
- **`WithRootCertificateFile(cert string)`**: Set root certificate file
- **`WithClientSslEnabled(sslEnabled bool)`**: Enable/disable client SSL
- **`WithClientTlsConfigBuilder(builder TlsConfigBuilder)`**: Set client TLS config

#### Configuration Examples

**TCP Server Configuration**
```go
// Create task pool
taskPool := gxsync.NewTaskPoolSimple(0)
defer taskPool.Close()

// TCP server configuration
server := getty.NewTCPServer(
    getty.WithLocalAddress(":8080"),       // Listen address
    getty.WithServerTaskPool(taskPool),    // Task pool
)

// Start server
server.RunEventLoop(newSession)
```

## Advanced Features

### Callback System

Getty provides a robust callback system that allows you to register and manage callback functions for session lifecycle events. This is particularly useful for cleanup operations, resource management, and custom event handling.

#### Key Features

- **Thread-safe operations**: All callback operations are protected by mutex locks
- **Replace semantics**: Adding with the same (handler, key) replaces the existing callback in place (position preserved)
- **Panic safety**: During session close, callbacks run in a dedicated goroutine with defer/recover; panics are logged with stack traces and do not escape the close path
- **Ordered execution**: Callbacks are executed in the order they were added

#### Usage Example

```go
// Add a close callback
session.AddCloseCallback("cleanup", "resources", func() {
    // Cleanup resources when session closes
    cleanupResources()
})

// Remove a specific callback
// Safe to call even if the pair was never added (no-op)
session.RemoveCloseCallback("cleanup", "resources")

// Callbacks are automatically executed when the session closes
```

**Note**: During session shutdown, callbacks are executed sequentially in a dedicated goroutine to preserve add-order, with defer/recover to log panics without letting them escape the close path.

#### Callback Management

- **AddCloseCallback**: Register a callback to be executed when the session closes
- **RemoveCloseCallback**: Remove a previously registered callback (no-op if not found; safe to call multiple times)
- **Thread Safety**: All operations are thread-safe and can be called concurrently

#### Type Requirements

The `handler` and `key` parameters must be **comparable types** that support the `==` operator:

**✅ Supported types:**
- **Basic types**: `string`, `int`, `int8`, `int16`, `int32`, `int64`, `uint`, `uint8`, `uint16`, `uint32`, `uint64`, `uintptr`, `float32`, `float64`, `bool`, `complex64`, `complex128`
  - ⚠️ Avoid `float*`/`complex*` as keys due to NaN and precision semantics; prefer strings/ints
- **Pointer types**: Pointers to any type (e.g., `*int`, `*string`, `*MyStruct`)
- **Interface types**: Interface types are comparable only when their dynamic values are comparable types; using "==" with non-comparable dynamic values will be safely ignored with error log
- **Channel types**: Channel types (compared by channel identity)
- **Array types**: Arrays of comparable elements (e.g., `[3]int`, `[2]string`)
- **Struct types**: Structs where all fields are comparable types

**⚠️ Non-comparable types (will be safely ignored with error log):**
- `map` types (e.g., `map[string]int`)
- `slice` types (e.g., `[]int`, `[]string`)
- `func` types (e.g., `func()`, `func(int) string`)
- Structs containing non-comparable fields (maps, slices, functions)

**Examples:**
```go
// ✅ Valid usage
session.AddCloseCallback("user", "cleanup", callback)
session.AddCloseCallback(123, "cleanup", callback)
session.AddCloseCallback(true, false, callback)

// ⚠️ Non-comparable types (safely ignored with error log)
session.AddCloseCallback(map[string]int{"a": 1}, "key", callback)  // Logged and ignored
session.AddCloseCallback([]int{1, 2, 3}, "key", callback)          // Logged and ignored
```

## LICENCE

Apache License 2.0
