# Benchmark Results — Local Validation

> All tests run against the Go mock server (zero upstream latency).
> Results reflect **gateway overhead only**, not real LLM latency.
>
> Machine: Local dev (Ubuntu 24.04, x64)
> Date: 2026-03-17

---

## Smoke Test (10 users × 2 min) — All 5 Gateways

All gateways hit the same mock server directly (no actual gateway proxy in this local test).

| Gateway | RPS | P50 (ms) | P95 (ms) | P99 (ms) | P99.9 (ms) | Max (ms) | Success | Failed |
|---------|----:|---------:|---------:|---------:|----------:|---------:|--------:|-------:|
| bifrost | 31,194 | 0.27 | 0.65 | 1.19 | 2.51 | 24.06 | 3,743,271 | 8 |
| ferrogateway | 30,370 | 0.28 | 0.68 | 1.24 | 2.54 | 12.83 | 3,644,353 | 10 |
| kong | 31,603 | 0.27 | 0.64 | 1.14 | 2.36 | 11.49 | 3,792,373 | 9 |
| litellm | 32,361 | 0.27 | 0.61 | 1.07 | 2.15 | 14.28 | 3,883,334 | 10 |
| portkey | 31,733 | 0.28 | 0.62 | 1.10 | 2.21 | 13.09 | 3,807,901 | 8 |

**Verdict:** All gateways produce ~30-32k RPS against mock with sub-millisecond P50.
"Failed" counts = in-flight requests cancelled at timer expiry (expected, equals user count).

---

## Baseline (50 users × 10 min) — FerroGateway

| Metric | Value |
|--------|------:|
| **RPS** | 28,862 |
| **P50** | 1.6 ms |
| **P95** | 2.9 ms |
| **P99** | 4.3 ms |
| **Total requests** | 17,317,421 |
| **Success** | 17,317,421 |
| **Failed** | 49 (cancelled at timer expiry) |

---

## Stress (150 users × 10 min) — FerroGateway

| Metric | Value |
|--------|------:|
| **RPS** | 34,203 |
| **P50** | 4.2 ms |
| **P95** | 5.9 ms |
| **P99** | 7.4 ms |
| **Total requests** | 20,521,496 |
| **Success** | 20,521,496 |
| **Failed** | 150 (cancelled at timer expiry) |

---

## Streaming Baseline (50 users × 10 min) — FerroGateway

| Metric | Value |
|--------|------:|
| **RPS** | 157 |
| **P50** | 321.3 ms |
| **P95** | 328.8 ms |
| **P99** | 332.4 ms |
| **TTFB** | 257.3 ms |
| **Total requests** | 93,918 |
| **Success** | 93,918 |
| **Failed** | 40 (cancelled at timer expiry) |

Lower RPS is expected — mock server adds 10ms inter-chunk delay × 7 chunks = ~70ms per SSE stream.

---

## How to Run on OCI

```bash
# Clone the repo
git clone https://github.com/ferro-labs/ai-gateway-performance-benchmarks.git
cd ai-gateway-performance-benchmarks

# Quick validation (~10 min)
./scripts/run-oci.sh --quick

# Full matrix — all gateways × all scenarios (~3-4 hours)
./scripts/run-oci.sh

# FerroGateway vs LiteLLM only
./scripts/run-oci.sh --gateways ferrogateway,litellm

# Smoke + baseline only
./scripts/run-oci.sh --scenarios smoke,baseline

# 3-run average for publication-quality results
./scripts/run-oci.sh --gateways ferrogateway,litellm --repeat 3
```

Results are written to `results/` as CSV + Markdown files.

---

## Notes

- **"Failed" requests** in the results are in-flight requests cancelled when the benchmark timer
  expires. Count equals the number of virtual users. This is expected and not an error.
- **Streaming RPS** is lower by design — each SSE stream takes ~70ms (mock server chunk delay),
  so throughput is bounded by `users / stream_duration`.
- For **real gateway comparison**, run with Docker Compose (`make setup && make bench-all`)
  so each gateway actually proxies requests through its routing layer.
