import { useEffect, useMemo, useState } from "react";
import { motion, useReducedMotion } from "framer-motion";

const API_BASE = import.meta.env.VITE_API_BASE || "http://localhost:8090";

const formatBytes = (bytes) => {
  if (bytes === 0) return "0 B";
  if (!bytes && bytes !== 0) return "n/a";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let index = 0;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${value.toFixed(1)} ${units[index]}`;
};

const formatPercent = (value) => {
  if (value === 0) return "0.0%";
  if (!value && value !== 0) return "n/a";
  return `${value.toFixed(1)}%`;
};

const formatTimestamp = (value) => {
  if (!value) return "n/a";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "n/a";
  return date.toLocaleString();
};

const severityClass = (severity) => {
  if (!severity) return "badge";
  if (severity === "critical") return "badge badge-critical";
  if (severity === "warn") return "badge badge-warn";
  return "badge badge-info";
};

async function fetchJSON(path, options = {}) {
  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...options.headers,
    },
  });

  const contentType = res.headers.get("content-type") || "";
  const payload = contentType.includes("application/json")
    ? await res.json()
    : null;

  if (!res.ok) {
    const message = payload?.error?.message || `Request failed (${res.status})`;
    throw new Error(message);
  }

  return payload;
}

function App() {
  const reduceMotion = useReducedMotion();
  const [status, setStatus] = useState(null);
  const [statusLoading, setStatusLoading] = useState(false);
  const [statusError, setStatusError] = useState("");

  const [configText, setConfigText] = useState("");
  const [configLoading, setConfigLoading] = useState(false);
  const [configError, setConfigError] = useState("");
  const [configSuccess, setConfigSuccess] = useState("");

  const [events, setEvents] = useState([]);
  const [eventsError, setEventsError] = useState("");
  const [streamState, setStreamState] = useState("connecting");

  const [demoMode, setDemoMode] = useState("cpu");
  const [demoMem, setDemoMem] = useState("200");
  const [demoLoading, setDemoLoading] = useState(false);
  const [demoError, setDemoError] = useState("");
  const [demoSuccess, setDemoSuccess] = useState("");
  const [activeDemos, setActiveDemos] = useState([]);

  const [stopPid, setStopPid] = useState("");
  const [stopLoading, setStopLoading] = useState(false);
  const [stopError, setStopError] = useState("");
  const [stopSuccess, setStopSuccess] = useState("");

  const layoutVariants = useMemo(
    () => ({
      hidden: {},
      show: {
        transition: {
          staggerChildren: 0.08,
        },
      },
    }),
    []
  );

  const cardVariants = useMemo(
    () => ({
      hidden: { opacity: 0, y: reduceMotion ? 0 : 18 },
      show: {
        opacity: 1,
        y: 0,
        transition: { duration: 0.45, ease: "easeOut" },
      },
    }),
    [reduceMotion]
  );

  useEffect(() => {
    let active = true;

    const loadStatus = async () => {
      setStatusLoading(true);
      setStatusError("");
      try {
        const data = await fetchJSON("/api/v1/status");
        if (!active) return;
        setStatus(data.data);
      } catch (err) {
        if (!active) return;
        setStatusError(err.message);
      } finally {
        if (active) setStatusLoading(false);
      }
    };

    loadStatus();
    const timer = window.setInterval(loadStatus, 10000);

    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, []);

  useEffect(() => {
    let active = true;

    const loadConfig = async () => {
      setConfigLoading(true);
      setConfigError("");
      try {
        const data = await fetchJSON("/api/v1/config");
        if (!active) return;
        setConfigText(JSON.stringify(data.data, null, 2));
      } catch (err) {
        if (!active) return;
        setConfigError(err.message);
      } finally {
        if (active) setConfigLoading(false);
      }
    };

    loadConfig();

    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    let active = true;

    const loadEvents = async () => {
      setEventsError("");
      try {
        const data = await fetchJSON("/api/v1/events?limit=50");
        if (!active) return;
        if (Array.isArray(data.data)) {
          setEvents(data.data.slice().reverse());
        }
      } catch (err) {
        if (!active) return;
        setEventsError(err.message);
      }
    };

    loadEvents();

    const source = new EventSource(`${API_BASE}/api/v1/events/stream`);
    setStreamState("connecting");

    const handleStreamEvent = (event) => {
      try {
        const next = JSON.parse(event.data);
        setEvents((prev) => [next, ...prev].slice(0, 200));
      } catch (err) {
        setEventsError("Failed to parse event payload.");
      }
    };

    source.onopen = () => {
      if (active) setStreamState("open");
    };

    source.onerror = () => {
      if (active) setStreamState("error");
    };

    source.addEventListener("fault", handleStreamEvent);
    source.addEventListener("action", handleStreamEvent);
    source.onmessage = handleStreamEvent;

    return () => {
      active = false;
      source.close();
      setStreamState("closed");
    };
  }, []);

  const handleConfigSave = async (event) => {
    event.preventDefault();
    setConfigError("");
    setConfigSuccess("");

    let payload;
    try {
      payload = JSON.parse(configText);
    } catch (err) {
      setConfigError("Config must be valid JSON.");
      return;
    }

    setConfigLoading(true);
    try {
      const data = await fetchJSON("/api/v1/config", {
        method: "PUT",
        body: JSON.stringify(payload),
      });
      setConfigText(JSON.stringify(data.data, null, 2));
      setConfigSuccess("Config saved successfully.");
    } catch (err) {
      setConfigError(err.message);
    } finally {
      setConfigLoading(false);
    }
  };

  const handleConfigReload = async () => {
    setConfigError("");
    setConfigSuccess("");
    setConfigLoading(true);
    try {
      const data = await fetchJSON("/api/v1/config");
      setConfigText(JSON.stringify(data.data, null, 2));
    } catch (err) {
      setConfigError(err.message);
    } finally {
      setConfigLoading(false);
    }
  };

  const handleDemoStart = async (event) => {
    event.preventDefault();
    setDemoError("");
    setDemoSuccess("");

    if (demoMode === "mem") {
      const value = Number(demoMem);
      if (!Number.isFinite(value) || value <= 0) {
        setDemoError("Memory MB must be a positive number.");
        return;
      }
    }

    setDemoLoading(true);
    try {
      const payload = { mode: demoMode };
      if (demoMode === "mem") {
        payload.mem_mb = Number(demoMem);
      }
      const data = await fetchJSON("/api/v1/demos", {
        method: "POST",
        body: JSON.stringify(payload),
      });
      setActiveDemos((prev) => [data.data, ...prev]);
      setDemoSuccess(`Started ${data.data.mode} demo (PID ${data.data.pid}).`);
    } catch (err) {
      setDemoError(err.message);
    } finally {
      setDemoLoading(false);
    }
  };

  const stopDemo = async (pid) => {
    setStopError("");
    setStopSuccess("");
    setStopLoading(true);
    try {
      const data = await fetchJSON(`/api/v1/demos/${pid}`, {
        method: "DELETE",
      });
      setActiveDemos((prev) => prev.filter((demo) => demo.pid !== pid));
      setStopSuccess(`Stopped ${data.data.mode} demo (PID ${data.data.pid}).`);
    } catch (err) {
      setStopError(err.message);
    } finally {
      setStopLoading(false);
    }
  };

  const handleStopByPid = async (event) => {
    event.preventDefault();
    const value = Number(stopPid);
    if (!Number.isFinite(value) || value <= 0) {
      setStopError("PID must be a positive integer.");
      return;
    }
    stopDemo(value);
  };

  const totalTargets = status?.targets?.length || 0;
  const totalProcesses = status?.targets?.reduce(
    (sum, target) => sum + (target.processes?.length || 0),
    0
  );

  return (
    <div className="app">
      <header className="hero">
        <div>
          <p className="eyebrow">Fault Isolation System</p>
          <h1>Operations Dashboard</h1>
          <p className="subhead">
            Realtime status, config edits, event stream, and demo controls.
          </p>
        </div>
        <div className="stream-pill" aria-live="polite">
          <span className={`stream-dot ${streamState}`}></span>
          <span className="stream-label">
            {streamState === "open"
              ? "Event stream connected"
              : streamState === "error"
              ? "Event stream error"
              : streamState === "closed"
              ? "Event stream closed"
              : "Event stream connecting"}
          </span>
        </div>
      </header>

      <motion.main
        className="grid"
        variants={layoutVariants}
        initial="hidden"
        animate="show"
      >
        <motion.section className="card" variants={cardVariants}>
          <div className="card-header">
            <div>
              <h2>Overview and Status</h2>
              <p className="muted">Latest snapshot from the runtime.</p>
            </div>
            <div className="status-meta">
              <span className="label">Last update</span>
              <span className="value">
                {status?.timestamp ? formatTimestamp(status.timestamp) : "n/a"}
              </span>
            </div>
          </div>

          {statusLoading && <div className="notice">Loading status...</div>}
          {statusError && <div className="notice error">{statusError}</div>}

          {!statusLoading && !statusError && !status && (
            <div className="empty-state">
              Status snapshot not available yet.
            </div>
          )}

          {status && (
            <div className="status-body">
              <div className="stat-grid">
                <div className="stat-card">
                  <span className="label">Targets</span>
                  <span className="stat-value">{totalTargets}</span>
                </div>
                <div className="stat-card">
                  <span className="label">Processes</span>
                  <span className="stat-value">{totalProcesses}</span>
                </div>
                <div className="stat-card">
                  <span className="label">Stream</span>
                  <span className="stat-value capitalize">{streamState}</span>
                </div>
              </div>

              <div className="targets">
                {status.targets?.map((target) => (
                  <div className="target-card" key={target.name}>
                    <div className="target-header">
                      <h3>{target.name}</h3>
                      <span className="badge">
                        {target.processes?.length || 0} processes
                      </span>
                    </div>
                    {target.processes?.length ? (
                      <ul className="process-list">
                        {target.processes.map((proc) => (
                          <li className="process-row" key={proc.pid}>
                            <div>
                              <div className="process-name">{proc.name}</div>
                              <div className="process-meta">
                                pid {proc.pid} {proc.cmdline ? "-" : ""} {proc.cmdline}
                              </div>
                            </div>
                            <div className="process-metrics">
                              <span>{formatPercent(proc.cpu_percent)} CPU</span>
                              <span>{formatBytes(proc.memory_bytes)}</span>
                            </div>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <div className="empty-state">No processes yet.</div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}
        </motion.section>

        <motion.section className="card" variants={cardVariants}>
          <div className="card-header">
            <div>
              <h2>Config Editor</h2>
              <p className="muted">Edit and apply live configuration.</p>
            </div>
            <div className="button-row">
              <button
                type="button"
                className="btn secondary"
                onClick={handleConfigReload}
                disabled={configLoading}
              >
                Reload
              </button>
            </div>
          </div>

          <form className="form" onSubmit={handleConfigSave}>
            <label className="label" htmlFor="config-text">
              Config JSON
            </label>
            <textarea
              id="config-text"
              className="textarea"
              value={configText}
              onChange={(event) => setConfigText(event.target.value)}
              spellCheck="false"
              rows={14}
            />

            {configError && <div className="notice error">{configError}</div>}
            {configSuccess && (
              <div className="notice success">{configSuccess}</div>
            )}

            <div className="button-row">
              <button className="btn primary" type="submit" disabled={configLoading}>
                {configLoading ? "Saving..." : "Save config"}
              </button>
            </div>
          </form>
        </motion.section>

        <motion.section className="card" variants={cardVariants}>
          <div className="card-header">
            <div>
              <h2>Events Stream</h2>
              <p className="muted">
                Streaming events from /api/v1/events/stream.
              </p>
            </div>
            <div className="status-meta">
              <span className="label">Connection</span>
              <span className={`badge badge-${streamState}`}>{streamState}</span>
            </div>
          </div>

          {eventsError && <div className="notice error">{eventsError}</div>}

          {!events.length && !eventsError && (
            <div className="empty-state">No events yet.</div>
          )}

          {!!events.length && (
            <ul className="event-list">
              {events.map((event) => {
                const time = formatTimestamp(event.timestamp);
                const kind = event.kind || "event";
                const isFault = kind === "fault";
                const fault = event.fault || {};
                const action = event.action || {};
                return (
                  <li
                    key={event.id}
                    className={`event-card ${isFault ? "event-fault" : "event-action"}`}
                  >
                    <div className="event-head">
                      <span className="event-kind">{kind}</span>
                      <span className="event-time">{time}</span>
                    </div>
                    {isFault ? (
                      <div className="event-body">
                        <div className="event-title">
                          {fault.message || "Fault detected"}
                        </div>
                        <div className="event-grid">
                          <div>
                            <span className="label">Target</span>
                            <span className="value">{fault.target || "n/a"}</span>
                          </div>
                          <div>
                            <span className="label">PID</span>
                            <span className="value">{fault.pid || "n/a"}</span>
                          </div>
                          <div>
                            <span className="label">Type</span>
                            <span className="value">{fault.type || "n/a"}</span>
                          </div>
                          <div>
                            <span className="label">CPU</span>
                            <span className="value">
                              {formatPercent(fault.cpu_percent)}
                            </span>
                          </div>
                          <div>
                            <span className="label">Memory</span>
                            <span className="value">
                              {formatBytes(fault.memory_bytes)}
                            </span>
                          </div>
                        </div>
                        <div className="event-footer">
                          <span className={severityClass(fault.severity)}>
                            {fault.severity || "unknown"}
                          </span>
                          {fault.correlation_id && (
                            <span className="muted">corr {fault.correlation_id}</span>
                          )}
                        </div>
                      </div>
                    ) : (
                      <div className="event-body">
                        <div className="event-title">
                          {action.action || "action"} {action.result ? `(${action.result})` : ""}
                        </div>
                        <div className="event-grid">
                          <div>
                            <span className="label">Target</span>
                            <span className="value">{action.target || "n/a"}</span>
                          </div>
                          <div>
                            <span className="label">PID</span>
                            <span className="value">{action.pid || "n/a"}</span>
                          </div>
                          <div>
                            <span className="label">Reason</span>
                            <span className="value">{action.reason || "n/a"}</span>
                          </div>
                          <div>
                            <span className="label">New PID</span>
                            <span className="value">{action.new_pid || "n/a"}</span>
                          </div>
                        </div>
                        {action.error && (
                          <div className="notice error">{action.error}</div>
                        )}
                      </div>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </motion.section>

        <motion.section className="card" variants={cardVariants}>
          <div className="card-header">
            <div>
              <h2>Demo Controls</h2>
              <p className="muted">
                Launch or stop demo processes for CPU, memory, or crash.
              </p>
            </div>
          </div>

          <form className="form" onSubmit={handleDemoStart}>
            <div className="form-row">
              <div className="field">
                <label className="label" htmlFor="demo-mode">
                  Mode
                </label>
                <select
                  id="demo-mode"
                  className="input"
                  value={demoMode}
                  onChange={(event) => setDemoMode(event.target.value)}
                >
                  <option value="cpu">cpu</option>
                  <option value="mem">mem</option>
                  <option value="crash">crash</option>
                </select>
              </div>
              <div className="field">
                <label className="label" htmlFor="demo-mem">
                  Mem MB
                </label>
                <input
                  id="demo-mem"
                  className="input"
                  type="number"
                  min="1"
                  step="1"
                  value={demoMem}
                  onChange={(event) => setDemoMem(event.target.value)}
                  disabled={demoMode !== "mem"}
                />
              </div>
            </div>

            {demoError && <div className="notice error">{demoError}</div>}
            {demoSuccess && <div className="notice success">{demoSuccess}</div>}

            <div className="button-row">
              <button className="btn primary" type="submit" disabled={demoLoading}>
                {demoLoading ? "Starting..." : "Start demo"}
              </button>
            </div>
          </form>

          <div className="divider"></div>

          <div className="subsection">
            <h3>Active demos</h3>
            {!activeDemos.length && (
              <div className="empty-state">No demos started from the UI.</div>
            )}
            {!!activeDemos.length && (
              <ul className="demo-list">
                {activeDemos.map((demo) => (
                  <li className="demo-row" key={demo.pid}>
                    <span className="demo-label">
                      PID {demo.pid} ({demo.mode})
                    </span>
                    <button
                      className="btn tertiary"
                      type="button"
                      onClick={() => stopDemo(demo.pid)}
                      disabled={stopLoading}
                    >
                      Stop
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>

          <form className="form inline" onSubmit={handleStopByPid}>
            <div className="form-row">
              <div className="field">
                <label className="label" htmlFor="stop-pid">
                  Stop by PID
                </label>
                <input
                  id="stop-pid"
                  className="input"
                  type="number"
                  min="1"
                  step="1"
                  value={stopPid}
                  onChange={(event) => setStopPid(event.target.value)}
                />
              </div>
              <div className="field button-field">
                <button className="btn secondary" type="submit" disabled={stopLoading}>
                  {stopLoading ? "Stopping..." : "Stop demo"}
                </button>
              </div>
            </div>
            {stopError && <div className="notice error">{stopError}</div>}
            {stopSuccess && <div className="notice success">{stopSuccess}</div>}
          </form>
        </motion.section>
      </motion.main>
    </div>
  );
}

export default App;
