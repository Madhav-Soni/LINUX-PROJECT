package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type ProcessStatus struct {
	PID         int     `json:"pid"`
	Name        string  `json:"name"`
	Cmdline     string  `json:"cmdline"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryBytes uint64  `json:"memory_bytes"`
}

type TargetStatus struct {
	Name      string          `json:"name"`
	Processes []ProcessStatus `json:"processes"`
}

type Status struct {
	Timestamp time.Time      `json:"timestamp"`
	Targets   []TargetStatus `json:"targets"`
}

func Write(path string, status Status) error {
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
