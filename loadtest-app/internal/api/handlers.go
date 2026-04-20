package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/hookdeck/outpost/loadtest-app/internal/group"
)

func (a *App) RegisterRoutes(mux *http.ServeMux) {
	// Group CRUD
	mux.HandleFunc("POST /api/groups", a.handleCreateGroup)
	mux.HandleFunc("GET /api/groups", a.handleListGroups)
	mux.HandleFunc("GET /api/groups/{name}", a.handleGetGroup)
	mux.HandleFunc("PATCH /api/groups/{name}", a.handleUpdateGroup)
	mux.HandleFunc("DELETE /api/groups/{name}", a.handleDeleteGroup)

	// Group lifecycle
	mux.HandleFunc("POST /api/groups/{name}/provision", a.handleProvision)
	mux.HandleFunc("POST /api/groups/{name}/start", a.handleStart)
	mux.HandleFunc("POST /api/groups/{name}/stop", a.handleStop)
	mux.HandleFunc("POST /api/groups/{name}/reset", a.handleReset)
	mux.HandleFunc("POST /api/groups/{name}/teardown", a.handleTeardown)

	// Global
	mux.HandleFunc("POST /api/start", a.handleStartAll)
	mux.HandleFunc("POST /api/stop", a.handleStopAll)
	mux.HandleFunc("POST /api/reset", a.handleResetAll)
	mux.HandleFunc("GET /api/metrics", a.handleMetrics)
	mux.HandleFunc("GET /api/status", a.handleStatus)
}

func (a *App) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var cfg group.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if cfg.Name == "" || cfg.TenantPrefix == "" || cfg.TenantCount == 0 || len(cfg.Topics) == 0 {
		writeError(w, http.StatusBadRequest, "name, tenant_prefix, tenant_count, and topics are required")
		return
	}

	g, err := a.Store.Create(cfg)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, groupResponse(g))
}

func (a *App) handleListGroups(w http.ResponseWriter, r *http.Request) {
	groups := a.Store.List()
	resp := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		resp = append(resp, groupSummary(g))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	g, err := a.Store.Get(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, groupResponse(g))
}

func (a *App) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	g, err := a.Store.Get(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	var update struct {
		RatePerTenant *int     `json:"rate_per_tenant,omitempty"`
		PayloadBytes  *int     `json:"payload_bytes,omitempty"`
		LatencyMs     *int64   `json:"latency_ms,omitempty"`
		JitterMs      *int64   `json:"latency_jitter_ms,omitempty"`
		ErrorRate     *float64 `json:"error_rate,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if update.RatePerTenant != nil {
		g.Config.Publish.RatePerTenant = *update.RatePerTenant
		totalRate := *update.RatePerTenant * g.Config.TenantCount
		a.Publisher.UpdateRate(g.Config.Name, totalRate)
	}
	if update.PayloadBytes != nil {
		g.Config.Publish.PayloadBytes = *update.PayloadBytes
	}
	if update.LatencyMs != nil {
		g.MockProfile.SetLatency(*update.LatencyMs)
		g.Config.MockProfile.LatencyMs = *update.LatencyMs
	}
	if update.JitterMs != nil {
		g.MockProfile.SetJitter(*update.JitterMs)
		g.Config.MockProfile.JitterMs = *update.JitterMs
	}
	if update.ErrorRate != nil {
		g.MockProfile.SetErrorRate(*update.ErrorRate)
		g.Config.MockProfile.ErrorRate = *update.ErrorRate
	}

	writeJSON(w, http.StatusOK, groupResponse(g))
}

func (a *App) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	g, err := a.Store.Get(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if g.State == group.StateRunning || g.State == group.StateProvisioning {
		writeError(w, http.StatusConflict, "cannot delete group in state: "+string(g.State))
		return
	}

	if g.State == group.StateProvisioned || g.State == group.StateStopped || g.State == group.StateError {
		_ = a.Provisioner.Teardown(r.Context(), g)
	}

	a.Store.Delete(name)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleProvision(w http.ResponseWriter, r *http.Request) {
	g, err := a.Store.Get(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if g.State != group.StateCreated && g.State != group.StateError {
		writeError(w, http.StatusConflict, "group must be in created or error state to provision")
		return
	}

	go func() {
		if err := a.Provisioner.Provision(context.Background(), g); err != nil {
			slog.Error("provision failed", "group", g.Config.Name, "error", err)
		} else {
			slog.Info("provision complete", "group", g.Config.Name)
		}
	}()

	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "provisioning"})
}

func (a *App) handleStart(w http.ResponseWriter, r *http.Request) {
	g, err := a.Store.Get(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if g.State != group.StateProvisioned && g.State != group.StateStopped {
		writeError(w, http.StatusConflict, "group must be provisioned or stopped to start")
		return
	}

	if err := g.Transition(group.StateRunning); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	if err := a.Publisher.Start(g); err != nil {
		g.SetError(err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "running"})
}

func (a *App) handleStop(w http.ResponseWriter, r *http.Request) {
	g, err := a.Store.Get(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if g.State != group.StateRunning {
		writeError(w, http.StatusConflict, "group must be running to stop")
		return
	}

	if err := g.Transition(group.StateStopping); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	a.Publisher.Stop(g)
	g.Transition(group.StateStopped)

	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (a *App) handleReset(w http.ResponseWriter, r *http.Request) {
	g, err := a.Store.Get(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if g.State != group.StateStopped {
		writeError(w, http.StatusConflict, "group must be stopped to reset")
		return
	}

	g.Metrics.Reset()
	g.Transition(group.StateProvisioned)
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

func (a *App) handleTeardown(w http.ResponseWriter, r *http.Request) {
	g, err := a.Store.Get(r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if g.State == group.StateRunning || g.State == group.StateProvisioning {
		writeError(w, http.StatusConflict, "cannot teardown group in state: "+string(g.State))
		return
	}

	go func() {
		if err := a.Provisioner.Teardown(context.Background(), g); err != nil {
			slog.Error("teardown failed", "group", g.Config.Name, "error", err)
		} else {
			slog.Info("teardown complete", "group", g.Config.Name)
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "tearing down"})
}

// Global handlers

func (a *App) handleStartAll(w http.ResponseWriter, r *http.Request) {
	started := 0
	for _, g := range a.Store.List() {
		if g.State == group.StateProvisioned || g.State == group.StateStopped {
			if err := g.Transition(group.StateRunning); err == nil {
				if err := a.Publisher.Start(g); err != nil {
					g.SetError(err)
				} else {
					started++
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"started": started})
}

func (a *App) handleStopAll(w http.ResponseWriter, r *http.Request) {
	stopped := 0
	for _, g := range a.Store.List() {
		if g.State == group.StateRunning {
			g.Transition(group.StateStopping)
			a.Publisher.Stop(g)
			g.Transition(group.StateStopped)
			stopped++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"stopped": stopped})
}

func (a *App) handleResetAll(w http.ResponseWriter, r *http.Request) {
	reset := 0
	for _, g := range a.Store.List() {
		if g.State == group.StateStopped {
			g.Metrics.Reset()
			g.Transition(group.StateProvisioned)
			reset++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reset": reset})
}

func (a *App) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("stream") == "true" {
		a.HandleMetricsSSE(w, r)
		return
	}

	groups := a.Store.List()
	result := make(map[string]any)
	for _, g := range groups {
		snap := g.Metrics.Snapshot()
		snap.InFlight = a.Tracker.InFlightCount()
		result[g.Config.Name] = snap
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	groups := a.Store.List()
	groupStates := make(map[string]string, len(groups))
	for _, g := range groups {
		groupStates[g.Config.Name] = string(g.State)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"outpost_url": a.Config.OutpostURL,
		"mock_url":    a.Config.MockURL,
		"groups":      groupStates,
	})
}

// Helpers

func groupResponse(g *group.Group) map[string]any {
	snap := g.Metrics.Snapshot()
	return map[string]any{
		"name":    g.Config.Name,
		"config":  g.Config,
		"state":   g.State,
		"error":   g.Error,
		"metrics": snap,
	}
}

func groupSummary(g *group.Group) map[string]any {
	snap := g.Metrics.Snapshot()
	return map[string]any{
		"name":              g.Config.Name,
		"state":             g.State,
		"publish_total":     snap.PublishTotal,
		"delivery_total":    snap.DeliveryTotal,
		"publish_rate":      snap.PublishRatePerSec,
		"e2e_latency_p50":  snap.E2ELatencyP50,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
