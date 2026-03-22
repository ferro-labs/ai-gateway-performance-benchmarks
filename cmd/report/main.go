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
	Gateway  string  `json:"gateway"`
	Scenario string  `json:"scenario"`
	Users    int     `json:"users"`
	Duration float64 `json:"duration_s"`
	Total    int64   `json:"total"`
	Success  int64   `json:"success"`
	Failed   int64   `json:"failed"`
	RPS      float64 `json:"rps"`
	Min      float64 `json:"min_ms"`
	P50      float64 `json:"p50_ms"`
	P95      float64 `json:"p95_ms"`
	P99      float64 `json:"p99_ms"`
	P999     float64 `json:"p999_ms"`
	Max      float64 `json:"max_ms"`
	TTFB     float64 `json:"ttfb_ms,omitempty"`
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

	writeReportMarkdown(mdPath, report)
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
			Gateway:  col(rec, idx, "gateway"),
			Scenario: col(rec, idx, "scenario"),
			Users:    atoi(col(rec, idx, "users")),
			Duration: atof(col(rec, idx, "duration_s")),
			Total:    atoi64(col(rec, idx, "total")),
			Success:  atoi64(col(rec, idx, "success")),
			Failed:   atoi64(col(rec, idx, "failed")),
			RPS:      atof(col(rec, idx, "rps")),
			Min:      atof(col(rec, idx, "min_ms")),
			P50:      atof(col(rec, idx, "p50_ms")),
			P95:      atof(col(rec, idx, "p95_ms")),
			P99:      atof(col(rec, idx, "p99_ms")),
			P999:     atof(col(rec, idx, "p99.9_ms")),
			Max:      atof(col(rec, idx, "max_ms")),
			TTFB:     atof(col(rec, idx, "ttfb_ms")),
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

func writeReportMarkdown(path string, report Report) {
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

	// ----- Executive summary -----
	w("## Executive Summary\n\n")
	w("| Gateway | Scenario | Users | RPS | P50 (ms) | P95 (ms) | P99 (ms) | P99.9 (ms) | Max (ms) | TTFB (ms) | Success | Failed |\n")
	w("|---------|----------|------:|----:|---------:|---------:|---------:|----------:|---------:|----------:|--------:|-------:|\n")
	for _, r := range report.Rows {
		ttfb := "—"
		if r.TTFB > 0 {
			ttfb = fmt.Sprintf("%.1f", r.TTFB)
		}
		w("| %s | %s | %d | %.1f | %.2f | %.2f | %.2f | %.2f | %.2f | %s | %d | %d |\n",
			r.Gateway, r.Scenario, r.Users,
			r.RPS, r.P50, r.P95, r.P99, r.P999, r.Max,
			ttfb, r.Success, r.Failed)
	}

	// ----- Key findings -----
	w("\n## Key Findings\n\n")
	writeFindings(f, report.Rows, scenarios)

	// ----- Per-scenario deep dives -----
	w("\n## Per-Scenario Results\n\n")
	for _, sc := range scenarios {
		scRows := filterByScenario(report.Rows, sc)
		if len(scRows) == 0 {
			continue
		}

		w("### %s (%d users, %.0fs)\n\n", sc, scRows[0].Users, scRows[0].Duration)
		w("| Gateway | RPS | P50 (ms) | P95 (ms) | P99 (ms) | P99.9 (ms) | Max (ms) |")
		if scRows[0].TTFB > 0 {
			w(" TTFB (ms) |")
		}
		w("\n")
		w("|---------|----:|---------:|---------:|---------:|----------:|---------:|")
		if scRows[0].TTFB > 0 {
			w("----------:|")
		}
		w("\n")
		for _, r := range scRows {
			w("| %s | %.1f | %.2f | %.2f | %.2f | %.2f | %.2f |",
				r.Gateway, r.RPS, r.P50, r.P95, r.P99, r.P999, r.Max)
			if scRows[0].TTFB > 0 {
				w(" %.1f |", r.TTFB)
			}
			w("\n")
		}
		w("\n")
	}

	// ----- Methodology -----
	w("## Methodology\n\n")
	w("- **Hardware**: GCP n2-standard-8 (8 vCPU, 32 GB RAM), Debian 12\n")
	w("- **Backend**: Go mock server (`cmd/mockserver`) returning fixed responses with ~0ms latency\n")
	w("- **Isolation**: All gateways and the mock server run on the same host via Docker Compose\n")
	w("- **Measurement**: Each scenario includes a warmup phase (not recorded) followed by the timed measurement window\n")
	w("- **Metrics**: Latency percentiles (p50/p95/p99/p99.9), throughput (RPS), TTFB for streaming scenarios\n")
	w("- **\"Failed\" requests**: In-flight requests cancelled when the benchmark timer expires (count equals VU count; not errors)\n")
	w("- **Gateways tested**: %s\n", strings.Join(gateways, ", "))
	w("- **Scenarios**: %s\n", strings.Join(scenarios, ", "))

	// ----- How to reproduce -----
	w("\n## How to Reproduce\n\n")
	w("```bash\n")
	w("# Clone the repo\n")
	w("git clone https://github.com/ferro-labs/ai-gateway-performance-benchmarks.git\n")
	w("cd ai-gateway-performance-benchmarks\n")
	w("\n")
	w("# Build, start all gateways, run full benchmark suite\n")
	w("make setup\n")
	w("make bench-compare\n")
	w("\n")
	w("# Generate this report from the results\n")
	w("make bench-report\n")
	w("\n")
	w("# Or do everything in one command\n")
	w("make bench-full\n")
	w("```\n\n")
	w("For publication-quality results, average 3 runs:\n\n")
	w("```bash\n")
	w("./scripts/run-oci.sh --gateways ferrogateway,litellm,bifrost,kong,portkey --repeat 3\n")
	w("```\n")
}

func writeFindings(f *os.File, rows []Row, scenarios []string) {
	w := func(format string, args ...any) {
		fmt.Fprintf(f, format, args...)
	}

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
