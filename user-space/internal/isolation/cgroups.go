package isolation

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/owais/fis/user-space/internal/config"
	"github.com/owais/fis/user-space/internal/logger"
)

type Manager struct {
	Root    string
	log     *logger.Logger
	mu      sync.Mutex
	created map[string]struct{}
}

func NewManager(root string, log *logger.Logger) *Manager {
	if root == "" {
		return nil
	}
	return &Manager{
		Root:    root,
		log:     log,
		created: make(map[string]struct{}),
	}
}

func (m *Manager) EnsureCgroup(name string, limits config.CgroupConfig) (string, error) {
	if m == nil {
		return "", errors.New("cgroup manager disabled")
	}
	path := filepath.Join(m.Root, sanitizeName(name))
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", err
	}

	if limits.CPUMax != "" {
		if err := os.WriteFile(filepath.Join(path, "cpu.max"), []byte(limits.CPUMax), 0644); err != nil {
			return "", err
		}
	}
	if limits.MemoryMaxBytes > 0 {
		value := strconv.FormatUint(limits.MemoryMaxBytes, 10)
		if err := os.WriteFile(filepath.Join(path, "memory.max"), []byte(value), 0644); err != nil {
			return "", err
		}
	}

	m.mu.Lock()
	m.created[path] = struct{}{}
	m.mu.Unlock()

	return path, nil
}

func (m *Manager) AttachProcess(name string, pid int, limits config.CgroupConfig) error {
	if m == nil {
		return errors.New("cgroup manager disabled")
	}
	path, err := m.EnsureCgroup(name, limits)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(path, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0644)
}

func (m *Manager) DetachProcess(pid int) error {
	if m == nil {
		return errors.New("cgroup manager disabled")
	}
	return os.WriteFile(filepath.Join(m.Root, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0644)
}

func (m *Manager) Cleanup() {
	if m == nil {
		return
	}
	m.mu.Lock()
	paths := make([]string, 0, len(m.created))
	for path := range m.created {
		paths = append(paths, path)
	}
	m.mu.Unlock()

	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			if m.log != nil {
				m.log.Error("cgroup cleanup failed", map[string]interface{}{"path": path, "error": err.Error()})
			}
		}
	}
}

func sanitizeName(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
