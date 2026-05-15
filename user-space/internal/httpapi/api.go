package httpapi

import (
	"net/http"

	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/app"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/ebpf"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/eventstream"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/procwatch"
)

type Dependencies struct {
	ConfigPath      string
	Runtime         *app.Runtime
	Status          *app.StatusStore
	Events          *eventstream.Store
	Demos           *DemoManager
	EBPFStore       *ebpf.Store
	ProcwatchTracker *procwatch.Tracker // may be nil; enables /api/v1/procwatch/* routes
}

func NewHandler(deps Dependencies) http.Handler {
	api := &API{
		configPath: deps.ConfigPath,
		runtime:    deps.Runtime,
		status:     deps.Status,
		events:     deps.Events,
		demos:      deps.Demos,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/config", api.handleConfig)
	mux.HandleFunc("/api/v1/status", api.handleStatus)
	mux.HandleFunc("/api/v1/events", api.handleEvents)
	mux.HandleFunc("/api/v1/events/stream", api.handleEventsStream)
	mux.HandleFunc("/api/v1/demos", api.handleDemos)
	mux.HandleFunc("/api/v1/demos/", api.handleDemoByPID)
	RegisterSyscallRoutes(mux, deps.EBPFStore)

	// Procwatch lifecycle-monitoring routes (always registered; tracker may be nil).
	RegisterProcwatchRoutes(mux, deps.Events, deps.ProcwatchTracker)

	return withCORS(mux)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,PUT,POST,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Vary", "Origin")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
