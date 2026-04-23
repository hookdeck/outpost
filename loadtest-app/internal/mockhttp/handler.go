package mockhttp

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// Handler provides httpbin-style mock endpoints under /mock/.
//
// Endpoints:
//
//	GET/POST /mock/delay/{ms}          — sleep for {ms} milliseconds, return 200
//	GET/POST /mock/status/{code}       — return the given HTTP status code
//	GET/POST /mock/status/{code}/{ms}  — return status after delay
//	GET/POST /mock/error?rate=0.3      — return 500 with 30% probability, else 200
//	GET/POST /mock/echo               — return request headers + body as JSON
//	ANY      /mock/*                  — 200 OK (catch-all)
//
// Query params (work on any endpoint):
//
//	?delay=5000    — add delay in ms (additive with path delay)
//	?status=503    — override response status code
//	?error_rate=0.3 — return 500 with given probability
//	?body=hello    — override response body
// Register adds mock HTTP routes to the given mux.
func Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /mock/delay/{ms}", handleDelay)
	mux.HandleFunc("POST /mock/delay/{ms}", handleDelay)
	mux.HandleFunc("GET /mock/status/{code}/{ms}", handleStatusDelay)
	mux.HandleFunc("POST /mock/status/{code}/{ms}", handleStatusDelay)
	mux.HandleFunc("GET /mock/status/{code}", handleStatus)
	mux.HandleFunc("POST /mock/status/{code}", handleStatus)
	mux.HandleFunc("GET /mock/error", handleError)
	mux.HandleFunc("POST /mock/error", handleError)
	mux.HandleFunc("GET /mock/echo", handleEcho)
	mux.HandleFunc("POST /mock/echo", handleEcho)
	mux.HandleFunc("GET /mock/", handleCatchAll)
	mux.HandleFunc("POST /mock/", handleCatchAll)
}

func handleDelay(w http.ResponseWriter, r *http.Request) {
	ms := pathInt(r, "ms", 0)
	ms += queryInt(r, "delay", 0)
	sleep(ms)
	status := queryInt(r, "status", 200)
	applyErrorRate(r, &status)
	writeResponse(w, r, status, ms)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	code := pathInt(r, "code", 200)
	ms := queryInt(r, "delay", 0)
	sleep(ms)
	applyErrorRate(r, &code)
	writeResponse(w, r, code, ms)
}

func handleStatusDelay(w http.ResponseWriter, r *http.Request) {
	code := pathInt(r, "code", 200)
	ms := pathInt(r, "ms", 0)
	ms += queryInt(r, "delay", 0)
	sleep(ms)
	applyErrorRate(r, &code)
	writeResponse(w, r, code, ms)
}

func handleError(w http.ResponseWriter, r *http.Request) {
	ms := queryInt(r, "delay", 0)
	sleep(ms)
	rate := queryFloat(r, "rate", 0.5)
	status := 200
	if rand.Float64() < rate {
		status = 500
	}
	writeResponse(w, r, status, ms)
}

func handleEcho(w http.ResponseWriter, r *http.Request) {
	ms := queryInt(r, "delay", 0)
	sleep(ms)

	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	resp := map[string]any{
		"method":  r.Method,
		"url":     r.URL.String(),
		"headers": headers,
	}

	status := queryInt(r, "status", 200)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func handleCatchAll(w http.ResponseWriter, r *http.Request) {
	ms := queryInt(r, "delay", 0)
	sleep(ms)
	status := queryInt(r, "status", 200)
	applyErrorRate(r, &status)
	writeResponse(w, r, status, ms)
}

// helpers

func sleep(ms int) {
	if ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

func applyErrorRate(r *http.Request, status *int) {
	rate := queryFloat(r, "error_rate", 0)
	if rate > 0 && rand.Float64() < rate {
		*status = 500
	}
}

func writeResponse(w http.ResponseWriter, r *http.Request, status, delayMs int) {
	if body := r.URL.Query().Get("body"); body != "" {
		w.WriteHeader(status)
		fmt.Fprint(w, body)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"status":   status,
		"delay_ms": delayMs,
	})
}

func pathInt(r *http.Request, key string, def int) int {
	v := r.PathValue(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func queryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func queryFloat(r *http.Request, key string, def float64) float64 {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
