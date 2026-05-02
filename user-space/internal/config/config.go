package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	PollIntervalSeconds int            `json:"poll_interval_seconds"`
	LogFile             string         `json:"log_file"`
	StatusFile          string         `json:"status_file"`
	CgroupRoot          string         `json:"cgroup_root"`
	Defaults            PolicyConfig   `json:"defaults"`
	Targets             []TargetConfig `json:"targets"`
	cgroupRootSet       bool           `json:"-"`
	logFileSet          bool           `json:"-"`
}

type configJSON struct {
	PollIntervalSeconds int            `json:"poll_interval_seconds"`
	LogFile             *string        `json:"log_file"`
	StatusFile          string         `json:"status_file"`
	CgroupRoot          *string        `json:"cgroup_root"`
	Defaults            PolicyConfig   `json:"defaults"`
	Targets             []TargetConfig `json:"targets"`
}

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationError struct {
	Fields []FieldError
}

func (e ValidationError) Error() string {
	return "config validation failed"
}

type PolicyConfig struct {
	CPUSpikePercent    float64       `json:"cpu_spike_percent"`
	MemoryOveruseBytes uint64        `json:"memory_overuse_bytes"`
	Actions            ActionConfig  `json:"actions"`
	Restart            RestartConfig `json:"restart"`
	Cgroup             CgroupConfig  `json:"cgroup"`
}

type ActionConfig struct {
	Crash         string `json:"crash"`
	CPUSpike      string `json:"cpu_spike"`
	MemoryOveruse string `json:"memory_overuse"`
}

type RestartConfig struct {
	MaxAttempts    int `json:"max_attempts"`
	WindowSeconds  int `json:"window_seconds"`
	BackoffSeconds int `json:"backoff_seconds"`
}

type CgroupConfig struct {
	CPUMax         string `json:"cpu_max"`
	MemoryMaxBytes uint64 `json:"memory_max_bytes"`
}

type TargetMatch struct {
	Name            string `json:"name"`
	CmdlineContains string `json:"cmdline_contains"`
	Regex           string `json:"regex"`
}

type TargetConfig struct {
	Name               string         `json:"name"`
	Match              TargetMatch    `json:"match"`
	Command            []string       `json:"command"`
	CPUSpikePercent    *float64       `json:"cpu_spike_percent"`
	MemoryOveruseBytes *uint64        `json:"memory_overuse_bytes"`
	Actions            ActionConfig   `json:"actions"`
	Restart            *RestartConfig `json:"restart"`
	Cgroup             *CgroupConfig  `json:"cgroup"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return Parse(data)
}

func Parse(data []byte) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	applyDefaults(&cfg)
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func Validate(cfg Config) error {
	return validateConfig(cfg)
}

func Write(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
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

func (cfg *Config) UnmarshalJSON(data []byte) error {
	var raw configJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	cfg.PollIntervalSeconds = raw.PollIntervalSeconds
	if raw.LogFile != nil {
		cfg.LogFile = *raw.LogFile
		cfg.logFileSet = true
	} else {
		cfg.LogFile = ""
		cfg.logFileSet = false
	}
	cfg.StatusFile = raw.StatusFile
	cfg.Defaults = raw.Defaults
	cfg.Targets = raw.Targets
	if raw.CgroupRoot != nil {
		cfg.CgroupRoot = *raw.CgroupRoot
		cfg.cgroupRootSet = true
		return nil
	}

	cfg.CgroupRoot = ""
	cfg.cgroupRootSet = false
	return nil
}

func applyDefaults(cfg *Config) {
	if cfg.PollIntervalSeconds <= 0 {
		cfg.PollIntervalSeconds = 2
	}
	if cfg.LogFile == "" && !cfg.logFileSet {
		cfg.LogFile = "fis.log"
	}
	if cfg.StatusFile == "" {
		cfg.StatusFile = "fis-status.json"
	}
	if !cfg.cgroupRootSet && cfg.CgroupRoot == "" {
		cfg.CgroupRoot = "/sys/fs/cgroup"
	}

	if cfg.Defaults.Actions.Crash == "" {
		cfg.Defaults.Actions.Crash = "restart"
	}
	if cfg.Defaults.Actions.CPUSpike == "" {
		cfg.Defaults.Actions.CPUSpike = "quarantine"
	}
	if cfg.Defaults.Actions.MemoryOveruse == "" {
		cfg.Defaults.Actions.MemoryOveruse = "kill"
	}

	if cfg.Defaults.Restart.MaxAttempts <= 0 {
		cfg.Defaults.Restart.MaxAttempts = 3
	}
	if cfg.Defaults.Restart.WindowSeconds <= 0 {
		cfg.Defaults.Restart.WindowSeconds = 60
	}
	if cfg.Defaults.Restart.BackoffSeconds < 0 {
		cfg.Defaults.Restart.BackoffSeconds = 0
	}
}

func validateConfig(cfg Config) error {
	var errs []FieldError
	for i, target := range cfg.Targets {
		if target.Name == "" {
			errs = append(errs, FieldError{
				Field:   fmt.Sprintf("targets[%d].name", i),
				Message: "name is required",
			})
		}
		if target.Match.Name == "" && target.Match.CmdlineContains == "" && target.Match.Regex == "" {
			errs = append(errs, FieldError{
				Field:   fmt.Sprintf("targets[%d].match", i),
				Message: "match criteria is required",
			})
		}
	}
	if len(errs) > 0 {
		return ValidationError{Fields: errs}
	}
	return nil
}
