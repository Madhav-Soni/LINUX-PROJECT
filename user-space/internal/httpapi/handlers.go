package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/owais/fis/user-space/internal/app"
	"github.com/owais/fis/user-space/internal/config"
	"github.com/owais/fis/user-space/internal/eventstream"
)

type API struct {
	configPath string
	runtime    *app.Runtime
	status     *app.StatusStore
	events     *eventstream.Store
	demos      *DemoManager
}

func (api *API) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := api.runtime.Config()
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": cfg})
	case http.MethodPut:
		api.handleConfigUpdate(w, r)
	default:
		methodNotAllowed(w)
	}
}

func (api *API) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r, 1024*1024)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Failed to read request body.", nil)
		return
	}

	cfg, err := config.Parse(body)
	if err != nil {
		var validationErr config.ValidationError
		if errors.As(err, &validationErr) {
			writeValidationError(w, validationErr)
			return
		}
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid JSON payload.", nil)
		return
	}

	engine, cgroups, rec, err := api.runtime.Build(cfg)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error(), nil)
		return
	}

	if err := config.Write(api.configPath, cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to write config file.", nil)
		return
	}

	api.runtime.Swap(cfg, engine, cgroups, rec)
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": cfg})
}

func (api *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	status, ok := api.status.Get()
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Status snapshot not available yet.", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": status})
}

func (api *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "limit must be a non-negative integer.", []fieldError{{Field: "limit", Message: "must be a non-negative integer"}})
			return
		}
		limit = value
	}

	if limit > 500 {
		limit = 500
	}

	events := api.events.List(limit)
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": events})
}

func (api *API) handleEventsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Streaming not supported.", nil)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := api.events.Subscribe(16)
	defer api.events.Unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: %s\n", event.Kind)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func (api *API) handleDemos(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	var req demoRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid JSON payload.", nil)
		return
	}

	if req.Mode == "" {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "mode is required.", []fieldError{{Field: "mode", Message: "mode is required"}})
		return
	}

	pid, mode, err := api.demos.Start(req.Mode, req.MemMB)
	if err != nil {
		if errors.Is(err, errDemoNotFound) {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Demo binary not available.", nil)
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{"data": map[string]interface{}{"pid": pid, "mode": mode}})
}

func (api *API) handleDemoByPID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}

	pidStr := strings.TrimPrefix(r.URL.Path, "/api/v1/demos/")
	if pidStr == "" || pidStr == r.URL.Path {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Demo not found.", nil)
		return
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "pid must be a positive integer.", []fieldError{{Field: "pid", Message: "must be a positive integer"}})
		return
	}

	mode, err := api.demos.Stop(pid)
	if err != nil {
		if errors.Is(err, errDemoNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Demo not found.", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to stop demo.", nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{"pid": pid, "mode": mode}})
}

func readBody(r *http.Request, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = 1024 * 1024
	}
	reader := io.LimitReader(r.Body, limit)
	defer r.Body.Close()
	return io.ReadAll(reader)
}
