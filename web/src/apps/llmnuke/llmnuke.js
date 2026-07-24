  /* ===== LLMNUKE.EXE — agent sessions, warez edition =========================
     The focused sibling of the fleet console's Sessions section (see
     docs/SESSIONS.md), reskinned as its own hacked-Win95 app: pick a fleet
     machine, the Unix user to run as, and a model, and LLMNUKE composes the
     one sudo+tmux command that spawns opencode or claude as that user — same
     "hush composes, you run" posture as the rest of the console. hush never
     runs the command; you copy it into a root shell (JuiceSSH) yourself. The
     Active Sessions list is a real read of every online box's `/sessions`
     (aggregated fleet-wide, unlike the per-machine fleet view), each with a
     Stop button that composes the kill command the same way.

     Live vs demo is decided once, by probing /api/fleet: a real hush-control
     answers with the live fleet and live /sessions + /users reads; anything
     else (static preview, offline, file://) falls back to a fabricated demo
     fleet — but it's the *same* UI and the *same* code path either way, just
     a different data source, so the demo is a faithful preview of the real
     thing rather than a separate theatre.
     Namespaced to this folder; nothing here touches the fleet console. */
  (function(){
    const scr = $("#llmnukeScreen");
    if(!scr) return;

    const boot = $("#nukeBoot"), bootSub = $("#nukeBootSub");
    const desk = $("#nukeDesk");
    const sessList = $("#nukeSessList");
    const modeBadge = $("#nukeModeBadge");
    const pick = $("#nukePick"), pickBody = $("#nukePickBody"), pickHint = $("#nukePickHint");
    const pickSum = $("#nukePickSum"), pickGo = $("#nukePickGo"), pickBack = $("#nukePickBack");
    const steps = $("#nukeSteps");
    const cmdDlg = $("#nukeCmd"), cmdTitle = $("#nukeCmdTitle"), cmdNote = $("#nukeCmdNote");
    const cmdCode = $("#nukeCmdCode"), cmdHint = $("#nukeCmdHint"), cmdCopy = $("#nukeCmdCopy");

    const USER_RE = /^[a-z_][a-z0-9_-]*$/; // conservative POSIX login, keeps a typo out of the composed command

    /* ---- demo fleet: same box identities the fleet console's own demo data
       uses (citadel/forge/atlas/pi-gate/lab-01 — same ids and IPs), shaped
       exactly like a live /api/fleet entry (llm.runtimes with exposure/models)
       so demoFleetModels() and buildOpencodeConfig() below run unmodified
       against either source. ---- */
    const DEMO_FLEET = [
      { id:"citadel", os:"Debian 12", ip:"100.71.8.9", online:true,
        llm:{runtimes:[{kind:"openai", addr:"100.71.8.9:8091", exposure:"tailnet",
          models:["qwen2.5-coder-32b-instruct","llama-3.3-70b-instruct"]}]},
        users:[ {name:"root",uid:0}, {name:"josh",uid:1000}, {name:"ci",uid:1001}, {name:"warez",uid:1337} ],
        sessions:[ {pid:48213,user:"josh",tool:"opencode",cmd:"opencode",uptime:5400},
                   {pid:52140,user:"warez",tool:"claude",cmd:"claude --remote-control --dangerously-skip-permissions",uptime:640} ] },
      { id:"forge", os:"Arch Linux", ip:"100.71.2.5", online:true,
        llm:{runtimes:[{kind:"openai", addr:"100.71.2.5:8091", exposure:"tailnet", models:["deepseek-coder-v2-lite"]}]},
        users:[ {name:"root",uid:0}, {name:"ci",uid:1000}, {name:"maya",uid:1001} ],
        sessions:[ {pid:11902,user:"ci",tool:"opencode",cmd:"opencode run --agent build",uptime:210} ] },
      { id:"lab-01", os:"NixOS 24.05", ip:"100.71.5.7", online:true,
        llm:{runtimes:[{kind:"ollama", addr:"100.71.5.7:11434", exposure:"tailnet",
          models:["qwen2.5-coder:32b","deepseek-r1:14b"]}]},
        users:[ {name:"root",uid:0}, {name:"lab",uid:1000} ], sessions:[] },
      { id:"pi-gate", os:"Raspberry Pi OS", ip:"100.71.0.1", online:true,
        llm:{runtimes:[]}, users:[ {name:"root",uid:0}, {name:"pi",uid:1000} ], sessions:[] },
      { id:"atlas", os:"Ubuntu 24.04", ip:"100.71.9.3", online:false,
        llm:{runtimes:[]}, users:[ {name:"root",uid:0}, {name:"media",uid:1000} ], sessions:[] },
    ];

    let LIVE = null;      // null=undetermined, true=live hush-control, false=demo fallback
    let FLEET = [];        // normalized machines, live and demo share this exact shape
    const USERS_CACHE = {};      // host -> users[] (live only; demo boxes carry their own)
    const SESSIONS_CACHE = {};   // host -> { sessions, err }

    // reachableRuntimes mirrors the fleet console's ocReachable verdict: a
    // runtime is callable from off-box iff it's bound past loopback.
    function reachableRuntimes(b){
      return ((b.llm && b.llm.runtimes) || []).filter(r => r.exposure==="tailnet" || r.exposure==="open");
    }
    function fleetModels(){
      const out = [];
      FLEET.forEach(b => { if(b.online) reachableRuntimes(b).forEach(r => (r.models||[]).forEach(m => out.push({model:m, host:b.id}))); });
      return out;
    }
    function splitAddr(addr){
      const i = (addr||"").lastIndexOf(":");
      return i < 0 ? { host:addr||"", port:"" } : { host:addr.slice(0,i), port:addr.slice(i+1) };
    }
    // buildOpencodeConfig writes the one provider/model the wizard picked —
    // narrower than the fleet console's export (which offers every reachable
    // runtime on the box), since a spawned session only needs the one model it
    // was pointed at. Same openai-compatible shape opencode expects.
    function buildOpencodeConfig(host, model){
      const rt = reachableRuntimes(host).find(r => (r.models||[]).includes(model));
      if(!rt) return null;
      const { host:h0, port } = splitAddr(rt.addr);
      const h = (h0==="0.0.0.0"||h0==="::"||h0==="") ? (host.ip||h0) : h0;
      const hostPart = h.includes(":") ? `[${h}]` : h;
      const baseURL = `http://${hostPart}${port?":"+port:""}/v1`;
      const baseKey = host.id.toLowerCase().replace(/[^a-z0-9-]+/g,"-").replace(/^-+|-+$/g,"") || "local";
      const models = {}; models[model] = { name:model, cost:{ input:0, output:0 } };
      const provider = {};
      provider[baseKey] = { npm:"@ai-sdk/openai-compatible", name:`${host.id} · ${rt.kind==="ollama"?"ollama":"openai-compatible"}`, options:{ baseURL }, models };
      return { "$schema":"https://opencode.ai/config.json", provider };
    }

    /* ---- live vs demo: probe /api/fleet once, then load the fleet from
       whichever source answered. A 200 JSON array means a real hush-control is
       behind the page; anything else (404 on the static preview, a network
       error offline) is the legitimate demo fallback — same signal the fleet
       console uses for its own demo mode. ---- */
    async function loadFleet(){
      try {
        const r = await fetch("api/fleet", {cache:"no-store"});
        if(!r.ok) throw 0;
        const d = await r.json();
        if(!Array.isArray(d)) throw 0;
        FLEET = d.map(m => ({ id:m.id, os:m.os||"", ip:m.ip||"", online:!!m.online, llm:m.llm||{runtimes:[]} }));
        LIVE = true;
      } catch(_){
        FLEET = DEMO_FLEET;
        LIVE = false;
      }
      modeBadge.textContent = LIVE ? "L1V3" : "D3M0 D4T4";
      modeBadge.classList.toggle("live", !!LIVE);
    }
    // usersFor lazily reads a box's human accounts: the demo fleet carries its
    // own list, a live box answers GET /api/machines/{host}/users (the same
    // read the fleet console's Users section uses).
    async function usersFor(box){
      if(!LIVE) return box.users || [];
      if(USERS_CACHE[box.id]) return USERS_CACHE[box.id];
      try {
        const r = await fetch(`api/machines/${encodeURIComponent(box.id)}/users`, {cache:"no-store"});
        if(!r.ok) return [];
        const d = await r.json();
        const list = (d && d.users) || [];
        USERS_CACHE[box.id] = list;
        return list;
      } catch(_){ return []; }
    }
    // sessionsFor lazily reads a box's running coding agents: GET
    // /api/machines/{host}/sessions live, or the demo fleet's own fabricated
    // list — same shape, same 404-means-"old agent" handling as fleet.js.
    async function sessionsFor(box){
      if(!LIVE) return { sessions: box.sessions || [], err:"" };
      try {
        const r = await fetch(`api/machines/${encodeURIComponent(box.id)}/sessions`, {cache:"no-store"});
        if(!r.ok) return { sessions:[], err: r.status===404 ? "old-agent" : "unreach" };
        const d = await r.json();
        return { sessions:(d && d.sessions) || [], err:"" };
      } catch(_){ return { sessions:[], err:"unreach" }; }
    }

    /* ---------- open / close ---------- */
    let clockTimer = null, sessPollTimer = null;
    function openNuke(){
      scr.classList.add("on");
      clockTick(); clockTimer = setInterval(clockTick, 15000);
      if(LIVE === null){
        boot.hidden = false; desk.hidden = true;
        const spin = ["pr0b1ng th3 fl33t . . .","c0unt1ng g31g3r t1ckz . . .","ch3ck1ng c0ntr0l pl4n3 . . ."];
        let i = 0; const t = setInterval(()=>{ bootSub.textContent = spin[++i % spin.length]; }, 260);
        Promise.all([loadFleet(), sleep(500)]).then(() => { clearInterval(t); boot.hidden = true; desk.hidden = false; paint(); });
      } else { paint(); }
    }
    function paint(){
      renderSessions();
      refreshSessions();
      if(sessPollTimer) clearInterval(sessPollTimer);
      sessPollTimer = setInterval(refreshSessions, 6000);
    }
    function closeNuke(){
      startMenu(false);
      pick.hidden = true;
      cmdDlg.hidden = true;
      if(clockTimer){ clearInterval(clockTimer); clockTimer = null; }
      if(sessPollTimer){ clearInterval(sessPollTimer); sessPollTimer = null; }
      scr.classList.remove("on");
      try { $("#homeLlmnuke").focus({preventScroll:true}); } catch(_){}
    }
    const sleep = ms => new Promise(r => setTimeout(r, ms));

    /* ---------- home: the Active Sessions list, fleet-wide ---------- */
    let sessSeq = 0;
    async function refreshSessions(){
      const seq = ++sessSeq;
      const online = FLEET.filter(b => b.online);
      const results = await Promise.all(online.map(async b => ({ box:b, ...(await sessionsFor(b)) })));
      if(seq !== sessSeq) return; // a newer refresh landed first
      results.forEach(r => { SESSIONS_CACHE[r.box.id] = r; });
      renderSessions();
    }
    function fmtUptime(sec){
      if(!sec || sec < 0) return "";
      if(sec < 60) return sec+"s";
      const m = Math.round(sec/60); if(m < 60) return m+"m";
      const h = Math.floor(m/60); return h < 24 ? h+"h" : Math.floor(h/24)+"d";
    }
    function renderSessions(){
      sessList.innerHTML = "";
      const rows = [];
      FLEET.forEach(b => {
        const st = SESSIONS_CACHE[b.id];
        if(st && st.sessions) st.sessions.forEach(s => rows.push({ box:b, s }));
      });
      if(!rows.length){
        const e = document.createElement("div");
        e.className = "nuke-empty";
        e.innerHTML = '<div class="nuke-empty-art" aria-hidden="true">☢</div>'
          + '<div class="nuke-empty-t">N0 4CT1V3 S3SS10NZ</div>'
          + '<div class="nuke-empty-s">n0th1n\' runn1ng 0n th3 fl33t r1ght n0w.<br>h1t <b>NUK3 N3W S3SS10N</b> 2 sp4wn 0n3.</div>';
        sessList.appendChild(e);
        return;
      }
      rows.sort((a,b) => (a.s.uptime||0) - (b.s.uptime||0));
      rows.forEach(({box, s}) => {
        const row = document.createElement("div");
        row.className = "nuke-sess"; row.setAttribute("role","treeitem"); row.tabIndex = 0;
        const ico = s.tool === "claude" ? "✳" : "⌁";
        row.innerHTML =
          '<span class="nuke-sess-ico" aria-hidden="true">'+ico+'</span>'
          + '<span class="nuke-sess-body">'
          +   '<span class="nuke-sess-title"></span>'
          +   '<span class="nuke-sess-sub"></span>'
          + '</span>'
          + '<span class="nuke-sess-live">l1v3</span>'
          + '<button class="nuke-sess-kill" type="button" aria-label="Stop session">×</button>';
        row.querySelector(".nuke-sess-title").textContent = (s.user||"?")+"@"+box.id;
        const up = fmtUptime(s.uptime);
        row.querySelector(".nuke-sess-sub").textContent = s.tool + (up?" · "+up:"") + (s.cmd?" · "+s.cmd:"");
        const stop = () => openStopSheet(box, s);
        row.addEventListener("click", e => { if(!e.target.closest(".nuke-sess-kill")) stop(); });
        row.addEventListener("keydown", e => { if(e.key==="Enter"||e.key===" "){ e.preventDefault(); stop(); } });
        const kill = row.querySelector(".nuke-sess-kill");
        kill.addEventListener("click", e => { e.stopPropagation(); stop(); });
        sessList.appendChild(row);
      });
    }

    /* ---------- the target selector (machine → user → model) ---------- */
    let wiz = null;
    function openPicker(){
      startMenu(false);
      wiz = { step:0, box:null, user:null, model:null, tool:"opencode", llmHost:null };
      pick.hidden = false;
      renderWizard();
      try { pickBody.querySelector(".nuke-row").focus(); } catch(_){}
    }
    function closePicker(){ pick.hidden = true; wiz = null; }
    function setStep(n){ wiz.step = n; renderWizard(); }

    function renderWizard(){
      Array.from(steps.children).forEach((c,i) => {
        c.classList.toggle("on", i === wiz.step);
        c.classList.toggle("done", i < wiz.step);
      });
      pickBack.hidden = wiz.step === 0;
      pickGo.disabled = !(wiz.step === 2 && wiz.model);
      pickSum.textContent = summarize();

      if(wiz.step === 0) return renderMachines();
      if(wiz.step === 1) return renderUsers();
      return renderModels();
    }
    function summarize(){
      const bits = [];
      if(wiz.box) bits.push(wiz.box.id);
      if(wiz.user) bits.push(wiz.user.name);
      if(wiz.model) bits.push(wiz.model);
      return bits.join(" › ");
    }
    function rowEl(cls, html){ const d = document.createElement("div"); d.className = "nuke-row"+(cls?" "+cls:"");
      d.setAttribute("role","treeitem"); d.tabIndex = 0; d.innerHTML = html; return d; }
    function bindRow(el, fn){
      el.addEventListener("click", fn);
      el.addEventListener("keydown", e => { if(e.key==="Enter"||e.key===" "){ e.preventDefault(); fn(); } });
    }
    function emptyRow(text){
      const e = document.createElement("div"); e.className = "nuke-empty";
      e.innerHTML = '<div class="nuke-empty-s"></div>'; e.querySelector(".nuke-empty-s").textContent = text;
      return e;
    }

    function renderMachines(){
      pickHint.textContent = "P1ck a b0x 0n th3 fl33t 2 0wn.";
      pickBody.innerHTML = "";
      const boxes = FLEET.slice().sort((a,b)=> (b.online?1:0)-(a.online?1:0));
      boxes.forEach(b => {
        const n = fleetModels().filter(m=>m.host===b.id).length;
        const sub = (b.os||"box") + " · " + (b.ip||"?") + " · " + (b.online ? (n ? n+" m0d3lz" : "n0 l0c4l m0d3l") : "s1gn3d 0ff");
        const el = rowEl(b.online ? "" : "off",
          '<span class="nuke-dot'+(b.online?"":" off")+'" aria-hidden="true"></span>'
          + '<span class="nuke-row-body"><span class="nuke-row-name"></span><span class="nuke-row-sub"></span></span>');
        el.querySelector(".nuke-row-name").textContent = b.id;
        el.querySelector(".nuke-row-sub").textContent = sub;
        if(wiz.box && wiz.box.id === b.id) el.classList.add("sel");
        bindRow(el, () => {
          if(!b.online){ warnPick("th4t b0x 1z s1gn3d 0ff — c4n't 4rm 0n 1t."); return; }
          wiz.box = b; wiz.user = null; setStep(1);
        });
        pickBody.appendChild(el);
      });
    }

    let usersReqSeq = 0;
    async function renderUsers(){
      pickHint.textContent = "Wh0 d0 w3 run az 0n " + wiz.box.id + "?";
      pickBody.innerHTML = "";
      pickBody.appendChild(emptyRow("r34d1ng us3rz…"));
      const seq = ++usersReqSeq;
      const users = await usersFor(wiz.box);
      if(seq !== usersReqSeq || !wiz || wiz.step !== 1) return; // stale — nav'd away
      pickBody.innerHTML = "";
      if(!users.length){ pickBody.appendChild(emptyRow("n0 us3r acc0untz f0und (0r th3 4g3nt'z t00 0ld 2 r3p0rt th3m).")); return; }
      users.slice().sort((a,b)=> a.uid - b.uid).forEach(u => {
        const el = rowEl("",
          '<span class="nuke-row-ico" aria-hidden="true">'+(u.uid===0?"#":"~")+'</span>'
          + '<span class="nuke-row-body"><span class="nuke-row-name"></span><span class="nuke-row-sub"></span></span>'
          + '<span class="nuke-tag'+(u.uid===0?" uid0":"")+'">'+(u.uid===0?"r00t":("uid "+u.uid))+'</span>');
        el.querySelector(".nuke-row-name").textContent = u.name;
        el.querySelector(".nuke-row-sub").textContent = "sudo -u "+u.name;
        if(wiz.user && wiz.user.name === u.name) el.classList.add("sel");
        bindRow(el, () => { wiz.user = u; setStep(2); });
        pickBody.appendChild(el);
      });
    }

    function renderModels(){
      pickHint.textContent = "P01nt th3 4g3nt @ a m0d3l 0n th3 tailnet.";
      pickBody.innerHTML = "";
      const models = fleetModels();
      models.forEach(m => {
        const el = rowEl("",
          '<span class="nuke-row-ico" aria-hidden="true">⌁</span>'
          + '<span class="nuke-row-body"><span class="nuke-row-name"></span><span class="nuke-row-sub"></span></span>'
          + '<span class="nuke-tag">opencode</span>');
        el.querySelector(".nuke-row-name").textContent = m.model;
        el.querySelector(".nuke-row-sub").textContent = "s3rv3d by "+m.host+" · 0p3n41-c0mp4t";
        if(wiz.tool==="opencode" && wiz.model === m.model && wiz.llmHost === m.host) el.classList.add("sel");
        bindRow(el, () => { wiz.tool = "opencode"; wiz.model = m.model; wiz.llmHost = m.host; renderWizard(); });
        pickBody.appendChild(el);
      });
      // claude runs against its own login — no local model to point at
      const c = rowEl("",
        '<span class="nuke-row-ico" aria-hidden="true">✳</span>'
        + '<span class="nuke-row-body"><span class="nuke-row-name">claude</span>'
        +   '<span class="nuke-row-sub">4nthr0p1c l0g1n · n0 l0c4l m0d3l</span></span>'
        + '<span class="nuke-tag">claude</span>');
      if(wiz.tool==="claude") c.classList.add("sel");
      bindRow(c, () => { wiz.tool = "claude"; wiz.model = "claude"; wiz.llmHost = null; renderWizard(); });
      pickBody.appendChild(c);
      if(!models.length) pickBody.insertBefore(emptyRow("n0 tailnet-r34ch4bl3 LLM 0n th3 fl33t — 0p3nc0d3 w0n't h4v3 a l0c4l m0d3l t0 p01nt 4t y3t."), c);
    }
    function warnPick(msg){ pickHint.textContent = msg; }

    /* ---------- arming a session: compose the real spawn command ----------
       "hush composes, you run": same posture as docs/SESSIONS.md and the
       fleet console's own Spawn sheet. hush never runs this — it hands you
       the line to paste into a root shell (JuiceSSH) on the box. */
    function composeCmd(w){
      const u = w.user.name;
      if(w.tool === "claude"){
        const prefix = (w.box.id+"."+u).replace(/[^a-zA-Z0-9._-]/g, "-");
        return "sudo -u "+u+" -H bash -lc '\n"
          + "  export CLAUDE_REMOTE_CONTROL_SESSION_NAME_PREFIX=\""+prefix+"\" &&\\\n"
          + "  exec tmux new-session -A -s hush-claude claude --remote-control --dangerously-skip-permissions'";
      }
      let lines = "";
      const cfg = buildOpencodeConfig(w.box, w.model);
      if(cfg){
        const json = JSON.stringify(cfg, null, 2);
        const b64 = btoa(unescape(encodeURIComponent(json)));
        lines += "  mkdir -p \"$HOME/.config/opencode\" &&\\\n";
        lines += "  printf %s \""+b64+"\" | base64 -d > \"$HOME/.config/opencode/opencode.json\" &&\\\n";
      }
      lines += "  exec tmux new-session -A -s hush-opencode opencode";
      return "sudo -u "+u+" -H bash -lc '\n"+lines+"'";
    }
    function armSession(){
      if(!(wiz && wiz.box && wiz.user && wiz.model)) return;
      const cmd = composeCmd(wiz);
      const box = wiz.box, tool = wiz.tool;
      closePicker();
      beep();
      openCmdSheet({
        title:"SP4WN CMD",
        note:"p4st3 th1s 4z r00t 0n "+box.id+" (JuiceSSH) 2 4rm th3 s3ss10n — hush n3v3r runz 1t 4 u.",
        cmd,
        hint:"l4unch3z 1n tmux::hush-"+tool+". r3-p4st3 th3 s4m3 l1n3 2 r34tt4ch. 1t sh0wz up 1n Act1v3 S3ss10nz 0nc3 th3 fl33t p1ckz 1t up (~6s)."
      });
    }

    /* ---------- stopping a session: compose the kill command ---------- */
    function openStopSheet(box, s){
      const u = s.user && USER_RE.test(s.user) ? s.user : "";
      const cmd = u ? "sudo -u "+u+" kill "+s.pid : "sudo kill "+s.pid;
      openCmdSheet({
        title:"ST0P S3SS10N",
        note:"run th1s az r00t 0n "+box.id+" 2 k1ll "+s.tool+" (pid "+s.pid+")"+(u?" 0wn3d by "+u:"")+" — hush n3v3r s3ndz th3 s1gn4l 1ts3lf.",
        cmd,
        hint:"1tz tmux s3ss10n cl0s3z w/ 1t. dr0ps 0ff Act1v3 S3ss10nz 0n th3 n3xt r3fr3sh (~6s)."
      });
    }

    /* ---------- the command sheet: shared by Spawn and Stop ---------- */
    let curCmd = "";
    function openCmdSheet({title, note, cmd, hint}){
      cmdTitle.textContent = title;
      cmdNote.textContent = note;
      cmdCode.textContent = cmd;
      cmdHint.textContent = hint || "";
      curCmd = cmd;
      cmdCopy.textContent = "☢ C0PY »";
      cmdDlg.hidden = false;
      try { cmdCopy.focus({preventScroll:true}); } catch(_){}
    }
    function closeCmdSheet(){ cmdDlg.hidden = true; curCmd = ""; }
    async function copyCmd(){
      if(!curCmd) return;
      try { await navigator.clipboard.writeText(curCmd); cmdCopy.textContent = "☢ C0P13D! »"; }
      catch(_){ cmdCopy.textContent = "s3l3ct + c0py m4nu4lly"; }
      setTimeout(()=>{ if(!cmdDlg.hidden) cmdCopy.textContent = "☢ C0PY »"; }, 1600);
    }

    /* ---------- taskbar / start menu / clock ---------- */
    const startBtn = $("#nukeStartBtn"), startMenuEl = $("#nukeStartMenu"), clockEl = $("#nukeClock");
    function startMenu(on){
      startMenuEl.classList.toggle("on", on);
      startBtn.classList.toggle("on", on);
      startBtn.setAttribute("aria-expanded", on ? "true" : "false");
    }
    function clockTick(){
      const d = new Date();
      clockEl.textContent = String(d.getHours()).padStart(2,"0")+":"+String(d.getMinutes()).padStart(2,"0");
    }
    startBtn.addEventListener("click", e => { e.stopPropagation(); startMenu(!startMenuEl.classList.contains("on")); });
    startMenuEl.addEventListener("click", e => {
      const item = e.target.closest(".nuke-sm-item"); if(!item) return;
      startMenu(false);
      const a = item.dataset.nukeSm;
      if(a === "shutdown") closeNuke();
      else if(a === "new") openPicker();
    });
    scr.addEventListener("click", e => {
      if(startMenuEl.classList.contains("on") && !startMenuEl.contains(e.target) && !startBtn.contains(e.target))
        startMenu(false);
    });

    /* ---------- tiny WebAudio "armed" blip (guarded, best-effort) ---------- */
    function beep(){
      try {
        const AC = window.AudioContext || window.webkitAudioContext; if(!AC) return;
        const ac = new AC(), o = ac.createOscillator(), g = ac.createGain();
        o.type = "square"; o.frequency.setValueAtTime(880, ac.currentTime);
        o.frequency.exponentialRampToValueAtTime(220, ac.currentTime + 0.18);
        g.gain.setValueAtTime(0.05, ac.currentTime);
        g.gain.exponentialRampToValueAtTime(0.0001, ac.currentTime + 0.22);
        o.connect(g); g.connect(ac.destination); o.start(); o.stop(ac.currentTime + 0.24);
        setTimeout(() => { try { ac.close(); } catch(_){} }, 400);
      } catch(_){}
    }

    /* ---------- wiring ---------- */
    $("#homeLlmnuke").addEventListener("click", openNuke);
    $("#nukeBack").addEventListener("click", closeNuke);
    $("#nukeHomeClose").addEventListener("click", closeNuke);
    $("#nukeHomeMin").addEventListener("click", closeNuke);
    $("#nukeNew").addEventListener("click", openPicker);
    $("#nukePickX").addEventListener("click", closePicker);
    $("#nukePickBack").addEventListener("click", () => { if(wiz && wiz.step > 0) setStep(wiz.step - 1); });
    $("#nukePickGo").addEventListener("click", armSession);
    $("#nukeCmdX").addEventListener("click", closeCmdSheet);
    $("#nukeCmdDone").addEventListener("click", closeCmdSheet);
    cmdCopy.addEventListener("click", copyCmd);
    // Enter/Space on the role="button" title-bar glyphs
    [["#nukeHomeClose"],["#nukeHomeMin"],["#nukePickX"],["#nukeCmdX"]].forEach(([id]) =>
      $(id).addEventListener("keydown", e => { if(e.key==="Enter"||e.key===" "){ e.preventDefault(); $(id).click(); } }));
    // Escape: close menu → command sheet → wizard → app, in that order of locality
    scr.addEventListener("keydown", e => {
      if(e.key !== "Escape") return;
      e.stopPropagation();
      if(startMenuEl.classList.contains("on")) startMenu(false);
      else if(!cmdDlg.hidden) closeCmdSheet();
      else if(!pick.hidden) closePicker();
      else closeNuke();
    });
  })();
