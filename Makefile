# Ferro Labs AI Gateway Performance Benchmarks
# -----------------------------------------------
# All gateways run as native processes for accurate µs-level measurements.
#
# Typical workflow:
#   make setup     — install all native dependencies (latest versions)
#   make bench     — run full benchmark suite (all gateways)
#   make report    — generate BENCHMARK-REPORT.md from results
#   make clean     — remove binaries, results, and virtualenv
#
# Usage: make help

.DEFAULT_GOAL := help

BIN_DIR      := bin
RESULTS_DIR  := results

.PHONY: help build \
        setup setup-mockserver setup-ferro setup-litellm setup-bifrost setup-kong \
        bench bench-ferrogateway bench-litellm bench-bifrost bench-kong \
        bench-repeat bench-dry \
        bench-k6 bench-k6-baseline bench-k6-peak \
        bench-wrk bench-wrk-light \
        report clean

help: ## Show all available targets
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | \
	awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

build: ## Compile Go bench runner, mock server, and report generator into bin/
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/bench ./cmd/bench
	go build -o $(BIN_DIR)/mockserver ./cmd/mockserver
	go build -o $(BIN_DIR)/report ./cmd/report
	@echo "Build complete: $(BIN_DIR)/bench  $(BIN_DIR)/mockserver  $(BIN_DIR)/report"

# ---------------------------------------------------------------------------
# Setup — install native dependencies (latest versions)
# ---------------------------------------------------------------------------

setup: ## Install all gateways natively (latest versions)
	bash scripts/setup-all.sh

setup-mockserver: ## Build Go mock server and bench tools from source
	bash scripts/setup-mockserver.sh

setup-ferro: ## Install latest Ferro AI Gateway binary
	bash scripts/setup-ferro.sh

setup-litellm: ## Install latest LiteLLM in .venv-litellm
	bash scripts/setup-litellm.sh

setup-bifrost: ## Install latest Bifrost binary
	bash scripts/setup-bifrost.sh

setup-kong: ## Install latest Kong natively (apt)
	bash scripts/setup-kong.sh

# ---------------------------------------------------------------------------
# Benchmarks — native process isolation
# ---------------------------------------------------------------------------

bench: ## Run full benchmark suite (all gateways, native processes)
	./scripts/run-benchmarks.sh

bench-ferrogateway: ## Benchmark Ferro AI Gateway only
	./scripts/run-benchmarks.sh --gateways ferrogateway

bench-litellm: ## Benchmark LiteLLM only
	./scripts/run-benchmarks.sh --gateways litellm

bench-bifrost: ## Benchmark Bifrost only
	./scripts/run-benchmarks.sh --gateways bifrost

bench-kong: ## Benchmark Kong only
	./scripts/run-benchmarks.sh --gateways kong

bench-repeat: ## Full benchmark suite, 3 runs averaged (publication quality)
	./scripts/run-benchmarks.sh --repeat 3

bench-dry: ## Preview benchmark matrix without executing
	$(BIN_DIR)/bench -config benchmarks.yaml -dotenv .env -dry-run

# ---------------------------------------------------------------------------
# k6 high-VU throughput benchmarks
# ---------------------------------------------------------------------------

bench-k6: ## k6 — baseline + stress + peak_5k (requires k6)
	@mkdir -p $(RESULTS_DIR)/k6
	k6 run \
	  --out json=$(RESULTS_DIR)/k6/ferrogateway-$(shell date +%Y%m%d-%H%M%S).json \
	  k6/chat_completions.js

bench-k6-baseline: ## k6 — baseline scenario only
	K6_SCENARIO=baseline k6 run \
	  --out json=$(RESULTS_DIR)/k6/baseline-$(shell date +%Y%m%d-%H%M%S).json \
	  k6/chat_completions.js

bench-k6-peak: ## k6 — peak_5k ramp only
	K6_SCENARIO=peak_5k k6 run \
	  --out json=$(RESULTS_DIR)/k6/peak5k-$(shell date +%Y%m%d-%H%M%S).json \
	  k6/chat_completions.js

# ---------------------------------------------------------------------------
# wrk peak-RPS benchmarks
# ---------------------------------------------------------------------------

bench-wrk: ## wrk — 12 threads, 500 connections, 60s (requires wrk)
	wrk -t12 -c500 -d60s \
	  -s wrk/chat_completions.lua \
	  http://localhost:8080

bench-wrk-light: ## wrk — 4 threads, 100 connections, 30s
	wrk -t4 -c100 -d30s \
	  -s wrk/chat_completions.lua \
	  http://localhost:8080

# ---------------------------------------------------------------------------
# Report
# ---------------------------------------------------------------------------

report: ## Generate Markdown + JSON report from latest results
	go run ./cmd/report --input=$(RESULTS_DIR)

# ---------------------------------------------------------------------------
# Clean
# ---------------------------------------------------------------------------

clean: ## Remove binaries, results, virtualenv, and setup marker
	rm -rf $(BIN_DIR) $(RESULTS_DIR) .venv-litellm .setup-complete .litellm-version
	@echo "Clean complete."
