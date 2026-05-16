//go:build !linux

package httpapi

import (
	"net/http"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/procwatch"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/ebpf"
)

func RegisterProcWatchRoutes(
	mux *http.ServeMux,
	tracker *procwatch.Tracker,
	lifecycleStore *procwatch.LifecycleStore,
	notifStore *procwatch.NotificationStore,
) {}

func RegisterSyscallRoutes(mux *http.ServeMux, store *ebpf.Store) {}
