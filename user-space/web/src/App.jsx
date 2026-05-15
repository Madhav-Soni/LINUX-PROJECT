import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { motion, AnimatePresence, useReducedMotion } from "framer-motion";

const API_BASE = import.meta.env.VITE_API_BASE || "http://localhost:8090";

const formatBytes = (b) => {
  if (!b && b !== 0) return "n/a";
  if (b === 0) return "0 B";
  const u = ["B", "KB", "MB", "GB"];
  let v = b, i = 0;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(1)} ${u[i]}`;
};
const formatPct = (v) => (!v && v !== 0) ? "n/a" : `${v.toFixed(1)}%`;
const formatTs = (v) => {
  if (!v) return "n/a";
  const d = new Date(v);
  return isNaN(d) ? "n/a" : d.toLocaleTimeString();
};
const formatDurNs = (ns) => {
  if (!ns) return null;
  if (ns < 1e6) return `${(ns/1e3).toFixed(0)} µs`;
  if (ns < 1e9) return `${(ns/1e6).toFixed(0)} ms`;
  return `${(ns/1e9).toFixed(1)} s`;
};

const LIFECYCLE_TYPES = new Set(["zombie","orphan","signal_death","parent_exit"]);
const FAULT_META = {
  zombie:        { icon: "☠",  label: "Zombie",        color: "#ff6b6b" },
  orphan:        { icon: "👻", label: "Orphan",        color: "#ffa94d" },
  signal_death:  { icon: "⚡", label: "Signal Death",  color: "#ff4d4d" },
  parent_exit:   { icon: "🔗", label: "Parent Exit",   color: "#f06595" },
  crash:         { icon: "💥", label: "Crash",         color: "#cc5de8" },
  cpu_spike:     { icon: "🔥", label: "CPU Spike",     color: "#f59f00" },
  memory_overuse:{ icon: "📈", label: "Memory",        color: "#74c0fc" },
};
const SEV_COLOR = { critical:"#ff4d4d", warn:"#ffa94d", info:"#74c0fc" };

async function fetchJSON(path, opts={}) {
  const res = await fetch(`${API_BASE}${path}`, {
    ...opts, headers:{"Content-Type":"application/json",...opts.headers}
  });
  const ct = res.headers.get("content-type")||"";
  const payload = ct.includes("application/json") ? await res.json() : null;
  if (!res.ok) throw new Error(payload?.error?.message||`HTTP ${res.status}`);
  return payload;
}

const TABS = [
  {id:"dashboard",label:"Dashboard"},
  {id:"procwatch",label:"Proc Monitor"},
  {id:"events",   label:"Events"},
  {id:"demos",    label:"Demos"},
  {id:"config",   label:"Config"},
];

export default function App() {
  const rm = useReducedMotion();
  const [tab,setTab] = useState("dashboard");
  const [streamState,setStreamState] = useState("connecting");
  const [allEvents,setAllEvents] = useState([]);
  const [notifications,setNotifications] = useState([]);
  const [status,setStatus] = useState(null);
  const [processes,setProcesses] = useState([]);
  const [pwEvents,setPwEvents] = useState([]);
  const [config, setConfig] = useState("");
  const [configLoading,setConfigLoading] = useState(false);
  const [configError,setConfigError] = useState("");
  const [configSuccess,setConfigSuccess] = useState("");
  const [demoMode,setDemoMode] = useState("zombie");
  const [demoLoading,setDemoLoading] = useState(false);
  const [demoError,setDemoError] = useState("");
  const [demoSuccess,setDemoSuccess] = useState("");
  const [activeDemos,setActiveDemos] = useState([]);

  // SSE
  useEffect(()=>{
    let active=true;
    const src = new EventSource(`${API_BASE}/api/v1/events/stream`);
    setStreamState("connecting");
    const handleEvent=(e)=>{
      try{
        const ev=JSON.parse(e.data);
        setAllEvents(p=>[ev,...p].slice(0,300));
        if(ev.kind==="fault"&&ev.fault&&LIFECYCLE_TYPES.has(ev.fault.type)){
          const n={id:ev.id,ts:Date.now(),fault:ev.fault};
          setNotifications(p=>[n,...p].slice(0,8));
          setTimeout(()=>setNotifications(p=>p.filter(x=>x.id!==n.id)),8000);
        }
      }catch{}
    };
    src.onopen=()=>{ if(active) setStreamState("open"); };
    src.onerror=()=>{ if(active) setStreamState("error"); };
    src.addEventListener("fault",handleEvent);
    src.addEventListener("action",handleEvent);
    src.addEventListener("procwatch",handleEvent);
    src.onmessage=handleEvent;
    return()=>{ active=false; src.close(); setStreamState("closed"); };
  },[]);

  // Poll status
  useEffect(()=>{
    let active=true;
    const load=async()=>{
      try{ const d=await fetchJSON("/api/v1/status"); if(active) setStatus(d.data); }catch{}
    };
    load();
    const t=setInterval(load,5000);
    return()=>{ active=false; clearInterval(t); };
  },[]);

  // Poll procwatch processes
  useEffect(()=>{
    let active=true;
    const load=async()=>{
      try{ const d=await fetchJSON("/api/v1/procwatch/processes"); if(active) setProcesses(d.data||[]); }catch{}
    };
    load();
    const t=setInterval(load,3000);
    return()=>{ active=false; clearInterval(t); };
  },[]);

  // Load procwatch events (initial)
  useEffect(()=>{
    fetchJSON("/api/v1/procwatch/events?limit=100")
      .then(d=>setPwEvents(d.data||[])).catch(()=>{});
  },[]);

  // Sync procwatch events from SSE
  useEffect(()=>{
    const lc=allEvents.filter(e=>e.kind==="fault"&&e.fault&&LIFECYCLE_TYPES.has(e.fault.type));
    if(!lc.length) return;
    setPwEvents(prev=>{
      const ids=new Set(prev.map(e=>e.id));
      const nw=lc.filter(e=>!ids.has(e.id));
      return [...nw,...prev].slice(0,200);
    });
  },[allEvents]);

  // Config
  useEffect(()=>{
    fetchJSON("/api/v1/config").then(d=>setConfig(JSON.stringify(d.data,null,2))).catch(()=>{});
  },[]);

  const saveConfig=async()=>{
    setConfigError(""); setConfigSuccess("");
    let payload;
    try{ payload=JSON.parse(config); }catch{ setConfigError("Must be valid JSON."); return; }
    setConfigLoading(true);
    try{
      const d=await fetchJSON("/api/v1/config",{method:"PUT",body:JSON.stringify(payload)});
      setConfig(JSON.stringify(d.data,null,2)); setConfigSuccess("Saved.");
    }catch(e){ setConfigError(e.message); }
    finally{ setConfigLoading(false); }
  };

  const startDemo=async()=>{
    setDemoError(""); setDemoSuccess(""); setDemoLoading(true);
    try{
      const d=await fetchJSON("/api/v1/demos",{method:"POST",body:JSON.stringify({mode:demoMode})});
      setActiveDemos(p=>[d.data,...p]);
      setDemoSuccess(`Started ${d.data.mode} demo (PID ${d.data.pid})`);
    }catch(e){ setDemoError(e.message); }
    finally{ setDemoLoading(false); }
  };

  const stopDemo=async(pid)=>{
    try{
      const d=await fetchJSON(`/api/v1/demos/${pid}`,{method:"DELETE"});
      setActiveDemos(p=>p.filter(x=>x.pid!==pid));
      setDemoSuccess(`Stopped PID ${d.data.pid}`);
    }catch(e){ setDemoError(e.message); }
  };

  const lifecycleEvents=useMemo(()=>
    allEvents.filter(e=>e.kind==="fault"&&e.fault&&LIFECYCLE_TYPES.has(e.fault.type)),
    [allEvents]);

  const zombies=processes.filter(p=>p.zombie_since&&p.alive);
  const alive=processes.filter(p=>p.alive&&!p.zombie_since);

  const fc={
    hidden:{opacity:0,y:rm?0:14},
    show:{opacity:1,y:0,transition:{duration:0.35,ease:"easeOut"}}
  };

  return (
    <div style={S.root}>
      {/* Toasts */}
      <div style={S.toastContainer}>
        <AnimatePresence>
          {notifications.map(n=>(
            <motion.div key={n.id}
              initial={{opacity:0,x:60}} animate={{opacity:1,x:0}} exit={{opacity:0,x:60}}
              transition={{duration:0.3}}
              style={{...S.toast,borderColor:SEV_COLOR[n.fault.severity]||"#555"}}
            >
              <span style={{fontSize:22}}>{FAULT_META[n.fault.type]?.icon||"⚠"}</span>
              <div style={{flex:1}}>
                <div style={S.toastTitle}>{FAULT_META[n.fault.type]?.label||n.fault.type}</div>
                <div style={S.toastMsg}>{n.fault.message}</div>
                <div style={S.toastMeta}>
                  PID {n.fault.pid}
                  {n.fault.parent_pid?` · parent ${n.fault.parent_pid}`:""}
                  {n.fault.signal?` · sig ${n.fault.signal}`:""}
                </div>
              </div>
              <button onClick={()=>setNotifications(p=>p.filter(x=>x.id!==n.id))} style={S.toastClose}>×</button>
            </motion.div>
          ))}
        </AnimatePresence>
      </div>

      {/* Header */}
      <header style={S.header}>
        <div>
          <div style={S.eyebrow}>FAULT ISOLATION SYSTEM</div>
          <div style={S.title}>Process Monitor</div>
        </div>
        <div style={S.streamPill}>
          <span style={{...S.streamDot,background:streamState==="open"?"#51cf66":streamState==="error"?"#ff4d4d":"#aaa"}}/>
          {streamState==="open"?"Live":streamState==="error"?"Error":streamState==="closed"?"Closed":"Connecting"}
        </div>
      </header>

      {/* Tabs */}
      <nav style={S.tabBar}>
        {TABS.map(t=>(
          <button key={t.id} onClick={()=>setTab(t.id)}
            style={{...S.tabBtn,...(tab===t.id?S.tabActive:{})}}
          >
            {t.label}
            {t.id==="procwatch"&&lifecycleEvents.length>0&&(
              <span style={S.badge}>{lifecycleEvents.length}</span>
            )}
          </button>
        ))}
      </nav>

      <main style={S.main}>
        {/* DASHBOARD */}
        {tab==="dashboard"&&(
          <motion.div variants={{show:{transition:{staggerChildren:0.06}}}} initial="hidden" animate="show" style={S.grid}>
            <motion.div variants={fc} style={S.card}>
              <div style={S.cardTitle}>Overview</div>
              <div style={S.statRow}>
                <Stat label="Targets" value={status?.targets?.length??"—"}/>
                <Stat label="Processes" value={status?.targets?.reduce((s,t)=>s+(t.processes?.length||0),0)??"—"}/>
                <Stat label="Zombies" value={zombies.length} accent={zombies.length>0?"#ff6b6b":null}/>
                <Stat label="Tracked" value={processes.length}/>
              </div>
              {status?.targets?.map(t=>(
                <div key={t.name} style={S.targetCard}>
                  <div style={S.targetName}>{t.name}</div>
                  {t.processes?.map(p=>(
                    <div key={p.pid} style={S.procRow}>
                      <span style={S.procName}>{p.name}</span>
                      <span style={S.procMeta}>PID {p.pid}</span>
                      <span style={S.procMeta}>{formatPct(p.cpu_percent)} CPU</span>
                      <span style={S.procMeta}>{formatBytes(p.memory_bytes)}</span>
                    </div>
                  ))}
                </div>
              ))}
              {!status&&<div style={S.empty}>No status yet — waiting for first poll…</div>}
            </motion.div>
            <motion.div variants={fc} style={S.card}>
              <div style={S.cardTitle}>Recent Lifecycle Faults</div>
              {lifecycleEvents.length===0&&<div style={S.empty}>No lifecycle faults detected yet.</div>}
              {lifecycleEvents.slice(0,6).map(ev=><FaultCard key={ev.id} ev={ev}/>)}
            </motion.div>
          </motion.div>
        )}

        {/* PROC MONITOR */}
        {tab==="procwatch"&&(
          <motion.div variants={{show:{transition:{staggerChildren:0.06}}}} initial="hidden" animate="show" style={S.grid}>
            <motion.div variants={fc} style={{...S.card,gridColumn:"1/-1"}}>
              <div style={S.cardTitle}>Lifecycle Fault Summary</div>
              <div style={S.statRow}>
                {Object.entries(
                  pwEvents.reduce((acc,ev)=>{
                    const t=ev.fault?.type||ev.kind;
                    acc[t]=(acc[t]||0)+1; return acc;
                  },{})
                ).map(([type,count])=>(
                  <Stat key={type} label={FAULT_META[type]?.label||type} value={count}
                    icon={FAULT_META[type]?.icon} accent={FAULT_META[type]?.color}/>
                ))}
                {pwEvents.length===0&&<span style={S.empty}>No lifecycle events yet.</span>}
              </div>
            </motion.div>

            <motion.div variants={fc} style={S.card}>
              <div style={S.cardTitle}><span style={{color:"#ff6b6b"}}>☠</span> Zombie Processes</div>
              {zombies.length===0&&<div style={S.empty}>No zombies currently tracked.</div>}
              {zombies.map(p=>(
                <div key={p.pid} style={S.procRowFull}>
                  <div>
                    <span style={{...S.procName,color:"#ff6b6b"}}>{p.name}</span>
                    <span style={S.procMeta}> PID {p.pid} · parent {p.ppid}</span>
                  </div>
                  <div style={S.procMeta}>zombie since {formatTs(p.zombie_since)}</div>
                </div>
              ))}
              <div style={{marginTop:20}}>
                <div style={S.cardTitle}><span style={{color:"#74c0fc"}}>●</span> Live Tracked Processes</div>
                {alive.length===0&&<div style={S.empty}>No tracked processes.</div>}
                <div style={S.procTable}>
                  {alive.slice(0,30).map(p=>(
                    <div key={p.pid} style={S.procTableRow}>
                      <span style={S.procName}>{p.name}</span>
                      <span style={S.procMeta}>PID {p.pid}</span>
                      <span style={S.procMeta}>PPID {p.ppid}</span>
                      <span style={{...S.procMeta,color:"#555"}}>seen {formatTs(p.first_seen)}</span>
                    </div>
                  ))}
                  {alive.length>30&&<div style={S.procMeta}>…and {alive.length-30} more</div>}
                </div>
              </div>
            </motion.div>

            <motion.div variants={fc} style={S.card}>
              <div style={S.cardTitle}>Lifecycle Event Feed</div>
              {pwEvents.length===0&&(
                <div style={S.empty}>No lifecycle events yet.<br/>Launch a demo to trigger events.</div>
              )}
              <div style={S.eventFeed}>
                <AnimatePresence initial={false}>
                  {pwEvents.slice(0,50).map(ev=>(
                    <motion.div key={ev.id}
                      initial={{opacity:0,height:0}} animate={{opacity:1,height:"auto"}}
                      exit={{opacity:0,height:0}} transition={{duration:0.25}}
                    ><FaultCard ev={ev}/></motion.div>
                  ))}
                </AnimatePresence>
              </div>
            </motion.div>
          </motion.div>
        )}

        {/* EVENTS */}
        {tab==="events"&&(
          <motion.div variants={fc} initial="hidden" animate="show" style={S.card}>
            <div style={S.cardTitle}>All Events</div>
            {allEvents.length===0&&<div style={S.empty}>No events yet.</div>}
            <div style={S.eventFeed}>
              {allEvents.slice(0,100).map(ev=>
                ev.kind==="fault"
                  ?<FaultCard key={ev.id} ev={ev}/>
                  :<ActionCard key={ev.id} ev={ev}/>
              )}
            </div>
          </motion.div>
        )}

        {/* DEMOS */}
        {tab==="demos"&&(
          <motion.div variants={fc} initial="hidden" animate="show" style={S.card}>
            <div style={S.cardTitle}>Demo Controls</div>
            <p style={S.muted}>Launch demo processes to trigger lifecycle faults visible in Proc Monitor.</p>
            <div style={S.formRow}>
              <label style={S.label}>Mode</label>
              <select value={demoMode} onChange={e=>setDemoMode(e.target.value)} style={S.select}>
                {["cpu","mem","crash","zombie","orphan","parent-nowait","sigkill","sigterm","sigsegv","sigabrt"].map(m=>(
                  <option key={m} value={m}>{m}</option>
                ))}
              </select>
              <button onClick={startDemo} disabled={demoLoading} style={S.btnPrimary}>
                {demoLoading?"Starting…":"Start Demo"}
              </button>
            </div>
            {demoError&&<div style={S.notice}>{demoError}</div>}
            {demoSuccess&&<div style={{...S.notice,background:"#0a2a0a",color:"#51cf66"}}>{demoSuccess}</div>}
            <div style={{marginTop:20}}>
              <div style={S.label}>Active Demos</div>
              {activeDemos.length===0&&<div style={S.empty}>None started from UI.</div>}
              {activeDemos.map(d=>(
                <div key={d.pid} style={S.procRowFull}>
                  <span>PID <strong>{d.pid}</strong> — {d.mode}</span>
                  <button onClick={()=>stopDemo(d.pid)} style={S.btnDanger}>Stop</button>
                </div>
              ))}
            </div>
            <div style={{marginTop:24,padding:16,background:"#0d0d18",borderRadius:8,border:"1px solid #1e1e2e"}}>
              <div style={{...S.label,marginBottom:12}}>CLI Demo Commands</div>
              {[
                ["Zombie","./procwatch-demo -mode zombie"],
                ["Orphan","./procwatch-demo -mode orphan"],
                ["Parent no-wait","./procwatch-demo -mode parent-nowait"],
                ["SIGKILL child","./procwatch-demo -mode sigkill"],
                ["SIGSEGV","./procwatch-demo -mode sigsegv"],
                ["SIGABRT","./procwatch-demo -mode sigabrt"],
              ].map(([label,cmd])=>(
                <div key={label} style={{marginBottom:10}}>
                  <div style={{...S.muted,marginBottom:3}}>{label}</div>
                  <code style={S.codeBlock}>{cmd}</code>
                </div>
              ))}
            </div>
          </motion.div>
        )}

        {/* CONFIG */}
        {tab==="config"&&(
          <motion.div variants={fc} initial="hidden" animate="show" style={S.card}>
            <div style={S.cardTitle}>Config Editor</div>
            <textarea value={config} onChange={e=>setConfig(e.target.value)}
              style={S.textarea} rows={22} spellCheck={false}/>
            {configError&&<div style={S.notice}>{configError}</div>}
            {configSuccess&&<div style={{...S.notice,background:"#0a2a0a",color:"#51cf66"}}>{configSuccess}</div>}
            <div style={{display:"flex",gap:12,marginTop:12}}>
              <button onClick={saveConfig} disabled={configLoading} style={S.btnPrimary}>
                {configLoading?"Saving…":"Save Config"}
              </button>
              <button onClick={()=>fetchJSON("/api/v1/config").then(d=>setConfig(JSON.stringify(d.data,null,2))).catch(()=>{})} style={S.btnSecondary}>
                Reload
              </button>
            </div>
          </motion.div>
        )}
      </main>
    </div>
  );
}

function Stat({label,value,icon,accent}){
  return(
    <div style={{textAlign:"center",minWidth:80}}>
      {icon&&<div style={{fontSize:20}}>{icon}</div>}
      <div style={{fontSize:26,fontWeight:700,color:accent||"#e0e0e0",fontFamily:"monospace"}}>{value}</div>
      <div style={{fontSize:10,color:"#666",textTransform:"uppercase",letterSpacing:1}}>{label}</div>
    </div>
  );
}

function FaultCard({ev}){
  const fault=ev.fault||ev;
  const meta=FAULT_META[fault.type]||{icon:"⚠",label:fault.type,color:"#888"};
  const sev=fault.severity||"info";
  return(
    <div style={{...S.eventCard,borderLeft:`3px solid ${SEV_COLOR[sev]||"#555"}`}}>
      <div style={S.eventHead}>
        <span style={{fontSize:16}}>{meta.icon}</span>
        <span style={{color:meta.color,fontWeight:600}}>{meta.label}</span>
        <span style={{...S.sevBadge,background:(SEV_COLOR[sev]||"#555")+"33",color:SEV_COLOR[sev]||"#888"}}>{sev}</span>
        <span style={{marginLeft:"auto",...S.muted}}>{formatTs(ev.timestamp||fault.timestamp)}</span>
      </div>
      <div style={{fontSize:12,color:"#ccc",margin:"4px 0"}}>{fault.message}</div>
      <div style={S.eventMeta}>
        {fault.target&&<span>target: <b>{fault.target}</b></span>}
        {fault.pid&&<span>PID: <b>{fault.pid}</b></span>}
        {fault.parent_pid&&<span>PPID: <b>{fault.parent_pid}</b></span>}
        {fault.signal&&<span>signal: <b>{fault.signal}</b></span>}
        {fault.zombie_duration_ns&&<span>duration: <b>{formatDurNs(fault.zombie_duration_ns)}</b></span>}
      </div>
    </div>
  );
}

function ActionCard({ev}){
  const a=ev.action||{};
  return(
    <div style={{...S.eventCard,borderLeft:"3px solid #51cf66"}}>
      <div style={S.eventHead}>
        <span>⚙</span>
        <span style={{color:"#51cf66",fontWeight:600}}>action: {a.action}</span>
        <span style={{...S.sevBadge,background:"#0a2a0a",color:"#51cf66"}}>{a.result}</span>
        <span style={{marginLeft:"auto",...S.muted}}>{formatTs(ev.timestamp)}</span>
      </div>
      <div style={S.eventMeta}>
        {a.target&&<span>target: <b>{a.target}</b></span>}
        {a.pid&&<span>PID: <b>{a.pid}</b></span>}
        {a.reason&&<span>reason: {a.reason}</span>}
        {a.new_pid&&<span>new PID: <b>{a.new_pid}</b></span>}
      </div>
    </div>
  );
}

const S = {
  root:{minHeight:"100vh",background:"#0d0d14",color:"#e0e0e0",fontFamily:"'JetBrains Mono','Fira Code','Cascadia Code',monospace",fontSize:13},
  header:{display:"flex",alignItems:"center",justifyContent:"space-between",padding:"20px 28px 16px",borderBottom:"1px solid #1e1e2e"},
  eyebrow:{fontSize:10,letterSpacing:3,color:"#555",textTransform:"uppercase",marginBottom:4},
  title:{fontSize:22,fontWeight:700,color:"#fff"},
  streamPill:{display:"flex",alignItems:"center",gap:8,background:"#1a1a2e",padding:"6px 14px",borderRadius:20,fontSize:12,color:"#aaa"},
  streamDot:{width:8,height:8,borderRadius:"50%",display:"inline-block"},
  tabBar:{display:"flex",padding:"0 28px",borderBottom:"1px solid #1e1e2e"},
  tabBtn:{background:"none",border:"none",color:"#777",padding:"10px 18px",cursor:"pointer",fontSize:13,fontFamily:"inherit",borderBottom:"2px solid transparent",display:"flex",alignItems:"center",gap:6},
  tabActive:{color:"#e0e0e0",borderBottom:"2px solid #7c6af7"},
  badge:{background:"#ff4d4d",color:"#fff",borderRadius:10,padding:"1px 6px",fontSize:10,fontWeight:700},
  main:{padding:"24px 28px",maxWidth:1400,margin:"0 auto"},
  grid:{display:"grid",gridTemplateColumns:"repeat(auto-fit,minmax(460px,1fr))",gap:20},
  card:{background:"#111118",border:"1px solid #1e1e2e",borderRadius:10,padding:20},
  cardTitle:{fontSize:13,fontWeight:700,color:"#bbb",marginBottom:14,letterSpacing:0.5,display:"flex",alignItems:"center",gap:8},
  statRow:{display:"flex",gap:24,flexWrap:"wrap",marginBottom:16},
  targetCard:{background:"#0d0d18",border:"1px solid #1e1e2e",borderRadius:6,padding:"10px 12px",marginBottom:8},
  targetName:{fontWeight:700,color:"#7c6af7",marginBottom:6,fontSize:13},
  procRow:{display:"flex",gap:12,alignItems:"center",padding:"4px 0",borderTop:"1px solid #1a1a28"},
  procRowFull:{display:"flex",justifyContent:"space-between",alignItems:"center",padding:"8px 0",borderTop:"1px solid #1a1a28"},
  procName:{color:"#e0e0e0",fontWeight:600},
  procMeta:{color:"#666",fontSize:11},
  procTable:{display:"flex",flexDirection:"column",gap:2},
  procTableRow:{display:"flex",gap:16,padding:"5px 0",borderTop:"1px solid #1a1a24",flexWrap:"wrap"},
  eventFeed:{display:"flex",flexDirection:"column",gap:6,maxHeight:540,overflowY:"auto"},
  eventCard:{background:"#0d0d18",borderRadius:6,padding:"10px 12px",marginBottom:2},
  eventHead:{display:"flex",alignItems:"center",gap:8,marginBottom:4},
  eventMeta:{display:"flex",gap:14,flexWrap:"wrap",fontSize:11,color:"#666"},
  sevBadge:{borderRadius:4,padding:"1px 6px",fontSize:10,fontWeight:700,textTransform:"uppercase"},
  empty:{color:"#555",fontStyle:"italic",padding:"8px 0",fontSize:12},
  muted:{color:"#666",fontSize:11},
  formRow:{display:"flex",gap:12,alignItems:"center",flexWrap:"wrap",marginBottom:12},
  label:{color:"#888",fontSize:10,letterSpacing:1,textTransform:"uppercase",marginBottom:4},
  select:{background:"#1a1a2e",border:"1px solid #2a2a3e",color:"#e0e0e0",padding:"7px 10px",borderRadius:6,fontFamily:"inherit",fontSize:13},
  textarea:{width:"100%",background:"#0a0a12",border:"1px solid #1e1e2e",color:"#e0e0e0",fontFamily:"inherit",fontSize:11,borderRadius:6,padding:12,resize:"vertical",boxSizing:"border-box"},
  notice:{background:"#2a1010",color:"#ff6b6b",borderRadius:6,padding:"8px 12px",marginTop:8,fontSize:12},
  btnPrimary:{background:"#7c6af7",color:"#fff",border:"none",padding:"8px 18px",borderRadius:6,cursor:"pointer",fontFamily:"inherit",fontSize:13,fontWeight:600},
  btnSecondary:{background:"#1a1a2e",color:"#bbb",border:"1px solid #2a2a3e",padding:"8px 18px",borderRadius:6,cursor:"pointer",fontFamily:"inherit",fontSize:13},
  btnDanger:{background:"#3a1010",color:"#ff6b6b",border:"1px solid #ff4d4d44",padding:"5px 12px",borderRadius:6,cursor:"pointer",fontFamily:"inherit",fontSize:12},
  codeBlock:{display:"block",background:"#0a0a12",color:"#74c0fc",padding:"7px 10px",borderRadius:4,fontSize:12},
  toastContainer:{position:"fixed",top:20,right:20,zIndex:9999,display:"flex",flexDirection:"column",gap:10,maxWidth:360},
  toast:{background:"#111118",border:"1px solid",borderRadius:8,padding:"12px 14px",display:"flex",gap:12,alignItems:"flex-start",boxShadow:"0 4px 24px #00000088"},
  toastTitle:{fontWeight:700,fontSize:13,marginBottom:2},
  toastMsg:{fontSize:11,color:"#aaa",lineHeight:1.4},
  toastMeta:{fontSize:10,color:"#666",marginTop:2},
  toastClose:{background:"none",border:"none",color:"#666",cursor:"pointer",fontSize:18,padding:0,lineHeight:1},
};
