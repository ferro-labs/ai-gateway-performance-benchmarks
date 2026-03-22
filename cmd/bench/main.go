// cmd/bench — Go-native benchmark orchestrator.
//
// Reads benchmarks.yaml, spawns virtual users, collects latency percentiles,
// and writes a CSV + Markdown summary to the output directory.
//
// Usage:
//
//	bench [-config benchmarks.yaml] [-dotenv .env] [-out-dir results]
//	      [-gateways ferrogateway,litellm] [-scenarios smoke,baseline]
//	      [-repeat 1] [-dry-run]
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Config types — mirror benchmarks.yaml structure
// ---------------------------------------------------------------------------

type Config struct {
	Gateways  map[string]GatewayConfig `yaml:"gateways"`
	Scenarios []ScenarioConfig         `yaml:"scenarios"`
}

type GatewayConfig struct {
	BaseURL      string            `yaml:"base_url"`
	APIKey       string            `yaml:"api_key"`
	RequestPath  string            `yaml:"request_path"`
	Model        string            `yaml:"model"`
	ExtraHeaders map[string]string `yaml:"extra_headers"`
}

type ScenarioConfig struct {
	Name      string `yaml:"name"`
	Users     int    `yaml:"users"`
	SpawnRate int    `yaml:"spawn_rate"`
	Warmup    string `yaml:"warmup_duration"`
	Duration  string `yaml:"duration"`
	Prompt    string `yaml:"prompt"`
	MaxTokens int    `yaml:"max_tokens"`
	Stream    bool   `yaml:"stream"`
}

// ---------------------------------------------------------------------------
// Result
// ---------------------------------------------------------------------------

type Result struct {
	Gateway  string
	Scenario string
	Users    int
	Duration time.Duration
	Total    int64
	Success  int64
	Failed   int64
	Min      float64
	P50      float64
	P95      float64
	P99      float64
	P999     float64
	Max      float64
	RPS      float64
	TTFB     float64 // Time-to-first-byte for streaming (ms)
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	configFile := flag.String("config", "benchmarks.yaml", "Benchmark config file")
	dotenvFile := flag.String("dotenv", ".env", "Env file to load")
	outDir := flag.String("out-dir", "results", "Output directory for results")
	gatewayStr := flag.String("gateways", "", "Comma-separated list of gateways (default: all)")
	scenarioStr := flag.String("scenarios", "", "Comma-separated list of scenarios (default: all)")
	dryRun := flag.Bool("dry-run", false, "Preview matrix without running")
	repeat := flag.Int("repeat", 1, "Repeat each benchmark N times and average results")
	flag.Parse()

	// Load .env — must happen before os.ExpandEnv on the config YAML
	if err := loadDotenv(*dotenvFile); err != nil && !os.IsNotExist(err) {
		log.Printf("warning: could not load %s: %v", *dotenvFile, err)
	}

	// Read + expand config
	raw, err := os.ReadFile(*configFile)
	if err != nil {
		log.Fatalf("cannot read config: %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(raw))), &cfg); err != nil {
		log.Fatalf("cannot parse config: %v", err)
	}

	gwFilter := parseFilter(*gatewayStr)
	scFilter := parseFilter(*scenarioStr)

	type RunKey struct{ gateway, scenario string }
	var runs []RunKey

	// Stable ordering: sort gateway names so output is deterministic
	gwNames := sortedKeys(cfg.Gateways)
	for _, gwName := range gwNames {
		if len(gwFilter) > 0 && !gwFilter[gwName] {
			continue
		}
		for _, sc := range cfg.Scenarios {
			if len(scFilter) > 0 && !scFilter[sc.Name] {
				continue
			}
			runs = append(runs, RunKey{gwName, sc.Name})
		}
	}

	if *dryRun {
		fmt.Printf("Dry-run — %d benchmark(s) to execute:\n", len(runs))
		for _, r := range runs {
			fmt.Printf("  gateway=%-16s  scenario=%s\n", r.gateway, r.scenario)
		}
		return
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("cannot create output directory: %v", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	csvPath := filepath.Join(*outDir, fmt.Sprintf("bench-%s.csv", timestamp))
	mdPath := filepath.Join(*outDir, fmt.Sprintf("bench-%s.md", timestamp))

	var allResults []Result

	for _, run := range runs {
		gw := cfg.Gateways[run.gateway]
		sc := findScenario(cfg.Scenarios, run.scenario)

		dur, err := time.ParseDuration(sc.Duration)
		if err != nil {
			log.Printf("warning: invalid duration %q for %s/%s — using 60s", sc.Duration, run.gateway, run.scenario)
			dur = 60 * time.Second
		}

		warmupStr := ""
		if sc.Warmup != "" {
			warmupStr = fmt.Sprintf("  warmup=%s", sc.Warmup)
		}
		fmt.Printf("\n==> gateway=%-16s  scenario=%-20s  users=%d  duration=%s%s\n",
			run.gateway, run.scenario, sc.Users, sc.Duration, warmupStr)

		var runResults []Result
		for i := range *repeat {
			if *repeat > 1 {
				fmt.Printf("    run %d/%d...\n", i+1, *repeat)
			}
			r := runBenchmark(run.gateway, gw, sc, dur)
			runResults = append(runResults, r)
			fmt.Printf("    rps=%.1f  p50=%.1fms  p95=%.1fms  p99=%.1fms  success=%d  failed=%d",
				r.RPS, r.P50, r.P95, r.P99, r.Success, r.Failed)
			if r.TTFB > 0 {
				fmt.Printf("  ttfb=%.1fms", r.TTFB)
			}
			fmt.Println()
		}
		allResults = append(allResults, averageResults(runResults))
	}

	writeCSV(csvPath, allResults)
	writeMarkdown(mdPath, allResults, timestamp)
	fmt.Printf("\nResults written to:\n  %s\n  %s\n", csvPath, mdPath)
}

// ---------------------------------------------------------------------------
// Benchmark runner
// ---------------------------------------------------------------------------

func runBenchmark(gwName string, gw GatewayConfig, sc ScenarioConfig, dur time.Duration) Result {
	url := gw.BaseURL + gw.RequestPath

	var total, success, failed int64
	var mu sync.Mutex
	var latencies []float64
	var ttfbValues []float64

	// Shared HTTP client with connection pooling across all VUs
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 500,
			MaxConnsPerHost:     0, // unlimited
		},
	}
	defer client.CloseIdleConnections()

	spawnRate := sc.SpawnRate
	if spawnRate <= 0 {
		spawnRate = 1
	}
	spawnInterval := time.Second / time.Duration(spawnRate)

	// Parse warmup duration (0 = no warmup)
	var warmupDur time.Duration
	if sc.Warmup != "" {
		if wd, err := time.ParseDuration(sc.Warmup); err == nil {
			warmupDur = wd
		}
	}

	// warming is 1 during warmup, 0 during measurement. VUs check this
	// atomically to decide whether to record metrics.
	var warming int32
	if warmupDur > 0 {
		atomic.StoreInt32(&warming, 1)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup

	// Spawn all VUs first, then run warmup, then start the measurement timer
	for range sc.Users {
		time.Sleep(spawnInterval)
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Create a context with deadline for clean cancellation
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				<-done
				cancel()
			}()

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				start := time.Now()
				ttfb := float64(-1) // -1 means not set
				reqErr := sendRequest(client, url, gw, sc, ctx, &ttfb)
				elapsedMs := float64(time.Since(start).Microseconds()) / 1000.0

				// Skip metric recording during warmup
				if atomic.LoadInt32(&warming) == 1 {
					continue
				}

				atomic.AddInt64(&total, 1)
				if reqErr != nil {
					atomic.AddInt64(&failed, 1)
				} else {
					atomic.AddInt64(&success, 1)
				}

				mu.Lock()
				latencies = append(latencies, elapsedMs)
				if ttfb >= 0 {
					ttfbValues = append(ttfbValues, ttfb)
				}
				mu.Unlock()
			}
		}()
	}

	// Run warmup phase, then flip to measurement
	if warmupDur > 0 {
		fmt.Printf("    warmup %s...\n", warmupDur)
		time.Sleep(warmupDur)
		atomic.StoreInt32(&warming, 0)
	}

	// Start measurement timer after warmup completes
	time.AfterFunc(dur, func() { close(done) })

	// Progress reporter — print stats every 30s for long-running benchmarks
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		startTime := time.Now()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				elapsed := time.Since(startTime).Truncate(time.Second)
				s := atomic.LoadInt64(&success)
				f := atomic.LoadInt64(&failed)
				rps := float64(s) / elapsed.Seconds()
				fmt.Printf("    [%s/%s] requests=%d  success=%d  failed=%d  rps=%.0f\n",
					elapsed, dur, s+f, s, f, rps)
			}
		}
	}()

	wg.Wait()
	<-progressDone

	mu.Lock()
	defer mu.Unlock()

	min, p50, p95, p99, p999, max := percentiles(latencies)
	var ttfbVal float64
	if len(ttfbValues) > 0 {
		ttfbVal = ttfbValues[0] // Use median TTFB
		if len(ttfbValues) > 1 {
			_, ttfbVal, _, _, _, _ = percentiles(ttfbValues)
		}
	}

	rps := float64(success) / dur.Seconds()

	return Result{
		Gateway:  gwName,
		Scenario: sc.Name,
		Users:    sc.Users,
		Duration: dur,
		Total:    total,
		Success:  success,
		Failed:   failed,
		Min:      min,
		P50:      p50,
		P95:      p95,
		P99:      p99,
		P999:     p999,
		Max:      max,
		RPS:      rps,
		TTFB:     ttfbVal,
	}
}

// sendRequest fires one HTTP request and drains the response body.
// For streaming responses, measures time-to-first-byte (TTFB).
func sendRequest(client *http.Client, url string, gw GatewayConfig, sc ScenarioConfig, ctx context.Context, ttfb *float64) error {
	payload := map[string]any{
		"model": gw.Model,
		"messages": []map[string]any{
			{"role": "user", "content": sc.Prompt},
		},
		"max_tokens": sc.MaxTokens,
		"stream":     sc.Stream,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if gw.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+gw.APIKey)
	}
	for k, v := range gw.ExtraHeaders {
		req.Header.Set(k, v)
	}

	reqStart := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// For streaming, record TTFB by reading first byte
	if sc.Stream && ttfb != nil {
		buf := make([]byte, 1)
		if _, err := resp.Body.Read(buf); err != nil && err != io.EOF {
			return err
		}
		*ttfb = float64(time.Since(reqStart).Microseconds()) / 1000.0 // TTFB in ms
	}

	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Statistics helpers
// ---------------------------------------------------------------------------

func percentiles(data []float64) (min, p50, p95, p99, p999, max float64) {
	if len(data) == 0 {
		return 0, 0, 0, 0, 0, 0
	}
	sorted := make([]float64, len(data))
	copy(sorted, data)
	sort.Float64s(sorted)
	n := float64(len(sorted))
	idx := func(pct float64) int {
		return int(math.Min(float64(len(sorted)-1), math.Ceil(n*pct)-1))
	}
	return sorted[0], sorted[idx(0.50)], sorted[idx(0.95)], sorted[idx(0.99)], sorted[idx(0.999)], sorted[len(sorted)-1]
}

func averageResults(results []Result) Result {
	if len(results) == 0 {
		return Result{}
	}
	if len(results) == 1 {
		return results[0]
	}
	avg := results[0]
	for _, r := range results[1:] {
		avg.Min += r.Min
		avg.P50 += r.P50
		avg.P95 += r.P95
		avg.P99 += r.P99
		avg.P999 += r.P999
		avg.Max += r.Max
		avg.RPS += r.RPS
		avg.Success += r.Success
		avg.Failed += r.Failed
		avg.Total += r.Total
		avg.TTFB += r.TTFB
	}
	n := float64(len(results))
	avg.Min /= n
	avg.P50 /= n
	avg.P95 /= n
	avg.P99 /= n
	avg.P999 /= n
	avg.Max /= n
	avg.RPS /= n
	avg.TTFB /= n
	// Use proper rounding before int64 cast
	avg.Success = int64(math.Round(float64(avg.Success) / n))
	avg.Failed = int64(math.Round(float64(avg.Failed) / n))
	avg.Total = int64(math.Round(float64(avg.Total) / n))
	return avg
}

// ---------------------------------------------------------------------------
// Output writers
// ---------------------------------------------------------------------------

func writeCSV(path string, results []Result) {
	f, err := os.Create(path)
	if err != nil {
		log.Printf("warning: cannot write CSV: %v", err)
		return
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	_ = w.Write([]string{
		"gateway", "scenario", "users", "duration_s",
		"total", "success", "failed",
		"rps", "min_ms", "p50_ms", "p95_ms", "p99_ms", "p99.9_ms", "max_ms", "ttfb_ms",
	})
	for _, r := range results {
		ttfbStr := ""
		if r.TTFB > 0 {
			ttfbStr = strconv.FormatFloat(r.TTFB, 'f', 2, 64)
		}
		_ = w.Write([]string{
			r.Gateway, r.Scenario,
			strconv.Itoa(r.Users),
			strconv.FormatFloat(r.Duration.Seconds(), 'f', 0, 64),
			strconv.FormatInt(r.Total, 10),
			strconv.FormatInt(r.Success, 10),
			strconv.FormatInt(r.Failed, 10),
			strconv.FormatFloat(r.RPS, 'f', 2, 64),
			strconv.FormatFloat(r.Min, 'f', 2, 64),
			strconv.FormatFloat(r.P50, 'f', 2, 64),
			strconv.FormatFloat(r.P95, 'f', 2, 64),
			strconv.FormatFloat(r.P99, 'f', 2, 64),
			strconv.FormatFloat(r.P999, 'f', 2, 64),
			strconv.FormatFloat(r.Max, 'f', 2, 64),
			ttfbStr,
		})
	}
}

func writeMarkdown(path string, results []Result, timestamp string) {
	f, err := os.Create(path)
	if err != nil {
		log.Printf("warning: cannot write markdown: %v", err)
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "# Benchmark Results — %s\n\n", timestamp)
	fmt.Fprintf(f, "Benchmark measured on %s.\n\n", time.Now().Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(f, "| Gateway | Scenario | Users | RPS | Min (ms) | P50 (ms) | P95 (ms) | P99 (ms) | P99.9 (ms) | Max (ms) | Success | Failed |\n")
	fmt.Fprintf(f, "|---------|----------|------:|----:|---------:|---------:|---------:|---------:|----------:|--------:|--------:|-------:|\n")
	for _, r := range results {
		ttfbStr := ""
		if r.TTFB > 0 {
			ttfbStr = fmt.Sprintf(" (TTFB: %.1fms)", r.TTFB)
		}
		fmt.Fprintf(f, "| %s | %s | %d | %.1f | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %d | %d |%s\n",
			r.Gateway, r.Scenario, r.Users,
			r.RPS, r.Min, r.P50, r.P95, r.P99, r.P999, r.Max,
			r.Success, r.Failed, ttfbStr)
	}
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

func parseFilter(s string) map[string]bool {
	out := make(map[string]bool)
	if s == "" {
		return out
	}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out[p] = true
		}
	}
	return out
}

func sortedKeys(m map[string]GatewayConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func findScenario(scenarios []ScenarioConfig, name string) ScenarioConfig {
	for _, s := range scenarios {
		if s.Name == name {
			return s
		}
	}
	return ScenarioConfig{}
}

// loadDotenv reads KEY=VALUE pairs from path and sets them in the environment.
// Variables already set in the environment are not overwritten.
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
		// Strip surrounding quotes
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
