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
  function mdDemoReply(t){                     // demo "local model" — always answers in Markdown
    // The other demo bots type plain AIM chatter; this one stands in for a real
    // llama.cpp model on the tailnet, so its replies are Markdown — the whole
    // point being to show the formatting (and the sideways-scrolling, copyable
    // code block) in demo mode, where there's no real fleet behind the console.
    const s = t.trim().toLowerCase();
    if(/\b(code|python|quicksort|sort|function|script|algorithm)\b/.test(s)){
      return [
        "Sure — here's a tiny quicksort in Python:",
        "",
        "```python",
        "def quicksort(xs):",
        "    if len(xs) <= 1:",
        "        return xs",
        "    pivot = xs[len(xs) // 2]",
        "    return quicksort([x for x in xs if x < pivot]) + [x for x in xs if x == pivot] + quicksort([x for x in xs if x > pivot])",
        "```",
        "",
        "Tap **Copy** to grab it — and that middle line runs long on purpose, so the code box scrolls left/right on your phone.",
      ].join("\n");
    }
    if(/\btable\b/.test(s)){
      return [
        "Here's what renders now:",
        "",
        "| feature | works |",
        "|:--------|:-----:|",
        "| headings & **bold** | ✅ |",
        "| lists | ✅ |",
        "| `inline code` | ✅ |",
        "| fenced code + Copy | ✅ |",
        "| tables (this one) | ✅ |",
      ].join("\n");
    }
    return [
      "### hey — I'm a *real* model 🦙",
      "",
      "Unlike the other buddies in here, I reply in **Markdown**. Ask me for **code** or a **table** and watch it format.",
      "",
      "- **bold**, *italic*, `inline_code`",
      "- fenced code scrolls sideways →",
      "- with a **Copy** button for your phone",
      "",
      "```js",
      "// a long line, on purpose — slide the code box left and right",
      "const greet = (name) => 'hello ' + name + ', welcome to 2001 — this line runs off the edge so the box scrolls instead of wrapping';",
      "```",
    ].join("\n");
  }

  // --- the buddies ----------------------------------------------------------
  const AIM_BUDDIES = [
    { sn:"SmarterChild", group:"active", open:"hey! you're finally online. what's on your mind? (type 'help' if you're lost)", respond:scReply },
    { sn:"ELIZA",        group:"active", open:"Hello. What brings you here today?", respond:elizaReply },
    { sn:"A.L.I.C.E",    group:"active", open:"Hi! I am A.L.I.C.E. Ask me anything.", respond:aliceReply },
    { sn:"TinyLlama 🦙", group:"active", md:true, respond:mdDemoReply,
      open:"### 🦙 hey — I'm a *model*\n\nThe other buddies type plain text, but I answer in **Markdown**. Ask me for `code` or a `table` and watch it format — code blocks slide sideways and carry a **Copy** button." },
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
  const CONVOS = {};         // conv key -> { live:[…], _awaySent, sessionId?, started? }
  const convBuddy = {};      // conv key -> the buddy the open IM is talking to
  let buddySig = "";         // fingerprint of the last render, to skip no-op rebuilds

  // --- saved sessions: the buddy list's memory ------------------------------
  // A "session" is one saved IM transcript with a real fleet model, persisted on
  // hush-control (PR: /api/payphone/sessions) so it survives a refresh and every
  // browser on the tailnet sees the same list of active chats — the same way the
  // launcher tile order is one global arrangement. Canned/demo bots stay pure
  // theatre and are never saved. PAY_SESSIONS is the server list; a live model
  // buddy's conversation is keyed by its session id ("s:"+id) so one model can
  // hold several distinct chats over time, each its own saved row.
  let PAY_SESSIONS = [];     // server-side saved sessions, newest first
  let paySessSig = "";       // fingerprint of the rendered session rows
  let paySessAt = 0;         // last successful pull time, to throttle the poll
  const convKeyOf = id => "s:" + id;

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
    pullSessions(force);       // keep the saved-chats list live alongside the fleet
    const model = buddyModel();
    const sig = model.buddies.map(b => b.key + (b.online?"1":"0")).join("|") + "#" + model.groups.map(g=>g.key).join(",");
    if(!force && sig === buddySig) return;
    buddySig = sig;
    BUDDIES = model.buddies;
    GROUPS = model.groups;
    for(const k in byKey) delete byKey[k];
    BUDDIES.forEach(b => { byKey[b.key] = b; });
    renderBuddyList();
    if(currentKey) selectBuddyRow(currentKey);
  }

  // --- session persistence: mirror a live chat up to hush-control -----------
  // Saved chats are console state, not fleet state — the transcript rides on
  // control, never on a box — so this is best-effort, like the launcher order:
  // a server hiccup or a static preview with no backend just means the chat
  // isn't remembered, never a broken UI. In demo mode there's no tailnet behind
  // the page, so persistence is skipped entirely and the canned bots stay pure
  // theatre.

  // genSessionId mints a short, slug-safe id (matches the server's sessionID
  // regex): a base-36 timestamp plus a little randomness so two chats started in
  // the same millisecond don't collide.
  function genSessionId(){
    const t = Date.now().toString(36);
    const r = Math.random().toString(36).slice(2, 7);
    return ("s" + t + r).toLowerCase().replace(/[^a-z0-9-]/g, "").slice(0, 64);
  }

  // sessionFor builds the {id, host, model, …, messages} record for a saved
  // conversation from its live transcript, keeping only the real model turns
  // (away-messages, nudges, and system lines aren't part of the chat). The title
  // is the first thing you said, so the buddy-list row reads like a subject line.
  function sessionFor(convKey){
    const b = convBuddy[convKey], c = CONVOS[convKey];
    if(!b || !c || !c.sessionId) return null;
    const messages = c.live
      .filter(e => (e.who === "me" || e.who === "them") && !e.auto)
      .map(e => ({ role: e.who === "me" ? "user" : "assistant", text: e.text }));
    const firstMe = messages.find(m => m.role === "user");
    const now = Date.now();
    if(!c.started) c.started = now;
    return {
      id: c.sessionId, host: b.host || "", model: b.model || b.sn || "", kind: b.kind || "",
      title: (firstMe ? firstMe.text : (b.model || b.sn || "chat")).slice(0, 160),
      started: c.started, updated: now, messages,
    };
  }

  // upsertLocalSession keeps PAY_SESSIONS in step with a chat as it happens, so
  // the "Active Chats" row appears (and its timestamp advances) the instant you
  // send — without waiting for the next server poll to echo it back.
  function upsertLocalSession(sess){
    const i = PAY_SESSIONS.findIndex(s => s.id === sess.id);
    if(i >= 0) PAY_SESSIONS[i] = sess; else PAY_SESSIONS.unshift(sess);
    renderSessionRows();
  }

  // persistSession mirrors one conversation up to control and reflects it locally
  // first. Skipped in demo mode (no backend) and for non-session convs (canned).
  function persistSession(convKey){
    if(MODE === "demo") return;
    const sess = sessionFor(convKey);
    if(!sess) return;
    upsertLocalSession(sess);
    fetch("api/payphone/sessions/" + encodeURIComponent(sess.id), {
      method:"PUT",
      headers:{ "Content-Type":"application/json" },
      body: JSON.stringify(sess),
    }).catch(() => {});
  }

  // forgetSession drops a saved chat from the server and the local list. Used by
  // the little × on a session row — the AIM way to clear an old conversation.
  function forgetSession(id){
    PAY_SESSIONS = PAY_SESSIONS.filter(s => s.id !== id);
    const ck = convKeyOf(id);
    if(currentKey === ck) closeIM();
    delete CONVOS[ck]; delete convBuddy[ck];
    renderSessionRows();
    if(MODE !== "demo") fetch("api/payphone/sessions/" + encodeURIComponent(id), { method:"DELETE" }).catch(() => {});
  }

  // pullSessions refreshes PAY_SESSIONS from control — throttled to ~4s so the
  // 2.5s fleet poll doesn't hammer it — and repaints the session rows only when
  // the list actually changed, so a chat someone else started on the tailnet
  // shows up here without churning an open window. Demo mode has no backend.
  function pullSessions(force){
    if(MODE === "demo") return;
    const now = Date.now();
    if(!force && now - paySessAt < 4000) return;
    paySessAt = now;
    fetch("api/payphone/sessions", { headers:{ "Accept":"application/json" } })
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if(!data || !Array.isArray(data.sessions)) return;
        PAY_SESSIONS = data.sessions;
        renderSessionRows();
      })
      .catch(() => {});
  }

  // awayText is the read-only message a signed-off (loopback / unverified)
  // buddy answers with — the loopback nudge, pointed at the specific box.
  function awayText(b){
    if(b.exposure === "loopback")
      return "i'm bound to loopback on " + b.host + " — nothing off-box can reach me. expose me past loopback and i light up green.";
    return "my bind scope couldn't be verified on " + b.host + ", so i'm signed off to be safe.";
  }

  // seed a conversation's running transcript the first time it's opened. key is
  // the conversation key — a buddy key for canned/theatre chats, a session key
  // ("s:"+id) for a live model, so one model can hold several distinct chats.
  function seedLive(b, key){
    if(CONVOS[key] && CONVOS[key].live) return;
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
    CONVOS[key] = c;
  }

  // classic emoticon → glyph, applied only to displayed text
  const EMO = [[/:-?\)/g,"☺"],[/:-?\(/g,"☹"],[/;-?\)/g,"😉"],[/:-?[pP]\b/g,"😛"],[/:-?[dD]\b/g,"😄"],[/<3/g,"❤"]];
  const emote = s => EMO.reduce((acc,[re,g]) => acc.replace(re,g), s);

  // --- Markdown → DOM (real models only) ------------------------------------
  // The canned bots type plain AIM chatter, but a real llama.cpp model on the
  // tailnet answers in Markdown — headings, lists, **bold**, and fenced code.
  // Rendered raw, that's a wall of asterisks and backticks. mdRender turns it
  // into formatted DOM the classic IM window can show. It builds nodes by hand
  // (textContent everywhere, never innerHTML) so untrusted model output can't
  // inject markup, and links are limited to http(s)/mailto. Code blocks and
  // wide tables get their own sideways-scrolling box so a phone can slide
  // through a long line instead of wrapping it into soup — with a Copy button
  // that works over plain-HTTP tailnet addresses (execCommand fallback), where
  // navigator.clipboard is unavailable.

  // copyText copies to the clipboard, flashing the button, and falls back to a
  // hidden-textarea execCommand for non-secure (plain-HTTP) contexts.
  function fallbackCopy(text, done){
    try {
      const ta = document.createElement("textarea");
      ta.value = text; ta.setAttribute("readonly", "");
      ta.style.position = "fixed"; ta.style.top = "-1000px"; ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select(); ta.setSelectionRange(0, text.length);
      const ok = document.execCommand("copy");
      document.body.removeChild(ta);
      done(!!ok);
    } catch(_){ done(false); }
  }
  function copyText(text, btn){
    const flash = ok => {
      if(!btn) return;
      const prev = btn.textContent;
      btn.textContent = ok ? "Copied ✓" : "Copy failed";
      btn.classList.toggle("ok", ok);
      setTimeout(() => { btn.textContent = prev; btn.classList.remove("ok"); }, 1200);
    };
    if(navigator.clipboard && navigator.clipboard.writeText){
      navigator.clipboard.writeText(text).then(() => flash(true), () => fallbackCopy(text, flash));
    } else {
      fallbackCopy(text, flash);
    }
  }

  // mdCodeBlock is the fenced-code panel: a title bar (language tag + Copy) over
  // a horizontally scrollable <pre> — the "slide left/right" box on mobile.
  function mdCodeBlock(code, lang){
    const wrap = document.createElement("div"); wrap.className = "aim-code";
    const bar  = document.createElement("div"); bar.className = "aim-codebar";
    const tag  = document.createElement("span"); tag.className = "aim-codelang";
    tag.textContent = lang || "code";
    const btn  = document.createElement("button"); btn.type = "button";
    btn.className = "aim-copy"; btn.textContent = "Copy";
    btn.setAttribute("aria-label", "Copy code to clipboard");
    btn.addEventListener("click", () => copyText(code, btn));
    bar.appendChild(tag); bar.appendChild(btn);
    const pre = document.createElement("pre"); pre.className = "aim-pre";
    const c   = document.createElement("code"); c.textContent = code;
    pre.appendChild(c);
    wrap.appendChild(bar); wrap.appendChild(pre);
    return wrap;
  }

  // mdInline renders one run of inline text: `code`, **bold**, *italic*,
  // ~~strike~~, and [links](url). Plain segments become text nodes (so nothing
  // is ever parsed as HTML); newlines become <br> so a model's line breaks
  // survive. Underscore emphasis is skipped mid-word so snake_case/__dunder__
  // aren't mangled.
  function mdInline(parent, text){
    text = String(text == null ? "" : text);
    const pushText = s => {
      if(!s) return;
      s.split("\n").forEach((seg, k) => {
        if(k > 0) parent.appendChild(document.createElement("br"));
        if(seg) parent.appendChild(document.createTextNode(seg));
      });
    };
    const RE = /(`+)([\s\S]*?)\1|(\*\*|__)([\s\S]+?)\3|(\*|_)([\s\S]+?)\5|(~~)([\s\S]+?)\7|\[([^\]]*)\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)/g;
    let last = 0, m;
    while((m = RE.exec(text))){
      pushText(text.slice(last, m.index));
      const before = text[m.index - 1] || "";
      if(m[1] !== undefined && m[1] !== ""){                 // inline code
        const code = document.createElement("code"); code.className = "aim-ic";
        code.textContent = m[2]; parent.appendChild(code);
      } else if(m[3]){                                       // bold
        if(m[3] === "__" && /\w/.test(before)){ pushText(m[0]); }
        else { const el = document.createElement("strong"); mdInline(el, m[4]); parent.appendChild(el); }
      } else if(m[5]){                                       // italic
        if(m[5] === "_" && /\w/.test(before)){ pushText(m[0]); }
        else { const el = document.createElement("em"); mdInline(el, m[6]); parent.appendChild(el); }
      } else if(m[7]){                                       // strikethrough
        const el = document.createElement("s"); mdInline(el, m[8]); parent.appendChild(el);
      } else if(m[9] !== undefined){                         // link
        const url = m[10] || "";
        if(/^(https?:|mailto:)/i.test(url)){
          const a = document.createElement("a"); a.className = "aim-link";
          a.href = url; a.target = "_blank"; a.rel = "noopener noreferrer nofollow";
          mdInline(a, m[9]); parent.appendChild(a);
        } else { pushText(m[0]); }                           // unsafe scheme → verbatim
      }
      last = RE.lastIndex;
    }
    pushText(text.slice(last));
  }

  // mdTable renders a GFM pipe table starting at lines[start] (header row +
  // divider). Returns the wrapped node and the index past the table.
  function mdTable(lines, start){
    const cellsOf = s => s.trim().replace(/^\|/, "").replace(/\|$/, "").split("|").map(c => c.trim());
    const header = cellsOf(lines[start]);
    const align  = cellsOf(lines[start + 1]).map(c => {
      const l = c.startsWith(":"), r = c.endsWith(":");
      return (l && r) ? "center" : r ? "right" : l ? "left" : "";
    });
    let i = start + 2; const rows = [];
    while(i < lines.length && lines[i].indexOf("|") >= 0 && lines[i].trim()){ rows.push(cellsOf(lines[i])); i++; }
    const table = document.createElement("table"); table.className = "aim-table";
    const thead = document.createElement("thead"), htr = document.createElement("tr");
    header.forEach((h, ci) => { const th = document.createElement("th");
      if(align[ci]) th.style.textAlign = align[ci]; mdInline(th, h); htr.appendChild(th); });
    thead.appendChild(htr); table.appendChild(thead);
    const tb = document.createElement("tbody");
    rows.forEach(cells => { const tr = document.createElement("tr");
      for(let ci = 0; ci < header.length; ci++){ const td = document.createElement("td");
        if(align[ci]) td.style.textAlign = align[ci]; mdInline(td, cells[ci] || ""); tr.appendChild(td); }
      tb.appendChild(tr); });
    table.appendChild(tb);
    const wrap = document.createElement("div"); wrap.className = "aim-tablewrap"; wrap.appendChild(table);
    return { node: wrap, next: i };
  }

  // mdRender parses block-level Markdown into a DocumentFragment: fenced code,
  // headings, lists, blockquotes, tables, rules, and paragraphs.
  function mdRender(src){
    const frag = document.createDocumentFragment();
    const lines = String(src == null ? "" : src).replace(/\t/g, "    ").split("\n");
    const blank = s => !s.trim();
    const isList = s => /^(\s*)([-*+]|\d+[.)])\s+/.test(s);
    let i = 0;
    while(i < lines.length){
      const line = lines[i];
      const fence = line.match(/^\s*(```+|~~~+)\s*([^\s`]*)\s*$/);
      if(fence){                                             // fenced code block
        const mark = fence[1][0], len = fence[1].length, lang = fence[2] || "";
        i++; const buf = [];
        while(i < lines.length){
          const close = lines[i].match(/^\s*(```+|~~~+)\s*$/);
          if(close && close[1][0] === mark && close[1].length >= len){ i++; break; }
          buf.push(lines[i]); i++;
        }
        frag.appendChild(mdCodeBlock(buf.join("\n"), lang));
        continue;
      }
      if(blank(line)){ i++; continue; }
      const h = line.match(/^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$/);
      if(h){ const el = document.createElement("h" + h[1].length); el.className = "aim-h";
        mdInline(el, h[2]); frag.appendChild(el); i++; continue; }
      if(/^\s{0,3}([-*_])\s*(\1\s*){2,}$/.test(line)){ frag.appendChild(document.createElement("hr")); i++; continue; }
      if(/^\s{0,3}>\s?/.test(line)){                          // blockquote
        const buf = [];
        while(i < lines.length && /^\s{0,3}>\s?/.test(lines[i])){ buf.push(lines[i].replace(/^\s{0,3}>\s?/, "")); i++; }
        const bq = document.createElement("blockquote"); bq.className = "aim-quote";
        bq.appendChild(mdRender(buf.join("\n"))); frag.appendChild(bq); continue;
      }
      if(line.indexOf("|") >= 0 && i + 1 < lines.length &&
         /^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)+\|?\s*$/.test(lines[i + 1])){
        const t = mdTable(lines, i); frag.appendChild(t.node); i = t.next; continue;
      }
      const lm = line.match(/^(\s*)([-*+]|\d+[.)])\s+/);
      if(lm){                                                 // list
        const ordered = /\d/.test(lm[2]);
        const list = document.createElement(ordered ? "ol" : "ul"); list.className = "aim-list";
        while(i < lines.length){
          if(blank(lines[i])) break;                          // a blank line ends the list
          const im = lines[i].match(/^(\s*)([-*+]|\d+[.)])\s+([\s\S]*)$/);
          if(!im || /\d/.test(im[2]) !== ordered) break;      // stop at a non-item or a switch of list type
          const li = document.createElement("li"); mdInline(li, im[3]);
          list.appendChild(li); i++;
        }
        frag.appendChild(list); continue;
      }
      const buf = [line]; i++;                                // paragraph
      while(i < lines.length && !blank(lines[i]) && !isList(lines[i])
        && !/^\s*(```+|~~~+)/.test(lines[i]) && !/^\s{0,3}#{1,6}\s/.test(lines[i])
        && !/^\s{0,3}>\s?/.test(lines[i])){
        buf.push(lines[i]); i++;
      }
      const p = document.createElement("p"); p.className = "aim-p";
      mdInline(p, buf.join("\n")); frag.appendChild(p);
    }
    return frag;
  }

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
        // Clicking a real, reachable model asks whether to start a new session
        // (or resume the latest one) rather than dropping straight in — the
        // pop-up the sessions feature adds. Canned bots, signed-off models, and
        // the empty placeholder open directly, as before.
        const open = () => {
          if(b.llm && b.online) askNewSession(b);
          else openIM(b.key);
        };
        li.addEventListener("click", open);
        li.addEventListener("keydown", e => { if(e.key === "Enter" || e.key === " "){ e.preventDefault(); open(); } });
        list.appendChild(li);
      });
      wrap.appendChild(list);
      aimGroups.appendChild(wrap);
    });
    renderSessionRows();       // the saved-chats group sits above the fleet
  }

  // agoText renders a coarse "how long ago" for a session row's last activity —
  // just enough to tell a chat from this minute apart from yesterday's.
  function agoText(ms){
    const s = Math.max(0, Math.floor((Date.now() - ms) / 1000));
    if(s < 45) return "now";
    const m = Math.floor(s / 60);
    if(m < 60) return m + "m ago";
    const h = Math.floor(m / 60);
    if(h < 24) return h + "h ago";
    return Math.floor(h / 24) + "d ago";
  }

  // liveModelFor finds the reachable buddy (same host + model) behind a saved
  // session, so resuming a chat streams for real when its box is still online. It
  // returns null when nothing matching is online — a session whose box has since
  // signed off is still readable, it just can't take a new turn.
  function liveModelFor(sess){
    return BUDDIES.find(b => b.llm && b.online && b.host === sess.host && b.model === sess.model) || null;
  }

  // renderSessionRows paints the "Active Chats" group at the top of the buddy
  // list from PAY_SESSIONS. It manages its own node so the throttled session poll
  // can repaint it without disturbing the fleet groups below; a signature guard
  // skips the repaint (and the focus churn) when nothing changed. An empty list
  // removes the group entirely, so the window reads exactly as it did before when
  // no chats are saved.
  function renderSessionRows(){
    const existing = aimGroups.querySelector("#aimSessGroup");
    if(!PAY_SESSIONS.length){ if(existing) existing.remove(); paySessSig = ""; return; }
    const sig = PAY_SESSIONS.map(s => s.id + ":" + s.updated).join("|");
    if(existing && sig === paySessSig) return;
    paySessSig = sig;

    const wrap = document.createElement("div");
    wrap.className = "aim-group open"; wrap.id = "aimSessGroup";
    wrap.setAttribute("role", "treeitem");
    const head = document.createElement("div");
    head.className = "aim-ghead"; head.tabIndex = 0;
    head.innerHTML = '<span class="aim-tri"></span><span class="aim-glabel"></span> <span class="aim-gcount"></span>';
    head.querySelector(".aim-glabel").textContent = "Active Chats";
    head.querySelector(".aim-gcount").textContent = "(" + PAY_SESSIONS.length + ")";
    const toggle = () => wrap.classList.toggle("open");
    head.addEventListener("click", toggle);
    head.addEventListener("keydown", e => { if(e.key === "Enter" || e.key === " "){ e.preventDefault(); toggle(); } });
    wrap.appendChild(head);

    const list = document.createElement("ul");
    list.className = "aim-blist";
    PAY_SESSIONS.forEach(sess => {
      const on = !!liveModelFor(sess);
      const li = document.createElement("li");
      li.className = "aim-buddy" + (on ? "" : " off");
      li.tabIndex = 0; li.dataset.key = convKeyOf(sess.id);
      li.innerHTML = '<svg class="aim-bico" viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" stroke="#000" stroke-width="1" stroke-linejoin="round" d="M13.6 2.6a2 2 0 1 1-2.8 2.8 2 2 0 0 1 2.8-2.8zM10.7 7.2l3-.2c.7 0 1.3.4 1.6 1l1.2 2.3 2.9 1.1-.7 1.8-3.4-1.3-1-1.9-1 3.3 2.5 2.4.5 4.6-1.9.2-.5-3.9-2.2 2-1.6 3.6-1.8-.8 1.9-4.3.5-2.6-2.1 1.3-1.4 2.9L4 12l.6-1.8 2.6.6 2.1-2.5c.3-.4.8-.8 1.4-1.1z"/></svg><span class="aim-bname"></span>';
      li.querySelector(".aim-bname").textContent = sess.model || sess.title || "chat";
      const sub = document.createElement("span");
      sub.className = "aim-idle";
      sub.textContent = "@ " + (sess.host || "?") + " · " + agoText(sess.updated);
      li.appendChild(sub);
      // A small × forgets the saved chat — the AIM way to clear an old window.
      const del = document.createElement("span");
      del.className = "aim-bx"; del.textContent = "×";
      del.setAttribute("role", "button"); del.tabIndex = 0;
      del.title = "Forget this chat"; del.setAttribute("aria-label", "Forget this chat");
      const forget = e => { e.stopPropagation(); forgetSession(sess.id); };
      del.addEventListener("click", forget);
      del.addEventListener("keydown", e => { if(e.key === "Enter" || e.key === " "){ e.preventDefault(); e.stopPropagation(); forgetSession(sess.id); } });
      li.appendChild(del);
      const open = () => resumeSession(sess.id);
      li.addEventListener("click", open);
      li.addEventListener("keydown", e => { if(e.key === "Enter" || e.key === " "){ e.preventDefault(); open(); } });
      list.appendChild(li);
    });
    wrap.appendChild(list);
    if(existing) existing.replaceWith(wrap); else aimGroups.insertBefore(wrap, aimGroups.firstChild);
    if(currentKey) selectBuddyRow(currentKey);
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
    const b = convBuddy[currentKey];
    if(entry.who === "me") return "Me";
    if(entry.auto) return "Auto-response from " + (b ? b.sn : "");
    return b ? b.sn : "";
  }
  // A real model's reply is Markdown; a canned bot's is plain AIM chatter.
  // mdWanted gates the formatted path so only live-model "them" turns (not away
  // auto-responses, not the user's own lines) get parsed.
  function mdWanted(entry){
    const b = byKey[currentKey];
    // Real tailnet models (b.llm) answer in Markdown; the demo "model" buddy
    // (b.md) does too, so the formatting is visible with no fleet behind it.
    return !!(b && (b.llm || b.md) && entry.who === "them" && !entry.auto);
  }
  function appendLine(entry){
    if(entry.who === "sys"){
      const d = document.createElement("div");
      d.className = "aim-sys"; d.textContent = entry.text;
      aimLog.appendChild(d);
    } else {
      const md = mdWanted(entry);
      const d = document.createElement("div");
      d.className = "aim-line " + (entry.who === "me" ? "me" : "them") + (md ? " md" : "");
      const sn = document.createElement("span");
      sn.className = "sn";
      sn.textContent = speakerName(entry) + ": ";
      d.appendChild(sn);
      if(md){
        const body = document.createElement("div"); body.className = "aim-md";
        body.appendChild(mdRender(entry.text));
        d.appendChild(body);
      } else {
        const body = document.createElement("span");
        body.textContent = emote(entry.text);
        d.appendChild(body);
      }
      aimLog.appendChild(d);
    }
    aimLog.scrollTop = aimLog.scrollHeight;
  }
  // newStreamLine opens an empty "them" line whose body span is returned, so a
  // streaming completion can append tokens into it as they arrive.
  function newStreamLine(){
    const b = convBuddy[currentKey];
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

  // openConv opens the IM window on a (buddy, conversation key) pair. A canned or
  // theatre buddy is its own conversation (key === buddy key); a live model chat
  // is a session (key === "s:"+id), which lets one model hold several. The buddy
  // supplies who you're talking to and how to route; the key selects the running
  // transcript in CONVOS.
  function openConv(b, key){
    if(!b) return;
    abortStream();
    currentKey = key; convBuddy[key] = b; seedLive(b, key);
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
  // openIM keeps the buddy-keyed entry point for canned/theatre and signed-off
  // rows: the conversation key is just the buddy key.
  function openIM(key){ openConv(byKey[key], key); }

  // startNewSession begins a fresh chat with a live model: mint a session id, open
  // an empty conversation on it, and register it with control right away so the
  // "Active Chats" row shows up the instant you start — before you've said a word.
  function startNewSession(b){
    const id = genSessionId();
    const key = convKeyOf(id);
    CONVOS[key] = { live:[], _awaySent:false, sessionId:id, started:Date.now() };
    openConv(b, key);
    persistSession(key);
  }

  // resumeSession reopens a saved chat. When its box is still online the live
  // buddy backs the window so a new turn streams for real; otherwise it opens
  // read-only over the saved transcript with an away note. The transcript is
  // seeded from the server record the first time, then kept in CONVOS.
  function resumeSession(id){
    const sess = PAY_SESSIONS.find(s => s.id === id);
    if(!sess) return;
    const key = convKeyOf(id);
    const live = liveModelFor(sess);
    const b = live || {
      key, sn:sess.model || sess.title || "chat", host:sess.host, model:sess.model,
      kind:sess.kind || "openai", exposure:"unknown", llm:true, online:false, group:"sessions",
    };
    if(!CONVOS[key]){
      const head = live
        ? { who:"sys", text:sess.model + " on " + sess.host + " · resuming your session." }
        : { who:"sys", text:sess.model + " on " + sess.host + " is offline — this is your saved conversation." };
      const lines = [head].concat((sess.messages || []).map(m => ({ who:m.role === "user" ? "me" : "them", text:m.text })));
      CONVOS[key] = { live:lines, _awaySent:false, sessionId:id, started:sess.started };
    }
    openConv(b, key);
  }

  // --- the "start a new session?" pop-up ------------------------------------
  // Clicking a live model no longer drops straight into a chat: it asks first, so
  // starting a brand-new session is a deliberate choice and any existing chat with
  // that model is one tap to resume. A Win95 dialog, same beige theatre.
  const paySessDlg    = $("#paySessDlg");
  const paySessDlgMsg = $("#paySessDlgMsg");
  const paySessNew    = $("#paySessNew");
  const paySessResume = $("#paySessResume");
  const paySessCancel = $("#paySessCancel");
  const paySessDlgX   = $("#paySessDlgX");
  let paySessPending  = null;      // { buddy, resumeId } while the dialog is up

  function askNewSession(b){
    // The most recent saved chat with this exact model (host + id), if any, is the
    // one "Resume" reopens.
    const prior = PAY_SESSIONS.find(s => s.host === b.host && s.model === b.model);
    paySessPending = { buddy:b, resumeId: prior ? prior.id : null };
    paySessDlgMsg.textContent = "Start a new session with " + (b.model || b.sn) + " on " + b.host + "?";
    paySessResume.hidden = !prior;
    paySessDlg.hidden = false;
    try { paySessNew.focus({ preventScroll:true }); } catch(_){}
  }
  function closeSessDlg(){
    paySessDlg.hidden = true;
    const p = paySessPending; paySessPending = null;
    // Return focus to the buddy row that opened the dialog.
    if(p && p.buddy){ const row = rowFor(p.buddy.key); try { (row || $("#aimBlClose")).focus({ preventScroll:true }); } catch(_){} }
  }
  paySessNew.addEventListener("click", () => { const p = paySessPending; closeSessDlg(); if(p) startNewSession(p.buddy); });
  paySessResume.addEventListener("click", () => { const p = paySessPending; closeSessDlg(); if(p && p.resumeId) resumeSession(p.resumeId); });
  paySessCancel.addEventListener("click", closeSessDlg);
  paySessDlgX.addEventListener("click", closeSessDlg);
  [paySessDlgX].forEach(el => el.addEventListener("keydown", e => { if(e.key === "Enter" || e.key === " "){ e.preventDefault(); closeSessDlg(); } }));
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
    const b = convBuddy[currentKey], c = CONVOS[currentKey];
    if(!b || !c || b.empty) return;
    if(b.llm && b.online && streamAbort) return;   // a completion is already streaming; one turn at a time
    const text = aimInput.value.replace(/\s+$/,"");
    if(!text.trim()) return;
    c.live.push({ who:"me", text });
    appendLine({ who:"me", text });
    aimInput.value = "";
    sndSend();
    clearTimeout(replyTimer);

    // Mirror the chat up as soon as you speak, so a session's "Active Chats" row
    // reflects the latest turn even if the reply is still streaming (or never
    // comes). streamChat persists again once the answer lands.
    if(c.sessionId) persistSession(currentKey);

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
                body.textContent = acc;   // raw while streaming; Markdown-rendered on completion
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
          // Swap the raw streamed text for the Markdown render now that the
          // whole message is in: replace the plain body span in place, keeping
          // the "Name:" prefix and the line's position in the log.
          if(body){
            const lineEl = body.parentNode;
            lineEl.classList.add("md");
            body.remove();
            const rendered = document.createElement("div"); rendered.className = "aim-md";
            rendered.appendChild(mdRender(acc));
            lineEl.appendChild(rendered);
            aimLog.scrollTop = aimLog.scrollHeight;
          }
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
      // Save the completed turn (or whatever landed before an error) so the saved
      // session keeps the model's reply, keyed to the pinned conversation.
      if(c.sessionId) persistSession(key);
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
    paySessDlg.hidden = true; paySessPending = null;
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
      if(!paySessDlg.hidden) closeSessDlg();
      else if(payStartMenu.classList.contains("on")) payMenu(false);
      else if(aimIM.classList.contains("on")) closeIM();
      else closePayphone(); }
  });

