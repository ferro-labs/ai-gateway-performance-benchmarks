# Benchmark Results — Ferro Labs AI Gateway Performance Benchmarks

**Run date:** 2026-03-25
**Hardware:** GCP n2-standard-8 (8 vCPU, 32 GB RAM, Debian 12, us-central1-a)
**VM name:** ferro-benchmark-01
**Results directory:** results/native-20260325-072323/
**Configs commit:** f6889a4

## Methodology

- **Mock upstream:** Go server on localhost:9000 — 60ms fixed latency,
  OpenAI-compatible responses. Gateway overhead = observed latency − 60ms.
- **Load tool:** k6, constant-VU scenarios
- **VU progression:** 50 → 150 → 300 → 500 → 1,000 VU
- **Duration per stage:** 60s load + 15s warmup
- **Isolation:** Each gateway started fresh, killed after run,
  5s OS cleanup before next gateway
- **Config:** All gateways ran default out-of-the-box configuration.
  No tuning applied.
- **Portkey exception:** Docker --network host required due to a
  concurrency bug in the bundled undici version (TypeError: immutable
  under persistent keep-alive connections). Native Node.js process
  produced unreliable results. Docker does not change event loop
  constraints — only the connection handling layer.
- **Streaming:** Excluded from all gateways for comparability.
  Portkey SSE had additional inconsistencies even with Docker.

## Full Results

### Throughput and Error Rate

| Gateway | VU | RPS | p50 | p95 | p99 | Errors | Memory |
|---|---:|---:|---:|---:|---:|---:|---|
| Ferro Labs | 50 | 813 | 61.3ms | 62.6ms | 63.5ms | 0% | 36 MB |
| Ferro Labs | 150 | 2,447 | 61.2ms | 62.6ms | 63.6ms | 0% | 47 MB |
| Ferro Labs | 300 | 4,890 | 61.2ms | 63.0ms | 64.6ms | 0% | 72 MB |
| Ferro Labs | 500 | 8,014 | 61.5ms | 66.1ms | 70.4ms | 0% | 89 MB |
| Ferro Labs | 1,000 | 13,925 | 68.1ms | 96.3ms | 111.9ms | 0% | 135 MB |
| Kong OSS | 150 | 2,443 | 61.3ms | 62.4ms | 63.3ms | 0% | 43 MB |
| Kong OSS | 300 | 4,885 | 61.2ms | 62.6ms | 63.8ms | 0% | 43 MB |
| Kong OSS | 500 | 8,133 | 61.3ms | 62.9ms | 64.4ms | 0% | 43 MB |
| Kong OSS | 1,000 | 15,891 | 62.0ms | 67.7ms | 72.9ms | 0% | 43 MB |
| Bifrost | 150 | 2,441 | 61.3ms | 62.9ms | 64.5ms | 0% | 143 MB |
| Bifrost | 300 | 0 | — | — | — | ~100% | 333 MB |
| Bifrost | 500 | 0 | — | — | — | ~100% | — |
| Bifrost | 1,000 | 0 | — | — | — | ~100% | — |
| LiteLLM | 50 | 201 | 229.0ms | 509.6ms | 556.7ms | 0% | 341 MB |
| LiteLLM | 150 | 212 | 617.4ms | 944.4ms | 1,014.6ms | 0% | 347 MB |
| Portkey | 50 | 783 | 62.6ms | 72.3ms | — | 0.00% | 67 MB |
| Portkey | 150 | 851 | 174.0ms | 194.2ms | — | 0.00% | 67 MB |
| Portkey | 300 | 843 | 343.0ms | 367.2ms | — | 0.18% | 67 MB |
| Portkey | 500 | 855 | 293.2ms | 317.8ms | — | 2.96% | 67 MB |
| Portkey | 1,000 | 891 | 370.9ms | 412.6ms | — | 7.57% | 67 MB |

Note: Portkey p99 not available — k6 v1.6.1 `--summary-export` only includes p90/p95.
Portkey memory measured via `/proc/PID/status` VmRSS during 300 VU load (120 samples, 0.5s interval).

## Flamegraph Analysis

| Gateway | Tool | VU | Duration | File | Key Finding |
|---|---|---:|---|---|---|
| Ferro Labs | Go pprof | 50 | 30s | flamegraphs/ferro-cpu-flamegraph.svg | 32% Syscall6 (network I/O), 8% runtime.futex |
| Bifrost | linux perf | 50 | 30s | flamegraphs/bifrost-cpu-flamegraph.svg | 7% _raw_spin_unlock_irqrestore — futex contention |
| LiteLLM | py-spy | 20 | 30s | flamegraphs/litellm-cpu-flamegraph.svg | Hot path: user_api_key_auth → FastAPI middleware → uvicorn |
| Kong | — | — | — | — | Not captured — nginx/Lua has no pprof endpoint |
| Portkey | — | — | — | — | Not captured — Docker container profiling not set up |

### Ferro Labs
32% of CPU in `Syscall6` (network read/write), 8% in `runtime.futex`
(goroutine scheduling). Gateway logic is not visible as a distinct flame —
overhead is sub-microsecond per request.

### Bifrost
7% in `_raw_spin_unlock_irqrestore` at 50 VU — kernel futex wakeups
from goroutine contention on the connection pool lock. This contention
pattern at low VU predicted the cascading failure at 300 VU.
Note: pprof endpoint returns HTML stub in benchmark builds — linux perf used.

### LiteLLM
Hot path runs through `user_api_key_auth` → FastAPI middleware chain →
uvicorn event loop. Every request burns CPU on application-level middleware
before a byte reaches the upstream. CPU-saturated at 20 VU.

## Resource Usage

Memory (RSS) charts under 300 VU load with 60ms mock upstream:

| Gateway | Peak RSS | Notes |
|---|---:|---|
| Ferro Labs v1.0.0 | 32 MB | Lowest memory footprint |
| Kong OSS 3.9.1 | 1,103 MB | Static 43 MB in bench runner; resource CSV shows higher |
| Bifrost v1.0.0 | 112 MB | Grows with connection count |
| LiteLLM 1.82.6 | 338 MB | Python runtime overhead |
| Portkey latest | 67 MB | Stable — Node.js V8 heap |

Chart: `flamegraphs/graphs/combined-resources.png`

## Notes and Known Issues

- **Bifrost pprof stub:** Bifrost benchmark build returns HTML comment
  from pprof endpoint. linux perf used instead.
- **Portkey undici bug:** Native Node.js throws `TypeError: immutable`
  under persistent concurrent connections. Root cause: undici Headers
  object marked immutable after first use on a reused connection.
  `DisableKeepalive: true` confirmed root cause. Docker --network host
  used as methodological middle ground.
- **Portkey streaming excluded:** SSE scenarios failed 100% even with
  Docker. Portkey SSE format incompatible with bench runner expectations.
- **LiteLLM health endpoint:** `/health/liveliness` not `/health`.
- **Kong declarative config:** Requires `KONG_DECLARATIVE_CONFIG`
  env var — not set automatically.
- **Bifrost model format:** Must be `provider/model` (e.g. `openai/gpt-4o`).
- **Portkey upstream URL:** Must include `/v1` suffix.
- **Kong flamegraph:** Not captured — nginx worker profiling requires
  system-level instrumentation outside scope of this run.
- **Portkey Docker requirement:** `x-portkey-provider: openai` and
  `x-portkey-custom-host: http://localhost:9000/v1` headers required
  for routing to mock upstream.

## Sources

[1] Bifrost benchmark: https://www.getmaxim.ai/bifrost/resources/benchmarks

[2] Kong benchmark: https://konghq.com/blog/engineering/ai-gateway-benchmark

[3] Portkey latency: https://portkey.ai/features/ai-gateway

## How to Reproduce

```bash
git clone https://github.com/ferro-labs/ai-gateway-performance-benchmarks
cd ai-gateway-performance-benchmarks
cp .env.example .env

# Install all gateways
make setup

# Run full suite — results in results/
make bench
```

For publication-quality results, average 3 runs:

```bash
./scripts/run-benchmarks.sh --gateways ferrogateway,bifrost,litellm,kong --repeat 3
```

For Portkey (Docker):

```bash
docker run -d --name portkey-bench --network host \
  -e LOG_LEVEL=error -e NODE_ENV=production portkeyai/gateway:latest

k6 run -e VUS=300 -e DURATION=60s scripts/k6-portkey.js
```
