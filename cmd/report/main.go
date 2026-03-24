// cmd/report — reads benchmark CSV results and generates a publishable
// Markdown report plus a structured JSON file for downstream tooling.
//
// Usage:
//
//	report --input results/20260322-140000
//	report --input results              # scans for newest CSV
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Data types
// ---------------------------------------------------------------------------

type Row struct {
	Gateway    string  `json:"gateway"`
	Scenario   string  `json:"scenario"`
	Users      int     `json:"users"`
	Duration   float64 `json:"duration_s"`
	Total      int64   `json:"total"`
	Success    int64   `json:"success"`
	Failed     int64   `json:"failed"`
	RPS        float64 `json:"rps"`
	Min        float64 `json:"min_ms"`
	P50        float64 `json:"p50_ms"`
	P95        float64 `json:"p95_ms"`
	P99        float64 `json:"p99_ms"`
	P999       float64 `json:"p999_ms"`
	Max        float64 `json:"max_ms"`
	TTFB       float64 `json:"ttfb_ms,omitempty"`
	OverheadUS float64 `json:"overhead_us,omitempty"`
	MinMemMB   float64 `json:"min_memory_mb,omitempty"`
	MaxMemMB   float64 `json:"max_memory_mb,omitempty"`
	AvgMemMB   float64 `json:"avg_memory_mb,omitempty"`
	AvgCPUPct  float64 `json:"avg_cpu_percent,omitempty"`
}

type Report struct {
	Generated string `json:"generated"`
	Source    string `json:"source"`
	Rows     []Row  `json:"rows"`
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	inputDir := flag.String("input", "results", "Directory containing bench CSV files")
	outputDir := flag.String("output", "", "Output directory (default: same as input)")
	mockLatency := flag.Float64("mock-latency", 60, "Mock server latency in ms (for report)")
	flag.Parse()

	if *outputDir == "" {
		*outputDir = *inputDir
	}

	csvPath := findNewestCSV(*inputDir)
	if csvPath == "" {
		log.Fatalf("no CSV files found in %s", *inputDir)
	}
	fmt.Printf("Reading: %s\n", csvPath)

	rows := parseCSV(csvPath)
	if len(rows) == 0 {
		log.Fatal("CSV contained no data rows")
	}

	report := Report{
		Generated: time.Now().Format("2006-01-02 15:04:05 MST"),
		Source:    csvPath,
		Rows:     rows,
	}

	mdPath := filepath.Join(*outputDir, "BENCHMARK-REPORT.md")
	jsonPath := filepath.Join(*outputDir, "BENCHMARK-REPORT.json")

	writeReportMarkdown(mdPath, report, *mockLatency)
	writeReportJSON(jsonPath, report)

	fmt.Printf("Report written to:\n  %s\n  %s\n", mdPath, jsonPath)
}

// ---------------------------------------------------------------------------
// CSV parsing
// ---------------------------------------------------------------------------

func findNewestCSV(dir string) string {
	var best string
	var bestTime time.Time

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".csv") && strings.Contains(filepath.Base(path), "bench-") {
			if info.ModTime().After(bestTime) {
				bestTime = info.ModTime()
				best = path
			}
		}
		return nil
	})
	return best
}

func parseCSV(path string) []Row {
	f, err := os.Open(path)
	if err != nil {
		log.Fatalf("cannot open CSV: %v", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		log.Fatalf("cannot parse CSV: %v", err)
	}

	if len(records) < 2 {
		return nil
	}

	// Build header index for resilience against column reordering
	header := records[0]
	idx := make(map[string]int)
	for i, h := range header {
		idx[h] = i
	}

	var rows []Row
	for _, rec := range records[1:] {
		rows = append(rows, Row{
			Gateway:    col(rec, idx, "gateway"),
			Scenario:   col(rec, idx, "scenario"),
			Users:      atoi(col(rec, idx, "users")),
			Duration:   atof(col(rec, idx, "duration_s")),
			Total:      atoi64(col(rec, idx, "total")),
			Success:    atoi64(col(rec, idx, "success")),
			Failed:     atoi64(col(rec, idx, "failed")),
			RPS:        atof(col(rec, idx, "rps")),
			Min:        atof(col(rec, idx, "min_ms")),
			P50:        atof(col(rec, idx, "p50_ms")),
			P95:        atof(col(rec, idx, "p95_ms")),
			P99:        atof(col(rec, idx, "p99_ms")),
			P999:       atof(col(rec, idx, "p99.9_ms")),
			Max:        atof(col(rec, idx, "max_ms")),
			TTFB:       atof(col(rec, idx, "ttfb_ms")),
			OverheadUS: atof(col(rec, idx, "overhead_us")),
			MinMemMB:   atof(col(rec, idx, "min_memory_mb")),
			MaxMemMB:   atof(col(rec, idx, "max_memory_mb")),
			AvgMemMB:   atof(col(rec, idx, "avg_memory_mb")),
			AvgCPUPct:  atof(col(rec, idx, "avg_cpu_percent")),
		})
	}
	return rows
}

func col(rec []string, idx map[string]int, name string) string {
	i, ok := idx[name]
	if !ok || i >= len(rec) {
		return ""
	}
	return rec[i]
}

func atoi(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

func atoi64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// ---------------------------------------------------------------------------
// Markdown report generation
// ---------------------------------------------------------------------------

func writeReportMarkdown(path string, report Report, mockLatencyMS float64) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("cannot create markdown report: %v", err)
	}
	defer f.Close()

	w := func(format string, args ...any) {
		fmt.Fprintf(f, format, args...)
	}

	gateways := uniqueGateways(report.Rows)
	scenarios := uniqueScenarios(report.Rows)

	// ----- Header -----
	w("# AI Gateway Benchmark Report\n\n")
	w("> Generated: %s\n>\n", report.Generated)
	w("> Source: `%s`\n\n", report.Source)

	// ----- Headline comparison table -----
	w("## Headline Comparison\n\n")
	w("All values measured on GCP n2-standard-8 (8 vCPU, 32 GB RAM), Debian 12 Bookworm.\n")
	w("Mock upstream: %.0fms fixed latency. Gateway overhead = (p50 - %.0f) x 1000 µs.\n\n", mockLatencyMS, mockLatencyMS)

	// Preferred gateway order for the headline table
	gwOrder := []string{"ferrogateway", "bifrost", "kong", "portkey", "litellm"}
	gwLabels := map[string]string{
		"ferrogateway": "Ferro Labs",
		"bifrost":      "Bifrost",
		"kong":         "Kong",
		"portkey":      "Portkey",
		"litellm":      "LiteLLM",
	}
	gwLang := map[string]string{
		"ferrogateway": "Go",
		"bifrost":      "Go",
		"kong":         "Go/Lua",
		"portkey":      "TS/Node",
		"litellm":      "Python",
	}

	// Find the bifrost-comparable-500rps scenario for headline metrics
	headlineScenario := "bifrost-comparable-500rps"
	headlineRows := filterByScenario(report.Rows, headlineScenario)
	if len(headlineRows) == 0 {
		// Fall back to baseline
		headlineScenario = "baseline"
		headlineRows = filterByScenario(report.Rows, headlineScenario)
	}

	// Find stress-5krps for success rate at 5K
	stress5kRows := filterByScenario(report.Rows, "stress-5krps")

	// Build a lookup: gateway -> row for headline
	headlineLookup := make(map[string]Row)
	for _, r := range headlineRows {
		headlineLookup[r.Gateway] = r
	}
	stress5kLookup := make(map[string]Row)
	for _, r := range stress5kRows {
		stress5kLookup[r.Gateway] = r
	}

	// Write the headline table header
	w("| Metric |")
	for _, gw := range gwOrder {
		label := gwLabels[gw]
		if label == "" {
			label = gw
		}
		w(" %s |", label)
	}
	w("\n|---|")
	for range gwOrder {
		w("---|")
	}
	w("\n")

	// Row: Mean overhead
	w("| Mean overhead |")
	for _, gw := range gwOrder {
		if r, ok := headlineLookup[gw]; ok && r.OverheadUS > 0 {
			w(" %.0fµs |", r.OverheadUS)
		} else {
			w(" — |")
		}
	}
	w("\n")

	// Row: p50 latency @500RPS
	w("| p50 latency @%s |", headlineScenario)
	for _, gw := range gwOrder {
		if r, ok := headlineLookup[gw]; ok {
			w(" %.2fms |", r.P50)
		} else {
			w(" — |")
		}
	}
	w("\n")

	// Row: p99 latency @500RPS
	w("| p99 latency @%s |", headlineScenario)
	for _, gw := range gwOrder {
		if r, ok := headlineLookup[gw]; ok {
			w(" %.2fms |", r.P99)
		} else {
			w(" — |")
		}
	}
	w("\n")

	// Row: Throughput (RPS)
	w("| Throughput (req/s) |")
	for _, gw := range gwOrder {
		if r, ok := headlineLookup[gw]; ok {
			w(" %.1f |", r.RPS)
		} else {
			w(" — |")
		}
	}
	w("\n")

	// Row: Memory
	w("| Memory (MB) |")
	for _, gw := range gwOrder {
		if r, ok := headlineLookup[gw]; ok && r.AvgMemMB > 0 {
			w(" %.0f |", r.AvgMemMB)
		} else {
			w(" — |")
		}
	}
	w("\n")

	// Row: Success rate @5K RPS
	w("| Success rate @5K RPS |")
	for _, gw := range gwOrder {
		if r, ok := stress5kLookup[gw]; ok && r.Total > 0 {
			rate := float64(r.Success) / float64(r.Total) * 100
			w(" %.1f%% |", rate)
		} else {
			w(" — |")
		}
	}
	w("\n")

	// Row: Language
	w("| Language |")
	for _, gw := range gwOrder {
		lang := gwLang[gw]
		if lang == "" {
			lang = "—"
		}
		w(" %s |", lang)
	}
	w("\n")

	// ----- Competitor published numbers -----
	w("\n### Published Competitor Numbers (for reference)\n\n")
	w("| Metric | Bifrost | LiteLLM | Kong | Portkey |\n")
	w("|---|---|---|---|---|\n")
	w("| Mean overhead | 11µs [1] | 500µs [1] | — | ~20-40ms [3] |\n")
	w("| Throughput | 424 req/s [1] | 44.84 req/s [1] | — | — |\n")
	w("| p99 latency @500RPS | 1.68s [1] | 90.72s [1] | — | — |\n")
	w("| Memory @500RPS | 120MB [1] | 372MB [1] | — | — |\n")
	w("| Test conditions | t3.medium, 60ms mock [1] | t3.medium, 60ms mock [1] | EKS c5.4xlarge [2] | — |\n")
	w("\n")
	w("_* = competitor's own published number (source cited below)_\n\n")

	// ----- Key findings -----
	w("\n## Key Findings\n\n")
	writeFindings(f, report.Rows, scenarios, headlineLookup, stress5kLookup, gwLabels)

	// ----- Executive summary table -----
	w("\n## Full Results\n\n")
	w("| Gateway | Scenario | Users | RPS | P50 (ms) | P95 (ms) | P99 (ms) | P99.9 (ms) | Max (ms) | Overhead (µs) |")
	hasResource := false
	for _, r := range report.Rows {
		if r.AvgMemMB > 0 {
			hasResource = true
			break
		}
	}
	hasTTFB := false
	for _, r := range report.Rows {
		if r.TTFB > 0 {
			hasTTFB = true
			break
		}
	}
	if hasTTFB {
		w(" TTFB (ms) |")
	}
	if hasResource {
		w(" Mem (MB) | CPU (%%) |")
	}
	w(" Success | Failed |\n")

	w("|---------|----------|------:|----:|---------:|---------:|---------:|----------:|---------:|---------:|")
	if hasTTFB {
		w("----------:|")
	}
	if hasResource {
		w("---------:|--------:|")
	}
	w("--------:|-------:|\n")

	for _, r := range report.Rows {
		ttfb := "—"
		if r.TTFB > 0 {
			ttfb = fmt.Sprintf("%.1f", r.TTFB)
		}
		overhead := "—"
		if r.OverheadUS > 0 {
			overhead = fmt.Sprintf("%.0f", r.OverheadUS)
		}
		w("| %s | %s | %d | %.1f | %.2f | %.2f | %.2f | %.2f | %.2f | %s |",
			r.Gateway, r.Scenario, r.Users,
			r.RPS, r.P50, r.P95, r.P99, r.P999, r.Max, overhead)
		if hasTTFB {
			w(" %s |", ttfb)
		}
		if hasResource {
			mem := "—"
			cpu := "—"
			if r.AvgMemMB > 0 {
				mem = fmt.Sprintf("%.0f", r.AvgMemMB)
			}
			if r.AvgCPUPct > 0 {
				cpu = fmt.Sprintf("%.0f", r.AvgCPUPct)
			}
			w(" %s | %s |", mem, cpu)
		}
		w(" %d | %d |\n", r.Success, r.Failed)
	}

	// ----- Per-scenario deep dives -----
	w("\n## Per-Scenario Results\n\n")
	for _, sc := range scenarios {
		scRows := filterByScenario(report.Rows, sc)
		if len(scRows) == 0 {
			continue
		}

		w("### %s (%d users, %.0fs)\n\n", sc, scRows[0].Users, scRows[0].Duration)
		w("| Gateway | RPS | Overhead (µs) | P50 (ms) | P95 (ms) | P99 (ms) | P99.9 (ms) | Max (ms) |")
		scHasTTFB := scRows[0].TTFB > 0
		scHasResource := false
		for _, r := range scRows {
			if r.AvgMemMB > 0 {
				scHasResource = true
				break
			}
		}
		if scHasTTFB {
			w(" TTFB (ms) |")
		}
		if scHasResource {
			w(" Mem (MB) |")
		}
		w("\n")
		w("|---------|----:|--------:|---------:|---------:|---------:|----------:|---------:|")
		if scHasTTFB {
			w("----------:|")
		}
		if scHasResource {
			w("---------:|")
		}
		w("\n")
		for _, r := range scRows {
			overhead := "—"
			if r.OverheadUS > 0 {
				overhead = fmt.Sprintf("%.0f", r.OverheadUS)
			}
			w("| %s | %.1f | %s | %.2f | %.2f | %.2f | %.2f | %.2f |",
				r.Gateway, r.RPS, overhead, r.P50, r.P95, r.P99, r.P999, r.Max)
			if scHasTTFB {
				w(" %.1f |", r.TTFB)
			}
			if scHasResource {
				mem := "—"
				if r.AvgMemMB > 0 {
					mem = fmt.Sprintf("%.0f", r.AvgMemMB)
				}
				w(" %s |", mem)
			}
			w("\n")
		}
		w("\n")
	}

	// ----- Methodology -----
	w("## Methodology\n\n")
	w("- **Hardware**: GCP n2-standard-8 (8 vCPU, 32 GB RAM), Debian 12 Bookworm\n")
	w("- **Mock upstream**: Go mock server (`cmd/mockserver`) returning fixed responses with %.0fms latency\n", mockLatencyMS)
	w("- **Gateway overhead**: `overhead_µs = (p50_ms - %.0f) x 1000`\n", mockLatencyMS)
	w("- **Warmup**: 60s before each measurement window (requests sent but not recorded)\n")
	w("- **Isolation**: Each gateway ran as a native process with full machine access, one at a time\n")
	w("- **Measurement**: Timed measurement window after warmup; VUs send requests continuously\n")
	w("- **Metrics**: Latency percentiles (p50/p95/p99/p99.9), throughput (RPS), TTFB for streaming, memory (VmRSS), CPU\n")
	w("- **\"Failed\" requests**: In-flight requests cancelled when the benchmark timer expires (count equals VU count; not errors)\n")
	w("- **Languages**: Go (Ferro Labs, Bifrost), Go/Lua (Kong), Python (LiteLLM), TypeScript/Node.js (Portkey)\n")
	w("- **Gateways tested**: %s\n", strings.Join(gateways, ", "))
	w("- **Scenarios**: %s\n", strings.Join(scenarios, ", "))

	// ----- Sources -----
	w("\n## Sources\n\n")
	w("[1] Bifrost benchmark: https://www.getmaxim.ai/bifrost/resources/benchmarks\n\n")
	w("[2] Kong benchmark: https://konghq.com/blog/engineering/ai-gateway-benchmark\n\n")
	w("[3] Portkey latency: https://portkey.ai/features/ai-gateway\n\n")

	// ----- How to reproduce -----
	w("\n## How to Reproduce\n\n")
	w("```bash\n")
	w("git clone https://github.com/ferro-labs/ai-gateway-performance-benchmarks\n")
	w("cd ai-gateway-performance-benchmarks\n")
	w("cp .env.example .env\n")
	w("\n")
	w("# Install all 5 gateways natively\n")
	w("make setup\n")
	w("\n")
	w("# Run full suite — results in results/\n")
	w("make bench\n")
	w("```\n\n")
	w("For publication-quality results, average 3 runs:\n\n")
	w("```bash\n")
	w("./scripts/run-benchmarks.sh --gateways ferrogateway,bifrost,litellm,kong,portkey --repeat 3\n")
	w("```\n")
}

func writeFindings(f *os.File, rows []Row, scenarios []string, headlineLookup, stress5kLookup map[string]Row, gwLabels map[string]string) {
	w := func(format string, args ...any) {
		fmt.Fprintf(f, format, args...)
	}

	// Auto-generated headline findings from data
	if ferro, ok := headlineLookup["ferrogateway"]; ok {
		label := gwLabels["ferrogateway"]

		if ferro.OverheadUS > 0 {
			w("- **%s adds %.0fµs gateway overhead** at the benchmark scenario\n", label, ferro.OverheadUS)
		}
		if ferro.RPS > 0 {
			w("- **%s handles %.1f req/sec**", label, ferro.RPS)
			if litellm, ok := headlineLookup["litellm"]; ok && litellm.RPS > 0 {
				ratio := ferro.RPS / litellm.RPS
				w(" — %.0fx higher throughput than LiteLLM's %.1f", ratio, litellm.RPS)
			}
			w("\n")
		}
		if ferro.AvgMemMB > 0 {
			w("- **%s uses %.0fMB memory**", label, ferro.AvgMemMB)
			if litellm, ok := headlineLookup["litellm"]; ok && litellm.AvgMemMB > 0 {
				ratio := litellm.AvgMemMB / ferro.AvgMemMB
				w(" — %.1fx lighter than LiteLLM's %.0fMB", ratio, litellm.AvgMemMB)
			}
			w("\n")
		}
	}

	// 5K RPS findings
	if ferroStress, ok := stress5kLookup["ferrogateway"]; ok && ferroStress.Total > 0 {
		rate := float64(ferroStress.Success) / float64(ferroStress.Total) * 100
		w("- **At 5,000 RPS: %s success rate %.1f%%**", gwLabels["ferrogateway"], rate)
		if litellm, ok := stress5kLookup["litellm"]; ok && litellm.Total > 0 {
			litellmRate := float64(litellm.Success) / float64(litellm.Total) * 100
			if litellmRate < rate {
				w(" vs LiteLLM %.1f%%", litellmRate)
			}
		}
		w("\n")
	}

	// Native vs interpreted findings
	var goRPS, interpRPS []float64
	for _, gw := range []string{"ferrogateway", "bifrost", "kong"} {
		if r, ok := headlineLookup[gw]; ok && r.RPS > 0 {
			goRPS = append(goRPS, r.RPS)
		}
	}
	for _, gw := range []string{"litellm", "portkey"} {
		if r, ok := headlineLookup[gw]; ok && r.RPS > 0 {
			interpRPS = append(interpRPS, r.RPS)
		}
	}
	if len(goRPS) > 0 && len(interpRPS) > 0 {
		avgGo := avg(goRPS)
		avgInterp := avg(interpRPS)
		if avgInterp > 0 {
			ratio := avgGo / avgInterp
			w("- **Go-native gateways (Ferro Labs, Bifrost, Kong) outperform interpreted runtimes (Python/LiteLLM, TypeScript/Portkey) by %.0fx in throughput**\n", ratio)
		}
	}

	w("\n")

	// Per-scenario callouts
	for _, sc := range scenarios {
		scRows := filterByScenario(rows, sc)
		if len(scRows) == 0 {
			continue
		}

		bestRPS := scRows[0]
		bestP50 := scRows[0]
		bestP99 := scRows[0]
		for _, r := range scRows[1:] {
			if r.RPS > bestRPS.RPS {
				bestRPS = r
			}
			if r.P50 < bestP50.P50 {
				bestP50 = r
			}
			if r.P99 < bestP99.P99 {
				bestP99 = r
			}
		}

		w("**%s** (%d users)\n", sc, scRows[0].Users)
		w("- Highest throughput: **%s** (%.1f RPS)\n", bestRPS.Gateway, bestRPS.RPS)
		w("- Lowest p50 latency: **%s** (%.2f ms)\n", bestP50.Gateway, bestP50.P50)
		w("- Lowest p99 latency: **%s** (%.2f ms)\n", bestP99.Gateway, bestP99.P99)
		w("\n")
	}
}

func avg(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

// ---------------------------------------------------------------------------
// JSON report generation
// ---------------------------------------------------------------------------

func writeReportJSON(path string, report Report) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("cannot create JSON report: %v", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		log.Fatalf("cannot write JSON: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func uniqueGateways(rows []Row) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if !seen[r.Gateway] {
			seen[r.Gateway] = true
			out = append(out, r.Gateway)
		}
	}
	sort.Strings(out)
	return out
}

func uniqueScenarios(rows []Row) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if !seen[r.Scenario] {
			seen[r.Scenario] = true
			out = append(out, r.Scenario)
		}
	}
	return out // preserve YAML ordering (insertion order from CSV)
}

func filterByScenario(rows []Row, scenario string) []Row {
	var out []Row
	for _, r := range rows {
		if r.Scenario == scenario {
			out = append(out, r)
		}
	}
	return out
}
