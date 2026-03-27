// cmd/realbench — Real-world overhead benchmark.
//
// Measures AI Gateway overhead against live LLM APIs using two independent
// methods: (1) paired direct-vs-gateway requests and (2) X-Gateway-Overhead-Ms
// response header instrumentation.
//
// Usage:
//
//	realbench [-config realworld.yaml] [-dotenv .env] [-out-dir results]
//	          [-samples 200] [-warmup 10]
package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Config types — mirror realworld.yaml structure
// ---------------------------------------------------------------------------

type Config struct {
	Direct   EndpointConfig   `yaml:"direct"`
	Gateway  EndpointConfig   `yaml:"gateway"`
	Scenarios []ScenarioConfig `yaml:"scenarios"`
}

type EndpointConfig struct {
	BaseURL     string `yaml:"base_url"`
	APIKey      string `yaml:"api_key"`
	RequestPath string `yaml:"request_path"`
	Model       string `yaml:"model"`
}

type ScenarioConfig struct {
	Name      string `yaml:"name"`
	Samples   int    `yaml:"samples"`
	Prompt    string `yaml:"prompt"`
	MaxTokens int    `yaml:"max_tokens"`
	Stream    bool   `yaml:"stream"`
	Notes     string `yaml:"notes"`
}

// ---------------------------------------------------------------------------
// Sample data
// ---------------------------------------------------------------------------

type Sample struct {
	Scenario        string
	Index           int
	DirectMs        float64
	GatewayMs       float64
	OverheadHeaderMs float64 // from X-Gateway-Overhead-Ms header
	DeltaMs         float64 // gateway - direct
	Stream          bool
	DirectTTFBMs    float64
	GatewayTTFBMs   float64
	TTFBDeltaMs     float64
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	configFile := flag.String("config", "realworld.yaml", "Benchmark config file")
	dotenvFile := flag.String("dotenv", ".env", "Env file to load")
	outDir := flag.String("out-dir", "results", "Output directory")
	samplesOverride := flag.Int("samples", 0, "Override sample count per scenario (0 = use config)")
	warmupCount := flag.Int("warmup", 10, "Warmup requests per endpoint before measurement")
	delayMs := flag.Int("delay", 200, "Delay between pairs in ms (rate limit protection)")
	flag.Parse()

	if err := loadDotenv(*dotenvFile); err != nil && !os.IsNotExist(err) {
		log.Printf("warning: could not load %s: %v", *dotenvFile, err)
	}

	raw, err := os.ReadFile(*configFile)
	if err != nil {
		log.Fatalf("cannot read config: %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(raw))), &cfg); err != nil {
		log.Fatalf("cannot parse config: %v", err)
	}

	if cfg.Direct.APIKey == "" {
		log.Fatal("direct.api_key is required (set OPENAI_API_KEY in .env)")
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("cannot create output directory: %v", err)
	}

	client := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 50,
		},
	}
	defer client.CloseIdleConnections()

	// Warmup both endpoints
	if *warmupCount > 0 {
		fmt.Printf("Warming up (%d requests per endpoint)...\n", *warmupCount)
		warmupPayload := buildPayload(cfg.Direct.Model, "Say hello.", 5, false)
		for i := range *warmupCount {
			_ = i
			sendOne(client, cfg.Direct.BaseURL+cfg.Direct.RequestPath, cfg.Direct.APIKey, warmupPayload, false)
			sendOne(client, cfg.Gateway.BaseURL+cfg.Gateway.RequestPath, cfg.Gateway.APIKey, warmupPayload, false)
		}
		fmt.Println("Warmup complete.")
	}

	timestamp := time.Now().Format("20060102-150405")
	var allSamples []Sample

	for _, sc := range cfg.Scenarios {
		samples := sc.Samples
		if *samplesOverride > 0 {
			samples = *samplesOverride
		}

		fmt.Printf("\n==> scenario=%-25s  stream=%v  samples=%d\n", sc.Name, sc.Stream, samples)
		fmt.Printf("    %s\n", sc.Notes)

		model := cfg.Gateway.Model
		if model == "" {
			model = cfg.Direct.Model
		}
		payload := buildPayload(model, sc.Prompt, sc.MaxTokens, sc.Stream)
		directPayload := buildPayload(cfg.Direct.Model, sc.Prompt, sc.MaxTokens, sc.Stream)

		directURL := cfg.Direct.BaseURL + cfg.Direct.RequestPath
		gatewayURL := cfg.Gateway.BaseURL + cfg.Gateway.RequestPath

		for i := range samples {
			// Randomize order to cancel temporal bias
			directFirst := rand.Intn(2) == 0

			var directMs, gatewayMs, overheadHeader float64
			var directTTFB, gatewayTTFB float64

			if directFirst {
				directMs, directTTFB, _ = sendOne(client, directURL, cfg.Direct.APIKey, directPayload, sc.Stream)
				gatewayMs, gatewayTTFB, overheadHeader = sendOne(client, gatewayURL, cfg.Gateway.APIKey, payload, sc.Stream)
			} else {
				gatewayMs, gatewayTTFB, overheadHeader = sendOne(client, gatewayURL, cfg.Gateway.APIKey, payload, sc.Stream)
				directMs, directTTFB, _ = sendOne(client, directURL, cfg.Direct.APIKey, directPayload, sc.Stream)
			}

			s := Sample{
				Scenario:        sc.Name,
				Index:           i + 1,
				DirectMs:        directMs,
				GatewayMs:       gatewayMs,
				OverheadHeaderMs: overheadHeader,
				DeltaMs:         gatewayMs - directMs,
				Stream:          sc.Stream,
				DirectTTFBMs:    directTTFB,
				GatewayTTFBMs:   gatewayTTFB,
				TTFBDeltaMs:     gatewayTTFB - directTTFB,
			}
			allSamples = append(allSamples, s)

			if (i+1)%50 == 0 || i+1 == samples {
				headerP50 := medianOf(allSamples, sc.Name, func(s Sample) float64 { return s.OverheadHeaderMs })
			fmt.Printf("    [%d/%d] direct_p50=%.0fms  gateway_p50=%.0fms  header_overhead=%.3fms  paired_delta=%.1fms\n",
					i+1, samples,
					medianOf(allSamples, sc.Name, func(s Sample) float64 { return s.DirectMs }),
					medianOf(allSamples, sc.Name, func(s Sample) float64 { return s.GatewayMs }),
					headerP50,
					medianOf(allSamples, sc.Name, func(s Sample) float64 { return s.DeltaMs }),
				)
			}

			if *delayMs > 0 {
				time.Sleep(time.Duration(*delayMs) * time.Millisecond)
			}
		}
	}

	csvPath := filepath.Join(*outDir, fmt.Sprintf("realworld-%s.csv", timestamp))
	mdPath := filepath.Join(*outDir, fmt.Sprintf("realworld-%s.md", timestamp))

	writeCSV(csvPath, allSamples)
	writeSummary(mdPath, cfg, allSamples, timestamp)
	fmt.Printf("\nResults written to:\n  %s\n  %s\n", csvPath, mdPath)
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func buildPayload(model, prompt string, maxTokens int, stream bool) []byte {
	payload := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
		"max_tokens": maxTokens,
	}
	if stream {
		payload["stream"] = true
	}
	b, _ := json.Marshal(payload)
	return b
}

// sendOne sends a single request and returns (latencyMs, ttfbMs, overheadHeaderMs).
// ttfbMs is only set for streaming requests. overheadHeaderMs is from the
// X-Gateway-Overhead-Ms response header (0 if absent).
func sendOne(client *http.Client, url, apiKey string, payload []byte, stream bool) (float64, float64, float64) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		log.Printf("request error: %v", err)
		return 0, 0, 0
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("request error: %v", err)
		return 0, 0, 0
	}
	defer resp.Body.Close()

	var ttfbMs float64
	if stream {
		buf := make([]byte, 1)
		if _, err := resp.Body.Read(buf); err != nil && err != io.EOF {
			log.Printf("stream read error: %v", err)
		}
		ttfbMs = float64(time.Since(start).Microseconds()) / 1000.0
	}

	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	latencyMs := float64(time.Since(start).Microseconds()) / 1000.0

	var overheadMs float64
	if oh := resp.Header.Get("X-Gateway-Overhead-Ms"); oh != "" {
		overheadMs, _ = strconv.ParseFloat(oh, 64)
	}

	if resp.StatusCode >= 400 {
		log.Printf("HTTP %d from %s", resp.StatusCode, url)
	}

	return latencyMs, ttfbMs, overheadMs
}

// ---------------------------------------------------------------------------
// Statistics
// ---------------------------------------------------------------------------

func percentiles(data []float64) (p50, p95, p99 float64) {
	if len(data) == 0 {
		return 0, 0, 0
	}
	sorted := make([]float64, len(data))
	copy(sorted, data)
	sort.Float64s(sorted)
	n := float64(len(sorted))
	idx := func(pct float64) int {
		return int(math.Min(float64(len(sorted)-1), math.Ceil(n*pct)-1))
	}
	return sorted[idx(0.50)], sorted[idx(0.95)], sorted[idx(0.99)]
}

func medianOf(samples []Sample, scenario string, extract func(Sample) float64) float64 {
	var vals []float64
	for _, s := range samples {
		if s.Scenario == scenario {
			v := extract(s)
			if v > 0 {
				vals = append(vals, v)
			}
		}
	}
	if len(vals) == 0 {
		return 0
	}
	p50, _, _ := percentiles(vals)
	return p50
}

func extractField(samples []Sample, scenario string, extract func(Sample) float64) []float64 {
	var vals []float64
	for _, s := range samples {
		if s.Scenario == scenario {
			vals = append(vals, extract(s))
		}
	}
	return vals
}

// bootstrap95CI computes a 95% confidence interval via bootstrap resampling.
func bootstrap95CI(data []float64, iterations int) (lo, hi float64) {
	if len(data) == 0 {
		return 0, 0
	}
	medians := make([]float64, iterations)
	n := len(data)
	for i := range iterations {
		sample := make([]float64, n)
		for j := range n {
			sample[j] = data[rand.Intn(n)]
		}
		sort.Float64s(sample)
		medians[i] = sample[len(sample)/2]
	}
	sort.Float64s(medians)
	lo = medians[int(float64(iterations)*0.025)]
	hi = medians[int(float64(iterations)*0.975)]
	return lo, hi
}

// ---------------------------------------------------------------------------
// Output: CSV
// ---------------------------------------------------------------------------

func writeCSV(path string, samples []Sample) {
	f, err := os.Create(path)
	if err != nil {
		log.Printf("warning: cannot write CSV: %v", err)
		return
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	_ = w.Write([]string{
		"scenario", "sample", "stream",
		"direct_ms", "gateway_ms", "overhead_header_ms", "delta_ms",
		"ttfb_direct_ms", "ttfb_gateway_ms", "ttfb_delta_ms",
	})
	for _, s := range samples {
		optFloat := func(v float64) string {
			if v > 0 {
				return strconv.FormatFloat(v, 'f', 3, 64)
			}
			return ""
		}
		_ = w.Write([]string{
			s.Scenario,
			strconv.Itoa(s.Index),
			strconv.FormatBool(s.Stream),
			strconv.FormatFloat(s.DirectMs, 'f', 3, 64),
			strconv.FormatFloat(s.GatewayMs, 'f', 3, 64),
			optFloat(s.OverheadHeaderMs),
			strconv.FormatFloat(s.DeltaMs, 'f', 3, 64),
			optFloat(s.DirectTTFBMs),
			optFloat(s.GatewayTTFBMs),
			optFloat(s.TTFBDeltaMs),
		})
	}
}

// ---------------------------------------------------------------------------
// Output: Markdown summary
// ---------------------------------------------------------------------------

func writeSummary(path string, cfg Config, samples []Sample, timestamp string) {
	f, err := os.Create(path)
	if err != nil {
		log.Printf("warning: cannot write summary: %v", err)
		return
	}
	defer f.Close()

	model := cfg.Gateway.Model
	if model == "" {
		model = cfg.Direct.Model
	}

	fmt.Fprintf(f, "# Real-World Overhead Benchmark\n\n")
	fmt.Fprintf(f, "> **Model**: %s | **Date**: %s | **Gateway**: Ferro Labs AI Gateway\n\n",
		model, time.Now().Format("2006-01-02"))
	fmt.Fprintf(f, "Two independent measurement methods:\n")
	fmt.Fprintf(f, "1. **Header**: Gateway self-reported `X-Gateway-Overhead-Ms` response header\n")
	fmt.Fprintf(f, "2. **Paired**: Latency delta from paired direct-vs-gateway requests\n\n")

	// Collect unique scenarios
	seen := map[string]bool{}
	var scenarios []string
	for _, s := range samples {
		if !seen[s.Scenario] {
			seen[s.Scenario] = true
			scenarios = append(scenarios, s.Scenario)
		}
	}

	// Non-streaming results
	fmt.Fprintf(f, "## Non-Streaming Results\n\n")
	fmt.Fprintf(f, "| Scenario | Samples | Header p50 | Header p99 | Paired p50 | Paired p99 | 95%% CI |\n")
	fmt.Fprintf(f, "|---|---:|---:|---:|---:|---:|---|\n")
	for _, sc := range scenarios {
		scSamples := extractField(samples, sc, func(s Sample) float64 { return s.DeltaMs })
		if len(scSamples) == 0 {
			continue
		}
		first := findFirst(samples, sc)
		if first.Stream {
			continue
		}

		headerVals := extractField(samples, sc, func(s Sample) float64 { return s.OverheadHeaderMs })
		hp50, _, hp99 := percentiles(headerVals)
		dp50, _, dp99 := percentiles(scSamples)
		lo, hi := bootstrap95CI(scSamples, 10000)

		fmt.Fprintf(f, "| %s | %d | %.3fms | %.3fms | %.1fms | %.1fms | [%.1f, %.1f] |\n",
			sc, len(scSamples), hp50, hp99, dp50, dp99, lo, hi)
	}

	// Streaming results (TTFB)
	hasStreaming := false
	for _, sc := range scenarios {
		first := findFirst(samples, sc)
		if first.Stream {
			hasStreaming = true
			break
		}
	}
	if hasStreaming {
		fmt.Fprintf(f, "\n## Streaming Results (TTFB)\n\n")
		fmt.Fprintf(f, "| Scenario | Samples | TTFB Delta p50 | TTFB Delta p99 | 95%% CI |\n")
		fmt.Fprintf(f, "|---|---:|---:|---:|---|\n")
		for _, sc := range scenarios {
			first := findFirst(samples, sc)
			if !first.Stream {
				continue
			}
			ttfbDeltas := extractField(samples, sc, func(s Sample) float64 { return s.TTFBDeltaMs })
			if len(ttfbDeltas) == 0 {
				continue
			}
			tp50, _, tp99 := percentiles(ttfbDeltas)
			lo, hi := bootstrap95CI(ttfbDeltas, 10000)
			fmt.Fprintf(f, "| %s | %d | %.2fms | %.2fms | [%.1f, %.1f] |\n",
				sc, len(ttfbDeltas), tp50, tp99, lo, hi)
		}
	}

	// Validation section
	fmt.Fprintf(f, "\n## Methodology\n\n")
	fmt.Fprintf(f, "- **Paired requests**: Each sample sends the same prompt to both OpenAI directly and through the gateway\n")
	fmt.Fprintf(f, "- **Randomized order**: Request order is coin-flipped per pair to cancel temporal bias\n")
	fmt.Fprintf(f, "- **Low concurrency**: Single-threaded sequential pairs (not a throughput test)\n")
	fmt.Fprintf(f, "- **Bootstrap CI**: 95%% confidence interval via 10,000 bootstrap resamples of the median\n")
	fmt.Fprintf(f, "- **Two methods agree**: Header and paired measurements should be within ~0.5ms of each other\n")
}

func findFirst(samples []Sample, scenario string) Sample {
	for _, s := range samples {
		if s.Scenario == scenario {
			return s
		}
	}
	return Sample{}
}

// ---------------------------------------------------------------------------
// Utility: dotenv loader (same as cmd/bench)
// ---------------------------------------------------------------------------

func loadDotenv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if len(val) >= 2 &&
			((val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		if os.Getenv(key) == "" {
			os.Setenv(key, val) //nolint:errcheck
		}
	}
	return scanner.Err()
}
