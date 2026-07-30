package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/hookdeck/outpost/loadtest/app/internal/run"
)

func (a *App) registerRunRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/runs", a.handleStartRun)
	mux.HandleFunc("GET /api/runs/current", a.handleCurrentRun)
	mux.HandleFunc("POST /api/runs/current/abort", a.handleAbortRun)
	mux.HandleFunc("GET /api/runs", a.handleListRuns)
	mux.HandleFunc("GET /api/runs/{id}", a.handleGetRunArtifact)
	mux.HandleFunc("POST /api/runs/validate", a.handleValidateSpec)
}

// parseSpec accepts a spec as YAML or JSON — YAML is what humans write, JSON
// is what tooling posts, and the YAML parser reads both.
func parseSpec(r *http.Request) (*run.Spec, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return run.ParseSpec(body)
}

func (a *App) handleStartRun(w http.ResponseWriter, r *http.Request) {
	spec, err := parseSpec(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rn, err := a.Runs.Start(spec)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, rn)
}

// handleValidateSpec reports the budget a spec would spend without running it,
// so a scenario set can be checked before committing hours to it.
func (a *App) handleValidateSpec(w http.ResponseWriter, r *http.Request) {
	spec, err := parseSpec(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp := map[string]any{"budget": spec.Budget()}
	if err := spec.Validate(); err != nil {
		resp["valid"] = false
		resp["error"] = err.Error()
		writeJSON(w, http.StatusUnprocessableEntity, resp)
		return
	}
	resp["valid"] = true
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleCurrentRun(w http.ResponseWriter, r *http.Request) {
	cur := a.Runs.Current()
	if cur == nil {
		writeError(w, http.StatusNotFound, "no run has been started")
		return
	}
	writeJSON(w, http.StatusOK, cur)
}

func (a *App) handleAbortRun(w http.ResponseWriter, r *http.Request) {
	if err := a.Runs.Abort(); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.Runs.Current())
}

func (a *App) handleListRuns(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(a.Config.ExportDir)
	if err != nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	var ids []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			ids = append(ids, e.Name()[:len(e.Name())-5])
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	writeJSON(w, http.StatusOK, ids)
}

func (a *App) handleGetRunArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Reject anything that could climb out of the export directory.
	if id != filepath.Base(id) || id == "." || id == ".." {
		writeError(w, http.StatusBadRequest, "invalid run id")
		return
	}
	data, err := os.ReadFile(filepath.Join(a.Config.ExportDir, id+".json"))
	if err != nil {
		writeError(w, http.StatusNotFound, "no export for run "+id)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
