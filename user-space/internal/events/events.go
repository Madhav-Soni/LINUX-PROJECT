package events

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type FaultType string

type Severity string

const (
	FaultCrash         FaultType = "crash"
	FaultCPUSpike      FaultType = "cpu_spike"
	FaultMemoryOveruse FaultType = "memory_overuse"
)

const (
	SeverityInfo     Severity = "info"
	SeverityWarn     Severity = "warn"
	SeverityCritical Severity = "critical"
)

type FaultEvent struct {
	ID            string    `json:"id"`
	CorrelationID string    `json:"correlation_id"`
	Timestamp     time.Time `json:"timestamp"`
	Target        string    `json:"target"`
	PID           int       `json:"pid"`
	Type          FaultType `json:"type"`
	Severity      Severity  `json:"severity"`
	Message       string    `json:"message"`
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryBytes   uint64    `json:"memory_bytes"`
}

func NewID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	return time.Now().UTC().Format("20060102150405.000000000")
}
