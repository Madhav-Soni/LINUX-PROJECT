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

// ── Original memory / CPU / crash fault types ─────────────────────────────────
const (
	FaultCrash         FaultType = "crash"
	FaultCPUSpike      FaultType = "cpu_spike"
	FaultMemoryOveruse FaultType = "memory_overuse"
)

// ── Process-lifecycle fault types (procwatch) ─────────────────────────────────
const (
	// FaultZombie fires when a process enters state "Z" (not reaped by parent).
	FaultZombie FaultType = "zombie"

	// FaultOrphan fires when a process is re-parented to PID 1 (init/systemd)
	// because its original parent exited without calling wait().
	FaultOrphan FaultType = "orphan"

	// FaultParentNoWait fires when a parent exits while its children are still
	// running, indicating it never called wait()/waitpid() before exiting.
	FaultParentNoWait FaultType = "parent_no_wait"

	// FaultChildKilled fires when a child process is terminated by an abnormal
	// signal (SIGKILL, SIGSEGV, SIGABRT, SIGTERM …).
	FaultChildKilled FaultType = "child_killed"
)

// ── Severity levels ───────────────────────────────────────────────────────────
const (
	SeverityInfo     Severity = "info"
	SeverityWarn     Severity = "warn"
	SeverityCritical Severity = "critical"
)

// FaultEvent is the canonical event structure emitted by both the resource
// detector (CPU / memory) and the process-lifecycle detector (procwatch).
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

// NewID generates a cryptographically random 8-byte hex string for event IDs.
func NewID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	return time.Now().UTC().Format("20060102150405.000000000")
}
