# Gateway Configurations

These are the exact configs used in the benchmark run.
No tuning was applied — all gateways ran with default settings.

## Philosophy

We deliberately did NOT tune any gateway for maximum performance.
Default configs represent what a developer gets out of the box.
Tuned benchmarks favor whoever spent more time optimizing.

## Versions Tested

| Gateway    | Version  | Language    | Started As        |
|------------|----------|-------------|-------------------|
| Ferro Labs | v1.0.0   | Go          | Native binary     |
| Kong OSS   | 3.9.1    | Go/Lua      | Native process    |
| Bifrost    | v1.0.0   | Go          | Native binary     |
| LiteLLM    | 1.82.6   | Python      | Native process    |
| Portkey    | latest   | TS/Node.js  | Docker host net   |

## Reproduce Any Gateway

### Ferro Labs
```bash
./bin/ferro-gw --config configs/ferrogateway.config.yaml
```

### Bifrost
```bash
mkdir -p /tmp/bifrost && cp configs/bifrost.config.json /tmp/bifrost/config.json
./bin/bifrost --app-dir /tmp/bifrost --host 0.0.0.0 --port 8081
```

### LiteLLM
```bash
pip install litellm[proxy]==1.82.6
litellm --config configs/litellm.native.config.yaml --port 4000
```

### Kong
```bash
kong start --conf configs/kong.conf
```

### Portkey
```bash
docker run --network host -e LOG_LEVEL=error portkeyai/gateway:latest
```

## Important Notes

- Bifrost: model format must be `provider/model` (e.g. `openai/gpt-4o`)
- LiteLLM: health endpoint is `/health/liveliness` not `/health`
- Kong: requires `KONG_DECLARATIVE_CONFIG` env var pointing to kong.yaml
- Portkey: requires `x-portkey-custom-host` header for mock routing
