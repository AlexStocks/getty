# getty [中文](./README_CN.md)

 *a netty like asynchronous network I/O library*

[![Build Status](https://travis-ci.org/AlexStocks/getty.svg?branch=master)](https://travis-ci.org/AlexStocks/getty)
[![codecov](https://codecov.io/gh/AlexStocks/getty/branch/master/graph/badge.svg)](https://codecov.io/gh/AlexStocks/getty)
[![go.dev reference](https://img.shields.io/badge/go.dev-reference-007d9c?logo=go&logoColor=white&style=flat-square)](https://pkg.go.dev/github.com/AlexStocks/getty?tab=doc)
[![Go Report Card](https://goreportcard.com/badge/github.com/AlexStocks/getty)](https://goreportcard.com/report/github.com/AlexStocks/getty)
![license](https://img.shields.io/badge/license-Apache--2.0-green.svg)

## INTRO

Getty is an asynchronous network I/O library developed in Golang. It operates on TCP, UDP, and WebSocket network protocols, providing a consistent interface [EventListener](https://github.com/AlexStocks/getty/blob/01184614ef72d0cf2dd11894ab31e0dace066b6c/transport/getty.go#L68).

Within Getty, each connection (session) involves two separate goroutines. One handles the reading of TCP streams, UDP packets, or WebSocket packages, while the other manages the logic processing and writes responses into the network write buffer. If your logic processing might take a considerable amount of time, it's recommended to start a new logic process goroutine yourself within codec.go's (Codec)OnMessage method.

Additionally, you can manage heartbeat logic within the (Codec)OnCron method in codec.go. If you're using TCP or UDP, you should send heartbeat packages yourself and then call session.go's (Session)UpdateActive method to update the session's activity timestamp. You can verify if a TCP session has timed out or not in codec.go's (Codec)OnCron method using session.go's (Session)GetActive method.

If you're using WebSocket, you don't need to worry about heartbeat request/response, as Getty handles this task within session.go's (Session)handleLoop method by sending and receiving WebSocket ping/pong frames. Your responsibility is to check whether the WebSocket session has timed out or not within codec.go's (Codec)OnCron method using session.go's (Session)GetActive method.

For code examples, you can refer to https://github.com/AlexStocks/getty-examples.

## About network transmission in getty

In network communication, the data transmission interface of getty does not guarantee that data will be sent successfully; it lacks an internal retry mechanism. Instead, getty delegates the outcome of data transmission to the underlying operating system mechanism. Under this mechanism, if data is successfully transmitted, it is considered a success; if transmission fails, it is regarded as a failure. These outcomes are then communicated back to the upper-layer caller.

Upper-layer callers need to determine whether to incorporate a retry mechanism based on these outcomes. This implies that when data transmission fails, upper-layer callers must handle the situation differently depending on the circumstances. For instance, if the failure is due to a disconnect in the connection, upper-layer callers can attempt to resend the data based on the result of getty's automatic reconnection. Alternatively, if the failure is caused by the sending buffer of the underlying operating system being full, the sender can implement its own retry mechanism to wait for the sending buffer to become available before attempting another transmission.

In summary, the data transmission interface of getty does not come with an inherent retry mechanism; instead, it is up to upper-layer callers to decide whether to implement retry logic based on specific situations. This design approach provides developers with greater flexibility in controlling the behavior of data transmission.

## LICENCE

Apache License 2.0

