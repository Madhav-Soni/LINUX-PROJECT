# FIS – Process Lifecycle Monitoring: Run Guide

## Quick Start (3 commands)

```bash
# 1. Build everything
cd user-space && go build ./...
cd web && npm install && npm run build && cd ..

# 2. Run FIS backend with HTTP + procwatch enabled
./fis -config configs/fis.json -http :8090

# 3. Open dashboard
open http://localhost:8090        # served by vite preview / your web server
```

---

## Build Commands

```bash
# Backend
cd user-space
go build -o fis          ./cmd/fis/
go build -o fisdemo      ./cmd/fisdemo/
go build -o procwatch-demo ./cmd/procwatch-demo/

# Frontend
cd web
npm install
npm run dev     # dev mode  (localhost:5173, proxies /api → :8090)
npm run build   # production bundle → web/dist/
```

---

## Run Commands

### Backend

```bash
# Standard run (2 s poll, port 8090)
./fis -config configs/fis.json -http :8090

# Single poll + exit (useful for CI)
./fis -config configs/fis.json -http :8090 -once

# Custom poll interval (set poll_interval_seconds in config)
./fis -config configs/fis.json -http :8090
```

### Demo processes

```bash
# Triggers zombie: child exits, parent never calls wait()
./procwatch-demo -mode zombie

# Triggers orphan: parent exits, child is re-parented to PID 1
./procwatch-demo -mode orphan

# Triggers parent-exit-without-wait: child exits, parent exits 5 s later
./procwatch-demo -mode parent-nowait

# Triggers SIGKILL: parent sends kill -9 to child after 2 s
./procwatch-demo -mode sigkill

# Triggers SIGTERM: parent sends kill -15 to child after 2 s
./procwatch-demo -mode sigterm

# Triggers SIGSEGV: current process dereferences nil
./procwatch-demo -mode sigsegv

# Triggers SIGABRT: current process sends SIGABRT to itself
./procwatch-demo -mode sigabrt

# Legacy demos (cpu/mem/crash)
./fisdemo -mode cpu
./fisdemo -mode mem -mem-mb 400
./fisdemo -mode crash
```

---

## Test Scenarios

### Scenario 1 – Zombie Detection

**What it tests**: parent forks child; child exits immediately; parent never calls
`wait()`; child stays in `/proc` as state `Z` (zombie).

```bash
./procwatch-demo -mode zombie
```

**Expected terminal output (procwatch-demo)**:
```
[zombie] parent PID=12345  – forking child…
[zombie] child PID=12346 started; child will exit immediately, parent will NOT reap it
[zombie] parent sleeping 60 s – watch /proc for state=Z on the child PID
```

**Expected FIS log (fis.log)**:
```json
{"level":"info","kind":"procwatch_event","type":"zombie","severity":"warn",
 "pid":12346,"parent_pid":12345,
 "message":"process 12346 (procwatch-demo) entered zombie state; parent 12345 has not called wait()"}
```

After 3 s (default `zombieAge`):
```json
{"level":"info","kind":"procwatch_event","type":"zombie","severity":"critical",
 "pid":12346,"zombie_duration_ns":3000000000,
 "message":"process 12346 (procwatch-demo) remains zombie for 3.0s; parent 12345 still has not reaped it"}
```

**Expected API response**:
```bash
curl http://localhost:8090/api/v1/procwatch/events?type=zombie
# → {"data":[{"id":"...","kind":"fault","fault":{"type":"zombie","severity":"critical",...}}],"total":1}
```

**Expected SSE event**:
```
event: procwatch
data: {"id":"a1b2c3d4","kind":"fault","fault":{"type":"zombie","severity":"critical","pid":12346,...}}
```

**Expected frontend**: Toast notification appears in top-right with ☠ icon and red border.
Proc Monitor tab badge increments. Zombie listed in "Zombie Processes" table.

---

### Scenario 2 – Orphan Detection

**What it tests**: parent exits immediately; child continues running; child PPID
changes from `parent` to `1` (init/systemd).

```bash
./procwatch-demo -mode orphan
```

**Expected terminal**:
```
[orphan] parent PID=12347  – forking long-running child…
[orphan] child PID=12348 started; parent exiting NOW – child will be adopted by init
[orphan] child sleeping 60 s (should be re-parented to init)
```

**Expected FIS log**:
```json
{"kind":"procwatch_event","type":"orphan","severity":"warn","pid":12348,
 "message":"process 12348 (procwatch-demo) orphaned: re-parented to PID 1 after parent 12347 exited"}
```

---

### Scenario 3 – Parent Exit Without wait()

**What it tests**: child exits at T+1 s; parent exits at T+5 s without calling
`wait()`; kernel briefly shows the child as a zombie before init reaps it.

```bash
./procwatch-demo -mode parent-nowait
```

**Expected FIS log**:
```json
{"kind":"procwatch_event","type":"parent_exit","severity":"warn","pid":12350,
 "message":"parent 12349 of process 12350 (procwatch-demo) exited without reaping the child"}
```

---

### Scenario 4 – SIGKILL Child Termination

**What it tests**: parent sends `SIGKILL` (signal 9) to sleeping child; child
disappears from `/proc` with `ExitSignal=9`.

```bash
./procwatch-demo -mode sigkill
```

**Expected terminal**:
```
[SIGKILL] parent PID=12351 – forking sleeping child…
[SIGKILL] child PID=12352 started; will send SIGKILL in 2 s
[SIGKILL] sending SIGKILL to child 12352
[SIGKILL] child reaped; parent exiting
```

**Expected FIS log**:
```json
{"kind":"procwatch_event","type":"signal_death","severity":"critical",
 "pid":12352,"signal":9,
 "message":"process 12352 (procwatch-demo) killed by SIGKILL (signal 9)"}
```

---

### Scenario 5 – SIGSEGV Self-Inflicted

```bash
./procwatch-demo -mode sigsegv
```

**Expected FIS log**:
```json
{"kind":"procwatch_event","type":"signal_death","severity":"warn",
 "pid":12353,"signal":11,
 "message":"process 12353 (procwatch-demo) killed by SIGSEGV (signal 11)"}
```

---

### Scenario 6 – Full SSE Streaming Verification

```bash
# In terminal 1: subscribe to procwatch SSE stream
curl -N http://localhost:8090/api/v1/procwatch/events/stream

# In terminal 2: trigger zombie
./procwatch-demo -mode zombie
```

**Expected curl output** (within ~2 s of launch):
```
: keepalive

event: procwatch
data: {"id":"a1b2c3d4","timestamp":"2025-01-01T00:00:00Z","kind":"fault","fault":{"type":"zombie","severity":"warn","pid":12360,...}}

event: procwatch
data: {"id":"a1b2c3d5","timestamp":"2025-01-01T00:00:03Z","kind":"fault","fault":{"type":"zombie","severity":"critical","zombie_duration_ns":3014000000,...}}
```

---

### Scenario 7 – HTTP API Verification

```bash
# List all lifecycle events
curl http://localhost:8090/api/v1/procwatch/events | jq .

# Filter by type
curl "http://localhost:8090/api/v1/procwatch/events?type=zombie&limit=10" | jq .

# Live tracker snapshot (all processes tracked in memory)
curl http://localhost:8090/api/v1/procwatch/processes | jq '.data | length'

# Main event store (includes all fault types)
curl "http://localhost:8090/api/v1/events?limit=20" | jq '.data[].fault.type'
```

---

### Scenario 8 – Via Frontend Demo Tab

1. Open `http://localhost:5173` (dev) or `http://localhost:8090` (prod).
2. Navigate to **Demos** tab.
3. Select mode `zombie` from the dropdown.
4. Click **Start Demo**.
5. Within 2 s: toast notification appears (top-right, red border, ☠ icon).
6. Navigate to **Proc Monitor** tab: badge on tab shows count, zombie listed in
   "Zombie Processes" table, event visible in "Lifecycle Event Feed".
7. After 3 s: second toast appears for `severity: critical`.

---

## Expected Runtime Outputs

### Startup log

```
eBPF execve tracer running
```

### Normal poll cycle (no faults)

No output (silent unless `-v` flag added).

### Fault burst (zombie + orphan in same poll)

```json
{"level":"info","kind":"procwatch_event","type":"zombie","severity":"warn","pid":1234}
{"level":"info","kind":"procwatch_event","type":"orphan","severity":"warn","pid":1235}
```

### Procwatch processes endpoint sample

```json
{
  "data": [
    {"pid":1234,"ppid":1233,"name":"procwatch-demo","alive":true,
     "first_seen":"2025-01-01T00:00:01Z","zombie_since":"2025-01-01T00:00:02Z","exit_signal":0},
    {"pid":1235,"ppid":1,"name":"procwatch-demo","alive":true,
     "first_seen":"2025-01-01T00:00:01Z","zombie_since":"0001-01-01T00:00:00Z"}
  ],
  "total": 2
}
```

---

## Config Example (add to fis.json to watch procwatch-demo)

```json
{
  "poll_interval_seconds": 2,
  "log_file": "fis.log",
  "status_file": "fis-status.json",
  "targets": [
    {
      "name": "procwatch-demo",
      "match": { "name": "procwatch-demo" },
      "policy": {
        "cpu_threshold": 90,
        "memory_threshold_mb": 512,
        "on_fault": "none"
      }
    }
  ]
}
```

With `"on_fault": "restart"`, the policy engine will also attempt to restart
processes that trigger `signal_death` or `crash` faults.
