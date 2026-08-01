# GitHub CI 全面加固实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 `superpowers:subagent-driven-development`（推荐）或 `superpowers:executing-plans` 逐任务实现此计划。使用复选框（`- [ ]`）跟踪步骤；每个任务都要先取得失败或缺口证据，再实施最小改动并验证。不得修改 PR #108 的 Getty 运行时源码，不得修改 branch protection，不得 force push。

**目标：** 在 PR #108 中把 Getty 的 GitHub CI 改造成可复现、失败可传播、最小权限、供应链可审计的门禁，并增加 race、真实跨平台构建、CodeQL 与 Dependabot；同时删除已失效且暴露明文凭据的 Travis 配置。

**架构：** 主 `CI` workflow 负责 license、格式、单测/coverage、lint、race 和三平台构建；独立 `CodeQL` workflow 负责安全分析；Dependabot 负责 Go module 与 GitHub Actions 更新。Makefile 提供本地与 CI 共用的确定性入口。所有 Action 固定到核验过的完整 commit SHA，Go 缓存只由 `setup-go` 管理，Codecov 使用 OIDC 并在上传失败时使 job 失败。

**技术栈：** GitHub Actions、Go 1.25、GNU Make/Bash、`actionlint v1.7.12`、Codecov Action v7 OIDC、GitHub CodeQL Action v3、Dependabot、WSL/Linux 与 GitHub-hosted Ubuntu/Windows/macOS runner。

---

## 文件结构与职责

- 修改 `.github/workflows/github-actions.yml`：主 CI 门禁、最小权限、并发取消、超时、唯一缓存、OIDC coverage、race 与三平台构建。
- 新增 `.github/workflows/codeql.yml`：Go CodeQL pull request、push 和每周扫描。
- 新增 `.github/dependabot.yml`：Go module 与 GitHub Actions 的受控自动更新。
- 修改 `Makefile`：确定性 `test`、只读结果门禁 `check-fmt`、独立 `test-race` 和固定工具版本。
- 修改 `README.md`：将 Travis badge 替换为 GitHub Actions `CI` badge。
- 修改 `README_CN.md`：同步英文 README 的 CI badge。
- 删除 `.travis.yml`：从当前树移除失效 Travis 配置及其中的明文凭据；不复述、不调用凭据。
- 保留 `doc/superpowers/specs/2026-08-01-github-ci-hardening-design.md`：用户已批准的设计边界和验收依据。
- 新增本计划：记录逐步实现、验证、提交、push 和 GitHub 实时复检流程。

## 固定基准

实现开始前重新查询；只有结果仍匹配时才能继续：

```text
PR: AlexStocks/getty#108
Base: master@cc9909dc9e0aab1307f553bc2a3d8400161be4e2
Remote Head branch: codex/fix-issue-97-remaining
Remote Head SHA: 087714342a09f1cc2318bee9d570c2b6ed028044
Approved design commit: 7539afef7d495ed43f94e0ae488d8da010b9a7f5
```

本次核验的 Action 提交：

```text
actions/checkout@v7: 3d3c42e5aac5ba805825da76410c181273ba90b1
actions/setup-go@v6: 924ae3a1cded613372ab5595356fb5720e22ba16
apache/skywalking-eyes@main: 315732dd4b8d3a015d8d9b91936b935a0b854817
codecov/codecov-action@v7: fb8b3582c8e4def4969c97caa2f19720cb33a72f
github/codeql-action@v3: a2983b8bed1923f44751c5c43237f479442827b3
```

即使计划中记录了 SHA，实施时也必须再次通过 GitHub API 查询对应版本引用；若上游引用移动，记录新旧值、核验 release/tag 后再更新计划内实际使用值，不得静默使用过期或未知提交。

### 任务 1：实时租约与旧门禁缺口基线

**文件：**
- 读取：PR #108 GitHub 实时状态
- 写入证据：`D:\test\github\review\AlexStocks-getty-pr-108\evidence\ci-preflight.json`
- 写入证据：`D:\test\github\review\AlexStocks-getty-pr-108\evidence\ci-old-policy-gaps.txt`

- [ ] **步骤 1：确认 PR 仍可实施且远端 Head 未漂移**

在 PowerShell 中运行：

```powershell
gh pr view 108 --repo AlexStocks/getty `
  --json number,state,headRefName,headRefOid,baseRefName,mergeable,mergeStateStatus,reviewDecision,statusCheckRollup `
  > D:\test\github\review\AlexStocks-getty-pr-108\evidence\ci-preflight.json

gh pr view 108 --repo AlexStocks/getty `
  --json state,headRefName,headRefOid,baseRefName `
  --jq 'select(.state == "OPEN" and .headRefName == "codex/fix-issue-97-remaining" and .headRefOid == "087714342a09f1cc2318bee9d570c2b6ed028044" and .baseRefName == "master") | .headRefOid'
```

预期：第二条命令只输出 `087714342a09f1cc2318bee9d570c2b6ed028044`。没有输出或 SHA 不同即停止，不得 push；先 fetch 并增量审查远端新增提交。

- [ ] **步骤 2：确认本地提交链只建立在远端 Head 上**

```powershell
git fetch origin codex/fix-issue-97-remaining
git merge-base --is-ancestor origin/codex/fix-issue-97-remaining HEAD
git log --oneline origin/codex/fix-issue-97-remaining..HEAD
git status --short --branch
```

预期：ancestor 检查退出码为 0；日志仅包含批准设计和本计划提交；工作树干净。

- [ ] **步骤 3：保存旧配置缺口的可复验基线**

```powershell
@(
  '--- workflow gaps ---'
  (rg -n 'setup-go@|actions/cache@|codecov\.io/bash|@main|permissions:|concurrency:|timeout-minutes:|-race' .github\workflows\github-actions.yml)
  '--- makefile gaps ---'
  (rg -n 'go env -w|go test|imports-formatter@|check-fmt|test-race' Makefile)
  '--- travis references ---'
  (rg -n 'travis-ci' README.md README_CN.md)
) | Set-Content -Encoding utf8 D:\test\github\review\AlexStocks-getty-pr-108\evidence\ci-old-policy-gaps.txt
```

预期：证据能定位 setup-go 在 checkout 前、第二套 cache、远程 Codecov bash uploader、`@main`、`go env -w`、`@latest` 和 Travis badge。不得把 `.travis.yml` 中的凭据值写入证据。

### 任务 2：Makefile 确定性门禁

**文件：**
- 修改：`Makefile:24-55`
- 验证副本：`D:\test\github\review\AlexStocks-getty-pr-108\probes\ci-makefile-gates`

- [ ] **步骤 1：证明当前 Makefile 缺少新入口且会写用户级 Go 配置**

```bash
wsl.exe --cd /mnt/d/test/github/review/AlexStocks-getty-pr-108/source -- bash -lc \
  "make -n test | tee ../evidence/make-test-before.txt; ! make -n check-fmt; ! make -n test-race"
```

预期：`make -n test` 输出包含 `go env -w GOTOOLCHAIN=...`；`check-fmt` 与 `test-race` 报 `No rule to make target`，两个否定命令因此成功。

- [ ] **步骤 2：补全 phony、help 与确定性目标**

将 Makefile 中目标声明和命令调整为：

```make
.PHONY: help test test-race fmt check-fmt clean lint install-golangci-lint install-imports-formatter

help:
	@echo "Available commands:"
	@echo "  test       - Run unit tests with coverage"
	@echo "  test-race  - Run transport tests with the race detector"
	@echo "  fmt        - Format code"
	@echo "  check-fmt  - Verify that formatting produces no diff"
	@echo "  lint       - Run go vet and golangci-lint"
	@echo "  clean      - Clean generated test files"

# Run unit tests with a command-scoped toolchain selection.
test: clean
	GOTOOLCHAIN=go1.25.0+auto go test ./... -count=1 -coverprofile=coverage.txt -covermode=atomic

# Run the concurrency-sensitive transport package under the race detector.
test-race:
	GOTOOLCHAIN=go1.25.0+auto go test -race ./transport -count=1

fmt: install-imports-formatter
	go fmt ./... && GOROOT=$(shell go env GOROOT) imports-formatter

check-fmt: fmt
	@git diff --exit-code -- . ':!coverage.txt' || { \
		echo "Formatting changes are required. Run 'make fmt'."; \
		exit 1; \
	}

# Clean generated test files.
clean:
	rm -rf coverage.txt

# Run golangci-lint.
lint: install-golangci-lint
	go vet ./...
	golangci-lint run ./... --timeout=10m

install-golangci-lint:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.4.0

install-imports-formatter:
	go install github.com/dubbogo/tools/cmd/imports-formatter@v1.0.10
```

不要修改 `.DEFAULT_GOAL`、`.SHELLFLAGS` 或当前清理文件范围。`test` 不得调用 `go env -w`。

- [ ] **步骤 3：验证命令展开没有全局写入且版本固定**

```bash
wsl.exe --cd /mnt/d/test/github/review/AlexStocks-getty-pr-108/source -- bash -lc \
  "make -n test test-race install-imports-formatter | tee ../evidence/make-targets-after.txt; ! rg -n 'go env -w|imports-formatter@latest' Makefile"
```

预期：输出包含命令级 `GOTOOLCHAIN=go1.25.0+auto`、两个 `-count=1` 和 `imports-formatter@v1.0.10`，反向检索无匹配。

- [ ] **步骤 4：提交 Makefile 改动**

```powershell
git diff --check -- Makefile
git add Makefile
git commit -m "build: make CI checks deterministic"
```

预期：只提交 `Makefile`。

### 任务 3：重写主 CI workflow

**文件：**
- 修改：`.github/workflows/github-actions.yml`

- [ ] **步骤 1：建立会使旧 workflow 失败的政策检查**

```bash
wsl.exe --cd /mnt/d/test/github/review/AlexStocks-getty-pr-108/source -- bash -lc '
  set -eu
  workflow=.github/workflows/github-actions.yml
  ! rg -q "actions/cache@|codecov.io/bash|@(main|latest)([[:space:]#]|$)" "$workflow"
  first_checkout=$(rg -n "uses: actions/checkout@" "$workflow" | head -1 | cut -d: -f1)
  first_setup=$(rg -n "uses: actions/setup-go@" "$workflow" | head -1 | cut -d: -f1)
  test "$first_checkout" -lt "$first_setup"
'
```

预期：在修改前失败，原因至少包括旧 `actions/cache`、远程 uploader、`@main` 或 setup-go 排在 checkout 前。

- [ ] **步骤 2：用完整内容替换主 workflow**

`.github/workflows/github-actions.yml` 应为：

```yaml
name: CI

on:
  push:
    branches:
      - master
  pull_request:
    branches:
      - master

permissions:
  contents: read

concurrency:
  group: ${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}
  cancel-in-progress: true

jobs:
  license:
    name: Check License Header
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - name: Check out repository
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7

      - name: Check license headers
        uses: apache/skywalking-eyes/header@315732dd4b8d3a015d8d9b91936b935a0b854817 # main, verified 2026-08-01
        with:
          config: .licenserc.yaml
          mode: check

  test-and-lint:
    name: Test and Lint
    runs-on: ubuntu-latest
    timeout-minutes: 20
    permissions:
      contents: read
      id-token: write
    steps:
      - name: Check out repository
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7

      - name: Set up Go
        uses: actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16 # v6
        with:
          go-version-file: go.mod
          cache-dependency-path: go.sum

      - name: Verify modules
        run: go mod verify

      - name: Check format
        run: make check-fmt

      - name: Run unit tests with coverage
        run: make test

      - name: Run lint
        run: make lint

      - name: Upload coverage
        uses: codecov/codecov-action@fb8b3582c8e4def4969c97caa2f19720cb33a72f # v7
        with:
          use_oidc: true
          fail_ci_if_error: true
          files: ./coverage.txt
          disable_search: true

  race:
    name: Race
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - name: Check out repository
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7

      - name: Set up Go
        uses: actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16 # v6
        with:
          go-version-file: go.mod
          cache-dependency-path: go.sum

      - name: Verify modules
        run: go mod verify

      - name: Run race detector
        run: make test-race

  build:
    name: Build (${{ matrix.os }})
    runs-on: ${{ matrix.os }}
    timeout-minutes: 15
    strategy:
      fail-fast: false
      matrix:
        os:
          - ubuntu-latest
          - windows-latest
          - macos-latest
    steps:
      - name: Check out repository
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7

      - name: Set up Go
        uses: actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16 # v6
        with:
          go-version-file: go.mod
          cache-dependency-path: go.sum

      - name: Verify modules
        run: go mod verify

      - name: Build packages
        run: go build ./...
```

不得恢复独立 `actions/cache`、CodeCov bash uploader 或任何可变 Action 引用。License job 不需要显式 `GITHUB_TOKEN` 环境变量；GitHub 会为 Action 提供最小权限 token 上下文。

- [ ] **步骤 3：运行政策检查并验证全部 Action 引用**

```bash
wsl.exe --cd /mnt/d/test/github/review/AlexStocks-getty-pr-108/source -- bash -lc '
  set -eu
  workflow=.github/workflows/github-actions.yml
  ! rg -n "actions/cache@|codecov.io/bash|@(main|latest)([[:space:]#]|$)" "$workflow"
  first_checkout=$(rg -n "uses: actions/checkout@" "$workflow" | head -1 | cut -d: -f1)
  first_setup=$(rg -n "uses: actions/setup-go@" "$workflow" | head -1 | cut -d: -f1)
  test "$first_checkout" -lt "$first_setup"
  python3 - <<"PY"
import pathlib
import re

for path in pathlib.Path(".github/workflows").glob("*.yml"):
    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        match = re.search(r"\buses:\s+\S+@([^\s#]+)", line)
        if match and not re.fullmatch(r"[0-9a-f]{40}", match.group(1)):
            raise SystemExit(f"{path}:{number}: mutable action ref {match.group(1)}")
PY
'
```

预期：退出码 0；无 mutable ref、重复 cache 或远程 shell uploader。

- [ ] **步骤 4：提交主 workflow**

```powershell
git diff --check -- .github/workflows/github-actions.yml
git add .github/workflows/github-actions.yml
git commit -m "ci: harden tests race and platform builds"
```

预期：只提交主 workflow。

### 任务 4：新增 CodeQL workflow

**文件：**
- 新增：`.github/workflows/codeql.yml`

- [ ] **步骤 1：证明当前仓库没有 CodeQL workflow**

```powershell
Test-Path .github\workflows\codeql.yml
rg -n 'github/codeql-action' .github\workflows
```

预期：`Test-Path` 输出 `False`，`rg` 无匹配并返回 1。

- [ ] **步骤 2：新增固定 SHA、最小权限的 Go CodeQL workflow**

```yaml
name: CodeQL

on:
  push:
    branches:
      - master
  pull_request:
    branches:
      - master
  schedule:
    - cron: '30 1 * * 1'

permissions:
  contents: read

concurrency:
  group: ${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}
  cancel-in-progress: true

jobs:
  analyze:
    name: Analyze (Go)
    runs-on: ubuntu-latest
    timeout-minutes: 20
    permissions:
      actions: read
      contents: read
      security-events: write
    steps:
      - name: Check out repository
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7

      - name: Initialize CodeQL
        uses: github/codeql-action/init@a2983b8bed1923f44751c5c43237f479442827b3 # v3
        with:
          languages: go
          build-mode: autobuild

      - name: Build
        uses: github/codeql-action/autobuild@a2983b8bed1923f44751c5c43237f479442827b3 # v3

      - name: Analyze
        uses: github/codeql-action/analyze@a2983b8bed1923f44751c5c43237f479442827b3 # v3
```

- [ ] **步骤 3：用 actionlint 验证两个 workflow**

```bash
wsl.exe --cd /mnt/d/test/github/review/AlexStocks-getty-pr-108/source -- bash -lc \
  "GOTOOLCHAIN=go1.25.0+auto go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 -color"
```

预期：退出码 0，无 workflow 语法、表达式、shell 或 action 输入错误。若 actionlint 对 action metadata 的远程可见性有限，不能把该限制当成 GitHub 运行成功证据；仍须等待远端 check。

- [ ] **步骤 4：提交 CodeQL workflow**

```powershell
git add .github/workflows/codeql.yml
git commit -m "ci: add CodeQL analysis"
```

预期：只提交 `codeql.yml`。

### 任务 5：新增 Dependabot 配置并严格解析 YAML

**文件：**
- 新增：`.github/dependabot.yml`
- 新增临时验证器：`D:\test\github\review\AlexStocks-getty-pr-108\probes\validate-dependabot-yaml.go`

- [ ] **步骤 1：证明当前仓库没有 Dependabot 配置**

```powershell
Test-Path .github\dependabot.yml
```

预期：输出 `False`。

- [ ] **步骤 2：新增受控更新配置**

```yaml
version: 2
updates:
  - package-ecosystem: gomod
    directory: /
    target-branch: master
    schedule:
      interval: weekly
    open-pull-requests-limit: 5
    commit-message:
      prefix: deps

  - package-ecosystem: github-actions
    directory: /
    target-branch: master
    schedule:
      interval: monthly
    open-pull-requests-limit: 3
    commit-message:
      prefix: ci
```

- [ ] **步骤 3：用仓库已有 YAML 依赖执行严格解析和结构断言**

在镜像 `probes` 目录通过 `apply_patch` 创建以下一次性验证器，不提交到 PR：

```go
package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

type config struct {
	Version int `yaml:"version"`
	Updates []struct {
		Ecosystem string `yaml:"package-ecosystem"`
		Directory string `yaml:"directory"`
		Target    string `yaml:"target-branch"`
		Schedule  struct {
			Interval string `yaml:"interval"`
		} `yaml:"schedule"`
		Limit int `yaml:"open-pull-requests-limit"`
		CommitMessage struct {
			Prefix string `yaml:"prefix"`
		} `yaml:"commit-message"`
	} `yaml:"updates"`
}

func main() {
	data, err := os.ReadFile(".github/dependabot.yml")
	if err != nil {
		panic(err)
	}
	var cfg config
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		panic(err)
	}
	if cfg.Version != 2 || len(cfg.Updates) != 2 {
		panic(fmt.Sprintf("unexpected Dependabot structure: %+v", cfg))
	}
	want := map[string]struct {
		interval string
		limit    int
		prefix   string
	}{
		"gomod":          {interval: "weekly", limit: 5, prefix: "deps"},
		"github-actions": {interval: "monthly", limit: 3, prefix: "ci"},
	}
	for _, update := range cfg.Updates {
		expected, ok := want[update.Ecosystem]
		if !ok || update.Directory != "/" || update.Target != "master" ||
			update.Schedule.Interval != expected.interval || update.Limit != expected.limit ||
			update.CommitMessage.Prefix != expected.prefix {
			panic(fmt.Sprintf("unexpected update entry: %+v", update))
		}
		delete(want, update.Ecosystem)
	}
	if len(want) != 0 {
		panic(fmt.Sprintf("missing ecosystems: %+v", want))
	}
}
```

运行：

```bash
wsl.exe --cd /mnt/d/test/github/review/AlexStocks-getty-pr-108/source -- bash -lc \
  "GOTOOLCHAIN=go1.25.0+auto go run ../probes/validate-dependabot-yaml.go"
```

预期：退出码 0，无输出；严格解析会拒绝未知字段。

- [ ] **步骤 4：提交 Dependabot 配置**

```powershell
git add .github/dependabot.yml
git commit -m "ci: add Dependabot updates"
```

预期：只提交 `.github/dependabot.yml`；临时验证器留在镜像 `probes`，不进入 Git index。

### 任务 6：替换 badge 并删除 Travis 当前树配置

**文件：**
- 修改：`README.md:5`
- 修改：`README_CN.md:5`
- 删除：`.travis.yml`

- [ ] **步骤 1：再次确认 Travis 不在当前 PR checks 中**

```powershell
$checks = gh pr view 108 --repo AlexStocks/getty --json statusCheckRollup --jq '.statusCheckRollup[].name'
$checks
if ($checks -match '(?i)travis') { throw 'Travis check is still active; stop deletion' }
```

预期：现有 check 名称中没有 Travis。若出现 Travis，停止删除并重新评估迁移覆盖。

- [ ] **步骤 2：只比较 Travis 命令范围，不输出敏感值**

```powershell
Select-String -Path .travis.yml -Pattern '^language:|^os:|^go:|^install:|^script:|^after_success:|^\s*-\s+(go|make)\s' |
  ForEach-Object { '{0}:{1}' -f $_.LineNumber,$_.Line.Trim() }
```

预期：有效范围为格式、测试/coverage 和 race；新主 workflow/Makefile 已覆盖这些门禁，并额外增加 lint、模块验证与跨平台构建。不得运行、复制或打印 uploader/webhook 行。

- [ ] **步骤 3：替换两个 README badge**

把两个 README 的 Travis badge 行替换为：

```markdown
[![CI](https://github.com/AlexStocks/getty/actions/workflows/github-actions.yml/badge.svg?branch=master)](https://github.com/AlexStocks/getty/actions/workflows/github-actions.yml)
```

- [ ] **步骤 4：删除 `.travis.yml`**

使用 `apply_patch` 删除整个文件。删除仅从当前树移除凭据，不能清除 Git 历史；不得在 commit message 或 PR 评论中复制任何 token。

- [ ] **步骤 5：验证当前树没有 Travis 引用和已知敏感配置键**

```powershell
if (Test-Path .travis.yml) { throw '.travis.yml still exists' }
if (rg -n 'travis-ci' README.md README_CN.md) { throw 'Travis badge remains' }
rg -n 'actions/workflows/github-actions\.yml/badge\.svg' README.md README_CN.md
```

预期：前两个检查通过；最后一条在两个 README 各匹配一次。

- [ ] **步骤 6：提交 README 与 Travis 清理**

```powershell
git add README.md README_CN.md .travis.yml
git commit -m "docs: replace Travis CI references"
```

预期：提交包含两个 badge 替换和 `.travis.yml` 删除，不包含其他文件。

### 任务 7：本地静态、变异和 Go 验证

**文件：**
- 读取：全部实施文件
- 验证副本：`D:\test\github\review\AlexStocks-getty-pr-108\probes\ci-check-fmt-clean`
- 验证副本：`D:\test\github\review\AlexStocks-getty-pr-108\probes\ci-check-fmt-mutation`
- 输出：`D:\test\github\review\AlexStocks-getty-pr-108\evidence\ci-local-validation-*.txt`

- [ ] **步骤 1：对全部 workflow 运行 actionlint**

```bash
wsl.exe --cd /mnt/d/test/github/review/AlexStocks-getty-pr-108/source -- bash -lc \
  "GOTOOLCHAIN=go1.25.0+auto go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 -color |& tee ../evidence/ci-local-validation-actionlint.txt"
```

预期：退出码 0，无诊断。

- [ ] **步骤 2：执行 workflow 供应链政策检查**

```bash
wsl.exe --cd /mnt/d/test/github/review/AlexStocks-getty-pr-108/source -- bash -lc '
  set -eu
  ! rg -n "actions/cache@|codecov.io/bash|curl[[:space:]].*\|[[:space:]]*(ba)?sh|@(main|master|latest)([[:space:]#]|$)" .github/workflows Makefile
  python3 - <<"PY"
import pathlib
import re

for path in pathlib.Path(".github/workflows").glob("*.yml"):
    for number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        match = re.search(r"\buses:\s+\S+@([^\s#]+)", line)
        if match and not re.fullmatch(r"[0-9a-f]{40}", match.group(1)):
            raise SystemExit(f"{path}:{number}: mutable action ref {match.group(1)}")
PY
'
```

预期：退出码 0。注释中的版本标签允许存在，但 `uses:` 的实际 ref 必须是完整 SHA。

- [ ] **步骤 3：在干净独立 worktree 证明 `check-fmt` 绿灯**

```powershell
git worktree add --detach D:\test\github\review\AlexStocks-getty-pr-108\probes\ci-check-fmt-clean HEAD
wsl.exe --cd /mnt/d/test/github/review/AlexStocks-getty-pr-108/probes/ci-check-fmt-clean -- bash -lc `
  'PATH=/home/alex/bin/go1.25/bin:$PATH make check-fmt |& tee ../../evidence/ci-local-validation-check-fmt-clean.txt'
```

预期：退出码 0，probe worktree 的 `git status --porcelain` 为空。主 source worktree 不运行写入式 formatter。

- [ ] **步骤 4：用格式变异证明 `check-fmt` 会阻断**

```powershell
git worktree add --detach D:\test\github\review\AlexStocks-getty-pr-108\probes\ci-check-fmt-mutation HEAD
```

在 mutation worktree 中通过 `apply_patch` 将一个已跟踪 Go 函数签名改成 gofmt 会修复的格式，例如：

```diff
-func udpReadBufferSize(maxMsgLen int32) int {
+func udpReadBufferSize( maxMsgLen int32 ) int {
```

然后运行：

```powershell
wsl.exe --cd /mnt/d/test/github/review/AlexStocks-getty-pr-108/probes/ci-check-fmt-mutation -- bash -lc `
  'PATH=/home/alex/bin/go1.25/bin:$PATH make check-fmt |& tee ../../evidence/ci-local-validation-check-fmt-mutation.txt; test ${PIPESTATUS[0]} -ne 0'
```

预期：formatter 修复变异后，`git diff --exit-code` 使 `make check-fmt` 非零退出；日志输出具体 diff 和修复提示。该 probe 不提交、不 push。

- [ ] **步骤 5：运行模块、测试、race 与 lint**

```bash
wsl.exe --cd /mnt/d/test/github/review/AlexStocks-getty-pr-108/source -- bash -lc '
  set -o pipefail
  export PATH=/home/alex/bin/go1.25/bin:$PATH
  go version
  go mod verify
  make test
  make test-race
  make lint
' |& tee /mnt/d/test/github/review/AlexStocks-getty-pr-108/evidence/ci-local-validation-go.txt
```

预期：Go 为 `go1.25.1 linux/amd64`；所有命令退出码 0；`coverage.txt` 是唯一预期生成文件。若失败，先按 `superpowers:systematic-debugging` 区分 PR 新增、Base 既有和环境问题，不得跳过失败。

- [ ] **步骤 6：执行跨编译补充验证**

```bash
wsl.exe --cd /mnt/d/test/github/review/AlexStocks-getty-pr-108/source -- bash -lc '
  set -eu -o pipefail
  export PATH=/home/alex/bin/go1.25/bin:$PATH
  GOOS=windows GOARCH=amd64 go build ./...
  GOOS=darwin GOARCH=amd64 go build ./...
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...
  CGO_ENABLED=0 GOOS=linux GOARCH=riscv64 go build ./...
' |& tee /mnt/d/test/github/review/AlexStocks-getty-pr-108/evidence/ci-local-validation-cross-build.txt
```

预期：退出码 0。交叉编译只是补充证据，不能替代远端 Windows/macOS runner。

- [ ] **步骤 7：检查 diff、index、意外文件和敏感值回流**

```powershell
git diff --check origin/codex/fix-issue-97-remaining...HEAD
git status --short --branch
git diff --name-status origin/codex/fix-issue-97-remaining...HEAD
git diff --stat origin/codex/fix-issue-97-remaining...HEAD
git grep -n -I -E 'travis-ci|codecov\.io/bash|go env -w GOTOOLCHAIN|imports-formatter@latest' -- . ':!doc/superpowers/specs/2026-08-01-github-ci-hardening-design.md' ':!doc/superpowers/plans/2026-08-01-github-ci-hardening.md'
```

预期：除 `coverage.txt` 外 source worktree无非预期生成文件；实际实施文件严格匹配设计。最后一条仅允许历史说明文档中的证据性文字，不允许生产配置出现旧模式。检查输出时不得复述任何已删除凭据。

### 任务 8：实现复核与必要修正

**文件：**
- 复核：本计划列出的全部变更文件
- 可能修改：只限 CI、Makefile、README 与设计/计划中已授权的文件

- [ ] **步骤 1：逐项对照批准设计的完成标准**

核对：

```text
[ ] 唯一 Go cache owner 是 setup-go v6
[ ] checkout 位于 setup-go 前
[ ] Test and Lint 具有 contents:read + id-token:write，其他 job 无多余权限
[ ] Codecov 使用 OIDC、显式 coverage 文件、fail_ci_if_error
[ ] Race 独立执行 transport race
[ ] Build matrix 使用真实 ubuntu/windows/macos runner
[ ] CodeQL 是独立 workflow，权限最小且 Action 固定 SHA
[ ] Dependabot 只有 gomod 与 github-actions 两个受控入口
[ ] Makefile 无 go env -w、无浮动工具版本，测试禁用缓存
[ ] 两个 README 使用 GitHub Actions badge
[ ] .travis.yml 从当前树删除
[ ] 未修改运行时 Go 源码、branch protection 或 GitHub ruleset
```

- [ ] **步骤 2：审阅提交边界和提交消息**

```powershell
git log --reverse --stat --oneline origin/codex/fix-issue-97-remaining..HEAD
git show --check --stat HEAD
```

预期：每个提交单一目的；没有凭据、生成二进制、`coverage.txt`、probe 或 evidence 进入提交。

- [ ] **步骤 3：如果复核发现 CI 配置问题，先取得失败证据再修正**

只允许修正本计划范围内文件。每个修正运行直接相关的 actionlint、YAML、Makefile 或 Go 验证后，以普通提交记录：

例如 actionlint 发现 CodeQL build mode 配置错误时，只暂存该 workflow 并使用具体消息：

```powershell
git add .github/workflows/codeql.yml
git commit -m "ci: fix CodeQL build configuration"
```

不得 amend 已提交历史，不得用 force push。

### 任务 9：push 前最终实时复检与普通 push

**文件：**
- 读取：PR #108 实时状态、远端分支 SHA、本地提交链
- 不修改：branch protection、ruleset、review threads

- [ ] **步骤 1：执行 `verification-before-completion` 新鲜验证**

至少重新运行：

```bash
wsl.exe --cd /mnt/d/test/github/review/AlexStocks-getty-pr-108/source -- bash -lc '
  set -eu -o pipefail
  export PATH=/home/alex/bin/go1.25/bin:$PATH
  GOTOOLCHAIN=go1.25.0+auto go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
  go mod verify
  go test ./... -count=1
  go test -race ./transport -count=1
  go vet ./...
'
```

以及：

```powershell
git diff --check origin/codex/fix-issue-97-remaining...HEAD
git status --short --branch
```

预期：全部退出码 0；不得用较早日志替代该步骤的新鲜结果。

- [ ] **步骤 2：再次获取远端 Head 并执行显式 lease**

```powershell
git fetch origin codex/fix-issue-97-remaining
$remoteHead = git rev-parse origin/codex/fix-issue-97-remaining
$liveHead = gh pr view 108 --repo AlexStocks/getty --json state,headRefOid --jq 'select(.state == "OPEN") | .headRefOid'
if ($remoteHead -ne '087714342a09f1cc2318bee9d570c2b6ed028044' -or $liveHead -ne $remoteHead) {
  throw "Remote PR Head drifted; stop before push"
}
git merge-base --is-ancestor $remoteHead HEAD
```

预期：远端 Git ref、GitHub PR Head 和实施基准三者相同；ancestor 检查成功。

- [ ] **步骤 3：普通 push 当前分支**

```powershell
git push origin HEAD:codex/fix-issue-97-remaining
```

预期：普通 fast-forward push 成功；不得添加 `--force` 或 `--force-with-lease`。

### 任务 10：等待并核验 GitHub 新 checks

**文件：**
- 写入证据：`D:\test\github\review\AlexStocks-getty-pr-108\evidence\ci-post-push-pr.json`
- 写入证据：`D:\test\github\review\AlexStocks-getty-pr-108\evidence\ci-post-push-checks.txt`
- 写入证据：各 workflow/job 的日志片段，仅保存无敏感值的诊断

- [ ] **步骤 1：获取 push 后新 Head 和 workflow runs**

```powershell
$newHead = gh pr view 108 --repo AlexStocks/getty --json headRefOid --jq .headRefOid
gh run list --repo AlexStocks/getty --branch codex/fix-issue-97-remaining --limit 20 `
  --json databaseId,workflowName,headSha,status,conclusion,url,createdAt `
  --jq ".[] | select(.headSha == \"$newHead\")"
```

预期：至少出现 `CI` 和 `CodeQL` 的新运行，Head 等于刚 push 的本地 `HEAD`。

- [ ] **步骤 2：等待当前 Head 的所有新运行完成**

通过 API 逐个等待上一步返回的 run ID：

```powershell
$headSha = gh pr view 108 --repo AlexStocks/getty --json headRefOid --jq .headRefOid
$runIds = gh run list --repo AlexStocks/getty --branch codex/fix-issue-97-remaining --limit 20 `
  --json databaseId,headSha --jq ".[] | select(.headSha == \"$headSha\") | .databaseId"
foreach ($runId in $runIds) {
  gh run watch $runId --repo AlexStocks/getty --exit-status
  if ($LASTEXITCODE -ne 0) { throw "GitHub Actions run $runId failed" }
}
```

预期：全部成功。若失败，下载精确失败 job 日志，按系统化调试区分配置、源码基线和外部服务问题；不得为了变绿而跳过门禁。

- [ ] **步骤 3：核对主 CI job 和真实 runner**

```powershell
$headSha = gh pr view 108 --repo AlexStocks/getty --json headRefOid --jq .headRefOid
$ciRunId = gh run list --repo AlexStocks/getty --branch codex/fix-issue-97-remaining --workflow CI --limit 20 `
  --json databaseId,headSha --jq ".[] | select(.headSha == \"$headSha\") | .databaseId" | Select-Object -First 1
if (-not $ciRunId) { throw 'CI run for current Head not found' }
gh run view $ciRunId --repo AlexStocks/getty --json headSha,status,conclusion,jobs,url `
  > D:\test\github\review\AlexStocks-getty-pr-108\evidence\ci-post-push-checks.txt
```

必须确认实际 job 包含并成功：

```text
Check License Header
Test and Lint
Race
Build (ubuntu-latest)
Build (windows-latest)
Build (macos-latest)
```

同时从 `Test and Lint` 日志确认：setup-go 在 checkout 后读取 `go.mod`/`go.sum`，没有第二个 `actions/cache`，coverage 上传没有 HTTP 400、tokenless upload 错误或被吞掉的失败。

- [ ] **步骤 4：核对 CodeQL result 上传**

```powershell
$headSha = gh pr view 108 --repo AlexStocks/getty --json headRefOid --jq .headRefOid
$codeqlRunId = gh run list --repo AlexStocks/getty --branch codex/fix-issue-97-remaining --workflow CodeQL --limit 20 `
  --json databaseId,headSha --jq ".[] | select(.headSha == \"$headSha\") | .databaseId" | Select-Object -First 1
if (-not $codeqlRunId) { throw 'CodeQL run for current Head not found' }
gh run view $codeqlRunId --repo AlexStocks/getty --log-failed
gh run view $codeqlRunId --repo AlexStocks/getty --json headSha,status,conclusion,jobs,url
```

预期：`Analyze (Go)` 成功，Head 与 PR 最新 Head 一致。若 GitHub 安全设置阻止上传，记录准确错误和所需外部设置，不把它伪装成源码缺陷。

- [ ] **步骤 5：保存最终 PR 状态并复核 Head 未变化**

```powershell
gh pr view 108 --repo AlexStocks/getty `
  --json number,state,headRefName,headRefOid,baseRefName,mergeable,mergeStateStatus,reviewDecision,statusCheckRollup `
  > D:\test\github\review\AlexStocks-getty-pr-108\evidence\ci-post-push-pr.json
git rev-parse HEAD
gh pr view 108 --repo AlexStocks/getty --json headRefOid --jq .headRefOid
```

预期：本地 HEAD 与 GitHub PR Head 完全一致。

### 任务 11：最终 Review、required checks 建议与收尾对账

**文件：**
- 读取：当前 PR 完整 files/diff、review comments、checks、branch protection/rulesets
- 可能更新：`D:\test\github\arch-practice\alg\openclaw\review-experience.md` 或 `review-AlexStocks-getty.md`，仅当本轮产生经过验证的新经验

- [ ] **步骤 1：重新保存完整 PR 文件列表和 Diff**

```powershell
gh api repos/AlexStocks/getty/pulls/108/files --paginate `
  > D:\test\github\review\AlexStocks-getty-pr-108\files.json
gh pr diff 108 --repo AlexStocks/getty `
  > D:\test\github\review\AlexStocks-getty-pr-108\pr.diff
```

逐文件增量审查 CI 改动，确认没有运行时源码漂移。若发现本轮 CI 变更引入的可定位问题，先本地修复、重新验证、普通 push，再重复任务 10；不要给自己的 CI 改动留下明知的 P0/P1。

- [ ] **步骤 2：检索 review threads 并保留 UDP P1 独立状态**

重新获取所有 review comments/threads，确认已存在的 UDP invalid-input 和生产调用路径测试缺口未因 CI 改造被误报为解决。除非用户另行授权，不修改 `transport/session.go`、`transport/session_test.go`，也不 resolve 对应线程。

- [ ] **步骤 3：读取而不修改 branch protection/rulesets**

根据 push 后真实 check 名称输出建议 required checks；预计为：

```text
Check License Header
Test and Lint
Race
Build (ubuntu-latest)
Build (windows-latest)
Build (macos-latest)
Analyze (Go)
```

实际名称以 GitHub API 返回为准。本任务禁止调用 branch protection/ruleset 写 API；需要用户单独授权。

- [ ] **步骤 4：明确外部凭据收尾**

收尾必须要求仓库维护者在外部系统轮换或吊销旧 `.travis.yml` 中暴露的 Codecov upload token 和第三方 webhook access tokens。只说明凭据类型和风险，不复述值。删除当前文件不等于清除 Git 历史。

- [ ] **步骤 5：按工业 Review 协议输出最终对账**

最终报告必须包含：

```text
结论：PR #108 因现存 UDP P1 仍为 🚫 不可 Merge；CI 改造本身的 checks 结果单独列明。
PR、镜像路径、WSL 路径、最终 Head、Base、分类。
gh 与 rg/fd/grep/ls 调用次数。
actionlint、YAML、Makefile、测试、race、lint、跨平台构建结果。
远端 CI/CodeQL/Codecov 结果和 URL。
已提交行内评论及去重说明。
所有本轮 commit 和普通 push 结果。
未修改 branch protection；给出建议 required checks 并请求单独授权。
必须轮换/吊销的凭据类型。
所有 evidence/probe/worktree 路径及是否可删除。
Review 经验复利：实际记录内容，或“无新增可复用经验”。
```

只有全部 CI 改造验证通过时，才能声称“CI 改造完成”；不得把这一结论扩展成 PR #108 可 Merge，因为 UDP P1 仍未修复。
