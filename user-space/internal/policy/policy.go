package policy

import (
	"errors"
	"regexp"
	"strings"

	"github.com/owais/fis/user-space/internal/config"
	"github.com/owais/fis/user-space/internal/events"
	"github.com/owais/fis/user-space/internal/monitor"
)

type ActionType string

const (
	ActionRestart    ActionType = "restart"
	ActionKill       ActionType = "kill"
	ActionQuarantine ActionType = "quarantine"
	ActionNone       ActionType = "none"
)

type ActionPlan struct {
	Type         ActionType
	Reason       string
	TargetName   string
	Command      []string
	Restart      config.RestartConfig
	Cgroup       config.CgroupConfig
}

type Target struct {
	Config config.TargetConfig
	regex  *regexp.Regexp
}

type Engine struct {
	defaults     config.PolicyConfig
	targets      []*Target
	targetByName map[string]*Target
}

func NewEngine(cfg config.Config) (*Engine, error) {
	engine := &Engine{
		defaults:     cfg.Defaults,
		targetByName: make(map[string]*Target),
	}
	for _, targetCfg := range cfg.Targets {
		target := &Target{Config: targetCfg}
		if targetCfg.Match.Regex != "" {
			re, err := regexp.Compile(targetCfg.Match.Regex)
			if err != nil {
				return nil, err
			}
			target.regex = re
		}
		if _, exists := engine.targetByName[targetCfg.Name]; exists {
			return nil, errors.New("duplicate target name")
		}
		engine.targets = append(engine.targets, target)
		engine.targetByName[targetCfg.Name] = target
	}
	return engine, nil
}

func (e *Engine) MatchProcesses(procs []monitor.ProcessSnapshot) map[int]*Target {
	matches := make(map[int]*Target)
	for _, proc := range procs {
		for _, target := range e.targets {
			if target.Matches(proc) {
				matches[proc.PID] = target
				break
			}
		}
	}
	return matches
}

func (e *Engine) TargetByName(name string) *Target {
	return e.targetByName[name]
}

func (t *Target) Matches(proc monitor.ProcessSnapshot) bool {
	if t.Config.Match.Name != "" && proc.Name != t.Config.Match.Name {
		return false
	}
	if t.Config.Match.CmdlineContains != "" && !strings.Contains(proc.Cmdline, t.Config.Match.CmdlineContains) {
		return false
	}
	if t.regex != nil {
		value := proc.Cmdline
		if value == "" {
			value = proc.Name
		}
		if !t.regex.MatchString(value) {
			return false
		}
	}
	return true
}

func (e *Engine) EffectivePolicy(target *Target) config.PolicyConfig {
	policy := e.defaults

	if target.Config.CPUSpikePercent != nil {
		policy.CPUSpikePercent = *target.Config.CPUSpikePercent
	}
	if target.Config.MemoryOveruseBytes != nil {
		policy.MemoryOveruseBytes = *target.Config.MemoryOveruseBytes
	}

	if target.Config.Actions.Crash != "" {
		policy.Actions.Crash = target.Config.Actions.Crash
	}
	if target.Config.Actions.CPUSpike != "" {
		policy.Actions.CPUSpike = target.Config.Actions.CPUSpike
	}
	if target.Config.Actions.MemoryOveruse != "" {
		policy.Actions.MemoryOveruse = target.Config.Actions.MemoryOveruse
	}

	if target.Config.Restart != nil {
		if target.Config.Restart.MaxAttempts > 0 {
			policy.Restart.MaxAttempts = target.Config.Restart.MaxAttempts
		}
		if target.Config.Restart.WindowSeconds > 0 {
			policy.Restart.WindowSeconds = target.Config.Restart.WindowSeconds
		}
		if target.Config.Restart.BackoffSeconds >= 0 {
			policy.Restart.BackoffSeconds = target.Config.Restart.BackoffSeconds
		}
	}

	if target.Config.Cgroup != nil {
		if target.Config.Cgroup.CPUMax != "" {
			policy.Cgroup.CPUMax = target.Config.Cgroup.CPUMax
		}
		if target.Config.Cgroup.MemoryMaxBytes > 0 {
			policy.Cgroup.MemoryMaxBytes = target.Config.Cgroup.MemoryMaxBytes
		}
	}

	return policy
}

func (e *Engine) ActionForEvent(target *Target, policy config.PolicyConfig, event events.FaultEvent) ActionPlan {
	var action string
	switch event.Type {
	case events.FaultCrash:
		action = policy.Actions.Crash
	case events.FaultCPUSpike:
		action = policy.Actions.CPUSpike
	case events.FaultMemoryOveruse:
		action = policy.Actions.MemoryOveruse
	default:
		action = "none"
	}

	return ActionPlan{
		Type:       parseAction(action),
		Reason:     "policy action",
		TargetName: target.Config.Name,
		Command:    target.Config.Command,
		Restart:    policy.Restart,
		Cgroup:     policy.Cgroup,
	}
}

func parseAction(action string) ActionType {
	switch strings.ToLower(action) {
	case string(ActionRestart):
		return ActionRestart
	case string(ActionKill):
		return ActionKill
	case string(ActionQuarantine):
		return ActionQuarantine
	default:
		return ActionNone
	}
}
