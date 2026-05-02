package recovery

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/owais/fis/user-space/internal/config"
	"github.com/owais/fis/user-space/internal/events"
	"github.com/owais/fis/user-space/internal/isolation"
	"github.com/owais/fis/user-space/internal/logger"
	"github.com/owais/fis/user-space/internal/policy"
)

type Engine struct {
	log      *logger.Logger
	cgroups  *isolation.Manager
	restarts map[string]*restartState
	observer ActionObserver
}

type restartState struct {
	attempts    []time.Time
	nextAllowed time.Time
}

type ActionResult struct {
	Type   policy.ActionType
	Target string
	PID    int
	Result string
	Reason string
	Error  string
	NewPID int
}

type ActionObserver func(event events.FaultEvent, result ActionResult)

const missingPID = "missing pid"

func New(log *logger.Logger, cgroups *isolation.Manager, observer ActionObserver) *Engine {
	return &Engine{
		log:      log,
		cgroups:  cgroups,
		restarts: make(map[string]*restartState),
		observer: observer,
	}
}

func (e *Engine) Execute(event events.FaultEvent, plan policy.ActionPlan) error {
	fields := map[string]interface{}{
		"kind":           "action",
		"event_id":       event.ID,
		"correlation_id": event.CorrelationID,
		"target":         plan.TargetName,
		"pid":            event.PID,
		"action":         plan.Type,
	}

	result := ActionResult{
		Type:   plan.Type,
		Target: plan.TargetName,
		PID:    event.PID,
	}

	switch plan.Type {
	case policy.ActionRestart:
		allowed, reason := e.allowRestart(plan.TargetName, plan.Restart)
		if !allowed {
			fields["reason"] = reason
			e.log.Info("restart skipped", fields)
			result.Result = "skipped"
			result.Reason = reason
			e.emit(event, result)
			return nil
		}
		if event.PID > 0 {
			_ = syscall.Kill(event.PID, syscall.SIGTERM)
			time.Sleep(500 * time.Millisecond)
		}
		pid, err := e.startProcess(plan)
		if err != nil {
			fields["error"] = err.Error()
			e.log.Error("restart failed", fields)
			result.Result = "failed"
			result.Error = err.Error()
			e.emit(event, result)
			return err
		}
		fields["new_pid"] = pid
		result.Result = "success"
		result.NewPID = pid
		if err := e.attachCgroup(plan, pid); err != nil {
			fields["cgroup_error"] = err.Error()
			e.log.Error("cgroup attach failed", fields)
			result.Error = err.Error()
		} else {
			e.log.Info("restart executed", fields)
		}
		e.emit(event, result)
		return nil
	case policy.ActionKill:
		if event.PID <= 0 {
			fields["error"] = missingPID
			e.log.Error("kill failed", fields)
			result.Result = "failed"
			result.Reason = missingPID
			e.emit(event, result)
			return errors.New(missingPID)
		}
		if err := syscall.Kill(event.PID, syscall.SIGKILL); err != nil {
			fields["error"] = err.Error()
			e.log.Error("kill failed", fields)
			result.Result = "failed"
			result.Error = err.Error()
			e.emit(event, result)
			return err
		}
		e.log.Info("kill executed", fields)
		result.Result = "success"
		e.emit(event, result)
		return nil
	case policy.ActionQuarantine:
		if event.PID <= 0 {
			fields["error"] = missingPID
			e.log.Error("quarantine failed", fields)
			result.Result = "failed"
			result.Reason = missingPID
			e.emit(event, result)
			return errors.New(missingPID)
		}
		if err := e.attachCgroup(plan, event.PID); err != nil {
			fields["error"] = err.Error()
			e.log.Error("quarantine failed", fields)
			result.Result = "failed"
			result.Error = err.Error()
			e.emit(event, result)
			return err
		}
		e.log.Info("quarantine executed", fields)
		result.Result = "success"
		e.emit(event, result)
		return nil
	default:
		e.log.Info("no action", fields)
		return nil
	}
}

func (e *Engine) emit(event events.FaultEvent, result ActionResult) {
	if e == nil || e.observer == nil {
		return
	}
	e.observer(event, result)
}

func (e *Engine) allowRestart(target string, policy config.RestartConfig) (bool, string) {
	state, ok := e.restarts[target]
	if !ok {
		state = &restartState{}
		e.restarts[target] = state
	}
	if policy.BackoffSeconds > 0 && time.Now().Before(state.nextAllowed) {
		return false, "backoff active"
	}
	window := time.Duration(policy.WindowSeconds) * time.Second
	cutoff := time.Now().Add(-window)
	filtered := state.attempts[:0]
	for _, attempt := range state.attempts {
		if attempt.After(cutoff) {
			filtered = append(filtered, attempt)
		}
	}
	state.attempts = filtered
	if policy.MaxAttempts > 0 && len(state.attempts) >= policy.MaxAttempts {
		return false, "restart limit reached"
	}
	state.attempts = append(state.attempts, time.Now())
	if policy.BackoffSeconds > 0 {
		state.nextAllowed = time.Now().Add(time.Duration(policy.BackoffSeconds) * time.Second)
	}
	return true, "allowed"
}

func (e *Engine) startProcess(plan policy.ActionPlan) (int, error) {
	if len(plan.Command) == 0 {
		return 0, errors.New("restart command missing")
	}
	cmd := exec.Command(plan.Command[0], plan.Command[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	go func() {
		_ = cmd.Wait()
	}()
	return cmd.Process.Pid, nil
}

func (e *Engine) attachCgroup(plan policy.ActionPlan, pid int) error {
	if e.cgroups == nil {
		return nil
	}
	if plan.Cgroup.CPUMax == "" && plan.Cgroup.MemoryMaxBytes == 0 {
		return nil
	}
	return e.cgroups.AttachProcess(plan.TargetName, pid, plan.Cgroup)
}
