/**
 * k6 load test — FerroGateway vs any OpenAI-compatible gateway
 *
 * Designed for high-VU throughput measurement. Complements the Go
 * benchmark runner (cmd/bench/main.go) by enabling extreme ramp-to-peak
 * tests that Go is not optimised for.
 *
 * Environment variables (all optional):
 *   K6_GATEWAY_URL   Base URL of the gateway under test. Default: http://localhost:8080
 *   K6_API_KEY       Bearer token. Default: (empty)
 *   K6_MODEL         Model name forwarded in the request body. Default: gpt-4o-mini
 *   K6_P95_THRESHOLD Target p95 latency (ms). Default: 100ms (for mock). Real upstreams: 500-2000ms
 *   K6_P99_THRESHOLD Target p99 latency (ms). Default: 250ms (for mock). Real upstreams: 1000-5000ms
 *
 * Usage (against local docker-compose setup):
 *   k6 run k6/chat_completions.js
 *
 * Usage (against a specific gateway):
 *   K6_GATEWAY_URL=http://localhost:4000 K6_API_KEY=mykey k6 run k6/chat_completions.js
 *
 * Run with relaxed thresholds for real upstream:
 *   K6_P95_THRESHOLD=500 K6_P99_THRESHOLD=1000 k6 run k6/chat_completions.js
 *
 * Run a single scenario:
 *   k6 run --env K6_SCENARIO=baseline k6/chat_completions.js
 *
 * Save JSON results:
 *   k6 run --out json=results/k6-ferrogateway.json k6/chat_completions.js
 */

import http from 'k6/http';
import { check } from 'k6';
import { Rate, Trend } from 'k6/metrics';

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------
const GATEWAY_URL = __ENV.K6_GATEWAY_URL || 'http://localhost:8080';
const API_KEY = __ENV.K6_API_KEY || '';
const MODEL = __ENV.K6_MODEL || 'gpt-4o-mini';
const SCENARIO = __ENV.K6_SCENARIO || ''; // empty = run all
const P95_THRESHOLD = __ENV.K6_P95_THRESHOLD || 100;
const P99_THRESHOLD = __ENV.K6_P99_THRESHOLD || 250;

const TARGET_URL = `${GATEWAY_URL}/v1/chat/completions`;

// ---------------------------------------------------------------------------
// Custom metrics
// ---------------------------------------------------------------------------
const errorRate = new Rate('errors');
const gatewayLatency = new Trend('gateway_latency_ms', true);

// ---------------------------------------------------------------------------
// Scenarios
//
// baseline   — steady 50 VU, 2 min  (matches Locust baseline scenario)
// stress     — steady 150 VU, 5 min (matches Locust stress scenario)
// peak_5k    — ramp 0→1k→3k→5k, isolates extreme-load behaviour
//              (single-host numbers will saturate CPU first; use dedicated hardware)
// ---------------------------------------------------------------------------
const ALL_SCENARIOS = {
    baseline: {
        executor: 'constant-vus',
        vus: 50,
        duration: '2m',
        tags: { scenario: 'baseline' },
    },
    stress: {
        executor: 'constant-vus',
        vus: 150,
        duration: '5m',
        tags: { scenario: 'stress' },
    },
    peak_5k: {
        executor: 'ramping-vus',
        startVUs: 0,
        stages: [
            { duration: '30s', target: 1000 },
            { duration: '60s', target: 3000 },
            { duration: '60s', target: 5000 },
            { duration: '30s', target: 0 },
        ],
        gracefulRampDown: '15s',
        tags: { scenario: 'peak_5k' },
    },
};

function buildScenarios() {
    if (SCENARIO && ALL_SCENARIOS[SCENARIO]) {
        return { [SCENARIO]: ALL_SCENARIOS[SCENARIO] };
    }
    return ALL_SCENARIOS;
}

export const options = {
    scenarios: buildScenarios(),
    thresholds: {
        // Thresholds are configurable via K6_P95_THRESHOLD and K6_P99_THRESHOLD.
        // Defaults: 100ms/250ms (mock server, zero upstream latency)
        // For real upstream: relax to 500ms/1000ms+ depending on provider
        http_req_failed: ['rate<0.01'],       // < 1% error rate
        http_req_duration: [
            `p(95)<${P95_THRESHOLD}`,
            `p(99)<${P99_THRESHOLD}`
        ],
    },
};

// ---------------------------------------------------------------------------
// Request payload & headers (built once, reused per VU)
// ---------------------------------------------------------------------------
const payload = JSON.stringify({
    model: MODEL,
    messages: [{ role: 'user', content: 'Return one short sentence about latency.' }],
    max_tokens: 16,
    stream: false,
});

const headers = {
    'Content-Type': 'application/json',
    ...(API_KEY ? { Authorization: `Bearer ${API_KEY}` } : {}),
};

const requestParams = { headers };

// ---------------------------------------------------------------------------
// Default function — executed by each VU on each iteration
// ---------------------------------------------------------------------------
export default function () {
    const res = http.post(TARGET_URL, payload, requestParams);

    const ok = check(res, {
        'status 200': (r) => r.status === 200,
        'has choices': (r) => {
            try { return r.json('choices') !== undefined; } catch { return false; }
        },
    });

    errorRate.add(!ok);
    gatewayLatency.add(res.timings.duration);
}
