package events

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// FaultType identifies what kind of fault occurred.
type FaultType string

// Severity describes how urgent the fault is.
type Severity string

// ── existing fault types ──────────────────────────────────────────────────────

const (
	FaultCrash         FaultType = "crash"
	FaultCPUSpike      FaultType = "cpu_spike"
	FaultMemoryOveruse FaultType = "memory_overuse"
)

// ── new process-lifecycle fault types ────────────────────────────────────────

const (
	// FaultZombie fires when a process is in state "Z" (defunct) for more than
	// one poll cycle, meaning its parent has not called wait().
	FaultZombie FaultType = "zombie"

	// FaultOrphan fires when a child process's original parent has exited and
	// the process has been re-parented to PID 1 (init/systemd).
	FaultOrphan FaultType = "orphan"

	// FaultSignalDeath fires when a monitored process disappears and its last
	// known /proc/<pid>/stat wchan or exit code indicates it was killed by a
	// signal (SIGKILL / SIGTERM).
	FaultSignalDeath FaultType = "signal_death"

	// FaultParentExit fires when the direct parent of a watched child exits
	// without first reaping the child (i.e. the child becomes an orphan because
	// the parent itself vanished, not because it called exit normally).
	FaultParentExit FaultType = "parent_exit"
)

// ── severities ────────────────────────────────────────────────────────────────

const (
	SeverityInfo     Severity = "info"
	SeverityWarn     Severity = "warn"
	SeverityCritical Severity = "critical"
)

// ── core event struct ─────────────────────────────────────────────────────────

// FaultEvent is the canonical event type published to the eventstream.Store.
// New fields are additions only; existing consumers remain compatible.
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

	// ── process-lifecycle additions ──────────────────────────────────────────

	// ParentPID is the PPID of the affected process at the time of detection.
	// Zero when not applicable.
	ParentPID int `json:"parent_pid,omitempty"`

	// Signal holds the signal number that caused a FaultSignalDeath, e.g. 9
	// for SIGKILL, 15 for SIGTERM.  Zero when not applicable.
	Signal int `json:"signal,omitempty"`

	// ZombieDuration is how long the process has been in state Z before the
	// event was raised.  Zero for non-zombie faults.
	ZombieDuration time.Duration `json:"zombie_duration_ns,omitempty"`
}

// NewID returns a short random hex identifier suitable for event IDs.
func NewID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	return time.Now().UTC().Format("20060102150405.000000000")
}
