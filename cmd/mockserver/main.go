// cmd/mockserver — lightweight HTTP mock that returns fixed OpenAI-compatible
// responses. Eliminates upstream LLM latency so benchmarks measure gateway
// overhead only.
//
// Usage:
//
//	mockserver [--port 9000] [--latency 60ms] [--stream-chunk-delay-ms 10]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync/atomic"
	"time"
)

var latency time.Duration
var streamChunkDelayMs int
var requestCount atomic.Int64

func main() {
	port := flag.Int("port", 9000, "Port to listen on")
	flag.DurationVar(&latency, "latency", 0, "Artificial latency added to every request (e.g. 60ms, 0ms)")
	flag.IntVar(&streamChunkDelayMs, "stream-chunk-delay-ms", 10, "Delay between SSE chunks (ms)")
	flag.Parse()

	// Log request rate periodically
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		var lastCount int64
		for range ticker.C {
			curr := requestCount.Load()
			rps := float64(curr-lastCount) / 5.0
			log.Printf("request rate: %.1f RPS (total: %d)", rps, curr)
			lastCount = curr
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/v1/models", handleModels)
	mux.HandleFunc("/v1/chat/completions", handleChatCompletions)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("mock-server listening on %s  (latency=%s, stream-chunk-delay=%dms)", addr, latency, streamChunkDelayMs)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// handleHealth — used by Docker healthcheck.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// handleModels — returns a minimal model list so gateways can validate config.
func handleModels(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data": []map[string]any{
			{
				"id":       "mock-gpt-4",
				"object":   "model",
				"created":  time.Now().Unix(),
				"owned_by": "mock",
			},
		},
	})
}

// handleChatCompletions — returns a blocking response or an SSE stream
// depending on the "stream" field in the request body.
func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	requestCount.Add(1)

	if latency > 0 {
		time.Sleep(latency)
	}

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	id := "chatcmpl-" + randHex(8)
	model := "mock-gpt-4"
	if m, ok := req["model"].(string); ok && m != "" {
		model = m
	}

	if stream, _ := req["stream"].(bool); stream {
		writeSSE(w, id, model)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Connection", "keep-alive")
	json.NewEncoder(w).Encode(map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"finish_reason": "stop",
				"message": map[string]any{
					"role":    "assistant",
					"content": "Mock response from FerroGateway benchmark server.",
				},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 8,
			"total_tokens":      18,
		},
	})
}

// writeSSE streams a short completion as Server-Sent Events.
func writeSSE(w http.ResponseWriter, id, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	words := []string{"Mock", " streaming", " response", " from", " benchmark", " server."}
	for _, word := range words {
		chunk := map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []map[string]any{
				{
					"index":         0,
					"delta":         map[string]any{"content": word},
					"finish_reason": nil,
				},
			},
		}
		b, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
		time.Sleep(time.Duration(streamChunkDelayMs) * time.Millisecond)
	}

	// Terminal chunk
	final := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": "stop",
			},
		},
	}
	b, _ := json.Marshal(final)
	fmt.Fprintf(w, "data: %s\n\n", b)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func randHex(n int) string {
	const letters = "abcdef0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
