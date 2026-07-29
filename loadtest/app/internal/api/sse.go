package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (a *App) HandleMetricsSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			groups := a.Store.List()
			result := make(map[string]any)
			for _, g := range groups {
				snap := g.Metrics.Snapshot()
				snap.InFlight = a.Tracker.InFlightCount()
				result[g.Config.Name] = map[string]any{
					"metrics": snap,
					"state":   g.State,
					"config":  g.Config,
				}
			}
			data, _ := json.Marshal(result)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
