package events

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// FaultType identifies the category of fault that was detected.
type FaultType string

// Severity indicates how urgent the fault is.
type Severity string

const (
	FaultCrash         FaultType = "crash"
	FaultCPUSpike      FaultType = "cpu_spike"
	FaultMemoryOveruse FaultType = "memory_overuse"

	// procwatch types
	FaultZombie      FaultType = "zombie"
	FaultOrphan      FaultType = "orphan"
	FaultParentExit  FaultType = "parent_exit"
	FaultSignalDeath FaultType = "signal_death"
)

const (
	SeverityInfo     Severity = "info"
	SeverityWarn     Severity = "warn"
	SeverityCritical Severity = "critical"
)

type FaultEvent struct {
	ID             string        `json:"id"`
	CorrelationID  string        `json:"correlation_id"`
	Timestamp      time.Time     `json:"timestamp"`
	Target         string        `json:"target"`
	PID            int           `json:"pid"`
	Type           FaultType     `json:"type"`
	Severity       Severity      `json:"severity"`
	Message        string        `json:"message"`
	CPUPercent     float64       `json:"cpu_percent,omitempty"`
	MemoryBytes    uint64        `json:"memory_bytes,omitempty"`
	ParentPID      int           `json:"parent_pid,omitempty"`
	Signal         int           `json:"signal,omitempty"`
	ZombieDuration time.Duration `json:"zombie_duration_ns,omitempty"`
}

func NewID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	return time.Now().UTC().Format("150405.000000")
}
