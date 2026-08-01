# GitHub CI 全面加固设计

## 背景与证据

本设计以 `AlexStocks/getty` PR #108 的实时快照为基准：

- Base：`master@cc9909dc9e0aab1307f553bc2a3d8400161be4e2`
- Head：`codex/fix-issue-97-remaining@087714342a09f1cc2318bee9d570c2b6ed028044`
- 现有 workflow：`.github/workflows/github-actions.yml`
- 对照仓库：`apache/dubbo-go@53d81d17c0f658b7151fb8c44f0d80371ef047e7`

PR #108 的 CI 日志确认了以下问题：

1. `actions/setup-go@v5` 在 checkout 之前执行，内置缓存找不到 `go.sum`；随后 workflow 又使用 `actions/cache@v4` 恢复相同的 Go module/build cache。
2. Coverage step 执行远程脚本 `bash <(curl -s https://codecov.io/bash)`，Codecov 返回 HTTP 400 和 `Token required - not valid tokenless upload`，但 step 和 job 仍显示成功。
3. CI 不执行 race detector，无法持续覆盖 Getty 的并发和生命周期风险。
4. workflow 未显式设置最小权限、并发取消或 job 超时。
5. `make test` 使用 `go env -w` 修改 runner 用户级 Go 配置；测试也未使用 `-count=1`。
6. `imports-formatter@latest` 是浮动工具依赖；GitHub Actions 也使用可变 major tag 或 `@main`。
7. README 仍展示 Travis CI badge，仓库仍保留已不参与当前 PR 检查的 `.travis.yml`；该文件还包含明文 Codecov upload token 和第三方 webhook access token。
8. `master` 分支当前没有 required status checks，也没有 repository ruleset。

## 目标

本次改造采用完整方案，目标是：

1. 让测试、race、格式、静态检查、coverage 上传失败能够真实反映到 GitHub check 结果。
2. 消除重复缓存、全局 Go 配置写入和浮动工具版本。
3. 将 workflow 权限限制在每个 job 实际需要的最小集合。
4. 增加跨平台构建、CodeQL 和 Dependabot，覆盖 Go 源码、GitHub Actions 与依赖维护。
5. 固定第三方 Action 到核验过的完整 commit SHA，并在受审查文档中保留版本与 SHA 映射，兼顾供应链可审计性和后续升级。
6. 清理已经被 GitHub Actions 取代的 Travis CI 展示与配置。
7. 保持 Getty 公开 Go API 和运行时行为不变。

## 非目标与权限边界

- 不在本次 CI 改造中修复 PR #108 已审查出的 UDP 运行时问题或测试缺口；这些问题继续由现有 Files changed 线程跟踪。
- 不增加 Getty 专属外部集成服务、数据库、消息队列或部署流程。
- 不在 workflow 中自动发布、创建 release、写回源码或提交生成文件。
- 不直接修改 GitHub branch protection 或 ruleset。required checks 属于 PR 文件之外的仓库设置；只有新 job 名称和实际运行结果稳定后，才提交精确配置建议，并在获得单独确认后修改。
- 不调用或验证 `.travis.yml` 中暴露的第三方 token。删除文件不能从 Git 历史撤销凭据；轮换或吊销 Codecov/DingTalk 凭据属于需要账号权限的外部安全收尾。
- 不把 Dubbo-Go 的 RPC integration test、RISC-V 工具子模块或 samples 流程机械复制到 Getty。

## 变更文件

### 修改

- `.github/workflows/github-actions.yml`
- `Makefile`
- `README.md`
- `README_CN.md`

### 新增

- `.github/workflows/codeql.yml`
- `.github/dependabot.yml`

### 删除

- `.travis.yml`

删除 `.travis.yml` 的前提是最终复核仍满足：PR status rollup 中没有 Travis check，GitHub Actions 已覆盖其有效命令，README badge 同步改为 GitHub Actions。

## 设计

### 1. 主 CI workflow

保留 `.github/workflows/github-actions.yml` 作为主 workflow，名称继续使用 `CI`，触发范围为：

- push 到 `master`
- 以 `master` 为 base 的 pull request

workflow 顶层设置：

```yaml
permissions:
  contents: read

concurrency:
  group: ${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}
  cancel-in-progress: true
```

`concurrency` 用于取消同一 PR 或同一 ref 的旧运行，避免过期 Head 继续占用 runner。主 CI 最终包含 5 个逻辑 job：`Check License Header`、`Test and Lint`、`Upload Coverage`、`Race` 和 `Build` matrix。所有 job 都设置显式 `timeout-minutes`，防止网络测试、工具下载或 race 测试无限挂起。

### 2. Action 固定策略

所有第三方 Action 使用完整 commit SHA。版本与 SHA 的对应关系必须在设计或计划中集中记录；workflow 同行可以保留版本注释，但不得用可变 tag 替代 SHA，例如：

```yaml
uses: actions/checkout@<commit-sha> # v7
```

实现前重新查询并固定：

- `actions/checkout@v7`
- `actions/setup-go@v7.0.0`
- `apache/skywalking-eyes/header` 当前核验的 official main verified commit
- `actions/upload-artifact@v7.0.1`
- `actions/download-artifact@v8.0.1`
- `codecov/codecov-action@v7`
- `github/codeql-action@v3`

Dependabot 的 `github-actions` ecosystem 负责后续 Action 更新。不得使用 `@main`，也不得在同一 workflow 中同时保留 major tag 与完整 SHA 两套引用方式。

本轮正式质量审查确认：`actions/upload-artifact@v7.0.1` 的 release 与 tag ref 都指向 `043fb46d1a93c77aae656e7c1c64a875d1fc6a0a`；`actions/download-artifact@v8.0.1` 的 release 与 tag ref 都指向 `3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c`。`download-artifact` v8.0.1 tag 下 README 仍有一处 `@v7` 示例，属于示例文本滞后；release 元数据和精确 tag ref 一致，因此实现以 release/ref 指向的完整 SHA 为准，不因 README 的单处旧示例降级到 v7。

Action 固定默认采用官方稳定 release/tag 对应的完整 SHA。仅当官方 main 上的 verified commit 明确晚于最新 release，且退回该 release 会撤销安全加固或可复现性改进时，才允许在设计中记录 provenance 后固定该 verified commit；这个例外不允许使用 `@main` 等可变引用。本轮 `actions/setup-go@v7.0.0` 的 release、`v7.0.0` 与 `v7` tag 均指向 `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e`，可直接替换 v6.5.0 SHA，现有输入和 Node 24 runner 要求不变。`apache/skywalking-eyes/header@315732dd4b8d3a015d8d9b91936b935a0b854817` 是 official main 上经 GitHub 验证、比 v0.8.0 release commit 多 27 个提交的固定提交；它已将内部 `setup-go` 固定到完整 SHA，并对 shell 输入进行环境变量和引用加固，因此保留该提交，避免降级到 v0.8.0。

### 3. License job

License job：

- `permissions: contents: read`
- checkout 固定到完整 SHA
- SkyWalking Eyes 固定到完整 SHA
- `timeout-minutes: 10`
- 保持 `.licenserc.yaml` 和 `mode: check`

License job 不获得 `id-token`、`security-events` 或写入仓库内容的权限。

### 4. Test and Lint job

主验证 job 使用稳定名称 `Test and Lint`，步骤顺序固定为：

1. Checkout
2. Setup Go
3. Verify modules
4. Check format
5. Unit tests and coverage
6. Lint
7. Upload coverage artifact

Setup Go 使用：

```yaml
with:
  go-version-file: go.mod
  cache-dependency-path: go.sum
```

删除独立 `actions/cache` step，让 `setup-go` 成为 Go module/build cache 的唯一 owner。

模块验证执行 `go mod verify`。格式检查执行 `make check-fmt`。测试执行 `make test`，生成 `coverage.txt`。Lint 执行 `make lint`。随后使用固定 SHA 的 `actions/upload-artifact@v7.0.1` 上传 artifact：名称为 `coverage`，路径为 `coverage.txt`，文件缺失时报错，保留 1 天。

`Test and Lint` 继承 workflow 顶层的 `contents: read`，不声明也不获得 `id-token: write`。OIDC 权限只授予后续隔离的 `Upload Coverage` job。

### 5. Coverage artifact 与隔离的 Codecov OIDC

新增 `Upload Coverage` job，`needs: test-and-lint`。它不 checkout 源码、不 setup Go，也不执行 shell 命令；只下载前一 job 产生的 `coverage` artifact，再调用固定到完整 SHA 的 `codecov/codecov-action`。这样只有 coverage 上传边界获得 OIDC 权限，不再下载并执行 Codecov bash uploader。

`Upload Coverage` job 的权限只有：

```yaml
permissions:
  id-token: write
```

artifact 下载固定为：

```yaml
- name: Download Coverage Artifact
  uses: actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c
  with:
    name: coverage
```

Codecov 参数至少包含：

```yaml
with:
  version: v11.3.1
  use_oidc: true
  fail_ci_if_error: true
  files: ./coverage.txt
  disable_search: true
```

设计要求：

- 上传失败必须使 job 失败。
- 不依赖 `CODECOV_TOKEN` 仓库 secret。
- 只上传明确生成的 `coverage.txt`，不扫描工作区中的其他 coverage 文件。
- `id-token: write` 只能出现在 `Upload Coverage` job；`Test and Lint`、License、Race 和 Build 均不得获得 OIDC。
- push 后必须核对日志中没有 HTTP 400、tokenless upload 错误或被吞掉的非零状态。

### 6. Race job

新增独立 job `Race`：

- Ubuntu runner
- checkout + setup-go 内置缓存
- `timeout-minutes: 15`
- 执行 `make test-race`

`make test-race` 固定执行：

```bash
GOTOOLCHAIN=go1.25.0+auto go test -race ./transport -count=1
```

Race job 与普通单测并行，单独展示结果，便于后续配置 required check。

### 7. 跨平台构建 job

新增 job `Build`，使用真实 GitHub-hosted runner matrix：

- `ubuntu-latest`
- `windows-latest`
- `macos-latest`

每个平台执行 checkout、setup-go、`go mod verify` 和 `go build ./...`。该 job 只验证编译兼容性，不在本次范围内把全量网络测试扩展到 Windows/macOS，避免把既有平台测试差异和 CI 架构改造混为一体。

Matrix 设置 `fail-fast: false`，保证一个平台失败时仍能取得另外两个平台的完整证据。job 名称包含 runner OS，便于将来精确配置 required checks。

### 8. Makefile

Makefile 调整为可由本地和 CI 复用的显式门禁：

- `.PHONY` 补全 `check-fmt`、`test-race` 和安装目标。
- `test` 不再执行 `go env -w`，改为命令级 `GOTOOLCHAIN`。
- `test` 增加 `-count=1`，同时保留 atomic coverage 输出。
- 新增 `test-race`，只运行 `./transport` 的 race 测试。
- 新增 `check-fmt`：执行项目格式化命令后，用 `git diff --exit-code -- . ':!coverage.txt'` 检测并输出格式化差异，同时排除测试生成的 coverage 文件。
- `imports-formatter` 从 `@latest` 固定到本次已验证的 `v1.0.10`。
- `golangci-lint` 暂时保持当前已验证的 `v2.4.0`，避免在 CI 架构改造中混入新 lint 规则导致的源码修复；升级到 Dubbo-Go 使用的更高版本应单独处理。

`check-fmt` 会在一次性 CI checkout 中运行写入式 formatter，但只用于验证差异；本地验证时必须在独立 probe 副本执行，不能改写 PR 主证据副本后再恢复。

### 9. CodeQL

新增 `.github/workflows/codeql.yml`：

- push 到 `master`
- 以 `master` 为 base 的 pull request
- 每周一次定时扫描
- `concurrency` 取消同一 PR 的旧扫描

权限限定为：

```yaml
permissions:
  contents: read

jobs:
  analyze:
    permissions:
      actions: read
      contents: read
      security-events: write
```

CodeQL 显式指定 `languages: go`，使用固定 commit SHA 的 `init` 和 `analyze`。当前官方形状是在 `init` 中设置 `build-mode: autobuild`，随后直接执行 `analyze`，不再增加显式 `github/codeql-action/autobuild` step。旧的 `init(build-mode: autobuild) -> autobuild -> analyze` 三步形状仍可兼容运行，但显式 `autobuild` 与 init 的 build mode 重复，属于冗余。不得复制 Dubbo-Go workflow 中手工 checkout PR merge commit 父节点的历史逻辑；使用 GitHub 当前标准 pull request checkout 语义。

### 10. Dependabot

新增 `.github/dependabot.yml`：

- `gomod`：根目录，每周更新，目标分支 `master`
- `github-actions`：根目录，每月更新，目标分支 `master`
- `gomod` 的 `open-pull-requests-limit` 设为 `5`，commit message 前缀设为 `deps`
- `github-actions` 的 `open-pull-requests-limit` 设为 `3`，commit message 前缀设为 `ci`

配置只负责创建依赖更新 PR，不自动 approve、merge 或修改 branch protection。

PR 分支阶段只能通过严格 YAML 解析和字段/结构断言验证该文件；GitHub 是否接受并启用 Dependabot、是否按计划创建更新 PR，只能在配置合并到默认分支 `master` 后确认。PR push 后没有可证明“已接受/已启用/已排程”的 GitHub 结果，不得把本地结构验证写成平台接受证据。

### 11. README 与 Travis 清理

`README.md` 和 `README_CN.md` 的 Travis badge 替换为 GitHub Actions `CI` workflow badge，并继续保留 Codecov、Go reference、Go Report Card 和 license badge。

确认当前 GitHub status rollup 没有 Travis check 后删除 `.travis.yml`。删除前对照其命令与新 workflow，确保 Go 测试、race、格式、lint、coverage 和构建范围不存在仅由 Travis 承担的路径。

旧 Travis 文件中的明文 Codecov token 和第三方 webhook token 已经进入 Git 历史。PR 负责从当前树删除这些值，并在收尾报告中列出必须由仓库维护者完成的轮换/吊销动作；不得在评论、日志、设计文档或 commit message 中复制 token 内容。

### 12. Branch protection 后续配置

本 PR 只提交可审查的仓库文件。新 workflow 在 PR #108 当前 Head 上全部稳定通过后，输出建议 required checks 列表，预计包含：

- `Check License Header`
- `Test and Lint`
- `Upload Coverage`
- `Race`
- 三个平台的 `Build` matrix checks
- `Analyze (Go)`（CodeQL workflow 的真实 check 名预计值）

实际 check 名称以 GitHub 新运行返回值为准。修改 branch protection/ruleset 前必须再次获取现有配置，使用增量更新，保留 force-push、review、conversation resolution 等与本任务无关的设置，并获得单独确认。

## 验证策略

### 静态与语法验证

- `git diff --check`
- 使用 `actionlint v1.7.12` 检查全部 `.github/workflows/*.yml` 与 `*.yaml`
- 解析 `.github/dependabot.yml`，确认 YAML 语法和必需字段
- 检查所有 `uses:` 都固定为完整 40 字符 SHA
- 确认当前两个 workflow 共 6 个逻辑 job、14 个 `uses:`；其中主 CI 为 5 个逻辑 job
- 确认 `id-token: write` 只出现 1 次且位于 `Upload Coverage`，`Test and Lint` 无 OIDC
- 确认 Codecov `version` 固定为 `v11.3.1`
- 检查不存在 `@main`、`@latest`、`curl | bash` 或 process substitution 远程执行

### Makefile 验证

在独立 probe 副本运行：

- `make check-fmt`
- `make test`
- `make test-race`
- `make lint`
- `git status --porcelain=v2 --branch --untracked-files=all`

确认 `make test` 不改写用户级 `go env`，工具版本与设计一致，格式检查能在故意制造格式差异时失败。

### Go 验证

- `go mod verify`
- `go vet ./...`
- `go test ./... -count=1`
- `go test -race ./transport -count=1`
- `GOOS=windows GOARCH=amd64 go build ./...`
- `GOOS=darwin GOARCH=amd64 go build ./...`
- `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./...`
- `GOOS=linux GOARCH=riscv64 CGO_ENABLED=0 go build ./...`

本地交叉编译不能替代真实 GitHub Windows/macOS runner；最终结论必须等待远端 matrix job。

### GitHub 实时验证

push 前：

1. 重新获取 PR state、Base、Head、checks 和远端 head branch SHA。
2. 确认远端 Head 仍等于本轮实现基准，不使用 force push。
3. commit 后再次核对本地 branch 只领先预期提交。

push 后：

1. 等待全部新 workflow run 完成。
2. 核对每个 job 的真实命令、平台、结论和日志。
3. 确认 `Test and Lint` 成功上传 `coverage` artifact，`Upload Coverage` 下载同名 artifact 后完成 Codecov 上传，且两段失败都可传播。
4. 确认 setup-go 在 checkout 后找到 `go.sum`，不存在第二套 Go cache。
5. 确认 CodeQL 上传 security result 成功。
6. 对 Dependabot 只确认 PR 内 YAML 和结构门禁通过；实际接受、启用和排程列为合并到默认分支后的验证项。
7. 重新获取 PR Base/Head、mergeable、mergeStateStatus、reviewDecision、required checks 和 review threads。

## 失败处理与回滚

- actionlint/YAML 失败：只修 workflow 语法，不绕过检查。
- Windows/macOS build 暴露既有源码不兼容：记录为独立产品问题；不为了让 CI 变绿而跳过失败包。若修复明显超出 CI 范围，保留失败证据并由用户决定拆分或扩大授权。
- Codecov OIDC 不被当前仓库接受：先核对 job 权限和 Codecov 官方日志；不得恢复旧 bash uploader。若需要 Codecov 侧启用设置，报告精确外部前置条件。
- CodeQL 因仓库安全设置不可用：保留 workflow 和失败证据，说明所需 GitHub 设置；不把环境/权限失败归责为 Go 源码问题。
- 远端 Head 漂移：停止 push，重新审查新增远端提交并适配；禁止 force push 覆盖。
- 回滚通过新增普通 commit 完成，不改写 PR 历史。

## 完成标准

只有同时满足以下条件，CI 改造才算完成：

1. 本设计列出的仓库文件完成修改，且没有越过非目标边界。
2. 本地 workflow、YAML、Makefile、Go 测试、race、lint 和交叉编译验证获得新鲜证据。
3. 新提交以普通 push 进入 PR #108，不覆盖远端新增提交。
4. GitHub 上 License、Test and Lint、Upload Coverage、Race、Build matrix、CodeQL 全部产生可识别的 checks。
5. Codecov Action 使用 CLI `v11.3.1`，上传成功，日志不再出现 HTTP 400 或被吞掉的失败。
6. setup-go 缓存由唯一 action 管理，并在 checkout 后读取 `go.sum`。
7. README badge 指向 GitHub Actions，旧 Travis 配置已在覆盖核对后删除。
8. Dependabot 配置在 PR 内通过严格 YAML 和结构验证；合并到默认分支后另行确认 GitHub 接受、启用和排程，不把该外部结果作为 PR push 阶段可完成的条件。
9. 最终 Head 与验证基准一致。
10. PR #108 的 UDP P1 finding 仍单独对账，不因 CI 改造而被误报为已修复。
11. 收尾报告明确要求轮换或吊销旧 Travis 文件中暴露的 Codecov 和第三方 webhook 凭据，并确认 PR 没有再次复制其值。
12. 静态政策断言与最终文件一致：主 CI 5 个逻辑 job、全部 workflow 合计 6 个逻辑 job 与 14 个 `uses:`，OIDC 只授予 `Upload Coverage`。
