# AGENTS.md — ai-gateway-performance-benchmarks

## Purpose

Reproducible benchmarking suite that compares **Ferro AI Gateway** against
**LiteLLM**, **Bifrost**, and **Kong** under identical load profiles.
All gateways run as native processes on the same machine, ensuring
µs-level measurements are accurate and not masked by networking overhead.

This is the canonical performance benchmark repo for `ferro-labs/ai-gateway`.

---

## Repository Structure

```
ai-gateway-performance-benchmarks/
├── cmd/
│   ├── bench/main.go             # Go benchmark orchestrator (multi-gateway, multi-scenario)
│   ├── mockserver/main.go        # Go mock server (zero-latency OpenAI-compatible upstream)
│   └── report/main.go            # Report generator (CSV → Markdown + JSON)
├── benchmarks.yaml               # Single source of truth: gateways × scenarios matrix
├── Makefile                      # All common commands
├── .env.example                  # Template for gateway URLs + API keys
├── configs/
│   ├── ferrogateway.config.yaml  # Ferro AI Gateway → mock upstream (env var driven)
│   ├── litellm.native.config.yaml # LiteLLM → mock upstream on localhost:9000
│   ├── litellm.config.yaml       # LiteLLM → real OpenAI (reference)
│   ├── bifrost.config.json       # Bifrost → mock upstream on localhost:9000
│   ├── kong.yaml                 # Kong declarative routes → localhost:9000
│   └── kong.conf                 # Kong native DB-less config
├── scripts/
│   ├── setup.sh                  # Install all native dependencies
│   └── run-benchmarks.sh         # Run full benchmark suite (native processes)
├── k6/
│   └── chat_completions.js       # k6 script: baseline + stress + peak_5k VU ramp
├── wrk/
│   └── chat_completions.lua      # wrk Lua script: peak RPS measurement
├── go.mod                        # Go 1.24, single dependency (gopkg.in/yaml.v3)
└── results/                      # Generated output (gitignored)
    ├── native-<timestamp>/
    │   ├── bench-*.csv
    │   ├── bench-*.md
    │   ├── BENCHMARK-REPORT.md
    │   └── BENCHMARK-REPORT.json
```

---

## Tools

| Tool | Purpose |
|---|---|
| **`cmd/bench`** (Go) | Multi-gateway comparative benchmarks — concurrent VU goroutines, warmup phase, p50/p95/p99/p99.9, TTFB for streaming, CSV + Markdown output |
| **`cmd/mockserver`** (Go) | Zero-latency OpenAI-compatible upstream (`/v1/chat/completions` blocking + SSE streaming, `/v1/models`, `/health`). Eliminates LLM latency from measurements |
| **`cmd/report`** (Go) | Reads bench CSV files, generates BENCHMARK-REPORT.md with executive summary, per-scenario tables, key findings, and methodology |
| **k6** | High-VU ramp tests (up to 5k VUs). Three scenarios: baseline, stress, peak_5k |
| **wrk** | Single-node peak RPS ceiling measurement |

---

## Gateways Under Test

| Gateway | Port | Config |
|---|---:|---|
| Ferro AI Gateway | 8080 | `configs/ferrogateway.config.yaml` |
| LiteLLM | 4000 | `configs/litellm.native.config.yaml` |
| Bifrost | 8081 | `configs/bifrost.config.json` |
| Kong | 8000 | `configs/kong.yaml` + `configs/kong.conf` |
| Mock server | 9000 | N/A (Go binary, zero-latency upstream) |

All gateways run as native processes. `scripts/run-benchmarks.sh` starts
each gateway one at a time in complete isolation for fair comparison.

---

## Prerequisites

- **Go 1.24+** — build `cmd/bench`, `cmd/mockserver`, and `cmd/report`
- **Python 3.11+** — LiteLLM proxy server
- **k6** _(optional)_ — high-VU throughput tests ([install](https://k6.io/docs/get-started/installation/))
- **wrk** _(optional)_ — peak RPS tests (`sudo apt-get install wrk` / `brew install wrk`)

Kong and Bifrost are installed by `scripts/setup.sh`.

---

## Quick Start

```bash
# Install all native dependencies
make setup

# Run full benchmark suite
make bench

# View results
cat results/native-*/BENCHMARK-REPORT.md
```

### Single gateway

```bash
make bench-ferrogateway   # Ferro AI Gateway only
make bench-litellm        # LiteLLM only
make bench-bifrost        # Bifrost only
make bench-kong           # Kong only
```

### Publication quality (3 averaged runs)

```bash
make bench-repeat
```

---

## Benchmark Matrix (`benchmarks.yaml`)

`benchmarks.yaml` is the single source of truth for the benchmark matrix.
Add gateways or scenarios by editing the file — no code changes needed.

| Scenario | Users | Duration | Warmup | Notes |
|---|---:|---|---|---|
| smoke | 10 | 2m | 30s | Quick sanity check |
| baseline | 50 | 10m | 60s | Steady-state comparison |
| stress | 150 | 10m | 60s | High concurrency ceiling |
| streaming-baseline | 50 | 10m | 60s | SSE streaming + TTFB |
| streaming-stress | 100 | 10m | 60s | SSE under load |
| high-concurrency-500 | 500 | 2m | 10s | Extreme concurrency |
| high-concurrency-1000 | 1000 | 2m | 10s | Peak concurrency |

---

## Key Commands

```bash
make help              # All available targets
make build             # Compile Go binaries
make setup             # Install all native dependencies
make bench             # Run full benchmark suite
make bench-repeat      # Full suite, 3 runs averaged
make bench-dry         # Preview matrix without executing
make report            # Generate report from latest results
make clean             # Remove binaries, results, virtualenv
```

---

## Do NOT

- Commit `.env` (contains API keys)
- Commit `results/` output (gitignored — commit selectively if publishing)
- Check in binary builds (`bin/`, root-level ELF files)
- Hard-code API keys; use `.env` or environment variables
- Compare results from different machines or system loads

---

## Relationship to Other Repos

- `ferro-labs/ai-gateway` — the OSS gateway under test
- `ferro-labs/ai-gateway-benchmark-performance` — **archived predecessor** (k6+wrk self-benchmark only, single gateway). Use this repo instead.
