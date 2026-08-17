# Issue #97 剩余确定性运行时问题实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法跟踪进度。原始任务 1-3 保留历史完成状态；2026-08-17 review follow-up 已授权 commit 并 push 到现有 PR #108 分支，但未授权 merge。

**目标：** 修复 WSS 正常关闭 panic 和 UDP 接收缓冲区计算死分支，并用先失败、后通过的回归测试锁定行为。

**架构：** WSS event loop 在 `Serve` 返回处区分预期关闭与非预期错误，正常关闭安静退出、其他错误记录后退出。UDP buffer 规则提取为包内私有纯函数，由 `handleUDPPackage` 调用并通过表驱动边界测试验证。

**技术栈：** Go 1.25.1、标准库 `net/http`/`crypto/tls`、Getty transport 包、Go test/race detector、WSL/Linux。

---

## 文件结构

- 修改 `transport/server_test.go`：增加 WSS 启动后正常关闭的集成回归测试。
- 修改 `transport/server.go`：将 WSS `Serve` 返回分类为预期关闭或需记录的运行错误。
- 修改 `transport/session_test.go`：增加 UDP buffer 大小的表驱动边界测试和真实 UDP 接收路径测试。
- 修改 `transport/session.go`：使 `udpReadBufferSize` 对非正值、溢出和 UDP 物理上限保持安全，并规范化 `SetMaxMsgLen` 输入。
- 修改 `doc/superpowers/specs/2026-08-01-issue-97-remaining-runtime-fixes-design.md`：记录 review follow-up 的已批准设计。
- 修改本计划文件：保留原始执行记录，并追加 review follow-up 的 TDD、变异和发布步骤。

### 任务 1：WSS 正常关闭回归测试与最小修复

**文件：**
- 修改：`transport/server_test.go:301-318`
- 修改：`transport/server.go:20-31`
- 修改：`transport/server.go:481-537`

- [x] **步骤 1：编写失败的 WSS 正常关闭测试**

在 `transport/server_test.go` 的 `TestServer` 后加入：

```go
func TestWSSServerCloseDoesNotPanic(t *testing.T) {
	certPath, err := filepath.Abs("../examples/profiles/wss/server_cert/server.crt")
	if err != nil {
		t.Fatal(err)
	}
	keyPath, err := filepath.Abs("../examples/profiles/wss/server_cert/server.key")
	if err != nil {
		t.Fatal(err)
	}

	server := newServer(
		WSS_SERVER,
		WithLocalAddress("127.0.0.1:0"),
		WithWebsocketServerPath("/ws"),
		WithWebsocketServerCert(certPath),
		WithWebsocketServerPrivateKey(keyPath),
	)
	server.RunEventLoop(func(Session) error { return nil })

	deadline := time.Now().Add(time.Second)
	for {
		server.lock.RLock()
		serving := server.server != nil
		server.lock.RUnlock()
		if serving {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("WSS event loop did not publish its HTTP server")
		}
		time.Sleep(time.Millisecond)
	}

	closed := make(chan struct{})
	go func() {
		server.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("WSS server Close did not return")
	}
}
```

该测试使用真实 listener、真实 TLS 证书和真实 `http.Server`，不 mock Getty 内部实现；它专门回归 Issue #97 的正常关闭 panic。

- [x] **步骤 2：运行测试，确认红灯来自当前 WSS panic**

运行：

```bash
wsl.exe --cd /mnt/d/test/github/review/getty-issue-97-fix -- bash -lc \
  "go test ./transport -run '^TestWSSServerCloseDoesNotPanic$' -count=1"
```

预期：FAIL，进程输出包含 `panic: http: Server closed`，证明测试命中了当前 `runWSSEventLoop` 的无条件 panic；如果失败来自证书、端口或启动超时，先修正测试夹具并重新取得正确红灯。

- [x] **步骤 3：实现最小 WSS 错误分类**

在 `transport/server.go` 标准库 import 组增加：

```go
"errors"
```

将 WSS `Serve` 返回处理替换为：

```go
	err = server.Serve(tls.NewListener(s.streamListener, config))
	if err != nil && !errors.Is(err, http.ErrServerClosed) && !s.IsClosed() {
		log.Errorf("http.server.Serve(addr{%s}) = err:%+v", s.addr, perrors.WithStack(err))
	}
```

删除 `panic(err)`。不修改证书加载错误，因为它们发生在启动配置阶段，不属于正常关闭问题。

- [x] **步骤 4：运行 WSS 测试，确认绿灯**

运行：

```bash
wsl.exe --cd /mnt/d/test/github/review/getty-issue-97-fix -- bash -lc \
  "go test ./transport -run '^TestWSSServerCloseDoesNotPanic$' -count=1"
```

预期：PASS，且没有 panic 或错误日志。

- [x] **步骤 5：检查任务 1 的变更边界**

运行：

```powershell
git diff --check
git diff -- transport/server.go transport/server_test.go
git status --short
```

预期：只出现 WSS 测试和错误分类所需变更，以及已批准的规格/计划文件；不 commit。

### 任务 2：UDP buffer 边界测试与最小修复

**文件：**
- 修改：`transport/session_test.go:29-33`
- 修改：`transport/session.go:48-64`
- 修改：`transport/session.go:914-937`

- [x] **步骤 1：编写失败的 UDP buffer 表驱动测试**

在 `transport/session_test.go` 的包级测试辅助类型之前加入：

```go
func TestUDPReadBufferSize(t *testing.T) {
	tests := []struct {
		name      string
		maxMsgLen int32
		want      int
	}{
		{name: "tiny message", maxMsgLen: 1, want: 2},
		{name: "below crossover", maxMsgLen: maxReadBufLen - 1, want: 2 * (maxReadBufLen - 1)},
		{name: "at crossover", maxMsgLen: maxReadBufLen, want: 2 * maxReadBufLen},
		{name: "above crossover", maxMsgLen: maxReadBufLen + 1, want: 2*maxReadBufLen + 1},
		{name: "large message", maxMsgLen: 128 * 1024, want: 128*1024 + maxReadBufLen},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := udpReadBufferSize(tt.maxMsgLen); got != tt.want {
				t.Fatalf("udpReadBufferSize(%d) = %d, want %d", tt.maxMsgLen, got, tt.want)
			}
		})
	}
}
```

一个表驱动测试覆盖同一计算规则的五个输入变体，避免重复测试体。

- [x] **步骤 2：运行测试，确认红灯来自 helper 缺失**

运行：

```bash
wsl.exe --cd /mnt/d/test/github/review/getty-issue-97-fix -- bash -lc \
  "go test ./transport -run '^TestUDPReadBufferSize$' -count=1"
```

预期：FAIL，编译错误包含 `undefined: udpReadBufferSize`。这证明生产 helper 尚不存在。

- [x] **步骤 3：实现最小 UDP buffer 计算并替换错误分支**

在 `transport/session.go` 常量块后加入：

```go
func udpReadBufferSize(maxMsgLen int32) int {
	maxBufLen := int(maxMsgLen + maxReadBufLen)
	if doubledMaxMsgLen := int(maxMsgLen << 1); doubledMaxMsgLen < maxBufLen {
		return doubledMaxMsgLen
	}
	return maxBufLen
}
```

在 `handleUDPPackage` 中删除局部变量 `maxBufLen`，并将：

```go
	maxBufLen = int(s.maxMsgLen + maxReadBufLen)
	if int(s.maxMsgLen<<1) < bufLen {
		maxBufLen = int(s.maxMsgLen << 1)
	}
	bufp = gxbytes.AcquireBytes(maxBufLen)
```

替换为：

```go
	bufp = gxbytes.AcquireBytes(udpReadBufferSize(s.maxMsgLen))
```

- [x] **步骤 4：运行 UDP 测试，确认绿灯**

运行：

```bash
wsl.exe --cd /mnt/d/test/github/review/getty-issue-97-fix -- bash -lc \
  "go test ./transport -run '^TestUDPReadBufferSize$' -count=1"
```

预期：PASS，五个子测试全部通过。

- [x] **步骤 5：运行两个回归测试的普通与 race 版本**

运行：

```bash
wsl.exe --cd /mnt/d/test/github/review/getty-issue-97-fix -- bash -lc \
  "go test ./transport -run '^(TestWSSServerCloseDoesNotPanic|TestUDPReadBufferSize)$' -count=1 && go test -race ./transport -run '^(TestWSSServerCloseDoesNotPanic|TestUDPReadBufferSize)$' -count=1"
```

预期：两个命令均 PASS，race detector 不报告竞态。

- [x] **步骤 6：检查任务 2 的变更边界**

运行：

```powershell
git diff --check
git diff -- transport/session.go transport/session_test.go
git status --short
```

预期：只出现 UDP helper、调用替换和表驱动测试；不 commit。

### 任务 3：测试质量门禁与完整验证

**文件：**
- 审查：`transport/server_test.go`
- 审查：`transport/session_test.go`
- 验证：全部已修改文件

- [x] **步骤 1：按 test-guard 审查新测试**

逐项确认：

- WSS 测试断言真实可观察行为，没有 mock 内部 helper。
- WSS 测试只覆盖正常关闭场景，并明确对应 Issue #97。
- UDP 的五个输入变体合并在一个表驱动测试中。
- 测试名称描述场景和期望，不测试 Go/http 框架自身保证。
- 没有仅为测试向生产类型添加公开方法。

若发现违反规则，先修改测试并重新运行对应红绿验证。

- [x] **步骤 2：运行 transport race 测试**

运行：

```bash
wsl.exe --cd /mnt/d/test/github/review/getty-issue-97-fix -- bash -lc \
  "go test -race ./transport -count=1"
```

预期：PASS，无 data race。

- [x] **步骤 3：运行静态检查**

运行：

```bash
wsl.exe --cd /mnt/d/test/github/review/getty-issue-97-fix -- bash -lc \
  "go vet ./..."
```

预期：退出码 0，无 vet 诊断。

- [x] **步骤 4：运行全仓测试**

运行：

```bash
wsl.exe --cd /mnt/d/test/github/review/getty-issue-97-fix -- bash -lc \
  "go test ./... -count=1"
```

预期：所有有测试的包 PASS，无失败包。

- [x] **步骤 5：最终 diff、格式和状态检查**

运行：

```powershell
git diff --check
git status --short --branch
git diff --stat
git diff -- transport/server.go transport/server_test.go transport/session.go transport/session_test.go
```

预期：无空白错误；生产代码和测试只覆盖原始批准范围。

### 任务 4：2026-08-17 review follow-up 测试红灯

**文件：**
- 修改：`transport/server_test.go`
- 修改：`transport/session_test.go`

- [ ] **步骤 1：用真实 TLS 握手替换 WSS 字段发布屏障**

读取 `server.crt`，加入测试 Root CA，并在 `server.RunEventLoop` 返回后执行：

```go
certPEM, err := os.ReadFile(certPath)
if err != nil {
	t.Fatal(err)
}
roots := x509.NewCertPool()
if !roots.AppendCertsFromPEM(certPEM) {
	t.Fatal("failed to add WSS test certificate to root pool")
}

conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", server.Listener().Addr().String(), &tls.Config{
	MinVersion: tls.VersionTLS12,
	RootCAs:    roots,
})
if err != nil {
	server.Close()
	t.Fatalf("WSS TLS handshake failed: %v", err)
}
if err := conn.Close(); err != nil {
	t.Fatal(err)
}
```

删除轮询 `server.server != nil` 的 readiness loop。真实 TLS 握手成功才允许测试调用 `server.Close()`。

- [ ] **步骤 2：增加 UDP 非正值、极值和生产调用链测试**

将 `TestUDPReadBufferSize` 的大消息预期改为 `maxUDPReadBufferSize`，并增加 `0`、`-1`、`math.MaxInt32`。增加一个 Reader，把收到的切片长度写入有缓冲 channel；真实 UDP 测试使用 `maxMsgLen=1`、发送 3 字节，并断言 Reader 收到 `udpReadBufferSize(1)` 即 2 字节：

```go
type udpReadSizeReader struct {
	readLen chan int
}

func (r *udpReadSizeReader) Read(_ Session, data []byte) (any, int, error) {
	r.readLen <- len(data)
	return nil, 0, errTestReadFailure
}
```

测试必须启动真实 `handleUDPPackage`，在断言后关闭 UDP listener，并有界等待 handler 返回；不向生产代码添加测试 hook。

- [ ] **步骤 3：运行边界测试并确认红灯原因**

运行：

```bash
go test ./transport -run '^(TestUDPReadBufferSize|TestHandleUDPPackageUsesConfiguredReadBuffer)$' -count=1
```

预期：`TestUDPReadBufferSize` 因 `udpReadBufferSize(0)` 返回 0、负值或极值溢出而 FAIL；真实生产路径子测试可以 PASS。失败必须来自缺失边界行为，不得来自测试夹具、端口或超时。

### 任务 5：UDP 最小修复与测试绿灯

**文件：**
- 修改：`transport/session.go`
- 测试：`transport/session_test.go`

- [ ] **步骤 1：实现安全、有界的 UDP buffer 计算**

在常量块增加 `maxUDPReadBufferSize = 64 * 1024`，并将 helper 改为：

```go
func udpReadBufferSize(maxMsgLen int32) int {
	if maxMsgLen <= 0 {
		return maxUDPReadBufferSize
	}

	bufferSize := int64(maxMsgLen) + int64(maxReadBufLen)
	if doubledMaxMsgLen := int64(maxMsgLen) * 2; doubledMaxMsgLen < bufferSize {
		bufferSize = doubledMaxMsgLen
	}
	if bufferSize > maxUDPReadBufferSize {
		return maxUDPReadBufferSize
	}
	return int(bufferSize)
}
```

`SetMaxMsgLen` 将 `length <= 0` 保存为 0，将超过 `math.MaxInt32` 的正数保存为 `math.MaxInt32`，其余值按现有 `int32` 字段保存。公开方法签名不变。

- [ ] **步骤 2：运行目标测试确认绿灯**

运行：

```bash
go test ./transport -run '^(TestWSSServerCloseDoesNotPanic|TestUDPReadBufferSize|TestSetMaxMsgLenNormalizesLimits|TestHandleUDPPackageUsesConfiguredReadBuffer)$' -count=1
```

预期：四个测试 PASS，WSS 测试完成真实 TLS 握手，UDP handler 在关闭 listener 后有界返回。

- [ ] **步骤 3：验证两个回归测试能杀死对应变异**

先将 `runWSSEventLoop` 的错误分类临时替换为 `if err != nil { panic(err) }`，运行 `TestWSSServerCloseDoesNotPanic`，预期出现 `panic: http: Server closed`；立即恢复文件。

再将 `handleUDPPackage` 的分配临时恢复为旧逻辑：

```go
maxBufLen := int(s.maxMsgLen + maxReadBufLen)
if int(s.maxMsgLen<<1) < bufLen {
	maxBufLen = int(s.maxMsgLen << 1)
}
bufp = gxbytes.AcquireBytes(maxBufLen)
```

运行 `TestHandleUDPPackageUsesConfiguredReadBuffer`，预期收到 3 字节而不是 2 字节并 FAIL；立即恢复文件。恢复后重跑步骤 2 中相同的四个目标测试并要求 PASS。

### 任务 6：review follow-up 完整验证与发布

**文件：**
- 验证：全部修改文件
- GitHub：PR #108 当前 Head、检查和八个原 review 线程

- [ ] **步骤 1：运行格式、race、静态和全仓门禁**

```bash
gofmt -w transport/server.go transport/server_test.go transport/session.go transport/session_test.go
git diff --check
go test -race ./transport -run '^(TestWSSServerCloseDoesNotPanic|TestUDPReadBufferSize|TestSetMaxMsgLenNormalizesLimits|TestHandleUDPPackageUsesConfiguredReadBuffer)$' -count=20
go test -race ./transport -count=1
go vet ./...
go test ./... -count=1
```

每条命令必须读取实际退出码和输出；Windows Go 1.26.2 的既有 `TestTCPClient` 基线失败单独记录，不修改该无关测试。

- [ ] **步骤 2：检查范围并 commit**

检查 `git status --short`、`git diff --stat`、完整 diff 和 `git diff --check`。只暂存两份文档、`transport/server.go`、`transport/server_test.go`、`transport/session.go`、`transport/session_test.go`，使用符合本地 Lore hook 的叙述式 commit message、Signed-off-by 和 `Co-authored-by: OmX <omx@oh-my-codex.dev>`。

- [ ] **步骤 3：推送并复核最终 Head**

推送 `codex/fix-issue-97-remaining`，重新获取 PR 的 `headRefOid`、完整检查、review decision、顶层评论和所有 review threads。Head 必须等于本地提交，检查失败或新反馈不得被旧证据覆盖。

- [ ] **步骤 4：在原线程回复并核对状态**

使用 `repos/AlexStocks/getty/pulls/108/comments/{id}/replies` 回复对应行内线程，说明具体修复和验证；同根因线程分别回复但不新建重复顶层评论。回复后重新获取 `isResolved`、`isOutdated`；不代替 reviewer Resolve，也不 merge PR。
