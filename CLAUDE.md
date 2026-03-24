# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Reproducible benchmarking suite comparing **Ferro Labs AI Gateway** against LiteLLM, Bifrost, and Kong under identical load profiles. All gateways run as native processes for accurate µs-level measurements. Go tooling (requires Go 1.24+), Python for LiteLLM proxy. This is the canonical performance benchmark repo for `ferro-labs/ai-gateway`.

## Build and Run

```bash
make build          # Compile bin/bench, bin/mockserver, bin/report
make setup          # Install all native dependencies (LiteLLM, Bifrost, Kong)
make bench          # Run full benchmark suite (all gateways, native processes)
make bench-repeat   # Publication quality: 3 averaged runs
make clean          # Remove binaries, results, virtualenv
```

### Running Specific Benchmarks

```bash
./scripts/run-benchmarks.sh --gateways ferrogateway,litellm  # Specific gateways
./scripts/run-benchmarks.sh --scenarios smoke,baseline        # Specific scenarios
./scripts/run-benchmarks.sh --repeat 3                        # 3 averaged runs

./bin/bench -gateways ferrogateway       # Single gateway (requires gateway already running)
./bin/bench -scenarios smoke,baseline    # Specific scenarios
./bin/bench -dry-run                     # Preview matrix without executing
```

All bench commands implicitly use `-config benchmarks.yaml -dotenv .env -out-dir results`.

## Architecture

All gateways run as native processes on localhost. `scripts/run-benchmarks.sh` starts each gateway one at a time in complete isolation — the only defensible methodology for publishable µs-level measurements.

**Three benchmark tools** target the same gateway endpoints through a shared mock server:

- **`cmd/bench/main.go`** — Go benchmark orchestrator. Reads `benchmarks.yaml`, spawns concurrent VU goroutines per scenario, collects latency percentiles (p50/p95/p99/p99.9), outputs CSV + Markdown. This is the primary comparative tool (all gateways × all scenarios). Measures TTFB for streaming scenarios.
- **`k6/chat_completions.js`** — High-VU ramp tests (up to 5k VUs) for Ferro Labs AI Gateway self-benchmarking. Three scenarios: baseline (50 VU, 2 min), stress (150 VU, 5 min), peak_5k (ramp 0→5k VU). Configurable via `K6_GATEWAY_URL`, `K6_API_KEY`, `K6_SCENARIO` env vars.
- **`wrk/chat_completions.lua`** — Peak RPS ceiling measurement. Configurable via `API_KEY` and `MODEL` env vars. Target URL is the wrk positional arg.

**`cmd/mockserver/main.go`** — Zero-latency OpenAI-compatible mock server (port 9000). Returns instant fixed responses (blocking) or SSE streaming with configurable chunk delay (`--stream-chunk-delay-ms`, default 10ms). Supports `/health`, `/v1/models`, `/v1/chat/completions`. Logs request rate every 5s.

**`cmd/report/main.go`** — Report generator. Reads bench CSV files, generates BENCHMARK-REPORT.md with executive summary, per-scenario comparison tables, key findings (highest RPS, lowest p99), methodology, and how-to-reproduce section. Also writes BENCHMARK-REPORT.json for downstream tooling.

**`benchmarks.yaml`** — Declarative benchmark matrix. Gateway URLs/API keys injected via `${ENV_VAR}` expansion from `.env`. Add gateways or scenarios without code changes. Five gateways configured: ferrogateway, bifrost, litellm, kong, portkey.

**`scripts/setup.sh`** — Installs all native dependencies: builds Go binaries, creates Python venv with LiteLLM, builds Bifrost from source, installs Kong via apt.

**`scripts/run-benchmarks.sh`** — Runs each gateway as a native process one at a time. Starts mock server, starts gateway, health-checks, runs bench, kills gateway, sleeps 5s, repeats for next gateway. Merges CSVs and generates report.

## Key Conventions

- Gateway URLs and API keys live in `.env` (never committed). Copy from `.env.example`.
- Results go to `results/` (gitignored). Output: `bench-<timestamp>.csv` and `bench-<timestamp>.md`.
- The bench runner loads `.env` then expands `${VAR}` in `benchmarks.yaml`. Already-set env vars take precedence.
- Isolated mode (mock server) is the default for fair gateway-overhead-only comparisons.
- "Failed" request counts in results equal the number of VUs — these are in-flight requests cancelled at timer expiry, not errors.
- Streaming RPS is intentionally lower (~157 RPS at 50 VU) due to mock server chunk delay (10ms × 7 chunks ≈ 70ms per stream).

## Benchmark Scenarios (benchmarks.yaml)

| Scenario | Users | Duration | Notes |
|---|---:|---|---|
| smoke | 10 | 2m | Quick sanity check |
| baseline | 50 | 10m | Steady-state comparison |
| stress | 150 | 10m | High concurrency ceiling |
| streaming-baseline | 50 | 10m | SSE streaming + TTFB |
| streaming-stress | 100 | 10m | SSE under load |
| high-concurrency-500 | 500 | 2m | Extreme concurrency |
| high-concurrency-1000 | 1000 | 2m | Peak concurrency |

## Default Service Ports

| Service | Port |
|---|---|
| mock-server | 9000 |
| ferrogateway | 8080 |
| litellm | 4000 |
| kong | 8000 (proxy), 8001 (admin) |
| bifrost | 8081 |
| portkey | 8787 |

## Running Benchmarks

### Quick start

```bash
make setup      # Install all dependencies natively
make bench      # Run full suite — results in results/native-<timestamp>/
```

### Publication quality

```bash
./scripts/run-benchmarks.sh --gateways ferrogateway,litellm,bifrost,kong --repeat 3
```

### Supplemental tools (k6 + wrk)

```bash
# k6 high-VU ramp against each gateway
K6_GATEWAY_URL=http://localhost:8080 k6 run k6/chat_completions.js   # Ferro
K6_GATEWAY_URL=http://localhost:4000 k6 run k6/chat_completions.js   # LiteLLM
K6_GATEWAY_URL=http://localhost:8081 k6 run k6/chat_completions.js   # Bifrost

# wrk peak RPS against each gateway
wrk -t12 -c500 -d60s -s wrk/chat_completions.lua http://localhost:8080  # Ferro
wrk -t12 -c500 -d60s -s wrk/chat_completions.lua http://localhost:4000  # LiteLLM
wrk -t12 -c500 -d60s -s wrk/chat_completions.lua http://localhost:8081  # Bifrost
```

## Generate the Benchmark Report

After all runs complete, use Claude Code to generate a publishable report:

1. Read all result files in `results/` (CSV + Markdown + k6 JSON)
2. Generate a markdown report with:
   - Tables comparing Ferro Labs vs LiteLLM vs Bifrost vs Kong (RPS, p50/p95/p99, error rate)
   - Headline callouts: peak RPS, p99 latency at load, memory/CPU if captured
   - Methodology section: machine spec, OS, mock backend, concurrency levels, duration
   - Latency distribution charts (ASCII or embedded)
   - "How to reproduce" section with exact commands
3. Format for both a README section and a standalone blog post

### Key metrics to highlight

| Metric | Source | Why it matters |
|---|---|---|
| Gateway overhead (p50 µs) | Go bench CSV | Core headline — Bifrost claims <11µs |
| P50 / P95 / P99 latency (ms) | Go bench + k6 | What engineers care about |
| Max RPS (req/s) | wrk output | Throughput ceiling |
| TTFB for streaming (ms) | Go bench CSV (ttfb_ms column) | SSE responsiveness |
| Latency under load | p99 at stress (150 VU) | Stability story |

### Concurrency levels to cover

Test at: 10 → 50 → 100 → 150 → 500 → 1000 connections. Run each for 60s minimum, 120s for headline numbers. The existing scenarios cover 10/50/100/150; add higher levels by editing `benchmarks.yaml`.

## Do NOT

- Commit `.env` or `results/`
- Hard-code API keys — use `.env` or environment variables
- Compare results from different machines or system loads
