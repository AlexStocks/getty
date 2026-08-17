# Issue #97 剩余确定性运行时问题修复设计

## 目标

修复 Issue #97 在当前 `master@cc9909dc9e0aab1307f553bc2a3d8400161be4e2` 上仍可由源码直接确认的两个确定性问题：

1. WSS 服务在正常关闭时，将 `http.Server.Serve` 返回的预期关闭错误升级为进程级 panic。
2. UDP 接收缓冲区大小计算使用尚未由 `recv` 填充的 `bufLen`，导致分支恒定按零值判断。

变更必须保持现有公开接口不变，并通过 Linux race 测试、静态检查和仓库测试验证。

## 范围

### 本批包含

- 修改 `transport/server.go` 的 WSS `Serve` 返回处理。
- 修改 `transport/session.go` 的 UDP 接收缓冲区大小计算。
- 在 `transport/server_test.go` 增加 WSS 正常启动和关闭的回归测试。
- 在 `transport/session_test.go` 增加 UDP 缓冲区大小边界测试和真实 UDP 接收路径测试。

### 本批不包含

- 不修改 `WithReconnectAttempts` 的语义。当前公开文档将其描述为最大重连尝试次数，现有测试也验证总尝试次数；把它改为连续失败次数需要单独设计和兼容性决策。
- 不修改 Issue #93 的 Bug 模板、自动回复或 Release Note workflow。
- 不重构 WS/WSS 服务生命周期之外的代码。
- 2026-08-17 review follow-up 已授权 commit 并 push 到现有 PR #108 分支；不 merge，也不修改或关闭 GitHub Issue。

## 设计

### 1. WSS 正常关闭不再 panic

当前 WSS event loop 对 `server.Serve(tls.NewListener(...))` 的任意非空错误执行 `panic(err)`。`http.Server.Serve` 在调用 `Shutdown` 或 `Close` 后会返回 `http.ErrServerClosed`，这是服务生命周期的正常结束信号。

修改后的行为：

- `errors.Is(err, http.ErrServerClosed)` 时直接退出 goroutine，不记录错误，不 panic。
- Server 已进入 Getty 自身关闭状态时，listener close 产生的返回同样作为预期退出处理。
- 其他 `Serve` 错误沿用非 TLS WS event loop 的容错方式：记录带地址和错误上下文的错误日志，然后退出 goroutine，不在后台服务 goroutine 中 panic 整个进程。
- 保留 `defer s.wg.Done()`，确保 `Server.Close()` 能完成等待。

不引入新的公开 API。错误分类逻辑优先保持在 `runWSSEventLoop` 附近，除非测试表明抽取小型私有 helper 能显著降低重复。

### 2. UDP 接收缓冲区大小使用目标变量计算

当前意图等价于在两个上限中取较小值：

```text
min(maxMsgLen + maxReadBufLen, 2 * maxMsgLen)
```

现有实现错误地将尚未赋值的 `bufLen` 与 `2 * maxMsgLen` 比较。修复将计算提取为包内私有函数：

```go
func udpReadBufferSize(maxMsgLen int32) int
```

函数规则：

- `maxMsgLen <= 0` 继续表示现有的“不限制消息长度”语义，UDP 接收使用 `64 KiB` 的有界回退；该大小足以容纳普通 UDP 数据报，同时不会向 `AcquireBytes` 传递零或负值。
- 正数输入先在 `int64` 中计算 `min(maxMsgLen + maxReadBufLen, 2 * maxMsgLen)`，避免 `int32` 加法和左移溢出。
- 计算结果上限为 `64 KiB`。UDP 数据报的长度字段只有 16 位，更大的单次接收分配没有可观察收益，只会扩大内存风险。
- `SetMaxMsgLen` 将非正值规范化为 `0`，并将超出 `int32` 的正数限制到 `math.MaxInt32`，避免公开 `int` 参数在保存时绕回负数。
- `handleUDPPackage` 只负责使用返回值申请和释放 buffer，不再保留尚未接收数据就读取 `bufLen` 的分支。

提取函数的目的是让边界规则可以直接测试，而不是暴露新的产品接口。

## 测试设计

### WSS 生命周期测试

新增集成回归测试，使用仓库已有 TLS 测试证书或测试内临时证书夹具：

1. 创建监听随机本地端口的 WSS Server。
2. 在 goroutine 中启动 `RunEventLoop`。
3. 使用测试证书作为 Root CA 建立一次真实 TLS 连接；握手成功证明 `http.Server.Serve` 已经接受连接，而不只是 `server.server` 字段已经赋值。
4. 关闭测试客户端连接，再调用 `Close()`。
5. 断言 `Close()` 和 event loop 在有界时间内返回。

在修复前，测试应因服务 goroutine 执行 `panic(http.ErrServerClosed)` 而失败；修复后应正常通过。测试不得通过 sleep 或字段发布猜测启动状态。review follow-up 通过临时恢复无条件 panic 的变异再次确认测试会变红，然后恢复生产实现。

### UDP 边界测试

对 `udpReadBufferSize` 使用表驱动测试，至少覆盖：

| `maxMsgLen` | 预期结果 | 说明 |
|---:|---:|---|
| `1` | `2` | 小消息由 `2 * maxMsgLen` 限制 |
| `4095` | `8190` | 低于交叉点一字节 |
| `4096` | `8192` | 两个公式在交叉点相等 |
| `4097` | `8193` | 高于交叉点后由 `maxMsgLen + 4096` 限制 |
| `0` | `64 * 1024` | 未设置限制时使用完整 UDP 数据报回退 |
| `-1` | `64 * 1024` | 防御负值，不产生负分配 |
| `128 * 1024` | `64 * 1024` | 大配置受 UDP 数据报上限约束 |
| `math.MaxInt32` | `64 * 1024` | 极值不发生 `int32` 溢出或超大分配 |

另增加真实 `net.UDPConn`、`session` 和 `handleUDPPackage` 测试。测试发送一个长度大于 helper 结果的数据报，并由真实 Reader 记录收到的切片长度。把生产分配临时恢复为旧的 `bufLen` 死分支时，该测试必须失败；恢复 helper 调用后必须通过。这条变异验证保证测试覆盖生产调用链，而不只覆盖纯函数。

## 验证

实现完成后在 WSL/Linux、Go 1.25.1 下依次运行：

```bash
go test ./transport -run '^(TestWSSServerCloseDoesNotPanic|TestUDPReadBufferSize|TestSetMaxMsgLenNormalizesLimits|TestHandleUDPPackageUsesConfiguredReadBuffer)$' -count=1
go test -race ./transport -run '^(TestWSSServerCloseDoesNotPanic|TestUDPReadBufferSize|TestSetMaxMsgLenNormalizesLimits|TestHandleUDPPackageUsesConfiguredReadBuffer)$' -count=1
go test -race ./transport -count=1
go vet ./...
go test ./... -count=1
```

若仓库级命令因为既有基线、环境或超时失败，必须区分本次新增失败和环境/基线失败，不得通过修改或跳过测试来制造通过结果。

## 完成标准

- WSS 正常关闭路径不产生 panic，并能完成 WaitGroup 等待。
- WSS 回归测试通过真实 TLS 握手证明 `Serve` 已经进入服务状态。
- 非预期 WSS `Serve` 错误仍被记录，不静默吞掉。
- UDP buffer 计算不再读取接收前的 `bufLen`。
- UDP buffer 对非正值和 `int32` 极值始终返回正数、有界结果。
- UDP 生产调用链测试能杀死恢复旧分配逻辑的变异。
- 新测试经过明确的红灯和绿灯阶段。
- WSL/Linux race 测试、静态检查和适用的仓库测试获得新鲜验证结果。
- 用户原始 checkout 和其中的未跟踪内容保持不变。
