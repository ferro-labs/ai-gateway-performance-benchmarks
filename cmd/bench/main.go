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
	P50      float64
	P95      float64
	P99      float64
	RPS      float64
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

		fmt.Printf("\n==> gateway=%-16s  scenario=%-20s  users=%d  duration=%s\n",
			run.gateway, run.scenario, sc.Users, sc.Duration)

		var runResults []Result
		for i := range *repeat {
			if *repeat > 1 {
				fmt.Printf("    run %d/%d...\n", i+1, *repeat)
			}
			r := runBenchmark(run.gateway, gw, sc, dur)
			runResults = append(runResults, r)
			fmt.Printf("    rps=%.1f  p50=%.1fms  p95=%.1fms  p99=%.1fms  success=%d  failed=%d\n",
				r.RPS, r.P50, r.P95, r.P99, r.Success, r.Failed)
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

	done := make(chan struct{})
	time.AfterFunc(dur, func() { close(done) })

	spawnRate := sc.SpawnRate
	if spawnRate <= 0 {
		spawnRate = 1
	}
	spawnInterval := time.Second / time.Duration(spawnRate)

	var wg sync.WaitGroup
	for range sc.Users {
		time.Sleep(spawnInterval)
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 30 * time.Second}
			for {
				select {
				case <-done:
					return
				default:
				}
				start := time.Now()
				reqErr := sendRequest(client, url, gw, sc)
				elapsedMs := float64(time.Since(start).Microseconds()) / 1000.0

				atomic.AddInt64(&total, 1)
				if reqErr != nil {
					atomic.AddInt64(&failed, 1)
				} else {
					atomic.AddInt64(&success, 1)
				}

				mu.Lock()
				latencies = append(latencies, elapsedMs)
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	p50, p95, p99 := percentiles(latencies)
	rps := float64(success) / dur.Seconds()

	return Result{
		Gateway:  gwName,
		Scenario: sc.Name,
		Users:    sc.Users,
		Duration: dur,
		Total:    total,
		Success:  success,
		Failed:   failed,
		P50:      p50,
		P95:      p95,
		P99:      p99,
		RPS:      rps,
	}
}

// sendRequest fires one HTTP request and drains the response body.
func sendRequest(client *http.Client, url string, gw GatewayConfig, sc ScenarioConfig) error {
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

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
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

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Statistics helpers
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
		return int(math.Min(float64(len(sorted)-1), math.Floor(n*pct)))
	}
	return sorted[idx(0.50)], sorted[idx(0.95)], sorted[idx(0.99)]
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
		avg.P50 += r.P50
		avg.P95 += r.P95
		avg.P99 += r.P99
		avg.RPS += r.RPS
		avg.Success += r.Success
		avg.Failed += r.Failed
		avg.Total += r.Total
	}
	n := float64(len(results))
	avg.P50 /= n
	avg.P95 /= n
	avg.P99 /= n
	avg.RPS /= n
	avg.Success = int64(float64(avg.Success) / n)
	avg.Failed = int64(float64(avg.Failed) / n)
	avg.Total = int64(float64(avg.Total) / n)
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
		"rps", "p50_ms", "p95_ms", "p99_ms",
	})
	for _, r := range results {
		_ = w.Write([]string{
			r.Gateway, r.Scenario,
			strconv.Itoa(r.Users),
			strconv.FormatFloat(r.Duration.Seconds(), 'f', 0, 64),
			strconv.FormatInt(r.Total, 10),
			strconv.FormatInt(r.Success, 10),
			strconv.FormatInt(r.Failed, 10),
			strconv.FormatFloat(r.RPS, 'f', 2, 64),
			strconv.FormatFloat(r.P50, 'f', 2, 64),
			strconv.FormatFloat(r.P95, 'f', 2, 64),
			strconv.FormatFloat(r.P99, 'f', 2, 64),
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
	fmt.Fprintf(f, "| Gateway | Scenario | Users | RPS | P50 (ms) | P95 (ms) | P99 (ms) | Success | Failed |\n")
	fmt.Fprintf(f, "|---------|----------|------:|----:|---------:|---------:|---------:|--------:|-------:|\n")
	for _, r := range results {
		fmt.Fprintf(f, "| %s | %s | %d | %.1f | %.1f | %.1f | %.1f | %d | %d |\n",
			r.Gateway, r.Scenario, r.Users,
			r.RPS, r.P50, r.P95, r.P99,
			r.Success, r.Failed)
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
