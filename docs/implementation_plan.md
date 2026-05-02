# Fault Isolation System - Implementation Plan

## Goal
Build a fault isolation and recovery system for Linux with a Go user-space core and an optional kernel module phase.

## Non-goals (initial release)
- No full orchestration platform (Kubernetes, Nomad)
- No automatic tuning or ML-based policies
- No cross-host coordination

## Assumptions and constraints
- Linux with /proc available
- cgroup v2 available for isolation
- Runs as root or with required capabilities
- Initial scope targets a single host and a fixed set of processes

---

## Architecture overview
Main components and data flow:

1. Monitor reads process snapshots from /proc on an interval.
2. Detector evaluates snapshots and emits fault events.
3. Policy engine decides actions for a given event.
4. Recovery engine executes actions and records outcomes.
5. Isolation applies cgroup limits when configured.
6. Logger/metrics records all events, actions, and errors.

Suggested Go package layout:

fault-isolation-system/
|
|-- user-space/
|   |-- cmd/
|   |   |-- fis/          # main service
|   |   `-- fisctl/       # CLI
|   |-- internal/
|   |   |-- monitor/
|   |   |-- detector/
|   |   |-- policy/
|   |   |-- recovery/
|   |   |-- isolation/
|   |   `-- logger/
|   |-- configs/
|   `-- main.go
|
|-- lkm/
|   |-- src/
|   `-- Makefile
|
`-- docs/
    `-- implementation_plan.md

---

## Part 1: User-space system (Go)

### Phase 0: Environment setup
Scope:
- Install Go toolchain and system build dependencies
- Initialize module and base directories

Deliverables:
- Go module with a buildable main package
- Base config file and example service config

Acceptance:
- `go build ./...` succeeds

---

### Phase 1: Process monitor and inventory
Scope:
- Read /proc for PID, name, state, and basic stats
- Capture snapshots at a configurable interval

Key files:
- internal/monitor/process.go

Deliverables:
- Process snapshot struct (pid, name, state, cmdline, cpu, mem)
- Snapshot reader with error handling for short-lived processes

Acceptance:
- Snapshot list returns stable output over multiple runs
- Processes that exit mid-read are handled without crashing

---

### Phase 2: Fault detection
Scope:
- Detect process exit/crash
- Detect CPU spike and memory overuse
- Emit fault events with severity and metadata

Key files:
- internal/detector/detector.go

Deliverables:
- Threshold configuration (per process or global)
- CPU delta calculation across snapshots
- Fault event model

Acceptance:
- Simulated CPU and memory stress triggers correct events
- Crash detection triggers within one polling interval

---

### Phase 3: Recovery engine
Scope:
- Execute policy decisions (restart, kill, quarantine)
- Implement backoff and restart limits

Key files:
- internal/recovery/recovery.go

Deliverables:
- Action executor with retries and timeouts
- Restart tracking (last N attempts, cool-down)

Acceptance:
- A stopped process is restarted by action policy
- Excessive restarts are capped by configured limits

---

### Phase 4: Resource isolation (cgroup v2)
Scope:
- Create and manage cgroup directories
- Apply cpu.max and memory.max limits

Key files:
- internal/isolation/cgroups.go

Deliverables:
- Cgroup manager with create/attach/detach cleanup
- Per-process or per-policy cgroup configuration

Acceptance:
- Target process runs within imposed limits
- Cgroup cleanup occurs on shutdown

---

### Phase 5: Logging and metrics
Scope:
- Structured logging of faults and actions
- Optional metrics counters for events and outcomes

Key files:
- internal/logger/logger.go

Deliverables:
- JSON log output to file or stdout
- Correlation IDs per fault event

Acceptance:
- Each event yields a log entry with action and outcome

---

### Phase 6: Policy engine
Scope:
- Policies defined by config (name match, regex, command)
- Map fault types to actions and thresholds

Key files:
- internal/policy/policy.go

Deliverables:
- Policy matcher
- Rule evaluation with precedence

Acceptance:
- Policies override global defaults as expected
- Unmatched processes fall back to safe defaults

---

### Phase 7: CLI / dashboard (optional)
Scope:
- Provide visibility into current faults and actions
- Support a basic status view and log tailing

Deliverables:
- `fisctl status` and `fisctl events` commands

Acceptance:
- CLI displays live events without service restart

---

## Part 2: LKM (separate, optional)
This phase is separate and only started after the user-space system is stable.

Phase K1: Hello world LKM
- Build and load a minimal module with clean unload

Phase K2: Process lifecycle tracking
- Track fork/exit and expose a minimal event stream

Phase K3: Kernel to user communication
- Choose a transport (netlink, char device, or procfs)
- Send lifecycle events to user-space

Phase K4: Advanced detection (optional)
- Kernel-side detection for specific signals or watchdogs

---

## Testing and validation
- Unit tests for /proc parsing and detector thresholds
- Integration tests with test processes that simulate CPU/memory spikes
- Manual test matrix for recovery and cgroup enforcement

---

## Safety and operational risks
- Ensure kill/restart policies are explicitly opt-in
- Provide dry-run mode for policy evaluation
- Log all actions with timestamps and reasons

---

## Roadmap summary
1. Setup and build skeleton
2. Monitor and snapshot
3. Detect and emit events
4. Recover with limits
5. Isolate with cgroups
6. Log and observe
7. Policy rules
8. Optional CLI
9. Optional LKM

---

## End result
- Self-healing Linux host for selected processes
- Fault detection, isolation, and recovery with clear audit logs
