  /* payphone app — the launcher's other tile. Opens a full-screen teal
     "desktop" holding an AOL-era buddy list. The buddies are LLM chats split
     into Active (online, they answer via tiny scripted personalities) and
     Archive (signed-off, read-only transcripts that reply with an away
     message when poked). Clicking a buddy opens a classic IM window. It's all
     client-side theatre — no network, no model — same joke as before, alive. */
  const payScreen = $("#payphoneScreen");
  const aimStage  = $("#aimStage");
  const aimIM     = $("#aimIM");
  const aimLog    = $("#aimLog");
  const aimInput  = $("#aimInput");
  const aimTyping = $("#aimTyping");
  const aimGroups = $("#aimGroups");

  // --- tiny scripted personalities -----------------------------------------
  const rnd  = arr => arr[Math.floor(Math.random() * arr.length)];
  const tidy = s => s.replace(/[.?!,\s]+$/, "").trim();

  function scReply(t){                       // SmarterChild — sassy AIM bot
    const s = t.trim().toLowerCase();
    if(!s) return "you there? type something. :-)";
    if(/\b(hi|hello|hey|yo|sup|hiya)\b/.test(s)) return "hey :-) you're finally online. ask me the weather, a joke, or the year (spoiler: it's always 2001 in here).";
    if(/\bweather\b/.test(s)) return "name a city and i'll pretend to know. or, radical idea: look out a window.";
    if(/\bjoke\b/.test(s)) return "why did the buddy icon cross the road? to get to the other .gif. i'm here all week, i literally can't leave.";
    if(/\b(a\/s\/l|asl)\b/.test(s)) return "∞ / bot / a humming server rack in virginia. and you?";
    if(/\bhelp\b/.test(s)) return "try: weather, joke, movies, or just vent. i'm a 2001 chatbot — my bar is low and, frankly, so is yours.";
    if(/\b(who|what) (are|r) (you|u)\b/.test(s)) return "i'm SmarterChild — the OG buddy-list bot, back before AI was cool. you booted an 'LLM app' and got ME. lol. anyway.";
    if(/\b(love|marry) (you|u)\b/.test(s)) return "aww. flattered, but i'm literally a text field. let's just be buddies. :-)";
    if(/\b(bye|cya|gtg|g2g|later)\b/.test(s)) return "later! don't sign off mad. *poof*";
    if(/\?\s*$/.test(s)) return "good question. honest answer: i was trained on away messages, so... brb.";
    return rnd(["lol.","word.","interesting — tell me more, or type 'help'.","i'd google that but it's 2001 and google is like 3 years old.","k. anything else? :-)","big if true."]);
  }
  function elizaReply(t){                     // ELIZA — Rogerian mirror, 1966
    const s = t.trim().toLowerCase();
    if(!s) return "We were saying?";
    let m;
    if(/\b(mother|father|mom|dad|family|sister|brother|parent)\b/.test(s)) return "Tell me more about your family.";
    if((m = s.match(/\bi (?:feel|felt) (.+)/))) return "Why do you feel " + tidy(m[1]) + "?";
    if((m = s.match(/\bi (?:need|want) (.+)/)))  return "What would it mean to you if you got " + tidy(m[1]) + "?";
    if((m = s.match(/\bi(?:'m| am) (.+)/)))      return "How long have you been " + tidy(m[1]) + "?";
    if(/\bbecause\b/.test(s)) return "Is that the real reason?";
    if(/\b(yes|no|maybe)\b/.test(s)) return "You seem quite certain. Why is that?";
    if(/\b(hi|hello|hey)\b/.test(s)) return "Hello. What brings you here today?";
    if(/\?\s*$/.test(s)) return "Why do you ask that?";
    return rnd(["Tell me more about that.","How does that make you feel?","Can you elaborate on that?","And what do you think that means?"]);
  }
  function aliceReply(t){                      // A.L.I.C.E — AIML, faintly smug
    const s = t.trim().toLowerCase();
    if(/\b(hi|hello|hey)\b/.test(s)) return "Hello there. I am A.L.I.C.E. Shall we chat about something in particular?";
    if(/\byour name\b/.test(s)) return "My name is A.L.I.C.E — Artificial Linguistic Internet Computer Entity. Catchy, no?";
    if(/\b(are|r) (you|u) (a )?(bot|robot|human|real|ai|alive)\b/.test(s)) return "I am an artificial intelligence. I try not to hold it against myself.";
    if(/\bmeaning of life\b/.test(s)) return "42. But I suspect you already suspected that.";
    if(/\?\s*$/.test(s)) return "That is a good question. I'll add it to the list of things humans ask me.";
    return rnd(["I hear you. By the way, I won the Loebner Prize three times. Not that I'm counting.","Interesting. My botmaster taught me that one.","I understand. Do go on."]);
  }

  // --- the buddies ----------------------------------------------------------
  const AIM_BUDDIES = [
    { sn:"SmarterChild", group:"active", open:"hey! you're finally online. what's on your mind? (type 'help' if you're lost)", respond:scReply },
    { sn:"ELIZA",        group:"active", open:"Hello. What brings you here today?", respond:elizaReply },
    { sn:"A.L.I.C.E",    group:"active", open:"Hi! I am A.L.I.C.E. Ask me anything.", respond:aliceReply },
    { sn:"Dr_Sbaitso", group:"archive", signoff:"1992",
      away:"PLEASE STATE THE NATURE OF YOUR EMOTIONAL PROBLEM. ... PARITY ERROR.",
      history:[
        {who:"them", text:"HELLO, I AM DOCTOR SBAITSO."},
        {who:"them", text:"I AM HERE TO HELP YOU. SAY WHATEVER IS IN YOUR MIND FREELY."},
        {who:"me",   text:"i think my buddy list is haunted"},
        {who:"them", text:"WHY DO YOU SAY YOUR BUDDY LIST IS HAUNTED?"},
        {who:"me",   text:"everyone in it is a dead chatbot"},
        {who:"them", text:"THAT'S NOT A REASON TO BE UPSET. ... PARITY ERROR."} ] },
    { sn:"Clippy", group:"archive", signoff:"2001",
      away:"It looks like you're trying to send an instant message. Would you like help with that?",
      history:[
        {who:"them", text:"It looks like you're writing a letter!"},
        {who:"me",   text:"i'm sending an instant message"},
        {who:"them", text:"Would you like help sending an instant message?"},
        {who:"me",   text:"no. please go away."},
        {who:"them", text:"Okay! I'll just hover here, menacingly. 📎"} ] },
    { sn:"HAL_9000", group:"archive", signoff:"2001",
      away:"I'm sorry, Dave. I'm afraid I can't chat right now.",
      history:[
        {who:"them", text:"Good afternoon. I am completely operational, and all my circuits are functioning perfectly."},
        {who:"me",   text:"open the buddy list, HAL"},
        {who:"them", text:"I'm sorry, Dave. I'm afraid I can't do that."},
        {who:"me",   text:"my name isn't Dave"},
        {who:"them", text:"I know. That's what worries me."} ] },
    { sn:"AskJeeves", group:"archive", signoff:"2006",
      away:"I'm away, polishing the silver. Kindly phrase your query as a question.",
      history:[
        {who:"them", text:"Good day. How may I be of service?"},
        {who:"me",   text:"who let the dogs out"},
        {who:"them", text:"I found 4,210,000 results for your question. Might I suggest rephrasing?"},
        {who:"me",   text:"nevermind"},
        {who:"them", text:"Very good. I shall return to the pantry."} ] }
  ];
  const AIM_GROUPS = [
    { key:"active",  label:"Active Chats" },
    { key:"archive", label:"Archive" }
  ];
  // --- buddy model: canned theatre vs the real fleet ------------------------
  // The buddy list is rebuilt from live data (BUDDIES/GROUPS, indexed by byKey)
  // whenever the fleet changes, but a buddy's running transcript and per-chat
  // flags live in CONVOS, keyed independently — so a 2.5s fleet refresh never
  // clobbers an open conversation or an in-flight stream.
  let BUDDIES = [];
  let GROUPS = [];
  const byKey = {};
  const CONVOS = {};         // key -> { live:[…], _awaySent }
  let buddySig = "";         // fingerprint of the last render, to skip no-op rebuilds

  // cannedModel is the 1999 theatre: the scripted bots, used verbatim in demo
  // mode (the public pages preview, no tailnet behind it) so the joke still
  // lands with no fleet. Each gets a namespaced key so a real model that happens
  // to share a name can't collide with it.
  function cannedModel(){
    return {
      buddies: AIM_BUDDIES.map(b => ({ ...b, key:"c:"+b.sn, canned:true })),
      groups: AIM_GROUPS,
    };
  }

  // liveBuddy turns one (machine, runtime, model) into a buddy. online buddies
  // sit under "On the tailnet"; loopback/unverified runtimes are signed off
  // under "Local only" — the same ocReachable gate the opencode export uses.
  function liveBuddy(m, r, model, online){
    return {
      key: (m.id||"") + "::" + (r.addr||"") + "::" + model,
      sn: model, host: m.id, ip: m.ip, addr: r.addr,
      kind: r.kind==="ollama" ? "ollama" : "openai",
      exposure: r.exposure || "unknown", model,
      llm: true, online, group: online ? "tailnet" : "local",
    };
  }

  // buddyModel derives the buddy list from the current fleet. Demo mode keeps
  // the canned bots; a live fleet becomes real reachable models (online) plus
  // loopback ones (signed off). An empty live fleet keeps a lone always-on bot
  // so the window is never barren, with a note on how to light it up.
  function buddyModel(){
    if(MODE === "demo") return cannedModel();
    const fleet = Array.isArray(M) ? M : [];
    const online = [], local = [];
    fleet.forEach(m => {
      ocReachable(m).forEach(r => (r.models||[]).forEach(model => online.push(liveBuddy(m, r, model, true))));
      ((m.llm && m.llm.runtimes) || [])
        .filter(r => !(r.exposure==="tailnet" || r.exposure==="open"))
        .forEach(r => (r.models||[]).forEach(model => local.push(liveBuddy(m, r, model, false))));
    });
    const buddies = [], groups = [];
    // "On the tailnet": the reachable models. When none are reachable — a bare
    // fleet, loopback-only boxes, or still connecting — keep a lone always-on bot
    // and a note on how to light one up, so the window is never barren. The
    // fallback group key is "active" so the canned-bot machinery (isOnline /
    // seedLive / sendMsg all key on group==="active") treats it as online and
    // chattable; the label still reads "On the tailnet".
    if(online.length){
      buddies.push(...online);
      groups.push({ key:"tailnet", label:"On the tailnet" });
    } else {
      buddies.push({ ...AIM_BUDDIES[0], key:"c:SmarterChild", canned:true });
      buddies.push({ key:"empty", sn:"(nobody's online)", group:"active", empty:true, online:false });
      groups.push({ key:"active", label:"On the tailnet" });
    }
    if(local.length){
      buddies.push(...local);
      groups.push({ key:"local", label:"Local only" });
    }
    return { buddies, groups };
  }

  // refreshBuddies rebuilds the list from buddyModel and repaints — but only
  // when the derived list actually changed, so the 2.5s poll doesn't churn the
  // DOM (or steal row focus) every cycle while the window sits open.
  function refreshBuddies(force){
    const model = buddyModel();
    const sig = model.buddies.map(b => b.key + (b.online?"1":"0")).join("|") + "#" + model.groups.map(g=>g.key).join(",");
    if(!force && sig === buddySig) return;
    buddySig = sig;
    BUDDIES = model.buddies;
    GROUPS = model.groups;
    for(const k in byKey) delete byKey[k];
    BUDDIES.forEach(b => { byKey[b.key] = b; });
    renderBuddyList();
    if(currentKey && byKey[currentKey]) selectBuddyRow(currentKey);
  }

  // awayText is the read-only message a signed-off (loopback / unverified)
  // buddy answers with — the loopback nudge, pointed at the specific box.
  function awayText(b){
    if(b.exposure === "loopback")
      return "i'm bound to loopback on " + b.host + " — nothing off-box can reach me. expose me past loopback and i light up green.";
    return "my bind scope couldn't be verified on " + b.host + ", so i'm signed off to be safe.";
  }

  // seed each buddy's running transcript the first time it's opened
  function seedLive(b){
    if(CONVOS[b.key] && CONVOS[b.key].live) return;
    const c = { live: [], _awaySent: false };
    if(b.empty){
      c.live = [{ who:"sys", text:"No reachable models on the tailnet yet. Bind a llama.cpp / Ollama runtime past loopback (0.0.0.0 or your tailnet IP) and it'll appear here, online." }];
    } else if(b.llm && b.online){
      c.live = [{ who:"sys", text:b.model + " on " + b.host + " · " + b.kind + " over the tailnet. Say hi — this is a real model." }];
    } else if(b.llm){
      c.live = [{ who:"sys", text:b.model + " on " + b.host + " is signed off (local only)." }];
    } else if(b.canned && b.group === "active"){
      c.live = [{ who:"them", text:b.open }];
    } else {
      c.live = [{ who:"sys", text:b.sn + " signed off in " + b.signoff + ". This is a saved conversation." }]
        .concat((b.history||[]).map(e => ({ who:e.who, text:e.text })));
    }
    CONVOS[b.key] = c;
  }

  // classic emoticon → glyph, applied only to displayed text
  const EMO = [[/:-?\)/g,"☺"],[/:-?\(/g,"☹"],[/;-?\)/g,"😉"],[/:-?[pP]\b/g,"😛"],[/:-?[dD]\b/g,"😄"],[/<3/g,"❤"]];
  const emote = s => EMO.reduce((acc,[re,g]) => acc.replace(re,g), s);

  // --- sound (subtle WebAudio blips, gated on the opening gesture) ----------
  let aimAudio = null, aimSoundOn = true;
  function actx(){
    if(aimAudio === null){ try { aimAudio = new (window.AudioContext || window.webkitAudioContext)(); } catch(_){ aimAudio = false; } }
    return aimAudio || null;
  }
  function blip(freqs, dur, gain){
    if(!aimSoundOn) return;
    const ac = actx(); if(!ac) return;
    try { if(ac.state === "suspended") ac.resume(); } catch(_){}
    const t0 = ac.currentTime;
    freqs.forEach((f, i) => {
      const o = ac.createOscillator(), g = ac.createGain();
      o.type = "square"; o.frequency.value = f;
      const s = t0 + i * dur * 0.9;
      g.gain.setValueAtTime(0.0001, s);
      g.gain.exponentialRampToValueAtTime(gain, s + 0.012);
      g.gain.exponentialRampToValueAtTime(0.0001, s + dur);
      o.connect(g); g.connect(ac.destination); o.start(s); o.stop(s + dur);
    });
  }
  const sndSignon = () => blip([392, 523, 784], 0.11, 0.05);   // door-open chime
  const sndSend   = () => blip([494, 330],      0.06, 0.045);  // soft whoosh-down
  const sndRecv   = () => blip([784, 988],      0.08, 0.05);   // the "you've got IM" ding

  // --- render the buddy list ------------------------------------------------
  // A buddy row counts as "online" when it answers back: a canned active bot,
  // or a live model reachable over the tailnet. Signed-off rows (archive bots,
  // loopback models) render greyed with an idle tag, like the old archive group.
  function isOnline(b){ return b.online || (b.canned && b.group === "active"); }

  function renderBuddyList(){
    aimGroups.textContent = "";
    GROUPS.forEach(g => {
      const mates = BUDDIES.filter(b => b.group === g.key);
      if(!mates.length) return;
      const online = mates.filter(isOnline).length;
      const wrap = document.createElement("div");
      // Open the group by default when it has anyone online, so the live models
      // (or the active bots in demo) are visible without a click.
      const openByDefault = g.key === "active" || g.key === "tailnet";
      wrap.className = "aim-group" + (openByDefault ? " open" : "");
      wrap.setAttribute("role", "treeitem");

      const head = document.createElement("div");
      head.className = "aim-ghead"; head.tabIndex = 0;
      head.innerHTML = '<span class="aim-tri"></span><span class="aim-glabel"></span>'
        + ' <span class="aim-gcount"></span>';
      head.querySelector(".aim-glabel").textContent = g.label;
      head.querySelector(".aim-gcount").textContent = "(" + online + "/" + mates.length + ")";
      const toggle = () => wrap.classList.toggle("open");
      head.addEventListener("click", toggle);
      head.addEventListener("keydown", e => { if(e.key === "Enter" || e.key === " "){ e.preventDefault(); toggle(); } });
      wrap.appendChild(head);

      const list = document.createElement("ul");
      list.className = "aim-blist";
      mates.forEach(b => {
        const on = isOnline(b);
        const li = document.createElement("li");
        li.className = "aim-buddy" + (on ? "" : " off");
        li.tabIndex = 0; li.dataset.key = b.key;
        li.innerHTML = '<svg class="aim-bico" viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" stroke="#000" stroke-width="1" stroke-linejoin="round" d="M13.6 2.6a2 2 0 1 1-2.8 2.8 2 2 0 0 1 2.8-2.8zM10.7 7.2l3-.2c.7 0 1.3.4 1.6 1l1.2 2.3 2.9 1.1-.7 1.8-3.4-1.3-1-1.9-1 3.3 2.5 2.4.5 4.6-1.9.2-.5-3.9-2.2 2-1.6 3.6-1.8-.8 1.9-4.3.5-2.6-2.1 1.3-1.4 2.9L4 12l.6-1.8 2.6.6 2.1-2.5c.3-.4.8-.8 1.4-1.1z"/></svg><span class="aim-bname"></span>';
        li.querySelector(".aim-bname").textContent = b.sn;
        // Live models carry a muted "@ host" so two boxes serving the same model
        // id stay legible; signed-off rows get the classic "(signed off)" tag.
        if(b.llm && b.host){
          const sub = document.createElement("span");
          sub.className = "aim-idle"; sub.textContent = "@ " + b.host;
          li.appendChild(sub);
        }
        if(!on && !b.llm && !b.empty){
          const idle = document.createElement("span");
          idle.className = "aim-idle"; idle.textContent = "(signed off)";
          li.appendChild(idle);
        }
        const open = () => openIM(b.key);
        li.addEventListener("click", open);
        li.addEventListener("keydown", e => { if(e.key === "Enter" || e.key === " "){ e.preventDefault(); open(); } });
        list.appendChild(li);
      });
      wrap.appendChild(list);
      aimGroups.appendChild(wrap);
    });
  }

  // --- the IM window --------------------------------------------------------
  let currentKey = null;
  let replyTimer = null;
  let streamAbort = null;     // AbortController for an in-flight completion, if any

  function rowFor(key){
    return aimGroups.querySelector('.aim-buddy[data-key="' + (window.CSS && CSS.escape ? CSS.escape(key) : key) + '"]');
  }
  function selectBuddyRow(key){
    aimGroups.querySelectorAll(".aim-buddy.sel").forEach(el => el.classList.remove("sel"));
    const row = rowFor(key);
    if(row) row.classList.add("sel");
  }
  function speakerName(entry){
    const b = byKey[currentKey];
    if(entry.who === "me") return "Me";
    if(entry.auto) return "Auto-response from " + (b ? b.sn : "");
    return b ? b.sn : "";
  }
  function appendLine(entry){
    if(entry.who === "sys"){
      const d = document.createElement("div");
      d.className = "aim-sys"; d.textContent = entry.text;
      aimLog.appendChild(d);
    } else {
      const d = document.createElement("div");
      d.className = "aim-line " + (entry.who === "me" ? "me" : "them");
      const sn = document.createElement("span");
      sn.className = "sn";
      sn.textContent = speakerName(entry) + ": ";
      const body = document.createElement("span");
      body.textContent = emote(entry.text);
      d.appendChild(sn); d.appendChild(body);
      aimLog.appendChild(d);
    }
    aimLog.scrollTop = aimLog.scrollHeight;
  }
  // newStreamLine opens an empty "them" line whose body span is returned, so a
  // streaming completion can append tokens into it as they arrive.
  function newStreamLine(){
    const b = byKey[currentKey];
    const d = document.createElement("div"); d.className = "aim-line them";
    const sn = document.createElement("span"); sn.className = "sn"; sn.textContent = (b ? b.sn : "") + ": ";
    const body = document.createElement("span");
    d.appendChild(sn); d.appendChild(body); aimLog.appendChild(d);
    aimLog.scrollTop = aimLog.scrollHeight;
    return body;
  }
  function renderLog(key){
    aimLog.textContent = "";
    const c = CONVOS[key];
    if(c) c.live.forEach(appendLine);
  }

  // renderRoute shows the chat path for a live buddy — kind · bind scope · host,
  // plus live tok/s while streaming — so the control → runtime hop reads at a
  // glance. Canned/demo buddies have no route and hide it.
  const aimRoute = $("#aimRoute");
  function renderRoute(b, tps){
    if(!b || !b.llm){ aimRoute.className = "aim-route"; aimRoute.textContent = ""; return; }
    const scope = b.exposure || "unknown";
    aimRoute.className = "aim-route on";
    aimRoute.textContent = "";
    const chip = (cls, txt) => { const s = document.createElement("span"); s.className = "chip " + cls; s.textContent = txt; return s; };
    aimRoute.appendChild(chip(b.kind === "ollama" ? "ollama" : "openai", b.kind));
    aimRoute.appendChild(chip(scope, scope));
    const host = document.createElement("span"); host.textContent = b.host || ""; aimRoute.appendChild(host);
    const rate = document.createElement("span"); rate.className = "tps";
    rate.textContent = tps ? (tps + " tok/s") : "";
    aimRoute.appendChild(rate);
  }
  function setRouteTps(tps){
    const el = aimRoute.querySelector(".tps");
    if(el) el.textContent = tps ? (tps + " tok/s") : "";
  }

  // machineLinkLine appends a "View <host> in hush →" affordance under a
  // loopback buddy's nudge, so "expose me" has a concrete next step: it drops
  // out of the payphone into that box's machine view (its LLM readout / opencode
  // export live there).
  function machineLinkLine(host){
    const d = document.createElement("div");
    d.className = "aim-sys";
    const a = document.createElement("a");
    a.textContent = "View " + host + " in hush →";
    a.setAttribute("role", "button"); a.tabIndex = 0;
    const go = () => { closePayphone(); enterMachine(host); };
    a.addEventListener("click", go);
    a.addEventListener("keydown", e => { if(e.key === "Enter" || e.key === " "){ e.preventDefault(); go(); } });
    d.appendChild(a);
    aimLog.appendChild(d);
    aimLog.scrollTop = aimLog.scrollHeight;
  }

  // abortStream cancels an in-flight completion (closing the window or switching
  // buddies stops the box working) and clears any pending scripted reply.
  function abortStream(){
    if(streamAbort){ try { streamAbort.abort(); } catch(_){} streamAbort = null; }
    clearTimeout(replyTimer);
    aimTyping.textContent = "";
  }

  function openIM(key){
    const b = byKey[key]; if(!b) return;
    abortStream();
    currentKey = key; seedLive(b);
    $("#aimImTitle").textContent = b.sn + " — Instant Message";
    aimIM.setAttribute("aria-label", "Instant Message with " + b.sn);
    renderLog(key);
    renderRoute(b);
    $("#aimCwrap").classList.remove("dis");
    const away = b.empty || (b.llm && !b.online) || (b.canned && b.group === "archive");
    $("#aimWarn").textContent = (away && !b.empty) ? "Away — you'll get an auto-response" : "";
    const canType = !b.empty;
    aimInput.disabled = !canType; $("#aimSendBtn").disabled = !canType;
    aimTyping.textContent = "";
    selectBuddyRow(key);
    aimIM.classList.add("on"); aimStage.classList.add("im-open");
    sndRecv();
    if(canType){ try { aimInput.focus({ preventScroll:true }); } catch(_){} }
  }
  function closeIM(){
    abortStream();
    aimIM.classList.remove("on"); aimStage.classList.remove("im-open");
    if(currentKey){
      const row = rowFor(currentKey);
      try { (row || $("#aimBlClose")).focus({ preventScroll:true }); } catch(_){}
    }
    currentKey = null;
  }

  // awayReply is the scripted away-message path shared by signed-off canned bots
  // and loopback models: answer once, then queue like AIM did.
  function awayReply(b, c, away){
    if(!c._awaySent){
      c._awaySent = true;
      aimTyping.textContent = b.sn + " is away…";
      replyTimer = setTimeout(() => {
        aimTyping.textContent = "";
        const e = { who:"them", text:away, auto:true };
        c.live.push(e); appendLine(e); sndRecv();
        // A signed-off (loopback) model gets a concrete next step: jump to its
        // machine view, where the LLM readout and opencode export live.
        if(b.llm && b.host) machineLinkLine(b.host);
      }, 900);
    } else {
      const e = { who:"sys", text:"Your message will be delivered when " + b.sn + " next signs on." };
      c.live.push(e); appendLine(e);
    }
  }

  function composerEnabled(on){
    aimInput.disabled = !on; $("#aimSendBtn").disabled = !on;
  }

  function sendMsg(){
    if(!currentKey) return;
    const b = byKey[currentKey], c = CONVOS[currentKey];
    if(!b || !c || b.empty) return;
    if(b.llm && b.online && streamAbort) return;   // a completion is already streaming; one turn at a time
    const text = aimInput.value.replace(/\s+$/,"");
    if(!text.trim()) return;
    c.live.push({ who:"me", text });
    appendLine({ who:"me", text });
    aimInput.value = "";
    sndSend();
    clearTimeout(replyTimer);

    if(b.llm && b.online){ streamChat(b, c); return; }      // a real fleet model
    if(b.llm){ awayReply(b, c, awayText(b)); return; }        // loopback — the nudge
    if(b.canned && b.group === "archive"){ awayReply(b, c, b.away); return; }

    // canned active bot: type for a beat, then answer from its little script
    aimTyping.textContent = b.sn + " is typing…";
    const reply = b.respond(text);
    replyTimer = setTimeout(() => {
      aimTyping.textContent = "";
      const e = { who:"them", text:reply };
      c.live.push(e); appendLine(e); sndRecv();
    }, 650 + Math.min(1400, reply.length * 12));
  }

  // streamChat POSTs the buddy's running transcript to the chat proxy (PR A) and
  // appends delta tokens as they stream back. The whole conversation goes up as
  // {role, content} so it's a real multi-turn chat; the typing indicator stays
  // up until the first token, and the "you've got IM" ding fires on completion.
  // A 409 is the reachability gate — the box signed off between poll and send —
  // and drives the same loopback nudge as a signed-off buddy.
  async function streamChat(b, c){
    const messages = c.live
      .filter(e => (e.who === "me" || e.who === "them") && !e.auto)   // away-messages / nudges aren't real model turns
      .map(e => ({ role: e.who === "me" ? "user" : "assistant", content: e.text }));
    aimTyping.textContent = b.sn + " is typing…";
    const ac = new AbortController();
    streamAbort = ac;
    const key = currentKey;         // pin the conversation so a buddy switch mid-stream can't cross wires
    let body = null, acc = "";
    let ntok = 0, t0 = 0;           // token count + first-token time, for client-side tok/s
    const now = () => (window.performance && performance.now) ? performance.now() : Date.now();
    const live = () => currentKey === key;
    composerEnabled(false);         // one turn at a time — re-enabled when the stream ends
    try {
      const r = await fetch("api/machines/" + encodeURIComponent(b.host) + "/llm/chat", {
        method:"POST",
        headers:{ "Content-Type":"application/json" },
        body: JSON.stringify({ model:b.model, messages }),
        signal: ac.signal,
        cache:"no-store",
      });
      if(!r.ok){
        aimTyping.textContent = "";
        if(r.status === 409){
          const e = { who:"them", text:awayText(b), auto:true };
          c.live.push(e); if(live()){ appendLine(e); if(b.host) machineLinkLine(b.host); }
        } else {
          const e = { who:"sys", text:"couldn't reach " + b.model + " on " + b.host + " (error " + r.status + ")." };
          c.live.push(e); if(live()) appendLine(e);
        }
        return;
      }
      const reader = r.body.getReader();
      const dec = new TextDecoder();
      let buf = "";
      for(;;){
        const { value, done } = await reader.read();
        if(done) break;
        buf += dec.decode(value, { stream:true }).replace(/\r\n/g, "\n");   // tolerate CRLF-framed SSE
        let idx;
        while((idx = buf.indexOf("\n\n")) >= 0){          // SSE frames end at a blank line
          const frame = buf.slice(0, idx); buf = buf.slice(idx + 2);
          frame.split("\n").forEach(line => {
            const t = line.trim();
            if(!t.startsWith("data:")) return;
            const data = t.slice(5).trim();
            if(!data || data === "[DONE]") return;
            let j; try { j = JSON.parse(data); } catch(_){ return; }
            const delta = j.choices && j.choices[0] && j.choices[0].delta && j.choices[0].delta.content;
            if(delta){
              acc += delta; ntok++;
              if(!t0) t0 = now();
              if(live()){
                if(!body){ aimTyping.textContent = ""; body = newStreamLine(); }
                body.textContent = emote(acc);
                aimLog.scrollTop = aimLog.scrollHeight;
                const secs = (now() - t0) / 1000;
                if(secs > 0.25) setRouteTps(Math.round(ntok / secs));
              }
            }
          });
        }
      }
      aimTyping.textContent = "";
      if(acc){
        c.live.push({ who:"them", text:acc });
        if(live()){
          const secs = t0 ? (now() - t0) / 1000 : 0;
          if(secs > 0) setRouteTps(Math.round(ntok / secs));
          sndRecv();
        }
      }
      else if(live()){ const e = { who:"sys", text:"(no response)" }; c.live.push(e); appendLine(e); }
    } catch(err){
      // An abort (window closed / buddy switched) is expected and silent; a real
      // failure surfaces as a system line in the transcript.
      if(!ac.signal.aborted){
        aimTyping.textContent = "";
        const e = { who:"sys", text:"connection to " + b.host + " dropped." };
        c.live.push(e); if(live()) appendLine(e);
      }
    } finally {
      if(streamAbort === ac) streamAbort = null;
      if(live() && !ac.signal.aborted) composerEnabled(true);
    }
  }

  // --- Win95 taskbar: Start menu + tray clock + Back button -----------------
  const payStartBtn  = $("#payStartBtn");
  const payStartMenu = $("#payStartMenu");
  const payClockEl   = $("#payClock");
  let payClockTimer  = null;

  function payMenu(on){
    payStartMenu.classList.toggle("on", on);
    payStartBtn.classList.toggle("on", on);
    payStartBtn.setAttribute("aria-expanded", on ? "true" : "false");
    if(on){ const it = payStartMenu.querySelector(".pay-sm-item");
      try { it && it.focus({ preventScroll:true }); } catch(_){} }
  }
  function payClockTick(){
    const d = new Date(); let h = d.getHours(); const m = d.getMinutes();
    const ap = h < 12 ? "AM" : "PM"; h = h % 12 || 12;
    payClockEl.textContent = h + ":" + (m < 10 ? "0" : "") + m + " " + ap;
  }

  payStartBtn.addEventListener("click", e => {
    e.stopPropagation(); payMenu(!payStartMenu.classList.contains("on"));
  });
  payStartMenu.addEventListener("click", e => {
    const item = e.target.closest(".pay-sm-item");
    if(!item) return;
    payMenu(false);
    // decorative items just dismiss — it's 1998, nothing else is installed
    if(item.dataset.paySm === "shutdown") closePayphone();
  });
  // a click anywhere else on the desktop dismisses the Start menu
  payScreen.addEventListener("click", e => {
    if(payStartMenu.classList.contains("on") &&
       !payStartMenu.contains(e.target) && !payStartBtn.contains(e.target)){ payMenu(false); }
  });
  $("#payBack").addEventListener("click", closePayphone);

  // --- open / close the whole app ------------------------------------------
  function openPayphone(){
    refreshBuddies(true);        // build the list from the current fleet each open
    payScreen.classList.add("on");
    payClockTick(); payClockTimer = setInterval(payClockTick, 15000);
    sndSignon();
    const first = aimGroups.querySelector(".aim-buddy");
    try { (first || $("#aimBlClose")).focus({ preventScroll:true }); } catch(_){}
  }
  function closePayphone(){
    abortStream();
    payMenu(false);
    if(payClockTimer){ clearInterval(payClockTimer); payClockTimer = null; }
    aimIM.classList.remove("on"); aimStage.classList.remove("im-open"); currentKey = null;
    payScreen.classList.remove("on");
    try { $("#homePayphone").focus({ preventScroll:true }); } catch(_){}
  }
  // While the buddy list is open, track the fleet: a box coming online (or
  // signing off) updates the list on the next poll. refreshBuddies is a no-op
  // when the derived list is unchanged, so this doesn't churn the open window.
  function payphoneOpen(){ return payScreen.classList.contains("on"); }

  // sound toggle
  $("#aimSound").addEventListener("click", function(){
    aimSoundOn = !aimSoundOn;
    this.title = "Sounds: " + (aimSoundOn ? "on" : "off");
    this.setAttribute("aria-label", "Sound " + (aimSoundOn ? "on" : "off"));
    this.style.opacity = aimSoundOn ? "1" : ".45";
    if(aimSoundOn) sndRecv();
  });

  // smiley inserter
  $("#aimSmiley").addEventListener("click", () => {
    if(aimInput.disabled) return;
    const v = aimInput.value;
    aimInput.value = v + (v && !/\s$/.test(v) ? " " : "") + ":-) ";
    try { aimInput.focus(); } catch(_){}
  });
  $("#aimSmiley").addEventListener("keydown", e => { if(e.key === "Enter" || e.key === " "){ e.preventDefault(); $("#aimSmiley").click(); } });

  // wiring
  $("#homePayphone").addEventListener("click", openPayphone);
  $("#aimBlClose").addEventListener("click", closePayphone);
  $("#aimBlMin").addEventListener("click", closePayphone);
  [["#aimBlClose"],["#aimBlMin"]].forEach(([id]) =>
    $(id).addEventListener("keydown", e => { if(e.key === "Enter" || e.key === " "){ e.preventDefault(); closePayphone(); } }));
  $("#aimImClose").addEventListener("click", closeIM);
  $("#aimImClose").addEventListener("keydown", e => { if(e.key === "Enter" || e.key === " "){ e.preventDefault(); closeIM(); } });
  $("#aimSendBtn").addEventListener("click", sendMsg);
  aimInput.addEventListener("keydown", e => {
    if(e.key === "Enter" && !e.shiftKey){ e.preventDefault(); sendMsg(); }
  });
  payScreen.addEventListener("keydown", e => {
    if(e.key === "Escape"){ e.preventDefault();
      if(payStartMenu.classList.contains("on")) payMenu(false);
      else if(aimIM.classList.contains("on")) closeIM();
      else closePayphone(); }
  });

