# AI Gateway Performance Benchmarks

Reproducible benchmarking suite comparing **Ferro Labs AI Gateway** against **LiteLLM**, **Bifrost**, and **Kong** under identical load profiles. All gateways run as native processes, ensuring µs-level measurements are not masked by infrastructure overhead.

All tooling is written in **Go**. LiteLLM requires Python for its proxy server.

## What this benchmark measures

- Pure gateway overhead (mock backend, ~0ms upstream latency)
- Throughput (Requests/s) at 10, 50, 150, 500, and 1000 concurrent users
- Latency percentiles: p50, p95, p99, p99.9
- SSE streaming performance and time-to-first-byte (TTFB)
- High-VU ramp behaviour up to 5,000 concurrent users (k6)
- Peak RPS ceiling (wrk)

## Latest Results (2026-03-23)

> GCP n2-standard-8 (8 vCPU, 32 GB RAM), Debian 12 — mock upstream with 60ms fixed latency

| Metric | Ferro Labs | Bifrost | Kong | LiteLLM | Portkey |
|---|---|---|---|---|---|
| **Gateway overhead** | 1.3ms | 1.5ms | 1.3ms | 218ms | — |
| **Peak throughput** | 13,926 RPS | 13,380 RPS | 15,891 RPS | 168 RPS | 0 RPS |
| **p99 @ 150 VU** | 63.4ms | 64.6ms | 63.4ms | 1,161ms | 162.7ms |
| **p99 @ 1000 VU** | 111.9ms | 127.2ms | 73.3ms | 30,001ms | 30,001ms |
| **Memory** | 57 MB | 146 MB | 43 MB | 653 MB | 423 MB |
| **Success @ 5K RPS** | 100% | 0% | 100% | 99% | 0% |

Go-native gateways (Ferro Labs, Bifrost, Kong) add ~1.3ms overhead and handle 8,000–16,000 RPS. Interpreted runtimes (LiteLLM/Python, Portkey/TS) lag 5–100x in throughput. See **[RESULTS.md](RESULTS.md)** for full breakdown.

## Prerequisites

| Requirement | Purpose |
| :--- | :--- |
| **Go 1.24+** | Build bench runner, mock server, report generator |
| **Python 3.11+** | LiteLLM proxy server |
| **k6** _(optional)_ | High-VU throughput tests — [install](https://k6.io/docs/get-started/installation/) |
| **wrk** _(optional)_ | Peak RPS tests — `sudo apt-get install wrk` / `brew install wrk` |

Kong and Bifrost are installed by `make setup`. Each gateway has its own setup script.

## Quick start

```bash
git clone https://github.com/ferro-labs/ai-gateway-performance-benchmarks
cd ai-gateway-performance-benchmarks
cp .env.example .env

# Install latest version of each gateway natively
make setup

# Run full benchmark suite — all gateways, all scenarios
make bench
```

Results are written to `results/native-<timestamp>/BENCHMARK-REPORT.md`.

### Or install individual gateways

```bash
make setup-ferro      # Download latest Ferro Labs AI Gateway binary
make setup-litellm    # Create .venv-litellm with latest LiteLLM
make setup-bifrost    # Download latest Bifrost binary
make setup-kong       # Install Kong from apt
make setup-mockserver # Build Go mock server + bench tools
```

## Quick start — publication quality

```bash
# 3 averaged runs for each gateway x scenario combination
./scripts/run-benchmarks.sh --repeat 3
```

## Structure

```
cmd/
  bench/main.go            — Go benchmark orchestrator (multi-gateway, multi-scenario)
  mockserver/main.go       — Go mock server (zero-latency OpenAI-compatible upstream)
  report/main.go           — Report generator (CSV → Markdown + JSON)
benchmarks.yaml            — Benchmark matrix: gateways × scenarios
configs/
  ferrogateway.config.yaml — Ferro Labs AI Gateway config
  litellm.native.config.yaml — LiteLLM → mock server on localhost:9000
  litellm.config.yaml      — LiteLLM → real OpenAI (reference)
  bifrost.config.json      — Bifrost → mock server on localhost:9000
  kong.yaml                — Kong declarative routes → localhost:9000
  kong.conf                — Kong native DB-less config
scripts/
  setup-all.sh             — Install all native dependencies
  setup-ferro.sh           — Download latest Ferro Labs binary
  setup-litellm.sh         — Create Python venv with latest LiteLLM
  setup-bifrost.sh         — Download/build latest Bifrost binary
  setup-kong.sh            — Install Kong from apt
  setup-mockserver.sh      — Build Go binaries from source
  run-benchmarks.sh        — Run full benchmark suite (native processes)
k6/chat_completions.js     — k6 script: baseline, stress, peak_5k VU ramp
wrk/chat_completions.lua   — wrk Lua script: peak RPS measurement
results/                   — Generated output (gitignored)
```

## Makefile reference

```bash
make help              # All available targets
make build             # Compile Go binaries into bin/
make setup             # Install all gateways natively (latest versions)
make setup-ferro       # Install latest Ferro Labs AI Gateway binary
make setup-litellm     # Install latest LiteLLM in .venv-litellm
make setup-bifrost     # Install latest Bifrost binary
make setup-kong        # Install latest Kong natively (apt)
make setup-mockserver  # Build Go mock server and bench tools
make bench             # Run full benchmark suite (all gateways)
make bench-ferrogateway # Ferro Labs AI Gateway only
make bench-litellm     # LiteLLM only
make bench-bifrost     # Bifrost only
make bench-kong        # Kong only
make bench-repeat      # Full suite, 3 runs averaged
make bench-dry         # Preview matrix without executing
make bench-k6          # k6 high-VU throughput test
make bench-wrk         # wrk peak RPS test
make report            # Generate report from latest results
make clean             # Remove binaries, results, virtualenv
```

## Scenarios

| Name | Users | Duration | Warmup | Notes |
| :--- | ----: | :------- | :----- | :---- |
| smoke | 10 | 2m | 30s | Quick sanity check |
| baseline | 50 | 10m | 60s | Steady-state comparison |
| stress | 150 | 10m | 60s | High concurrency ceiling |
| streaming-baseline | 50 | 10m | 60s | SSE streaming + TTFB |
| streaming-stress | 100 | 10m | 60s | SSE under load |
| high-concurrency-500 | 500 | 2m | 10s | Extreme concurrency |
| high-concurrency-1000 | 1000 | 2m | 10s | Peak concurrency |

## Mock server

`cmd/mockserver` is a zero-dependency Go HTTP server returning instant fixed responses. It eliminates upstream LLM latency so benchmarks measure gateway overhead only.

```bash
./bin/mockserver --port 9000 --latency-ms 0
```

| Endpoint | Description |
| :--- | :--- |
| `GET /health` | Liveness probe |
| `GET /v1/models` | Minimal model list |
| `POST /v1/chat/completions` | Blocking or SSE streaming (`"stream": true`) |

## Fair benchmark checklist

- All gateways run as native processes on the same machine.
- Same model and token settings across all gateways.
- Connection warmup applied before every measurement window.
- Run each scenario multiple times (`make bench-repeat`) and compare averaged results.
- No background traffic during measurement windows.
- Mock server backend isolates gateway overhead from LLM latency.
