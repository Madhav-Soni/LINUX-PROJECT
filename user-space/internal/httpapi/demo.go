package httpapi

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

var errDemoNotFound = errors.New("demo not found")

// validModes is the complete set of modes the fisdemo binary understands.
var validModes = map[string]bool{
	"cpu":          true,
	"mem":          true,
	"crash":        true,
	"zombie":       true,
	"orphan":       true,
	"parent-nowait":true,
	"sigkill":      true,
	"sigterm":      true,
	"sigsegv":      true,
	"sigabrt":      true,
}

type demoRequest struct {
	Mode  string `json:"mode"`
	MemMB int    `json:"mem_mb"`
}

// DemoManager tracks running demo processes and manages their lifecycle.
type DemoManager struct {
	mu     sync.Mutex
	demos  map[int]*demoProcess
	binary string
}

type demoProcess struct {
	pid  int
	mode string
	cmd  *exec.Cmd
}

// NewDemoManager returns a DemoManager.
// binary is the base name or full path of the demo executable (default: "fisdemo").
func NewDemoManager(binary string) *DemoManager {
	if binary == "" {
		binary = "fisdemo"
	}
	return &DemoManager{
		demos:  make(map[int]*demoProcess),
		binary: binary,
	}
}

// Start launches a demo process in the given mode.
// Returns (pid, mode, error).
func (m *DemoManager) Start(mode string, memMB int) (int, string, error) {
	if m == nil {
		return 0, "", errors.New("demo manager not configured")
	}

	mode = normalizeMode(mode)
	if !validModes[mode] {
		return 0, "", fmt.Errorf("unsupported mode %q – valid modes: cpu, mem, crash, zombie, orphan, parent-nowait, sigkill, sigterm, sigsegv, sigabrt", mode)
	}

	path, err := resolveDemoBinary(m.binary)
	if err != nil {
		return 0, "", fmt.Errorf("demo binary not found (%s): %w", m.binary, err)
	}

	args := []string{"-mode", mode}
	if mode == "mem" {
		if memMB <= 0 {
			memMB = 100
		}
		args = append(args, "-mem-mb", fmt.Sprintf("%d", memMB))
	}

	cmd := exec.Command(path, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Give the demo its own process group so SIGTERM/SIGKILL on the parent
	// doesn't immediately kill children we want to observe as zombies/orphans.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return 0, "", fmt.Errorf("failed to start demo: %w", err)
	}

	proc := &demoProcess{pid: cmd.Process.Pid, mode: mode, cmd: cmd}

	m.mu.Lock()
	m.demos[proc.pid] = proc
	m.mu.Unlock()

	// Reap the top-level process asynchronously; child processes spawned by the
	// demo (e.g. for zombie/orphan demos) are tracked separately by procwatch.
	go func() {
		_ = cmd.Wait()
		m.mu.Lock()
		delete(m.demos, proc.pid)
		m.mu.Unlock()
	}()

	return proc.pid, proc.mode, nil
}

// Stop sends SIGTERM to the demo with the given PID.
func (m *DemoManager) Stop(pid int) (string, error) {
	if m == nil {
		return "", errDemoNotFound
	}

	m.mu.Lock()
	proc, ok := m.demos[pid]
	m.mu.Unlock()
	if !ok {
		return "", errDemoNotFound
	}

	if proc.cmd.Process != nil {
		_ = proc.cmd.Process.Signal(syscall.SIGTERM)
	}
	return proc.mode, nil
}

// List returns a snapshot of all currently tracked demo processes.
func (m *DemoManager) List() []map[string]interface{} {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]map[string]interface{}, 0, len(m.demos))
	for _, p := range m.demos {
		out = append(out, map[string]interface{}{"pid": p.pid, "mode": p.mode})
	}
	return out
}

// resolveDemoBinary tries several candidate paths and returns the first that
// exists and is executable.  Priority:
//
//  1. exec.LookPath (PATH search)
//  2. /app/user-space/<binary>    (Docker container layout)
//  3. ./user-space/<binary>       (running from repo root)
//  4. ./<binary>                  (running from user-space/)
func resolveDemoBinary(binary string) (string, error) {
	candidates := []string{
		binary,                             // PATH search handled by LookPath
		"/app/user-space/" + binary,        // Docker: WORKDIR /app
		"./user-space/" + binary,           // repo root
		"./" + binary,                      // user-space/ dir
	}

	// LookPath first (handles PATH correctly)
	if path, err := exec.LookPath(binary); err == nil {
		return path, nil
	}

	// Absolute/relative candidates
	for _, c := range candidates[1:] {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c, nil
		}
	}

	return "", fmt.Errorf("%w: tried PATH and %v", errDemoNotFound, candidates[1:])
}

func normalizeMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}
