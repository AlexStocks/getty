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

有关代码示例，请参阅 https://github.com/AlexStocks/getty-examples。

## 关于 Getty 中的网络传输

在网络通信中，Getty 的数据传输接口并不保证数据一定会成功发送，它缺乏内部的重试机制。相反，Getty 将数据传输的结果委托给底层操作系统机制处理。在这种机制下，如果数据成功传输，将被视为成功；如果传输失败，则被视为失败。这些结果随后会传递给上层调用者。

上层调用者需要根据这些结果决定是否加入重试机制。这意味着当数据传输失败时，上层调用者必须根据情况采取不同的处理方式。例如，如果失败是由于连接断开导致的，上层调用者可以尝试基于 Getty 的自动重新连接结果重新发送数据。另外，如果失败是因为底层操作系统的发送缓冲区已满，发送者可以自行实现重试机制，在再次尝试传输之前等待发送缓冲区可用。

总之，Getty 的数据传输接口并不自带内部的重试机制；相反，是否在特定情况下实现重试逻辑由上层调用者决定。这种设计方法为开发者在控制数据传输行为方面提供了更大的灵活性。

## 许可证

Apache 许可证 2.0
