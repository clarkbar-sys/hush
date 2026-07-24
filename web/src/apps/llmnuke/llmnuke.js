  /* ===== LLMNUKE.EXE — agent sessions, warez edition =========================
     payphone chats a model; LLMNUKE arms an *agent*. You pick a fleet machine,
     the Unix user to run as (filtered by group), and a model, and drop into a
     terminal running opencode as that user — resumable, so you leave a session
     and pick it right back up. This is the proof-of-life build: the whole UX is
     driven by fabricated fleet data so it can be test-driven offline. Against a
     live hush-control backend (GET /api/fleet answers) it parks on a
     LIVE-coming-soon crest instead — the real spawn path (compose a sudo+tmux
     command the way docs/SESSIONS.md does) is deliberately not wired yet.
     Namespaced to this folder; nothing here touches the fleet console. */
  (function(){
    const scr = $("#llmnukeScreen");
    if(!scr) return;

    const boot = $("#nukeBoot"), bootSub = $("#nukeBootSub");
    const soon = $("#nukeSoon"), desk = $("#nukeDesk");
    const sessList = $("#nukeSessList");
    const term = $("#nukeTerm"), termTitle = $("#nukeTermTitle"), route = $("#nukeRoute");
    const logEl = $("#nukeLog"), typingEl = $("#nukeTyping");
    const input = $("#nukeInput"), sendBtn = $("#nukeSend"), warnEl = $("#nukeWarn");
    const pick = $("#nukePick"), pickBody = $("#nukePickBody"), pickHint = $("#nukePickHint");
    const pickSum = $("#nukePickSum"), pickGo = $("#nukePickGo"), pickBack = $("#nukePickBack");
    const steps = $("#nukeSteps");

    /* ---- demo fleet: boxes, their Unix users (with a group to filter on), and
       the models each box serves on the tailnet. opencode on any box can point
       at any online box's model, so the model step lists the whole fleet. ---- */
    const FLEET = [
      { id:"citadel", os:"Debian 12", online:true, ip:"100.71.8.9",
        models:["qwen2.5-coder-32b","llama-3.3-70b"],
        users:[ {name:"root",uid:0,group:"wheel"}, {name:"josh",uid:1000,group:"wheel"},
                {name:"ci",uid:1001,group:"agents"}, {name:"warez",uid:1337,group:"agents"} ] },
      { id:"forge", os:"Arch Linux", online:true, ip:"100.71.2.5",
        models:["deepseek-coder-v2-lite"],
        users:[ {name:"root",uid:0,group:"wheel"}, {name:"ci",uid:1000,group:"agents"},
                {name:"maya",uid:1001,group:"devs"} ] },
      { id:"lab-01", os:"NixOS 24.05", online:true, ip:"100.71.5.7",
        models:["qwen2.5-coder:32b","deepseek-r1:14b"],
        users:[ {name:"root",uid:0,group:"wheel"}, {name:"lab",uid:1000,group:"devs"} ] },
      { id:"pi-gate", os:"Raspberry Pi OS", online:true, ip:"100.71.0.1", models:[],
        users:[ {name:"root",uid:0,group:"wheel"}, {name:"pi",uid:1000,group:"devs"} ] },
      { id:"atlas", os:"Ubuntu 24.04", online:false, ip:"100.71.9.3", models:[],
        users:[ {name:"root",uid:0,group:"wheel"}, {name:"media",uid:1000,group:"devs"} ] },
    ];
    const boxById = id => FLEET.find(b => b.id === id);
    function fleetModels(){
      const out = [];
      FLEET.forEach(b => { if(b.online) b.models.forEach(m => out.push({model:m, host:b.id})); });
      return out;
    }

    /* ---- session store: demo only, so it lives in localStorage (no backend).
       That's what makes "leave a chat and come back like I never left" work
       across reloads without a control node behind the page. ---- */
    const LS_KEY = "llmnuke.sessions.v1", MAX_SESS = 40, MAX_MSGS = 400;
    let SESSIONS = loadSessions();
    let seq = 0;
    function loadSessions(){
      try {
        const raw = localStorage.getItem(LS_KEY);
        const a = raw ? JSON.parse(raw) : [];
        return Array.isArray(a) ? a : [];
      } catch(_){ return []; }
    }
    function saveSessions(){
      try { localStorage.setItem(LS_KEY, JSON.stringify(SESSIONS.slice(0, MAX_SESS))); } catch(_){}
    }
    function newId(){
      seq++;
      return "s" + Date.now().toString(36) + seq.toString(36);
    }
    function touch(s){ s.updated = Date.now(); SESSIONS.sort((a,b)=>b.updated-a.updated); saveSessions(); }

    /* ---- live vs demo: probe /api/fleet once. A 200 JSON array means a real
       hush-control is behind the page → stand down to the coming-soon crest.
       Anything else (404 on the static preview, a network error offline) is the
       legitimate demo fallback — exactly how the fleet console derives its
       demo mode. ---- */
    let LIVE = null;
    async function probeLive(){
      try {
        const r = await fetch("api/fleet", {cache:"no-store"});
        if(!r.ok) return false;
        const d = await r.json();
        return Array.isArray(d);
      } catch(_){ return false; }
    }

    /* ---------- open / close ---------- */
    let clockTimer = null;
    function openNuke(){
      scr.classList.add("on");
      clockTick(); clockTimer = setInterval(clockTick, 15000);
      if(LIVE === null){
        boot.hidden = false; soon.hidden = true; desk.hidden = true;
        const spin = ["pr0b1ng th3 fl33t . . .","c0unt1ng g31g3r t1ckz . . .","ch3ck1ng c0ntr0l pl4n3 . . ."];
        let i = 0; const t = setInterval(()=>{ bootSub.textContent = spin[++i % spin.length]; }, 260);
        Promise.all([probeLive(), sleep(700)]).then(([live]) => {
          clearInterval(t); LIVE = live; paint();
        });
      } else { paint(); }
    }
    function paint(){
      boot.hidden = true;
      if(LIVE){ soon.hidden = false; desk.hidden = true; try{ $("#nukeSoonBack").focus({preventScroll:true}); }catch(_){} return; }
      soon.hidden = true; desk.hidden = false;
      showHome();
      renderSessions();
    }
    function closeNuke(){
      stopAgent();
      startMenu(false);
      pick.hidden = true;
      if(clockTimer){ clearInterval(clockTimer); clockTimer = null; }
      showHome();
      scr.classList.remove("on");
      try { $("#homeLlmnuke").focus({preventScroll:true}); } catch(_){}
    }
    const sleep = ms => new Promise(r => setTimeout(r, ms));

    /* ---------- home: the Active Sessions list ---------- */
    function showHome(){ desk.classList.remove("term-open"); }
    function renderSessions(){
      sessList.innerHTML = "";
      if(!SESSIONS.length){
        const e = document.createElement("div");
        e.className = "nuke-empty";
        e.innerHTML = '<div class="nuke-empty-art" aria-hidden="true">☢</div>'
          + '<div class="nuke-empty-t">N0 4CT1V3 S3SS10NZ</div>'
          + '<div class="nuke-empty-s">y0u ain\'t 0wn1ng n0th1n\' y3t.<br>h1t <b>NUK3 N3W S3SS10N</b> 2 4rm 0n3.</div>';
        sessList.appendChild(e);
        return;
      }
      SESSIONS.forEach(s => {
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
          + '<span class="nuke-sess-kill" role="button" tabindex="0" aria-label="Kill session">×</span>';
        row.querySelector(".nuke-sess-title").textContent = s.title;
        const n = s.messages ? s.messages.filter(m=>m.who==="me"||m.who==="agent").length : 0;
        row.querySelector(".nuke-sess-sub").textContent =
          s.tool + " · " + s.ip + " · " + ago(s.updated) + " · " + n + " lin3z";
        const open = () => openSession(s.id);
        row.addEventListener("click", e => { if(!e.target.closest(".nuke-sess-kill")) open(); });
        row.addEventListener("keydown", e => { if(e.key==="Enter"||e.key===" "){ e.preventDefault(); open(); } });
        const kill = row.querySelector(".nuke-sess-kill");
        kill.addEventListener("click", e => { e.stopPropagation(); killSession(s.id); });
        kill.addEventListener("keydown", e => { if(e.key==="Enter"||e.key===" "){ e.preventDefault(); e.stopPropagation(); killSession(s.id); } });
        sessList.appendChild(row);
      });
    }
    function ago(ts){
      const s = Math.max(0, (Date.now()-ts)/1000);
      if(s < 60) return "n0w";
      if(s < 3600) return Math.floor(s/60)+"m";
      if(s < 86400) return Math.floor(s/3600)+"h";
      return Math.floor(s/86400)+"d";
    }
    function killSession(id){
      SESSIONS = SESSIONS.filter(s => s.id !== id);
      saveSessions();
      if(curSess && curSess.id === id){ curSess = null; showHome(); }
      renderSessions();
    }

    /* ---------- the target selector (machine → user → model) ---------- */
    let wiz = null;
    function openPicker(){
      startMenu(false);
      wiz = { step:0, box:null, user:null, group:"all", model:null, tool:"opencode", llmHost:null };
      pick.hidden = false;
      renderWizard();
      try { pickBody.querySelector(".nuke-row").focus(); } catch(_){}
    }
    function closePicker(){ pick.hidden = true; wiz = null; }
    function setStep(n){ wiz.step = n; renderWizard(); }

    function renderWizard(){
      // step chrome
      Array.from(steps.children).forEach((c,i) => {
        c.classList.toggle("on", i === wiz.step);
        c.classList.toggle("done", i < wiz.step);
      });
      pickBack.hidden = wiz.step === 0;
      pickBody.innerHTML = "";
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

    function renderMachines(){
      pickHint.textContent = "P1ck a b0x 0n th3 fl33t 2 0wn.";
      const boxes = FLEET.slice().sort((a,b)=> (b.online?1:0)-(a.online?1:0));
      boxes.forEach(b => {
        const sub = b.os + " · " + b.ip + " · " + (b.online ? (b.models.length? b.models.length+" m0d3lz" : "n0 l0c4l m0d3l") : "s1gn3d 0ff");
        const el = rowEl(b.online ? "" : "off",
          '<span class="nuke-dot'+(b.online?"":" off")+'" aria-hidden="true"></span>'
          + '<span class="nuke-row-body"><span class="nuke-row-name"></span><span class="nuke-row-sub"></span></span>');
        el.querySelector(".nuke-row-name").textContent = b.id;
        el.querySelector(".nuke-row-sub").textContent = sub;
        if(wiz.box && wiz.box.id === b.id) el.classList.add("sel");
        bindRow(el, () => {
          if(!b.online){ warnPick("th4t b0x 1z s1gn3d 0ff — c4n't 4rm 0n 1t."); return; }
          wiz.box = b; wiz.user = null; wiz.group = "all"; setStep(1);
        });
        pickBody.appendChild(el);
      });
    }

    function renderUsers(){
      pickHint.textContent = "Wh0 d0 w3 run az 0n " + wiz.box.id + "? (f1lt3r by gr0up)";
      // group filter chips
      const groups = ["all"].concat(Array.from(new Set(wiz.box.users.map(u=>u.group))));
      const chips = document.createElement("div"); chips.className = "nuke-chips";
      groups.forEach(g => {
        const c = document.createElement("button"); c.type = "button";
        c.className = "nuke-chip" + (wiz.group===g?" on":""); c.textContent = g==="all"?"all gr0upz":g;
        c.addEventListener("click", () => { wiz.group = g; renderWizard(); });
        chips.appendChild(c);
      });
      pickBody.appendChild(chips);
      const users = wiz.box.users.filter(u => wiz.group==="all" || u.group===wiz.group);
      users.forEach(u => {
        const el = rowEl("",
          '<span class="nuke-row-ico" aria-hidden="true">'+(u.uid===0?"#":"~")+'</span>'
          + '<span class="nuke-row-body"><span class="nuke-row-name"></span><span class="nuke-row-sub"></span></span>'
          + '<span class="nuke-tag'+(u.uid===0?" uid0":"")+'">'+(u.uid===0?"r00t":("uid "+u.uid))+'</span>');
        el.querySelector(".nuke-row-name").textContent = u.name;
        el.querySelector(".nuke-row-sub").textContent = "gr0up: "+u.group+" · sudo -u "+u.name;
        if(wiz.user && wiz.user.name === u.name) el.classList.add("sel");
        bindRow(el, () => { wiz.user = u; setStep(2); });
        pickBody.appendChild(el);
      });
    }

    function renderModels(){
      pickHint.textContent = "P01nt th3 4g3nt @ a m0d3l 0n th3 tailnet.";
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
    }
    function warnPick(msg){ pickHint.textContent = msg; }

    /* ---------- arming a session ---------- */
    function armSession(){
      if(!(wiz && wiz.box && wiz.user && wiz.model)) return;
      const b = wiz.box, u = wiz.user;
      const s = {
        id:newId(), machine:b.id, ip:b.ip, user:u.name, group:u.group,
        tool:wiz.tool, model:wiz.model, llmHost:wiz.llmHost,
        title: u.name+"@"+b.id+" "+(wiz.tool==="claude"?"✳":"⌁")+" "+wiz.model,
        started:Date.now(), updated:Date.now(), messages:[],
      };
      // "hush composes, you run": the session opens on the command hush would
      // hand you (see docs/SESSIONS.md) — theatre here, real shape.
      const cmd = composeCmd(s);
      s.messages.push({who:"sys", text:cmd});
      s.messages.push({who:"sys", text:"[+] 4tt4ch3d 2 tmux::hush-"+s.tool+" 0n "+s.machine+" az "+s.user});
      SESSIONS.unshift(s); saveSessions();
      closePicker();
      beep();
      openSession(s.id, /*fresh*/true);
    }
    function composeCmd(s){
      if(s.tool === "claude"){
        return "sudo -u "+s.user+" -H bash -lc 'exec tmux new-session -A -s hush-claude "
          + "claude --remote-control --dangerously-skip-permissions'";
      }
      return "sudo -u "+s.user+" -H bash -lc '\\\n"
        + "  mkdir -p \"$HOME/.config/opencode\" &&\\\n"
        + "  printf %s \"<base64 opencode.json → "+(s.llmHost||"?")+"/"+s.model+">\" | base64 -d "
        + "> \"$HOME/.config/opencode/opencode.json\" &&\\\n"
        + "  exec tmux new-session -A -s hush-opencode opencode'";
    }

    /* ---------- terminal / agent chat ---------- */
    let curSess = null;
    function openSession(id, fresh){
      const s = SESSIONS.find(x => x.id === id);
      if(!s) return;
      curSess = s;
      termTitle.textContent = (s.tool==="claude"?"✳ ":"⌁ ") + s.tool;
      route.innerHTML = "";
      route.append(
        document.createTextNode("["+s.tool+"] "),
        Object.assign(document.createElement("b"), {textContent:s.user+"@"+s.machine}),
        document.createTextNode("  ::  "+s.model+(s.llmHost?("  ::  "+s.llmHost):""))
      );
      logEl.innerHTML = "";
      (s.messages||[]).forEach(m => appendLine(m.who, m.text));
      typingEl.textContent = "";
      desk.classList.add("term-open");
      warnEl.textContent = "";
      input.value = ""; input.disabled = false; sendBtn.disabled = false;
      scrollLog();
      if(fresh){
        // the agent's opening line — greet in character once, after the attach
        runAgent(greeting(s));
      } else {
        try { input.focus({preventScroll:true}); } catch(_){}
      }
    }
    function appendLine(who, text){
      const d = document.createElement("div");
      d.className = "nuke-line " + who;
      d.textContent = text;
      logEl.appendChild(d);
      return d;
    }
    function scrollLog(){ logEl.scrollTop = logEl.scrollHeight; }

    // --- scripted agent: reveals a run of lines (tool calls + a reply) with
    //     delays so it reads like opencode working. Demo only; no model call. ---
    let agentTimers = [];
    let busy = false;
    function stopAgent(){ agentTimers.forEach(clearTimeout); agentTimers = []; busy = false; }
    function runAgent(lines){
      stopAgent(); busy = true;
      input.disabled = true; sendBtn.disabled = true;
      typingEl.textContent = "◼ 4g3nt 1z th1nk1ng…";
      let t = 380;
      lines.forEach((ln, i) => {
        agentTimers.push(setTimeout(() => {
          typingEl.textContent = i < lines.length-1 ? "◼ 4g3nt 1z w0rk1ng…" : "";
          appendLine(ln.who, ln.text);
          if(curSess){ curSess.messages.push(ln); if(curSess.messages.length > MAX_MSGS) curSess.messages.splice(0, curSess.messages.length-MAX_MSGS); touch(curSess); }
          scrollLog();
        }, t));
        t += ln.who === "tool" ? 520 : 780;
      });
      agentTimers.push(setTimeout(() => {
        typingEl.textContent = ""; busy = false;
        input.disabled = false; sendBtn.disabled = false;
        try { input.focus({preventScroll:true}); } catch(_){}
        renderSessions();
      }, t + 120));
    }
    function greeting(s){
      const where = s.tool === "claude" ? "cl4ud3 c0d3" : "opencode";
      const at = s.llmHost ? (" p01nt3d @ "+s.model+" 0n "+s.llmHost) : "";
      return [
        {who:"sys", text:"[*] tmux 4tt4ch OK — "+where+" 0nl1n3"+at},
        {who:"agent", text:"y0 g0d. "+where+" up 0n "+s.machine+" az "+s.user+". wh4t r w3 h4ck1n 0n t0d4y? (d3m0 m0d3 — n0 r34l wr1t3z l34v3 th1s b0x lol)"},
      ];
    }

    // canned turns — cycle through, with a couple of keyword hooks so it feels
    // like it's listening. Pure theatre for the UX test-drive.
    const TURNS = [
      [ {who:"tool",text:"read src/main.go"}, {who:"tool",text:"grep -rn \"TODO\" ."},
        {who:"agent",text:"sc0p3d th3 tr33. f0und 3 T0D0z n s0m3 sk3tch nil-h4ndl1ng n listen.go. w4nt m3 2 p4tch 1t?"} ],
      [ {who:"tool",text:"edit internal/vitals/top.go"}, {who:"tool",text:"bash: go build ./..."},
        {who:"agent",text:"p4tch3d th3 nil d3r3f n r3-w1r3d th3 gu4rd. bu1ld 1z gr33n ✓. sh1p 1t 0r k33p d1gg1n?"} ],
      [ {who:"tool",text:"read README.md"}, {who:"tool",text:"write docs/NOTES.md"},
        {who:"agent",text:"dr0pp3d s0m3 n0t3z n docs/NOTES.md 4 th3 n00bz. rtfm 1z updated. n3xt m0v3?"} ],
      [ {who:"agent",text:"h3h th4t'z 4 sp1cy 1d34. 1 c0uld r3f4ct0r th3 wh0l3 th1ng but l3t'z n0t nuke pr0duct10n 0n 4 fr1d4y 😏"} ],
    ];
    let turnIx = 0;
    function agentReply(userText){
      const t = userText.toLowerCase();
      if(/\b(test|tests|t3st)\b/.test(t))
        return [ {who:"tool",text:"bash: go test ./..."},
                 {who:"agent",text:"r4n th3 su1t3 — 4ll gr33n, 0 f41l. cl34n az h3ck. c0mm1t?"} ];
      if(/\b(commit|push|pr|ship|merge|deploy)\b/.test(t))
        return [ {who:"tool",text:"git status"},
                 {who:"agent",text:"1'd br4nch n 0p3n a PR here — but th1z 1z d3m0 m0d3, s0 n0 r34l c0mm1tz g0 0ut. 0n a l1v3 b0x th1z 1z wh3r3 hush h4ndz y0u th3 sudo l1n3 n u run 1t. 😎"} ];
      if(/\b(build|comp1l3|compile|make)\b/.test(t))
        return [ {who:"tool",text:"bash: go build ./..."},
                 {who:"agent",text:"bu1lt cl34n, n0 3rr0rz. b1n4ry'z w4rm. wh4t n3xt?"} ];
      const r = TURNS[turnIx % TURNS.length]; turnIx++;
      return r;
    }
    function sendMsg(){
      if(busy || !curSess) return;
      const v = input.value.trim();
      if(!v){ warnEl.textContent = "typ3 s0m3th1ng 4 th3 4g3nt f1rst."; return; }
      warnEl.textContent = "";
      const me = {who:"me", text:v};
      curSess.messages.push(me); touch(curSess);
      appendLine("me", v); scrollLog();
      input.value = "";
      runAgent(agentReply(v));
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
      else if(a === "new"){ if(!LIVE){ showHome(); openPicker(); } }
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
    $("#nukeSoonBack").addEventListener("click", closeNuke);
    $("#nukeHomeClose").addEventListener("click", closeNuke);
    $("#nukeHomeMin").addEventListener("click", closeNuke);
    $("#nukeNew").addEventListener("click", openPicker);
    $("#nukePickX").addEventListener("click", closePicker);
    $("#nukePickBack").addEventListener("click", () => { if(wiz && wiz.step > 0) setStep(wiz.step - 1); });
    $("#nukePickGo").addEventListener("click", armSession);
    $("#nukeTermClose").addEventListener("click", () => { curSess = null; stopAgent(); showHome(); renderSessions(); });
    $("#nukeTermMin").addEventListener("click", () => { stopAgent(); showHome(); renderSessions(); });
    sendBtn.addEventListener("click", sendMsg);
    input.addEventListener("keydown", e => { if(e.key==="Enter" && !e.shiftKey){ e.preventDefault(); sendMsg(); } });
    // Enter/Space on the role="button" title-bar glyphs
    [["#nukeHomeClose"],["#nukeHomeMin"],["#nukePickX"],["#nukeTermClose"],["#nukeTermMin"]].forEach(([id]) =>
      $(id).addEventListener("keydown", e => { if(e.key==="Enter"||e.key===" "){ e.preventDefault(); $(id).click(); } }));
    // Escape: close menu → wizard → terminal → app, in that order of locality
    scr.addEventListener("keydown", e => {
      if(e.key !== "Escape") return;
      e.stopPropagation();
      if(startMenuEl.classList.contains("on")) startMenu(false);
      else if(!pick.hidden) closePicker();
      else if(desk.classList.contains("term-open")){ curSess = null; stopAgent(); showHome(); }
      else closeNuke();
    });
  })();
