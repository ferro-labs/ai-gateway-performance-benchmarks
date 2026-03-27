# AI Gateway Performance Benchmarks

Reproducible benchmarking suite comparing **Ferro Labs AI Gateway** against **LiteLLM**, **Bifrost**, **Kong**, and **Portkey** under identical load profiles. All 5 gateways run as native processes, ensuring µs-level measurements are not masked by infrastructure overhead.

All tooling is written in **Go**. LiteLLM requires Python for its proxy server. Portkey runs via Docker with `--network host`.

## What this benchmark measures

- Pure gateway overhead (mock backend, 60ms fixed upstream latency)
- Throughput (Requests/s) at 50, 150, 300, 500, and 1,000 concurrent users
- Latency percentiles: p50, p95, p99, p99.9
- SSE streaming performance and time-to-first-byte (TTFB)
- High-VU ramp behaviour up to 5,000 concurrent users (k6)
- Peak RPS ceiling (wrk)

## Benchmark Results

> Full methodology, raw data, and reproduction instructions:
> [results.md](results.md)

Tested on **GCP n2-standard-8** (8 vCPU, 32 GB RAM, Debian 12)
against a **60ms fixed-latency mock upstream** — results reflect
gateway overhead only, not provider latency.

#### Throughput (RPS) by Concurrency

| Gateway | 150 VU | 300 VU | 500 VU | 1,000 VU | Memory |
|---|---:|---:|---:|---:|---|
| **Ferro Labs v1.0.0** | **2,447** | **4,890** | **8,014** | **13,925** | 32–135 MB |
| Kong OSS 3.9.1 | 2,443 | 4,885 | 8,133 | 15,891 | 43 MB flat |
| Bifrost v1.0.0 | 2,441 | 0 † | 0 † | 0 † | 107–333 MB |
| LiteLLM 1.82.6 | 175 ‡ | — | — | — | 335–1,124 MB |
| Portkey latest | 851 § | 843 § | 855 § | 891 § | 67 MB |

**†** Bifrost: connection pool starvation at ≥300 VU — 10M+ failures
**‡** LiteLLM: CPU-bound ceiling ~175 RPS regardless of concurrency
**§** Portkey: event loop congestion — throughput plateaus, latency 3–6×, errors accumulate at 500+ VU

#### Ferro Labs Latency Profile

| VU | RPS | p50 | p99 | Memory |
|---:|---:|---:|---:|---:|
| 50 | 813 | 61.3ms | 64.1ms | 36 MB |
| 150 | 2,447 | 61.2ms | 63.4ms | 47 MB |
| 300 | 4,890 | 61.2ms | 64.4ms | 72 MB |
| 500 | 8,014 | 61.5ms | 72.9ms | 89 MB |
| 1,000 | 13,925 | 68.1ms | 111.9ms | 135 MB |

p50 overhead at 500 VU: **1.5ms**. p50 overhead at 1,000 VU: **8.1ms**.

#### Live Upstream Overhead (OpenAI API)

In addition to mock-based throughput benchmarks, we measure gateway overhead
against the **live OpenAI API** (gpt-4o-mini) using two independent methods:

1. **`X-Gateway-Overhead-Ms` header** — precise internal timing that isolates
   gateway processing from provider latency (same approach as LiteLLM's
   `x-litellm-overhead-duration-ms`)
2. **Paired requests** — send identical requests both directly to OpenAI and
   through the gateway, then compare latency distributions

| Configuration | Overhead p50 | Overhead p99 | Method |
|:---|---:|---:|:---|
| No plugins (bare proxy) | **0.002ms** (2 microseconds) | 0.03ms | Header |
| With plugins (word-filter, max-token, logger, rate-limit) | **0.025ms** (25 microseconds) | 0.074ms | Header |

The paired-request delta confirms the header measurement but requires 200+ samples
to overcome LLM response variance (500ms-2s per call).

```bash
# Live upstream overhead benchmark (~$0.03 for 1,600 gpt-4o-mini calls)
make bench-realworld

# Quick validation run (50 samples per scenario, ~$0.01)
make bench-realworld-quick
```

#### Reproduce

```bash
git clone https://github.com/ferro-labs/ai-gateway-performance-benchmarks
cd ai-gateway-performance-benchmarks
make setup
make bench
```

See [configs/README.md](configs/README.md) for per-gateway setup notes.

## Prerequisites

| Requirement | Purpose |
| :--- | :--- |
| **Go 1.24+** | Build bench runner, mock server, report generator |
| **Python 3.11+** | LiteLLM proxy server |
| **k6** _(optional)_ | High-VU throughput tests — [install](https://k6.io/docs/get-started/installation/) |
| **wrk** _(optional)_ | Peak RPS tests — `sudo apt-get install wrk` / `brew install wrk` |

Kong and Bifrost are installed by `make setup`. Portkey runs via Docker. Each gateway has its own setup script.

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
  realbench/main.go        — Live upstream overhead benchmark (paired direct-vs-gateway)
  mockserver/main.go       — Go mock server (zero-latency OpenAI-compatible upstream)
  report/main.go           — Report generator (CSV → Markdown + JSON)
benchmarks.yaml            — Benchmark matrix: gateways × scenarios
realworld.yaml             — Live upstream benchmark config (OpenAI scenarios)
configs/
  ferrogateway.config.yaml — Ferro Labs AI Gateway config (mock upstream)
  ferrogateway-realworld.config.yaml — Ferro Labs config (live OpenAI upstream)
  ferrogateway-realworld-plugins.config.yaml — Ferro Labs config (live OpenAI + plugins)
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
  run-realworld.sh         — Run live upstream overhead benchmark (OpenAI)
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
make bench-realworld   # Live upstream overhead benchmark (requires OPENAI_API_KEY)
make bench-realworld-quick # Quick live upstream benchmark (50 samples)
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
