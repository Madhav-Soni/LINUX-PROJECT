package detector

import (
	"time"

	"github.com/owais/fis/user-space/internal/events"
	"github.com/owais/fis/user-space/internal/monitor"
	"github.com/owais/fis/user-space/internal/policy"
)

type Result struct {
	Events     []events.FaultEvent
	CPUPercent map[int]float64
}

type Detector struct {
	prevSnapshot  map[int]monitor.ProcessSnapshot
	prevTotalCPU  uint64
	prevTargets   map[string]map[int]struct{}
}

func New() *Detector {
	return &Detector{
		prevSnapshot: make(map[int]monitor.ProcessSnapshot),
		prevTargets:  make(map[string]map[int]struct{}),
	}
}

func (d *Detector) Detect(snapshot monitor.Snapshot, matches map[int]*policy.Target, engine *policy.Engine) Result {
	result := Result{
		CPUPercent: make(map[int]float64),
	}
	currentTargets := make(map[string]map[int]struct{})
	var deltaTotal uint64
	if d.prevTotalCPU > 0 && snapshot.TotalCPUJiffies >= d.prevTotalCPU {
		deltaTotal = snapshot.TotalCPUJiffies - d.prevTotalCPU
	}

	for _, proc := range snapshot.Processes {
		target := matches[proc.PID]
		if target == nil {
			continue
		}
		policyCfg := engine.EffectivePolicy(target)

		if _, ok := currentTargets[target.Config.Name]; !ok {
			currentTargets[target.Config.Name] = make(map[int]struct{})
		}
		currentTargets[target.Config.Name][proc.PID] = struct{}{}

		cpuPercent := 0.0
		if deltaTotal > 0 {
			if prevProc, ok := d.prevSnapshot[proc.PID]; ok && proc.CPUJiffies >= prevProc.CPUJiffies {
				deltaProc := proc.CPUJiffies - prevProc.CPUJiffies
				cpuPercent = float64(deltaProc) * 100 / float64(deltaTotal)
			}
		}
		result.CPUPercent[proc.PID] = cpuPercent

		if policyCfg.CPUSpikePercent > 0 && cpuPercent >= policyCfg.CPUSpikePercent {
			result.Events = append(result.Events, newEvent(target.Config.Name, proc.PID, events.FaultCPUSpike, events.SeverityWarn, "cpu spike detected", cpuPercent, proc.MemoryBytes))
		}

		if policyCfg.MemoryOveruseBytes > 0 && proc.MemoryBytes >= policyCfg.MemoryOveruseBytes {
			result.Events = append(result.Events, newEvent(target.Config.Name, proc.PID, events.FaultMemoryOveruse, events.SeverityWarn, "memory overuse detected", cpuPercent, proc.MemoryBytes))
		}
	}

	for targetName, prevPIDs := range d.prevTargets {
		currentPIDs := currentTargets[targetName]
		for pid := range prevPIDs {
			if currentPIDs == nil {
				result.Events = append(result.Events, newEvent(targetName, pid, events.FaultCrash, events.SeverityCritical, "process missing", 0, 0))
				continue
			}
			if _, ok := currentPIDs[pid]; !ok {
				result.Events = append(result.Events, newEvent(targetName, pid, events.FaultCrash, events.SeverityCritical, "process missing", 0, 0))
			}
		}
	}

	d.prevSnapshot = make(map[int]monitor.ProcessSnapshot, len(snapshot.Processes))
	for _, proc := range snapshot.Processes {
		d.prevSnapshot[proc.PID] = proc
	}
	d.prevTotalCPU = snapshot.TotalCPUJiffies
	d.prevTargets = currentTargets

	return result
}

func newEvent(target string, pid int, faultType events.FaultType, severity events.Severity, message string, cpuPercent float64, memBytes uint64) events.FaultEvent {
	id := events.NewID()
	return events.FaultEvent{
		ID:            id,
		CorrelationID: id,
		Timestamp:     time.Now().UTC(),
		Target:        target,
		PID:           pid,
		Type:          faultType,
		Severity:      severity,
		Message:       message,
		CPUPercent:    cpuPercent,
		MemoryBytes:   memBytes,
	}
}
