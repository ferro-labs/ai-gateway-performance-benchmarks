# AGENTS.md — ai-gateway-performance-benchmarks

## Purpose

Reproducible benchmarking suite that compares **Ferro AI Gateway** against other
OpenAI-compatible gateways (LiteLLM, Portkey, Kong) under identical load profiles.
Also contains high-VU throughput tools (k6, wrk) for deep FerroGateway self-benchmarks.

This is the canonical performance benchmark repo for `ferro-labs/ai-gateway`.

---

## Repository Structure

```
ai-gateway-performance-benchmarks/
├── benchmarks.yaml           # Benchmark matrix: gateways + scenarios
├── locustfile.py             # Locust load test (blocking + SSE streaming)
├── docker-compose.yml        # Spins up mock-server, FerroGateway, LiteLLM, Kong
├── requirements.txt          # Python deps (locust, PyYAML)
├── Makefile                  # All common commands
├── .env.example              # Template for gateway URLs + API keys
├── configs/
│   ├── ferrogateway.config.yaml   # FerroGateway config (routes to mock upstream)
│   ├── litellm.config.yaml        # LiteLLM config (real OpenAI)
│   ├── litellm.mock.config.yaml   # LiteLLM config (mock upstream)
│   ├── kong.mock.yaml             # Kong DB-less declarative config (mock upstream)
│   ├── kong.deck.yaml             # Kong deck config (real upstream, sync manually)
│   └── portkey.sample.json        # Portkey gateway sample config
├── k6/
│   └── chat_completions.js        # k6 script: baseline + stress + peak_5k VU ramp
├── wrk/
│   └── chat_completions.lua       # wrk Lua script: peak RPS measurement
├── scripts/
│   ├── mock_server.py             # Python OpenAI-compatible mock server (port 9000)
│   └── run_benchmarks.py          # Locust orchestrator: runs matrix, writes CSV + MD reports
└── results/                       # Generated output (gitignored)
    └── <timestamp>/
        ├── summary.md
        ├── summary.json
        └── *_stats.csv
```

---

## Tools

| Tool | Purpose |
|---|---|
| **Locust** | Multi-gateway comparative benchmarks (Requests/s, p50/p95/p99, error rate) |
| **k6** | High-VU throughput & ramp tests on FerroGateway; provides VU-level percentile distributions |
| **wrk** | Single-node peak RPS ceiling; fast feedback on max sustainable throughput |
| **Python mock server** | Zero-latency OpenAI-compatible upstream; eliminates LLM latency from measurements |

---

## Prerequisites

- **Docker + Docker Compose** (for `make up` / service orchestration)
- **Python 3.11+** + pip (for Locust benchmarks)
- **k6** (optional, for `make bench-k6`)
- **wrk** (optional, for `make bench-wrk`)

Install Python deps:

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

---

## Quick Start

### Comparative benchmarks (Locust — all gateways)

```bash
# 1. Start all services
make up

# 2. Run full scenario matrix
make bench

# 3. Check results
cat results/<timestamp>/summary.md
```

### FerroGateway self-benchmark (k6 — high VU)

```bash
make up
make bench-k6        # baseline + stress + peak_5k
make bench-k6-peak   # peak_5k ramp only
```

### FerroGateway peak RPS (wrk)

```bash
make up
make bench-wrk       # 12 threads, 500 connections, 60s
make bench-wrk-light # 4 threads, 100 connections, 30s
```

---

## Benchmark Matrix (`benchmarks.yaml`)

| Scenario | Users | Duration | Notes |
|---|---|---|---|
| smoke | 10 | 2m | Sanity check |
| baseline | 50 | 10m | Steady-state comparison |
| stress | 150 | 10m | Sustained high-load comparison |
| streaming-baseline | 50 | 10m | SSE streaming throughput |
| streaming-stress | 100 | 10m | SSE streaming under load |

Add new scenarios by editing `benchmarks.yaml`. No code changes required.

---

## Isolated vs Real-Upstream Mode

**Isolated mode (recommended):** All gateways route to `scripts/mock_server.py` at zero
latency — measurements reflect gateway overhead only. The `docker-compose.yml` uses this by
default.

**Real-upstream mode:** Fill `.env` with real API keys, remove `OPENAI_BASE_URL` overrides
from docker-compose environment blocks. Gateway overhead will be small relative to upstream
latency so differences between gateways will be noisier.

---

## Key Commands

```bash
make help            # All available targets
make up              # Start mock-server + FerroGateway + LiteLLM
make up-kong         # Also start Kong
make down            # Stop all containers
make bench           # Full Locust matrix
make bench-repeat    # Run matrix 3× and average results
make bench-k6        # k6 all scenarios
make bench-wrk       # wrk peak RPS
make clean           # Delete results/
```

---

## Config Files Quick Reference

| File | Use |
|---|---|
| `configs/ferrogateway.config.yaml` | FerroGateway benchmark config |
| `configs/litellm.mock.config.yaml` | LiteLLM → mock server |
| `configs/litellm.config.yaml` | LiteLLM → real OpenAI |
| `configs/kong.mock.yaml` | Kong DB-less → mock server |
| `configs/kong.deck.yaml` | Kong deck → real upstream |
| `configs/portkey.sample.json` | Portkey reference config |
| `.env.example` | Gateway URLs + API key template |

---

## Do NOT

- Commit `.env` (contains API keys)
- Commit `results/` output (gitignored — commit selectively if publishing)
- Check in binary builds or `.pid` files
- Hard-code API keys in scripts; use `.env` or environment variables
- Compare gateway results from different machines or system loads — always use the same isolated setup

---

## Relationship to Other Repos

- `ferro-labs/ai-gateway` — The OSS gateway under test; build from `../ai-gateway` via `docker-compose.yml`
- `ferro-labs/ai-gateway-benchmark-performance` — **Archived predecessor** (k6+wrk self-benchmark only, single gateway). Use this repo instead.
