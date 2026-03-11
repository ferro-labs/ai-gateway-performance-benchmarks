# AI Gateway Performance Benchmarks

Reproducible benchmarking suite to compare **Ferro AI Gateway** against **LiteLLM**, **Portkey AI**, and **Kong** under the same load profile. Also includes k6 and wrk tools for deep FerroGateway throughput testing.

All tooling is written in **Go** — no Python or Node.js runtime required on the host.

## What this benchmark measures

- End-to-end user latency from load generator to gateway
- Throughput (`Requests/s`)
- Error rate (`Failure %`)
- Percentiles (`p50`, `p95`, `p99`)
- SSE streaming performance via the `streaming-*` scenarios
- High-VU ramp behaviour up to 5,000 concurrent users (k6)
- Peak RPS ceiling (wrk)

## Tools

| Tool | Role |
| :--- | :--- |
| **`cmd/bench`** (Go) | Multi-gateway comparative benchmarks — concurrent VU goroutines, p50/p95/p99, CSV + Markdown report |
| **`cmd/mockserver`** (Go) | Zero-latency upstream — isolates gateway overhead from LLM latency |
| **k6** | High-VU ramp tests on FerroGateway; baseline/stress/peak\_5k scenarios |
| **wrk** | Single-node peak RPS ceiling; fastest feedback on max throughput |

## Prerequisites

| Requirement | Purpose |
| :--- | :--- |
| **Go 1.22+** | Build `cmd/bench` and `cmd/mockserver` |
| **Docker + Docker Compose v2** | Run gateway containers |
| **k6** _(optional)_ | High-VU throughput tests — [install](https://k6.io/docs/get-started/installation/) |
| **wrk** _(optional)_ | Peak RPS tests — `sudo apt-get install wrk` / `brew install wrk` |

## Structure

```
cmd/
  bench/main.go          — Go benchmark orchestrator (replaces Locust)
  mockserver/main.go     — Go mock server (replaces Python mock)
benchmarks.yaml          — Benchmark matrix (gateways + scenarios)
k6/chat_completions.js   — k6 script: baseline, stress, peak_5k VU ramp
wrk/chat_completions.lua — wrk Lua script: peak RPS measurement
Dockerfile.mockserver    — Multi-stage Go build for the mock server container
configs/                 — Gateway config templates
results/<timestamp>/     — Generated output (gitignored)
```

## Quick start

```bash
# 1. Build Go binaries
make build

# 2. Pull latest gateway images, build mock-server container, start all services
make setup

# 3. Run full benchmark matrix (all gateways × all scenarios) + k6 + wrk
make bench-all

# 4. Clean up everything
make clean
```

Or as a single one-shot command:

```bash
make run   # build → setup → bench-all
```

## Quick start — isolated mode (recommended)

Isolated mode routes all gateways to the local Go mock server, so measurements reflect **gateway overhead only**, not live LLM latency.

```bash
# Copy and fill in .env (URLs + API keys — see .env.example)
cp .env.example .env

# Build, start, and benchmark in one command
make run
```

Results are written to `results/bench-<timestamp>.csv` and `results/bench-<timestamp>.md`.

## Quick start — real upstream mode

Uses live API keys. Gateway overhead is much smaller than upstream latency so differences between gateways will be noisier.

1. Configure target gateways:

```bash
cp .env.example .env
# Fill in real gateway URLs and API keys
```

2. Ensure all gateways route to the **same upstream model** and similar infra.

3. Run:

```bash
make bench
```

## Quick start — k6 high-VU throughput (FerroGateway self-benchmark)

k6 is ideal for finding throughput limits and measuring ramp-to-5k-VU behaviour.
The same local mock server is used, so measurements reflect gateway overhead only.

```bash
# Start services
make setup

# All scenarios (baseline + stress + peak_5k)
make bench-k6

# Peak_5k ramp only
make bench-k6-peak

# Against a custom gateway URL
K6_GATEWAY_URL=http://localhost:4000 K6_API_KEY=mykey k6 run k6/chat_completions.js
```

Results are written as JSON to `results/k6/`.

## Quick start — wrk peak RPS

wrk gives the fastest single-number answer: maximum sustainable requests/second.

```bash
make bench-wrk        # 12 threads, 500 connections, 60s
make bench-wrk-light  # 4 threads, 100 connections, 30s

# Custom target
wrk -t4 -c100 -d30s -s wrk/chat_completions.lua http://localhost:4000
```

## Makefile reference

```bash
make help               # All available targets
make build              # Compile bin/bench and bin/mockserver
make setup              # Pull latest images + build mock-server container + start all services
make run                # build → setup → bench-all (one-shot)
make update             # Pull latest images, rebuild mock-server, restart services
make up                 # Start core services only (mock-server + FerroGateway + LiteLLM)
make up-kong            # Start core services + Kong
make up-portkey         # Start core services + Portkey
make down               # Stop all services
make mock               # Run mock server locally on port 9000 (outside Docker)
make bench-all          # Go bench (all gateways) + k6 + wrk
make bench              # Go bench — full matrix (all gateways × all scenarios)
make bench-ferrogateway # Go bench — FerroGateway only
make bench-litellm      # Go bench — LiteLLM only
make bench-kong         # Go bench — Kong only
make bench-portkey      # Go bench — Portkey only
make bench-repeat       # Go bench — full matrix, 3 runs averaged
make bench-dry          # Preview matrix without executing
make bench-k6           # k6 all scenarios
make bench-k6-baseline  # k6 baseline only
make bench-k6-peak      # k6 peak_5k only
make bench-wrk          # wrk peak RPS (500 connections, 60s)
make bench-wrk-light    # wrk (100 connections, 30s)
make clean              # Stop services, remove images/bins/results
```

## Useful commands

Run only one gateway:

```bash
./bin/bench -gateways ferrogateway
```

Run only specific scenarios:

```bash
./bin/bench -scenarios smoke,baseline
```

Repeat each run 3 times and average results:

```bash
./bin/bench -repeat 3
# or: make bench-repeat
```

Preview the benchmark matrix without executing:

```bash
./bin/bench -dry-run
# or: make bench-dry
```

## Scenarios

| Name | Users | Duration | Notes |
| :--- | ----: | :------- | :---- |
| smoke | 10 | 2 min | Quick sanity check |
| baseline | 50 | 10 min | Standard throughput test |
| stress | 150 | 10 min | High concurrency ceiling |
| streaming-baseline | 50 | 10 min | SSE streaming, moderate load |
| streaming-stress | 100 | 10 min | SSE streaming, high load |

## Mock server

`cmd/mockserver` is a zero-dependency Go HTTP server returning instant fixed responses. It eliminates upstream LLM latency so benchmarks measure gateway overhead only.

Run locally:

```bash
./bin/mockserver --port 9000 --latency-ms 0
```

Available endpoints:

| Endpoint | Description |
| :--- | :--- |
| `GET /health` | Liveness probe (used by Docker healthcheck) |
| `GET /v1/models` | Returns a minimal model list |
| `POST /v1/chat/completions` | Blocking or SSE streaming (`"stream": true`) |

## Fair benchmark checklist

- Use the same model and token settings across all gateways.
- Keep gateway deployments in similar regions/specs.
- Warm up each gateway before measured runs.
- Run each scenario multiple times (`make bench-repeat`) and compare averaged results.
- Avoid mixed background traffic during measurement windows.
- Prefer isolated mode (mock server) for apples-to-apples gateway comparisons.

## Notes for each gateway

- FerroGateway config: `configs/ferrogateway.config.yaml`
- LiteLLM real upstream: `configs/litellm.config.yaml`
- LiteLLM mock upstream: `configs/litellm.mock.config.yaml`
- Kong real upstream (deck sync): `configs/kong.deck.yaml`
- Kong mock/DB-less (docker-compose): `configs/kong.mock.yaml`
- Portkey setup notes: `configs/portkey.sample.json`

Adjust `benchmarks.yaml` if your endpoint path or required headers differ.
