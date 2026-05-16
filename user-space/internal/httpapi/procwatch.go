//go:build linux

package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/procwatch"
)

// RegisterProcWatchRoutes wires the procwatch subsystem into the existing mux.
func RegisterProcWatchRoutes(
	mux *http.ServeMux,
	tracker *procwatch.Tracker,
	lifecycleStore *procwatch.LifecycleStore,
	notifStore *procwatch.NotificationStore,
) {
	mux.HandleFunc("/api/v1/procwatch/processes", procwatchProcessesHandler(tracker))
	mux.HandleFunc("/api/v1/procwatch/events", procwatchEventsHandler(lifecycleStore))
	mux.HandleFunc("/api/v1/procwatch/events/stream", procwatchEventsStreamHandler(lifecycleStore))
	mux.HandleFunc("/api/v1/procwatch/notifications", procwatchNotifHandler(notifStore))
	mux.HandleFunc("/api/v1/procwatch/notifications/stream", procwatchNotifStreamHandler(notifStore))
}

// ── /api/v1/procwatch/processes ──────────────────────────────────────────────

func procwatchProcessesHandler(tracker *procwatch.Tracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		procs := tracker.Live()
		zombies := 0
		for _, p := range procs {
			if p.IsZombie() {
				zombies++
			}
		}
		summary := procwatch.Summary{
			Timestamp:   time.Now().UTC(),
			TotalLive:   len(procs),
			ZombieCount: zombies,
			Processes:   procs,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"data": summary})
	}
}

// ── /api/v1/procwatch/events ─────────────────────────────────────────────────

func procwatchEventsHandler(store *procwatch.LifecycleStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		limit := 100
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 1000 {
				limit = n
			}
		}
		evts := store.List(limit)
		if evts == nil {
			evts = []procwatch.LifecycleEvent{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"data": evts})
	}
}

// ── /api/v1/procwatch/events/stream ─────────────────────────────────────────

func procwatchEventsStreamHandler(store *procwatch.LifecycleStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		ch := store.Subscribe(32)
		defer store.Unsubscribe(ch)

		for {
			select {
			case <-r.Context().Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				payload, err := json.Marshal(evt)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "event: lifecycle\ndata: %s\n\n", payload)
				flusher.Flush()
			}
		}
	}
}

// ── /api/v1/procwatch/notifications ─────────────────────────────────────────

func procwatchNotifHandler(store *procwatch.NotificationStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				limit = n
			}
		}
		notifs := store.Recent(limit)
		if notifs == nil {
			notifs = []procwatch.Notification{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"data": notifs})
	}
}

// ── /api/v1/procwatch/notifications/stream ───────────────────────────────────

func procwatchNotifStreamHandler(store *procwatch.NotificationStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		ch := store.Subscribe(32)
		defer store.Unsubscribe(ch)

		for {
			select {
			case <-r.Context().Done():
				return
			case n, ok := <-ch:
				if !ok {
					return
				}
				payload, err := json.Marshal(n)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "event: notification\ndata: %s\n\n", payload)
				flusher.Flush()
			}
		}
	}
}
