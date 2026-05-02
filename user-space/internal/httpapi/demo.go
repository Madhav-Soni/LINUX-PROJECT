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

type demoRequest struct {
	Mode  string `json:"mode"`
	MemMB int    `json:"mem_mb"`
}

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

func NewDemoManager(binary string) *DemoManager {
	if binary == "" {
		binary = "fisdemo"
	}
	return &DemoManager{
		demos:  make(map[int]*demoProcess),
		binary: binary,
	}
}

func (m *DemoManager) Start(mode string, memMB int) (int, string, error) {
	if m == nil {
		return 0, "", errors.New("demo manager not configured")
	}

	mode = normalizeMode(mode)
	if mode != "cpu" && mode != "mem" && mode != "crash" {
		return 0, "", fmt.Errorf("unsupported mode: %s", mode)
	}

	path, err := resolveDemoBinary(m.binary)
	if err != nil {
		return 0, "", err
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
	if err := cmd.Start(); err != nil {
		return 0, "", err
	}

	proc := &demoProcess{pid: cmd.Process.Pid, mode: mode, cmd: cmd}

	m.mu.Lock()
	m.demos[proc.pid] = proc
	m.mu.Unlock()

	go func() {
		_ = cmd.Wait()
		m.mu.Lock()
		delete(m.demos, proc.pid)
		m.mu.Unlock()
	}()

	return proc.pid, proc.mode, nil
}

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

func resolveDemoBinary(binary string) (string, error) {
	path, err := exec.LookPath(binary)
	if err == nil {
		return path, nil
	}
	if _, statErr := os.Stat("./" + binary); statErr == nil {
		return "./" + binary, nil
	}
	return "", fmt.Errorf("%w: %s", errDemoNotFound, binary)
}

func normalizeMode(mode string) string {
	return strings.ToLower(mode)
}
