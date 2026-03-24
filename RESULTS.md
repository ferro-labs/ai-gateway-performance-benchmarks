# Benchmark Results Summary

> Run date: 2026-03-23 | Machine: GCP n2-standard-8 (8 vCPU, 32 GB RAM), Debian 12 | Mock upstream: 60ms fixed latency

## Headline Comparison

| Metric | Ferro Labs | Bifrost | Kong | LiteLLM | Portkey |
|---|---|---|---|---|---|
| **Gateway overhead** | 1.3ms | 1.5ms | 1.3ms | 218ms | — |
| **Peak throughput** | 13,926 RPS | 13,380 RPS | 15,891 RPS | 168 RPS | 0 RPS* |
| **p99 @ 50 VU** | 63.9ms | 64.5ms | 62.9ms | 583.2ms | 60.1ms* |
| **p99 @ 150 VU** | 63.4ms | 64.6ms | 63.4ms | 1,161ms | 162.7ms* |
| **p99 @ 1000 VU** | 111.9ms | 127.2ms | 73.3ms | 30,001ms | 30,001ms* |
| **Memory (avg)** | 57 MB | 146 MB | 43 MB | 653 MB | 423 MB |
| **Success @ 5K RPS** | 100% | 0% | 100% | 99% | 0% |
| **Language** | Go | Go | Go/Lua | Python | TS/Node |

_*Portkey returned 0% success on all standard scenarios — all requests counted as failed._

## Key Findings

1. **Go-native gateways dominate.** Ferro Labs, Bifrost, and Kong all add ~1.3ms overhead and handle 8,000–16,000 RPS. Interpreted runtimes (Python/LiteLLM, TS/Portkey) lag by 5–100x in throughput.

2. **Ferro Labs scales linearly to 1000 VUs** with p99 of 111.9ms — the best tail latency among gateways that maintained 100% success at all concurrency levels, except Kong which achieved p99 of 73.3ms.

3. **Kong leads at peak concurrency** (15,891 RPS at 1000 VU) with the lowest memory footprint (43 MB). However, Kong's memory reporting was static, suggesting the monitoring may not have captured actual usage.

4. **Bifrost collapses under high load.** While competitive at low-to-medium concurrency, Bifrost drops to 0% success rate at 5K RPS stress tests and streaming comparison scenarios.

5. **LiteLLM has the highest overhead** (~218ms) and caps around 168 RPS. At 1000 VU, p99 latency reaches 30 seconds (timeouts). Memory usage climbs to 1.2 GB under load.

6. **Streaming performance** is comparable across Go-native gateways. TTFB is ~61ms for Ferro Labs, Bifrost, and Kong (matching the 60ms mock latency + ~1ms overhead).

## Scenario Breakdown

### Non-Streaming Scenarios

| Scenario | VUs | Ferro Labs RPS | Bifrost RPS | Kong RPS | LiteLLM RPS |
|---|---:|---:|---:|---:|---:|
| smoke | 10 | 163 | 163 | 163 | 107 |
| baseline | 50 | 813 | 812 | 814 | 175 |
| stress | 150 | 2,448 | 2,441 | 2,444 | 175 |
| high-concurrency-500 | 500 | 8,042 | 8,016 | 8,136 | 166 |
| high-concurrency-1000 | 1000 | 13,926 | 13,380 | 15,891 | 163 |

### Streaming Scenarios

| Scenario | VUs | Ferro Labs RPS | Bifrost RPS | Kong RPS | LiteLLM RPS |
|---|---:|---:|---:|---:|---:|
| streaming-baseline | 50 | 809 | 811 | 814 | 85 |
| streaming-stress | 100 | 1,620 | 1,623 | 1,630 | 82 |

### Tail Latency (p99) at Scale

| Scenario | VUs | Ferro Labs | Bifrost | Kong | LiteLLM |
|---|---:|---:|---:|---:|---:|
| baseline | 50 | 64.1ms | 64.3ms | 62.9ms | 569.8ms |
| stress | 150 | 63.4ms | 64.6ms | 63.4ms | 1,161ms |
| high-concurrency-500 | 500 | 70.8ms | 74.2ms | 64.7ms | 30,001ms |
| high-concurrency-1000 | 1000 | 111.9ms | 127.2ms | 73.3ms | 30,001ms |

## Methodology

- All gateways run as native processes on the same machine, one at a time in complete isolation
- Mock upstream server returns fixed responses with 60ms latency — measures gateway overhead only
- Each gateway is health-checked before benchmarks begin
- "Failed" counts equal VU count — these are in-flight requests cancelled at timer expiry, not errors
- Full benchmark suite: 13 scenarios × 5 gateways = 65 runs

## How to Reproduce

```bash
git clone https://github.com/ferro-labs/ai-gateway-performance-benchmarks
cd ai-gateway-performance-benchmarks
cp .env.example .env
make setup
make bench
```

For publication-quality results (3 averaged runs):

```bash
./scripts/run-benchmarks.sh --gateways ferrogateway,litellm,bifrost,kong --repeat 3
```
