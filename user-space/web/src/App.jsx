import { useEffect, useMemo, useRef, useState } from "react";
import { motion, AnimatePresence, useReducedMotion } from "framer-motion";

const API_BASE = import.meta.env.VITE_API_BASE || "http://localhost:8090";

// ── utils ────────────────────────────────────────────────────────────────────

const fmtBytes = (b) => {
  if (!b && b !== 0) return "n/a";
  if (b === 0) return "0 B";
  const u = ["B","KB","MB","GB"]; let v=b,i=0;
  while(v>=1024&&i<u.length-1){v/=1024;i++;}
  return `${v.toFixed(1)} ${u[i]}`;
};
const fmtPct = (v) => (!v&&v!==0)?"n/a":`${v.toFixed(1)}%`;
const fmtTs  = (v) => { if(!v)return"n/a"; const d=new Date(v); return isNaN(d)?"n/a":d.toLocaleTimeString(); };
const fmtNs  = (ns) => {
  if(!ns)return null;
  if(ns<1e6)return`${(ns/1e3).toFixed(0)} µs`;
  if(ns<1e9)return`${(ns/1e6).toFixed(0)} ms`;
  return`${(ns/1e9).toFixed(1)} s`;
};

const LIFECYCLE = new Set(["zombie","orphan","signal_death","parent_exit"]);

const FAULT_META = {
  zombie:        {icon:"☠",  label:"Zombie",       color:"#ff6b6b"},
  orphan:        {icon:"👻", label:"Orphan",       color:"#ffa94d"},
  signal_death:  {icon:"⚡", label:"Signal Death", color:"#ff4d4d"},
  parent_exit:   {icon:"🔗", label:"Parent Exit",  color:"#f06595"},
  crash:         {icon:"💥", label:"Crash",        color:"#cc5de8"},
  cpu_spike:     {icon:"🔥", label:"CPU Spike",    color:"#f59f00"},
  memory_overuse:{icon:"📈", label:"Memory",       color:"#74c0fc"},
};

const SEV = {critical:"#ff4d4d", warn:"#ffa94d", info:"#74c0fc"};

const ALL_MODES = [
  {value:"zombie",       label:"zombie       – parent never calls wait()"},
  {value:"orphan",       label:"orphan       – parent exits, child re-parents to init"},
  {value:"parent-nowait",label:"parent-nowait – brief zombie window"},
  {value:"sigkill",      label:"sigkill      – parent sends SIGKILL to child"},
  {value:"sigterm",      label:"sigterm      – parent sends SIGTERM to child"},
  {value:"sigsegv",      label:"sigsegv      – nil-pointer dereference"},
  {value:"sigabrt",      label:"sigabrt      – SIGABRT self"},
  {value:"cpu",          label:"cpu          – busy-loop CPU spike"},
  {value:"mem",          label:"mem          – allocate memory"},
  {value:"crash",        label:"crash        – clean exit(2)"},
];

async function api(path, opts={}) {
  const r = await fetch(`${API_BASE}${path}`, {
    ...opts,
    headers:{"Content-Type":"application/json",...opts.headers},
  });
  const ct = r.headers.get("content-type")||"";
  const data = ct.includes("application/json") ? await r.json() : null;
  if(!r.ok) throw new Error(data?.error?.message||`HTTP ${r.status}`);
  return data;
}

const TABS = [
  {id:"dashboard", label:"Dashboard"},
  {id:"procwatch", label:"Proc Monitor"},
  {id:"events",    label:"Events"},
  {id:"demos",     label:"Demos"},
  {id:"config",    label:"Config"},
];

export default function App() {
  const rm = useReducedMotion();
  const [tab,setTab]             = useState("dashboard");
  const [stream,setStream]       = useState("connecting");

  // global event log
  const [allEvents,setAllEvents] = useState([]);
  const [toasts,setToasts]       = useState([]);

  // per-section data
  const [status,setStatus]       = useState(null);
  const [procs,setProcs]         = useState([]);
  const [pwEvents,setPwEvents]   = useState([]);

  // demos
  const [demoMode,setDemoMode]   = useState("zombie");
  const [demoLoading,setDemoLoading] = useState(false);
  const [demoMsg,setDemoMsg]     = useState(null); // {ok, text}
  const [activeDemo,setActiveDemo] = useState([]); // [{pid,mode}]

  // config
  const [cfg,setCfg]             = useState("");
  const [cfgLoading,setCfgLoading] = useState(false);
  const [cfgMsg,setCfgMsg]       = useState(null);

  // proc-monitor refresh trigger
  const procRefTimer = useRef(null);
  const refreshProcs = () => {
    api("/api/v1/procwatch/processes")
      .then(d=>setProcs(d.data||[])).catch(()=>{});
  };

  // ── SSE ───────────────────────────────────────────────────────────────────
  useEffect(()=>{
    let alive=true;
    const es = new EventSource(`${API_BASE}/api/v1/events/stream`);
    setStream("connecting");
    const onEvent = (e) => {
      try{
        const ev = JSON.parse(e.data);
        setAllEvents(p=>[ev,...p].slice(0,400));
        if(ev.kind==="fault"&&ev.fault&&LIFECYCLE.has(ev.fault.type)){
          const t={id:ev.id,ts:Date.now(),fault:ev.fault};
          setToasts(p=>[t,...p].slice(0,6));
          setTimeout(()=>setToasts(p=>p.filter(x=>x.id!==t.id)),7000);
          // Refresh the process table so zombie/orphan counts update immediately.
          refreshProcs();
        }
      }catch{}
    };
    es.onopen = ()=>{ if(alive) setStream("open"); };
    es.onerror= ()=>{ if(alive) setStream("error"); };
    ["fault","action","procwatch"].forEach(k=>es.addEventListener(k,onEvent));
    es.onmessage = onEvent;
    return()=>{ alive=false; es.close(); setStream("closed"); };
  },[]);

  // ── Poll status ──────────────────────────────────────────────────────────
  useEffect(()=>{
    let alive=true;
    const go=()=>api("/api/v1/status").then(d=>{ if(alive)setStatus(d.data); }).catch(()=>{});
    go(); const t=setInterval(go,5000); return()=>{ alive=false; clearInterval(t); };
  },[]);

  // ── Poll procwatch processes (every 3 s) ─────────────────────────────────
  useEffect(()=>{
    let alive=true;
    const go=()=>api("/api/v1/procwatch/processes").then(d=>{ if(alive)setProcs(d.data||[]); }).catch(()=>{});
    go(); const t=setInterval(go,3000); return()=>{ alive=false; clearInterval(t); };
  },[]);

  // ── Load initial procwatch events ────────────────────────────────────────
  useEffect(()=>{
    api("/api/v1/procwatch/events?limit=100").then(d=>setPwEvents(d.data||[])).catch(()=>{});
  },[]);

  // ── Sync lifecycle events from SSE into pwEvents feed ───────────────────
  useEffect(()=>{
    const lc = allEvents.filter(e=>e.kind==="fault"&&e.fault&&LIFECYCLE.has(e.fault.type));
    if(!lc.length) return;
    setPwEvents(prev=>{
      const seen=new Set(prev.map(e=>e.id));
      return [...lc.filter(e=>!seen.has(e.id)),...prev].slice(0,300);
    });
  },[allEvents]);

  // ── Config ───────────────────────────────────────────────────────────────
  useEffect(()=>{
    api("/api/v1/config").then(d=>setCfg(JSON.stringify(d.data,null,2))).catch(()=>{});
  },[]);

  const saveConfig = async()=>{
    setCfgMsg(null); setCfgLoading(true);
    let p; try{ p=JSON.parse(cfg); }catch{ setCfgMsg({ok:false,text:"Must be valid JSON."}); setCfgLoading(false); return; }
    try{
      const d=await api("/api/v1/config",{method:"PUT",body:JSON.stringify(p)});
      setCfg(JSON.stringify(d.data,null,2)); setCfgMsg({ok:true,text:"Saved."});
    }catch(e){ setCfgMsg({ok:false,text:e.message}); }
    finally{ setCfgLoading(false); }
  };

  // ── Start demo ───────────────────────────────────────────────────────────
  const startDemo = async()=>{
    setDemoMsg(null); setDemoLoading(true);
    try{
      const d = await api("/api/v1/demos",{method:"POST",body:JSON.stringify({mode:demoMode})});
      // Response shape: {success:true, data:{pid,mode}}
      const info = d.data || d;
      setActiveDemo(p=>[...p,{pid:info.pid,mode:info.mode||demoMode}]);
      setDemoMsg({ok:true,text:`✓ Started ${info.mode||demoMode} demo — PID ${info.pid}`});
      // Switch to Proc Monitor tab so user sees the effect immediately.
      setTab("procwatch");
      // Immediate refresh + trigger repeated refreshes for 15 s.
      refreshProcs();
      let n=0;
      const iv=setInterval(()=>{ refreshProcs(); if(++n>=7) clearInterval(iv); },2000);
    }catch(e){
      setDemoMsg({ok:false,text:`✗ ${e.message}`});
    }finally{
      setDemoLoading(false);
    }
  };

  const stopDemo = async(pid)=>{
    try{
      await api(`/api/v1/demos/${pid}`,{method:"DELETE"});
      setActiveDemo(p=>p.filter(x=>x.pid!==pid));
      setDemoMsg({ok:true,text:`Stopped PID ${pid}`});
    }catch(e){ setDemoMsg({ok:false,text:e.message}); }
  };

  // ── Derived ───────────────────────────────────────────────────────────────
  const lifecycleEvents = useMemo(()=>
    allEvents.filter(e=>e.kind==="fault"&&e.fault&&LIFECYCLE.has(e.fault.type)),
    [allEvents]);
  const zombies = procs.filter(p=>p.zombie_since&&p.alive);
  const alive   = procs.filter(p=>p.alive&&!p.zombie_since);

  const fc = {hidden:{opacity:0,y:rm?0:14},show:{opacity:1,y:0,transition:{duration:0.3,ease:"easeOut"}}};

  // ── Render ────────────────────────────────────────────────────────────────
  return (
    <div style={S.root}>

      {/* Toast container */}
      <div style={S.toastBox}>
        <AnimatePresence>
          {toasts.map(t=>(
            <motion.div key={t.id}
              initial={{opacity:0,x:64}} animate={{opacity:1,x:0}} exit={{opacity:0,x:64}}
              transition={{duration:0.28}}
              style={{...S.toast,borderLeftColor:SEV[t.fault.severity]||"#555"}}
            >
              <span style={{fontSize:22,lineHeight:1}}>{FAULT_META[t.fault.type]?.icon||"⚠"}</span>
              <div style={{flex:1,minWidth:0}}>
                <div style={S.toastTitle}>{FAULT_META[t.fault.type]?.label||t.fault.type}</div>
                <div style={S.toastMsg}>{t.fault.message}</div>
                <div style={S.toastMeta}>
                  PID {t.fault.pid}
                  {t.fault.parent_pid?` · PPID ${t.fault.parent_pid}`:""}
                  {t.fault.signal?` · sig ${t.fault.signal}`:""}
                </div>
              </div>
              <button onClick={()=>setToasts(p=>p.filter(x=>x.id!==t.id))} style={S.toastX}>×</button>
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
        <div style={S.pill}>
          <span style={{...S.dot,background:stream==="open"?"#51cf66":stream==="error"?"#ff4d4d":"#888"}}/>
          {stream==="open"?"Live":stream==="error"?"Error":stream==="closed"?"Closed":"Connecting…"}
        </div>
      </header>

      {/* Tab bar */}
      <nav style={S.tabs}>
        {TABS.map(t=>(
          <button key={t.id} onClick={()=>setTab(t.id)}
            style={{...S.tab,...(tab===t.id?S.tabOn:{})}}
          >
            {t.label}
            {t.id==="procwatch"&&lifecycleEvents.length>0&&(
              <span style={S.badge}>{lifecycleEvents.length}</span>
            )}
            {t.id==="demos"&&activeDemo.length>0&&(
              <span style={{...S.badge,background:"#51cf66"}}>{activeDemo.length}</span>
            )}
          </button>
        ))}
      </nav>

      <main style={S.main}>

        {/* ── DASHBOARD ──────────────────────────────────────────────────── */}
        {tab==="dashboard"&&(
          <motion.div variants={{show:{transition:{staggerChildren:0.07}}}} initial="hidden" animate="show" style={S.grid}>
            <motion.div variants={fc} style={S.card}>
              <div style={S.cardTitle}>Overview</div>
              <div style={S.statRow}>
                <Stat label="Targets"   value={status?.targets?.length??"—"}/>
                <Stat label="Processes" value={status?.targets?.reduce((s,t)=>s+(t.processes?.length||0),0)??"—"}/>
                <Stat label="Zombies"   value={zombies.length} accent={zombies.length>0?"#ff6b6b":null}/>
                <Stat label="Tracked"   value={procs.length}/>
                <Stat label="LC Faults" value={lifecycleEvents.length} accent={lifecycleEvents.length>0?"#ffa94d":null}/>
              </div>
              {status?.targets?.map(t=>(
                <div key={t.name} style={S.targetBox}>
                  <div style={S.targetName}>{t.name}</div>
                  {t.processes?.map(p=>(
                    <div key={p.pid} style={S.prow}>
                      <span style={S.pname}>{p.name}</span>
                      <span style={S.pmeta}>PID {p.pid}</span>
                      <span style={S.pmeta}>{fmtPct(p.cpu_percent)} CPU</span>
                      <span style={S.pmeta}>{fmtBytes(p.memory_bytes)}</span>
                    </div>
                  ))}
                  {!t.processes?.length&&<div style={S.empty}>No processes matched.</div>}
                </div>
              ))}
              {!status&&<div style={S.empty}>Waiting for first poll…</div>}
            </motion.div>

            <motion.div variants={fc} style={S.card}>
              <div style={S.cardTitle}>Recent Lifecycle Faults</div>
              {lifecycleEvents.length===0&&(
                <div style={S.empty}>No lifecycle faults yet — use the Demos tab to trigger one.</div>
              )}
              {lifecycleEvents.slice(0,8).map(ev=><FaultCard key={ev.id} ev={ev}/>)}
            </motion.div>
          </motion.div>
        )}

        {/* ── PROC MONITOR ───────────────────────────────────────────────── */}
        {tab==="procwatch"&&(
          <motion.div variants={{show:{transition:{staggerChildren:0.07}}}} initial="hidden" animate="show" style={S.grid}>

            {/* Summary strip */}
            <motion.div variants={fc} style={{...S.card,gridColumn:"1/-1"}}>
              <div style={S.cardTitle}>Lifecycle Fault Summary</div>
              <div style={S.statRow}>
                {Object.entries(
                  pwEvents.reduce((acc,ev)=>{
                    const t=ev.fault?.type||ev.kind;
                    acc[t]=(acc[t]||0)+1; return acc;
                  },{})
                ).map(([type,count])=>(
                  <Stat key={type}
                    label={FAULT_META[type]?.label||type}
                    value={count}
                    icon={FAULT_META[type]?.icon}
                    accent={FAULT_META[type]?.color}/>
                ))}
                {pwEvents.length===0&&<span style={S.empty}>No lifecycle events yet — launch a demo.</span>}
              </div>
            </motion.div>

            {/* Zombie / live process table */}
            <motion.div variants={fc} style={S.card}>
              <div style={S.cardTitle}><span style={{color:"#ff6b6b"}}>☠</span> Zombie Processes ({zombies.length})</div>
              {zombies.length===0&&<div style={S.empty}>No zombies tracked.</div>}
              {zombies.map(p=>(
                <div key={p.pid} style={S.prowFull}>
                  <div>
                    <span style={{...S.pname,color:"#ff6b6b"}}>{p.name}</span>
                    <span style={S.pmeta}> PID {p.pid} · PPID {p.ppid}</span>
                  </div>
                  <div style={S.pmeta}>since {fmtTs(p.zombie_since)}</div>
                </div>
              ))}

              <div style={{marginTop:20}}>
                <div style={S.cardTitle}><span style={{color:"#74c0fc"}}>●</span> Live Tracked ({alive.length})</div>
                {alive.length===0&&<div style={S.empty}>No tracked processes.</div>}
                <div style={{display:"flex",flexDirection:"column",gap:2}}>
                  {alive.slice(0,40).map(p=>(
                    <div key={p.pid} style={{...S.prow,gap:16}}>
                      <span style={S.pname}>{p.name}</span>
                      <span style={S.pmeta}>PID {p.pid}</span>
                      <span style={S.pmeta}>PPID {p.ppid}</span>
                      <span style={{...S.pmeta,color:"#444"}}>seen {fmtTs(p.first_seen)}</span>
                    </div>
                  ))}
                  {alive.length>40&&<div style={S.pmeta}>…and {alive.length-40} more</div>}
                </div>
              </div>
            </motion.div>

            {/* Live event feed */}
            <motion.div variants={fc} style={S.card}>
              <div style={S.cardTitle}>Live Lifecycle Feed</div>
              {pwEvents.length===0&&(
                <div style={S.empty}>No events yet.<br/>Start a demo from the <b>Demos</b> tab.</div>
              )}
              <div style={S.feed}>
                <AnimatePresence initial={false}>
                  {pwEvents.slice(0,60).map(ev=>(
                    <motion.div key={ev.id}
                      initial={{opacity:0,height:0}} animate={{opacity:1,height:"auto"}}
                      exit={{opacity:0,height:0}} transition={{duration:0.22}}
                    >
                      <FaultCard ev={ev}/>
                    </motion.div>
                  ))}
                </AnimatePresence>
              </div>
            </motion.div>
          </motion.div>
        )}

        {/* ── EVENTS ─────────────────────────────────────────────────────── */}
        {tab==="events"&&(
          <motion.div variants={fc} initial="hidden" animate="show" style={S.card}>
            <div style={S.cardTitle}>All Events ({allEvents.length})</div>
            {allEvents.length===0&&<div style={S.empty}>No events yet.</div>}
            <div style={S.feed}>
              {allEvents.slice(0,150).map(ev=>
                ev.kind==="fault"
                  ?<FaultCard key={ev.id} ev={ev}/>
                  :<ActionCard key={ev.id} ev={ev}/>
              )}
            </div>
          </motion.div>
        )}

        {/* ── DEMOS ──────────────────────────────────────────────────────── */}
        {tab==="demos"&&(
          <motion.div variants={fc} initial="hidden" animate="show" style={S.card}>
            <div style={S.cardTitle}>Demo Controls</div>
            <p style={{...S.pmeta,marginBottom:16,lineHeight:1.6}}>
              Select a lifecycle fault scenario and click <b>Launch Demo</b>.
              The backend executes <code style={{color:"#74c0fc"}}>fisdemo -mode &lt;mode&gt;</code> and
              procwatch automatically detects the resulting fault within 2–4 s.
              Results appear in the <b>Proc Monitor</b> tab and as toast notifications.
            </p>

            <div style={S.demoGrid}>
              <div>
                <div style={S.label}>Fault scenario</div>
                <select value={demoMode} onChange={e=>setDemoMode(e.target.value)} style={S.sel}>
                  {ALL_MODES.map(m=>(
                    <option key={m.value} value={m.value}>{m.label}</option>
                  ))}
                </select>
              </div>
              <button
                onClick={startDemo}
                disabled={demoLoading}
                style={{...S.btnPrimary,...(demoLoading?{opacity:0.6,cursor:"not-allowed"}:{})}}
              >
                {demoLoading?"Launching…":"⚡ Launch Demo"}
              </button>
            </div>

            {demoMsg&&(
              <div style={{
                ...S.msg,
                background: demoMsg.ok?"#0d1f0d":"#1f0d0d",
                color: demoMsg.ok?"#51cf66":"#ff6b6b",
                border: `1px solid ${demoMsg.ok?"#1a4a1a":"#4a1a1a"}`,
              }}>
                {demoMsg.text}
              </div>
            )}

            {/* Active demos */}
            <div style={{marginTop:20}}>
              <div style={S.label}>Active demo processes ({activeDemo.length})</div>
              {activeDemo.length===0&&(
                <div style={S.empty}>No demos running. Launch one above.</div>
              )}
              {activeDemo.map(d=>(
                <div key={d.pid} style={S.prowFull}>
                  <div>
                    <span style={{...S.pname,color:"#7c6af7"}}>{d.mode}</span>
                    <span style={S.pmeta}> PID {d.pid}</span>
                  </div>
                  <button onClick={()=>stopDemo(d.pid)} style={S.btnDanger}>Stop</button>
                </div>
              ))}
            </div>

            {/* What to expect */}
            <div style={{marginTop:28,padding:16,background:"#0d0d18",borderRadius:8,border:"1px solid #1e1e2e"}}>
              <div style={{...S.label,marginBottom:12}}>Expected behaviour per mode</div>
              {[
                ["zombie","Child exits instantly. Parent never calls wait(). Child appears as state Z. Toast + zombie table update within 2 s."],
                ["orphan","Parent exits. Child re-parents to PID 1 (init). Orphan event appears in feed."],
                ["parent-nowait","Child exits at T+1 s, parent at T+5 s. Brief zombie window detected."],
                ["sigkill","Parent sends SIGKILL (signal 9) to child after 2 s. Critical signal_death event."],
                ["sigterm","Parent sends SIGTERM (signal 15) to child after 2 s. Signal_death event."],
                ["sigsegv","Process crashes with SIGSEGV (signal 11). Signal_death warn event."],
                ["sigabrt","Process sends SIGABRT to itself (signal 6). Signal_death warn event."],
                ["cpu","Busy-loop. CPU spike fault if above threshold."],
                ["mem","Allocates memory. Memory fault if above threshold."],
                ["crash","Calls os.Exit(2) after 2 s. Crash fault + restart action."],
              ].map(([m,desc])=>(
                <div key={m} style={{marginBottom:10}}>
                  <code style={{...S.code,display:"inline-block",marginBottom:4}}>{m}</code>
                  <div style={{...S.pmeta,lineHeight:1.5}}>{desc}</div>
                </div>
              ))}
            </div>
          </motion.div>
        )}

        {/* ── CONFIG ─────────────────────────────────────────────────────── */}
        {tab==="config"&&(
          <motion.div variants={fc} initial="hidden" animate="show" style={S.card}>
            <div style={S.cardTitle}>Config Editor</div>
            <textarea value={cfg} onChange={e=>setCfg(e.target.value)}
              style={S.ta} rows={24} spellCheck={false}/>
            {cfgMsg&&(
              <div style={{...S.msg,
                background:cfgMsg.ok?"#0d1f0d":"#1f0d0d",
                color:cfgMsg.ok?"#51cf66":"#ff6b6b",
                border:`1px solid ${cfgMsg.ok?"#1a4a1a":"#4a1a1a"}`
              }}>{cfgMsg.text}</div>
            )}
            <div style={{display:"flex",gap:12,marginTop:12}}>
              <button onClick={saveConfig} disabled={cfgLoading} style={S.btnPrimary}>
                {cfgLoading?"Saving…":"Save Config"}
              </button>
              <button onClick={()=>api("/api/v1/config").then(d=>setCfg(JSON.stringify(d.data,null,2))).catch(()=>{})}
                style={S.btnSecondary}>Reload</button>
            </div>
          </motion.div>
        )}
      </main>
    </div>
  );
}

// ── Sub-components ────────────────────────────────────────────────────────────

function Stat({label,value,icon,accent}){
  return(
    <div style={{textAlign:"center",minWidth:76}}>
      {icon&&<div style={{fontSize:18,marginBottom:2}}>{icon}</div>}
      <div style={{fontSize:24,fontWeight:700,color:accent||"#e0e0e0",fontFamily:"monospace",lineHeight:1}}>{value}</div>
      <div style={{fontSize:10,color:"#666",textTransform:"uppercase",letterSpacing:1,marginTop:3}}>{label}</div>
    </div>
  );
}

function FaultCard({ev}){
  const f   = ev.fault||ev;
  const m   = FAULT_META[f.type]||{icon:"⚠",label:f.type||"fault",color:"#888"};
  const sev = f.severity||"info";
  return(
    <div style={{...S.evCard,borderLeft:`3px solid ${SEV[sev]||"#555"}`}}>
      <div style={S.evHead}>
        <span style={{fontSize:15}}>{m.icon}</span>
        <span style={{color:m.color,fontWeight:600}}>{m.label}</span>
        <span style={{...S.sevChip,background:(SEV[sev]||"#555")+"30",color:SEV[sev]||"#888"}}>{sev}</span>
        {f.target&&f.target!=="untracked"&&<span style={{...S.pmeta,color:"#7c6af7"}}>{f.target}</span>}
        <span style={{marginLeft:"auto",...S.pmeta}}>{fmtTs(ev.timestamp||f.timestamp)}</span>
      </div>
      <div style={{fontSize:12,color:"#ccc",margin:"4px 0",lineHeight:1.5}}>{f.message}</div>
      <div style={S.evMeta}>
        {f.pid&&<span>PID <b>{f.pid}</b></span>}
        {f.parent_pid&&<span>PPID <b>{f.parent_pid}</b></span>}
        {f.signal&&<span>signal <b>{f.signal}</b></span>}
        {f.zombie_duration_ns&&<span>age <b>{fmtNs(f.zombie_duration_ns)}</b></span>}
      </div>
    </div>
  );
}

function ActionCard({ev}){
  const a=ev.action||{};
  return(
    <div style={{...S.evCard,borderLeft:"3px solid #51cf66"}}>
      <div style={S.evHead}>
        <span>⚙</span>
        <span style={{color:"#51cf66",fontWeight:600}}>{a.action}</span>
        <span style={{...S.sevChip,background:"#0a2a0a",color:"#51cf66"}}>{a.result}</span>
        <span style={{marginLeft:"auto",...S.pmeta}}>{fmtTs(ev.timestamp)}</span>
      </div>
      <div style={S.evMeta}>
        {a.target&&<span>target <b>{a.target}</b></span>}
        {a.pid&&<span>PID <b>{a.pid}</b></span>}
        {a.reason&&<span>{a.reason}</span>}
        {a.new_pid&&<span>new PID <b>{a.new_pid}</b></span>}
      </div>
    </div>
  );
}

// ── Styles ────────────────────────────────────────────────────────────────────

const S = {
  root:{minHeight:"100vh",background:"#0d0d14",color:"#e0e0e0",fontFamily:"'JetBrains Mono','Fira Code',monospace",fontSize:13},
  header:{display:"flex",alignItems:"center",justifyContent:"space-between",padding:"18px 28px 14px",borderBottom:"1px solid #1a1a2a"},
  eyebrow:{fontSize:10,letterSpacing:3,color:"#555",textTransform:"uppercase",marginBottom:4},
  title:{fontSize:21,fontWeight:700,color:"#fff"},
  pill:{display:"flex",alignItems:"center",gap:8,background:"#1a1a2e",padding:"6px 14px",borderRadius:20,fontSize:12,color:"#aaa"},
  dot:{width:8,height:8,borderRadius:"50%",display:"inline-block"},
  tabs:{display:"flex",padding:"0 28px",borderBottom:"1px solid #1a1a2a"},
  tab:{background:"none",border:"none",color:"#777",padding:"10px 16px",cursor:"pointer",fontSize:13,fontFamily:"inherit",borderBottom:"2px solid transparent",display:"flex",alignItems:"center",gap:6,whiteSpace:"nowrap"},
  tabOn:{color:"#e0e0e0",borderBottom:"2px solid #7c6af7"},
  badge:{background:"#ff4d4d",color:"#fff",borderRadius:10,padding:"1px 5px",fontSize:10,fontWeight:700},
  main:{padding:"20px 28px",maxWidth:1380,margin:"0 auto"},
  grid:{display:"grid",gridTemplateColumns:"repeat(auto-fit,minmax(440px,1fr))",gap:18},
  card:{background:"#111118",border:"1px solid #1a1a2a",borderRadius:10,padding:20},
  cardTitle:{fontSize:13,fontWeight:700,color:"#bbb",marginBottom:14,letterSpacing:0.4,display:"flex",alignItems:"center",gap:8},
  statRow:{display:"flex",gap:20,flexWrap:"wrap",marginBottom:16,paddingBottom:14,borderBottom:"1px solid #1a1a2a"},
  targetBox:{background:"#0d0d18",border:"1px solid #1a1a2a",borderRadius:6,padding:"10px 12px",marginBottom:8},
  targetName:{fontWeight:700,color:"#7c6af7",marginBottom:6,fontSize:13},
  prow:{display:"flex",gap:12,alignItems:"center",padding:"4px 0",borderTop:"1px solid #141420",flexWrap:"wrap"},
  prowFull:{display:"flex",justifyContent:"space-between",alignItems:"center",padding:"8px 0",borderTop:"1px solid #141420"},
  pname:{color:"#e0e0e0",fontWeight:600},
  pmeta:{color:"#666",fontSize:11},
  feed:{display:"flex",flexDirection:"column",gap:5,maxHeight:520,overflowY:"auto",paddingRight:2},
  evCard:{background:"#0d0d18",borderRadius:6,padding:"10px 12px",marginBottom:2},
  evHead:{display:"flex",alignItems:"center",gap:8,marginBottom:4,flexWrap:"wrap"},
  evMeta:{display:"flex",gap:14,flexWrap:"wrap",fontSize:11,color:"#666"},
  sevChip:{borderRadius:4,padding:"1px 6px",fontSize:10,fontWeight:700,textTransform:"uppercase"},
  empty:{color:"#555",fontStyle:"italic",padding:"8px 0",fontSize:12,lineHeight:1.6},
  label:{color:"#888",fontSize:10,letterSpacing:1,textTransform:"uppercase",marginBottom:6},
  sel:{background:"#1a1a2e",border:"1px solid #2a2a3e",color:"#e0e0e0",padding:"8px 10px",borderRadius:6,fontFamily:"inherit",fontSize:13,width:"100%"},
  demoGrid:{display:"grid",gridTemplateColumns:"1fr auto",gap:12,alignItems:"end",marginBottom:12},
  btnPrimary:{background:"#7c6af7",color:"#fff",border:"none",padding:"10px 22px",borderRadius:6,cursor:"pointer",fontFamily:"inherit",fontSize:13,fontWeight:700,whiteSpace:"nowrap"},
  btnSecondary:{background:"#1a1a2e",color:"#bbb",border:"1px solid #2a2a3e",padding:"8px 18px",borderRadius:6,cursor:"pointer",fontFamily:"inherit",fontSize:13},
  btnDanger:{background:"#2a0d0d",color:"#ff6b6b",border:"1px solid #4a1a1a",padding:"5px 12px",borderRadius:6,cursor:"pointer",fontFamily:"inherit",fontSize:12},
  msg:{borderRadius:6,padding:"10px 14px",marginTop:10,fontSize:13,fontWeight:500},
  ta:{width:"100%",background:"#0a0a12",border:"1px solid #1a1a2a",color:"#e0e0e0",fontFamily:"inherit",fontSize:11,borderRadius:6,padding:12,resize:"vertical",boxSizing:"border-box"},
  code:{background:"#0a0a12",color:"#74c0fc",padding:"2px 6px",borderRadius:4,fontSize:12},
  toastBox:{position:"fixed",top:20,right:20,zIndex:9999,display:"flex",flexDirection:"column",gap:10,maxWidth:370,pointerEvents:"none"},
  toast:{background:"#111118",borderLeft:"3px solid #555",borderRadius:8,padding:"12px 14px",display:"flex",gap:12,alignItems:"flex-start",boxShadow:"0 4px 28px #00000099",pointerEvents:"all"},
  toastTitle:{fontWeight:700,fontSize:13,marginBottom:2},
  toastMsg:{fontSize:11,color:"#aaa",lineHeight:1.5},
  toastMeta:{fontSize:10,color:"#666",marginTop:3},
  toastX:{background:"none",border:"none",color:"#555",cursor:"pointer",fontSize:18,padding:0,lineHeight:1,marginLeft:4},
};
