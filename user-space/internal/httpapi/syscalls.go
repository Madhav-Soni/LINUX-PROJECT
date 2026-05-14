//go:build linux

package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/ebpf"
)

// RegisterSyscallRoutes wires the eBPF store into your existing mux.
// Call this from wherever you register your existing /api/v1/* routes.
func RegisterSyscallRoutes(mux *http.ServeMux, store *ebpf.Store) {
	mux.HandleFunc("/api/v1/syscalls", syscallsHandler(store))
}

func syscallsHandler(store *ebpf.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// ?limit=N, default 100
		limit := 100
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}

		events := store.Recent(limit)
		if events == nil {
			events = []ebpf.ExecveEvent{} // never return null JSON array
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(events)
	}
}
