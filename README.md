# AI Gateway Performance Benchmarks

Reproducible benchmarking suite to compare **Ferro AI Gateway** against **LiteLLM**, **Portkey AI**, and **Kong** under the same load profile.

This setup is inspired by the benchmark style in `BerriAI/litellm-performance-benchmarks`, but organized as a reusable harness so you can rerun tests, change workloads, and regenerate summary tables.

## What this benchmark measures

- End-to-end user latency from load generator to gateway (Locust metrics)
- Throughput (`Requests/s`)
- Error rate (`Failure %`)
- Percentiles (`p50`, `p95`, `p99`)
- Streaming (SSE) performance via the `streaming-*` scenarios

## Structure

- `locustfile.py`: OpenAI-compatible chat completion load test (blocking + SSE streaming).
- `benchmarks.yaml`: benchmark matrix (gateways + scenarios).
- `scripts/run_benchmarks.py`: orchestrates runs and writes report files.
- `scripts/mock_server.py`: zero-latency OpenAI-compatible mock server for isolated overhead measurement.
- `configs/`: gateway config templates.
- `results/<timestamp>/`: generated CSV + summary files.

## Quick start — isolated mode (recommended)

Isolated mode routes all gateways to a local mock server, so measurements reflect **gateway overhead only**, not live LLM latency.

1. Install Python dependencies:

```bash
cd ai-gateway-performance-benchmarks
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

2. Start all gateway services via Docker Compose (builds FerroGateway from `../ai-gateway`):

```bash
docker compose up -d
# Optional: also start Kong
docker compose --profile kong up -d
```

3. Run full benchmark matrix:

```bash
python scripts/run_benchmarks.py --gateways ferrogateway,litellm
```

4. Check output:

- `results/<timestamp>/summary.md`
- `results/<timestamp>/summary.json`
- `results/<timestamp>/*_stats.csv`

## Quick start — real upstream mode

Uses live API keys. Gateway overhead is much smaller than upstream latency so differences between gateways will be noisier.

1. Configure target gateways:

```bash
cp .env.example .env
# Fill gateway URLs and API keys in .env
```

2. Ensure all gateways route to the **same upstream model** and similar infra.

3. Run:

```bash
python scripts/run_benchmarks.py
```

## Useful commands

Run only one scenario:

```bash
python scripts/run_benchmarks.py --scenarios baseline
```

Run only two gateways:

```bash
python scripts/run_benchmarks.py --gateways ferrogateway,litellm
```

Repeat each run 3 times and average results:

```bash
python scripts/run_benchmarks.py --repeat 3
```

Preview commands without executing:

```bash
python scripts/run_benchmarks.py --dry-run
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

`scripts/mock_server.py` is a zero-dependency stdlib server returning instant fixed responses. Use it to measure pure gateway overhead:

```bash
python scripts/mock_server.py --port 9000 --latency-ms 0
```

Supports both blocking and SSE streaming (`stream: true`) matching the OpenAI API shape.

## Fair benchmark checklist

- Use same model and token settings across all gateways.
- Keep gateway deployments in similar regions/specs.
- Warm up each gateway before measured runs.
- Run each scenario multiple times (`--repeat 3`) and compare averaged results.
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
