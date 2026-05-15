package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/eventstream"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/events"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/procwatch"
)

// ProcwatchAPI extends the main HTTP handler with endpoints for process
// lifecycle monitoring.  Register via RegisterProcwatchRoutes.
type ProcwatchAPI struct {
	events  *eventstream.Store
	tracker *procwatch.Tracker
}

// RegisterProcwatchRoutes mounts procwatch-specific routes on the provided mux.
//
//	GET  /api/v1/procwatch/events         – recent lifecycle fault events (JSON)
//	GET  /api/v1/procwatch/events/stream  – SSE stream (fault kind only)
//	GET  /api/v1/procwatch/processes      – live tracker snapshot
func RegisterProcwatchRoutes(mux *http.ServeMux, store *eventstream.Store, tracker *procwatch.Tracker) {
	api := &ProcwatchAPI{events: store, tracker: tracker}
	mux.HandleFunc("/api/v1/procwatch/events", api.handleEvents)
	mux.HandleFunc("/api/v1/procwatch/events/stream", api.handleStream)
	mux.HandleFunc("/api/v1/procwatch/processes", api.handleProcesses)
}

// handleEvents returns recent lifecycle fault events, optionally filtered by
// fault type via ?type=zombie|orphan|signal_death|parent_exit.
//
//	GET /api/v1/procwatch/events?limit=50&type=zombie
func (a *ProcwatchAPI) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 500 {
			limit = v
		}
	}

	filterType := events.FaultType(r.URL.Query().Get("type"))

	all := a.events.List(500)
	out := make([]eventstream.Event, 0, len(all))
	for _, ev := range all {
		if ev.Kind != eventstream.KindFault || ev.Fault == nil {
			continue
		}
		if !isLifecycleFault(ev.Fault.Type) {
			continue
		}
		if filterType != "" && ev.Fault.Type != filterType {
			continue
		}
		out = append(out, ev)
	}

	// Return newest first.
	reverse(out)
	if len(out) > limit {
		out = out[:limit]
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data":  out,
		"total": len(out),
	})
}

// handleStream is an SSE endpoint that forwards only lifecycle-fault events
// (zombie / orphan / signal_death / parent_exit) from the shared event store.
//
//	GET /api/v1/procwatch/events/stream
func (a *ProcwatchAPI) handleStream(w http.ResponseWriter, r *http.Request) {
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

	// Send a keepalive comment every 15 s so proxies don't drop the connection.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	ch := a.events.Subscribe(32)
	defer a.events.Unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		case event, ok := <-ch:
			if !ok {
				return
			}
			// Filter: only lifecycle faults.
			if event.Kind != eventstream.KindFault || event.Fault == nil {
				continue
			}
			if !isLifecycleFault(event.Fault.Type) {
				continue
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = w.Write([]byte("event: procwatch\n"))
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(payload)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}
}

// handleProcesses returns a snapshot of the tracker's current process registry.
//
//	GET /api/v1/procwatch/processes
func (a *ProcwatchAPI) handleProcesses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	if a.tracker == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": []interface{}{}})
		return
	}

	snapshot := a.tracker.Snapshot()

	type procView struct {
		PID         int       `json:"pid"`
		PPID        int       `json:"ppid"`
		Name        string    `json:"name"`
		Alive       bool      `json:"alive"`
		FirstSeen   time.Time `json:"first_seen"`
		ZombieSince time.Time `json:"zombie_since,omitempty"`
		ExitSignal  int       `json:"exit_signal,omitempty"`
	}

	out := make([]procView, 0, len(snapshot))
	for _, e := range snapshot {
		out = append(out, procView{
			PID:         e.PID,
			PPID:        e.PPID,
			Name:        e.Name,
			Alive:       e.Alive,
			FirstSeen:   e.FirstSeen,
			ZombieSince: e.ZombieSince,
			ExitSignal:  e.ExitSignal,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data":  out,
		"total": len(out),
	})
}

// ── helpers ───────────────────────────────────────────────────────────────────

func isLifecycleFault(t events.FaultType) bool {
	switch t {
	case events.FaultZombie, events.FaultOrphan, events.FaultSignalDeath, events.FaultParentExit:
		return true
	}
	return false
}

func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
