# TCP reconnect verification for issue #169

The fix was tested on macOS arm64 with Go 1.25.0. The local changes are based on upstream commit `66d1ffa`. The failure baseline used an isolated checkout of `62bbf45`, the new regression tests, and the FIN fixture from #174.

## Reconnect behavior

Plain TCP and TLS-over-TCP sessions now request a pool refill after an owned client session reads `io.EOF`. The TLS loopback test verifies the server certificate and closes the established TLS connection normally. TLS setup and handshake use the existing client path.

An EOF sets the earliest retry time for the client. The deadline applies across reconnect passes and can delay other vacant slots in the same client's pool. Repeated closures can extend it. Consecutive failed attempts still use the configured attempt budget and existing backoff. `Close()` cancels the retry wait. An already in-flight dial follows the existing shutdown lifecycle.

## Failure baseline

The following excerpts are from the regression tests run on `62bbf45`. Plain TCP and TLS-over-TCP both read EOF and lost their only pooled session. The replacement accept timed out on both paths.

```text
=== RUN   TestTCPReconnectAfterPeerFIN/plain
    client_test.go:379: healthy peer over 2s: callbacks=1, pool=1
    client_test.go:379: after FIN: callbacks=1, pool=0, replacement Accept failed: accept tcp 127.0.0.1:56854: i/o timeout
=== RUN   TestTCPReconnectAfterPeerFIN/TLS
    client_test.go:380: healthy peer over 2s: callbacks=1, pool=1
    client_test.go:380: after FIN: callbacks=1, pool=0, replacement Accept failed: accept tcp 127.0.0.1:56871: i/o timeout
--- FAIL: TestTCPReconnectAfterPeerFIN (8.01s)
    --- FAIL: TestTCPReconnectAfterPeerFIN/plain (4.00s)
    --- FAIL: TestTCPReconnectAfterPeerFIN/TLS (4.01s)
```

```text
    client_test.go:501: continuous FIN over 2s: accepts=1, callbacks=1, pool=0
    client_test.go:503: accepts=1, want 2 to 100
    client_test.go:539: after Close: accepts=1, goroutines=4 (baseline=4)
--- FAIL: TestTCPReconnectRateAfterRepeatedPeerFIN (2.20s)
```

```text
    client_test.go:564: peer FIN did not start a reconnect worker
--- FAIL: TestTCPReconnectAfterPeerFINIsCancelledByClose (0.00s)
```

The TLS test also failed on the intermediate working tree that already restored plain TCP retries but still gated them on `!clt.sslEnabled`. It observed `callbacks=1, pool=0` before the replacement accept timed out.

## Results after the fix

```text
=== RUN   TestTCPReconnectAfterPeerFIN/plain
    client_test.go:379: healthy peer over 2s: callbacks=1, pool=1
    client_test.go:379: after FIN: callbacks=2, pool=1
=== RUN   TestTCPReconnectAfterPeerFIN/TLS
    client_test.go:380: healthy peer over 2s: callbacks=1, pool=1
    client_test.go:380: after FIN: callbacks=2, pool=1
--- PASS: TestTCPReconnectAfterPeerFIN (4.11s)
    --- PASS: TestTCPReconnectAfterPeerFIN/plain (2.05s)
    --- PASS: TestTCPReconnectAfterPeerFIN/TLS (2.06s)
```

```text
    client_test.go:501: continuous FIN over 2s: accepts=40, callbacks=40, pool=0
    client_test.go:539: after Close: accepts=40, goroutines=4 (baseline=5)
--- PASS: TestTCPReconnectRateAfterRepeatedPeerFIN (2.20s)
--- PASS: TestTCPReconnectAfterPeerFINIsCancelledByClose (0.00s)
```

| Check | Result |
| --- | --- |
| One peer shutdown followed by a stable replacement | PASS. Plain TCP and verified TLS-over-TCP each made 2 connections and recovered to 1 active session. |
| Continuous plain TCP FIN with a `50ms` retry interval | PASS. 40 accepts in 2 seconds, within the required range of 2–100. |
| Healthy peer with no disconnect | PASS. 1 connection over 2 seconds for plain TCP and TLS-over-TCP. |
| Client shutdown during retry wait | PASS. Closing during a `5s` delay retired the worker before a second accept. In the repeated FIN test, the count stayed at 40 for 200ms after the reconnect worker exited. The fixture was then closed before checking that the process goroutine count was at or below the pre-fixture baseline. |
| Existing reconnect and pool tests | PASS. The close/reset test now expects one automatic attempt with a budget of one. The existing explicit refill test still covers pool admission after reset. |
| `go test ./transport ./util -count=1` | PASS. |
| `go test -race ./transport ./util -count=1` | PASS. No race diagnostics. The macOS linker emitted an `LC_DYSYMTAB` warning. |
| `go vet ./...` | PASS. |
| CPU profile | NOT_RUN. The continuous FIN test measures accept counts over the 2-second window. |

The sustained disconnect test uses plain TCP. The TLS test covers a normal TLS shutdown and a verified replacement handshake. RST, WS, and WSS retry behavior were outside these tests.
