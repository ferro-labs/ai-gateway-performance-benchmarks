# Ferro Labs AI Gateway Performance Benchmarks
# -----------------------------------------------
# Typical workflow:
#   make build     — compile Go bench runner and mock server into bin/
#   make setup     — pull latest images, build mock-server container, start all services
#   make bench-all — run Go bench runner (all gateways) + k6 + wrk
#   make clean     — stop everything, remove containers/images/bins, delete results
#
#   make run       — one-shot: build → setup → bench-all → print results path
#
# Usage: make help

.DEFAULT_GOAL := help

COMPOSE      := docker compose
# All optional profiles (kong, portkey) — used for setup/clean/run
COMPOSE_ALL  := docker compose --profile kong --profile portkey
BIN_DIR      := bin
RESULTS_DIR  := results

.PHONY: help build setup run update \
        up up-kong up-portkey down mock \
        bench-all \
        bench bench-ferrogateway bench-litellm bench-kong bench-portkey \
        bench-repeat bench-dry \
        bench-k6 bench-k6-baseline bench-k6-peak \
        bench-wrk bench-wrk-light \
        clean

help: ## Show all available targets
@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | \
  awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------------------
# Build — compile both Go binaries
# ---------------------------------------------------------------------------

build: ## Compile Go bench runner and mock server into bin/
@mkdir -p $(BIN_DIR)
go build -o $(BIN_DIR)/bench ./cmd/bench
go build -o $(BIN_DIR)/mockserver ./cmd/mockserver
@echo "Build complete: $(BIN_DIR)/bench  $(BIN_DIR)/mockserver"

# ---------------------------------------------------------------------------
# One-command workflow
# ---------------------------------------------------------------------------

setup: build ## Pull latest images, build mock-server container, start all services
@echo "==> Pulling latest gateway images..."
$(COMPOSE_ALL) pull --ignore-pull-failures
@echo "==> Building mock-server container..."
$(COMPOSE_ALL) build mock-server
@echo "==> Starting all services (waiting until healthy)..."
@# --wait blocks until every service with a healthcheck is healthy (Compose v2.1+)
$(COMPOSE_ALL) up -d --wait
@echo ""
@echo "All services ready:"
@$(COMPOSE_ALL) ps --format "  {{.Service}}: {{.Status}}" 2>/dev/null || $(COMPOSE_ALL) ps
@echo ""
@echo "Run 'make bench-all' to benchmark all gateways."

run: setup bench-all ## Full one-shot: build → setup → benchmark all gateways → print results path
@echo ""
@echo "Benchmarks complete. Results: $(RESULTS_DIR)/"
@ls -1t $(RESULTS_DIR)/ 2>/dev/null | head -1 | xargs -I{} echo "Latest run: $(RESULTS_DIR)/{}"

update: ## Pull latest images for all gateways and rebuild mock-server
$(COMPOSE_ALL) pull --ignore-pull-failures
$(COMPOSE_ALL) build mock-server
$(COMPOSE_ALL) up -d --wait
@echo "Update complete."

# ---------------------------------------------------------------------------
# Infrastructure
# ---------------------------------------------------------------------------

up: ## Start mock server + FerroGateway + LiteLLM (core services only)
$(COMPOSE) up -d --wait

up-kong: ## Start core services + Kong
$(COMPOSE) --profile kong up -d --wait

up-portkey: ## Start core services + Portkey
$(COMPOSE) --profile portkey up -d --wait

down: ## Stop all services (all profiles) — does not remove images or results
$(COMPOSE_ALL) down --remove-orphans

mock: ## Run the Go mock server locally (outside Docker), port 9000
$(BIN_DIR)/mockserver --port 9000

# ---------------------------------------------------------------------------
# bench-all — run all benchmark types in sequence
# ---------------------------------------------------------------------------

bench-all: ## Go bench (all gateways × all scenarios) + k6 + wrk
@echo "==> [1/3] Go comparative benchmarks (all gateways)..."
$(BIN_DIR)/bench -config benchmarks.yaml -dotenv .env -out-dir $(RESULTS_DIR)
@echo ""
@echo "==> [2/3] k6 high-VU throughput benchmark (FerroGateway)..."
@mkdir -p $(RESULTS_DIR)/k6
@if which k6 > /dev/null 2>&1; then \
    k6 run \
      --out json=$(RESULTS_DIR)/k6/ferrogateway-$$(date +%Y%m%d-%H%M%S).json \
      k6/chat_completions.js; \
else \
    echo "  [skip] k6 not installed — https://k6.io/docs/get-started/installation/"; \
fi
@echo ""
@echo "==> [3/3] wrk peak-RPS benchmark (FerroGateway)..."
@if which wrk > /dev/null 2>&1; then \
    wrk -t12 -c500 -d60s -s wrk/chat_completions.lua http://localhost:8080; \
else \
    echo "  [skip] wrk not installed — sudo apt-get install wrk  (or: brew install wrk)"; \
fi

# ---------------------------------------------------------------------------
# Go comparative benchmarks
# ---------------------------------------------------------------------------

bench: ## Go — full matrix (all gateways × all scenarios)
$(BIN_DIR)/bench -config benchmarks.yaml -dotenv .env -out-dir $(RESULTS_DIR)

bench-ferrogateway: ## Go — FerroGateway only
$(BIN_DIR)/bench -config benchmarks.yaml -dotenv .env -out-dir $(RESULTS_DIR) -gateways ferrogateway

bench-litellm: ## Go — LiteLLM only
$(BIN_DIR)/bench -config benchmarks.yaml -dotenv .env -out-dir $(RESULTS_DIR) -gateways litellm

bench-kong: ## Go — Kong only
$(BIN_DIR)/bench -config benchmarks.yaml -dotenv .env -out-dir $(RESULTS_DIR) -gateways kong

bench-portkey: ## Go — Portkey only
$(BIN_DIR)/bench -config benchmarks.yaml -dotenv .env -out-dir $(RESULTS_DIR) -gateways portkey

bench-repeat: ## Go — full matrix, 3 runs averaged
$(BIN_DIR)/bench -config benchmarks.yaml -dotenv .env -out-dir $(RESULTS_DIR) -repeat 3

bench-dry: ## Go — preview matrix without executing
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
# Clean
# ---------------------------------------------------------------------------

clean: ## Stop all services, remove images/bins, delete results
@echo "==> Stopping all services..."
$(COMPOSE_ALL) down --volumes --remove-orphans
@echo "==> Removing pulled gateway images..."
docker rmi ghcr.io/ferro-labs/ai-gateway:latest 2>/dev/null || true
docker rmi ghcr.io/berriai/litellm:main-stable 2>/dev/null || true
docker rmi kong:latest 2>/dev/null || true
docker rmi portkeyai/gateway:latest 2>/dev/null || true
docker rmi bench-mockserver:latest 2>/dev/null || true
@echo "==> Removing build artifacts and results..."
rm -rf $(BIN_DIR) $(RESULTS_DIR)
@echo "Clean complete."
