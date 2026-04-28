# Contributing to AI Gateway Performance Benchmarks

Thank you for your interest in contributing to the Ferro Labs AI Gateway benchmarks!

## Guidelines

This repository follows the same contributing guidelines as the main AI Gateway project. Please see the [AI Gateway CONTRIBUTING.md](https://github.com/ferro-labs/ai-gateway/blob/main/CONTRIBUTING.md) for:

- Code style and conventions
- Commit message format (Conventional Commits)
- Pull request process

## Adding a New Gateway

1. Create a setup script in `scripts/setup-<gateway>.sh`
2. Add a configuration file in `configs/<gateway>.config.*`
3. Add the gateway entry to `benchmarks.yaml`
4. Test locally with `make bench GATEWAYS=<gateway>`
5. Document any prerequisites in the setup script header

## Adding a New Scenario

1. Add the scenario to `benchmarks.yaml` under the `scenarios` section
2. Include warmup duration, spawn rate, and appropriate timeouts
3. Test with at least one gateway before submitting

## Running Benchmarks

```bash
cp .env.example .env   # Configure API keys
make setup             # Install dependencies
make bench             # Run full benchmark suite
```

## Fair Benchmarking Principles

- All gateways run as native processes (no Docker networking overhead)
- Same mock backend for all gateways
- Default configurations only (no per-gateway tuning)
- Warmup phase before measurement
- Multiple runs for publication-quality results

## Questions?

Open a [GitHub Discussion](https://github.com/ferro-labs/ai-gateway/discussions) or reach out on [Discord](https://discord.gg/YYSKrgBXMz).
