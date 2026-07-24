  /* ---------- state ---------- */

  // BUILD_SHA is stamped in by scripts/build-web-preview.sh for static builds
  // (GitHub Pages / PR previews) that have no hush-control backend and thus no
  // api/version. Left blank here for real deployments.
  const BUILD_SHA = "";

  let M = [];                      // current fleet
  let MODE = "connecting";         // connecting | live | demo | lost
  let everLive = false;            // true once /api/fleet has answered at least once
  let lostSince = 0;               // ms timestamp poll() started failing after being live
  const DOC_TITLE = document.title;
  const cpuHist = {};              // id -> rolling cpu samples (live mode)
  const gpuHist = {};              // id -> rolling gpu-util samples (live mode)
  const netRxHist = {};            // id -> rolling inbound bytes/sec samples (live mode)
  const netTxHist = {};            // id -> rolling outbound bytes/sec samples (live mode)
  // Condensed fleet cards: rings and load lines are rolled up by default and
  // only unrolled on demand. showRings/showLines are the global "unroll every
  // card" toggles; openRings/openLines hold the ids a user opened one at a time.
  // Both survive the 2.5s poll re-render because they live on `state`.
  const state = { view:"fleet", mid:null, summLegendOpen:false,
    showRings:false, showLines:false,
    openRings:new Set(), openLines:new Set(),
    openSections:new Set() };
  const byId = id => M.find(m=>m.id===id);
  let PENDING = [];                // machines just added via the UI, awaiting first check-in
  const JUST_ARRIVED = new Set();  // ids to pop-highlight for one render after their first check-in
  function esc(s){ return String(s??"").replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

  /* ---------- svg helpers ---------- */
  function ring(pct, size){
    const cx = size/2;
    const r1 = cx-3, c1 = 2*Math.PI*r1, off = c1*(1-pct/100);
    const r2 = cx-9, c2 = 2*Math.PI*r2;
    const col = pct>=85 ? "var(--crit)" : pct>=70 ? "var(--warn)" : "var(--accent)";
    const sw = 3.2;
    return `<svg width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">
      <circle cx="${cx}" cy="${cx}" r="${r1}" fill="none" stroke="var(--border)" stroke-width="${sw}"/>
      <circle cx="${cx}" cy="${cx}" r="${r2}" fill="none" stroke="var(--border)" stroke-width="${sw}"/>
      <circle cx="${cx}" cy="${cx}" r="${r1}" fill="none" stroke="${col}" stroke-width="${sw}"
        stroke-linecap="round" stroke-dasharray="${c1.toFixed(1)}" stroke-dashoffset="${off.toFixed(1)}"
        transform="rotate(-90 ${cx} ${cx})"/></svg>`;
  }
  function utilCol(p){ return p>=85?"var(--crit)":p>=70?"var(--warn)":"var(--accent)"; }
  function memCol(p){ return p>=90?"var(--crit)":p>=80?"var(--warn)":"var(--ring2)"; }
  function dualRing(outer, inner, size){
    const cx = size/2;
    const r1 = cx-3, c1 = 2*Math.PI*r1, o1 = c1*(1-outer/100);
    const r2 = cx-9, c2 = 2*Math.PI*r2, o2 = c2*(1-inner/100);
    const sw = 3.2;
    return `<svg width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">
      <circle cx="${cx}" cy="${cx}" r="${r1}" fill="none" stroke="var(--border)" stroke-width="${sw}"/>
      <circle cx="${cx}" cy="${cx}" r="${r2}" fill="none" stroke="var(--border)" stroke-width="${sw}"/>
      <circle cx="${cx}" cy="${cx}" r="${r1}" fill="none" stroke="${utilCol(outer)}" stroke-width="${sw}"
        stroke-linecap="round" stroke-dasharray="${c1.toFixed(1)}" stroke-dashoffset="${o1.toFixed(1)}"
        transform="rotate(-90 ${cx} ${cx})"/>
      <circle cx="${cx}" cy="${cx}" r="${r2}" fill="none" stroke="${memCol(inner)}" stroke-width="${sw}"
        stroke-linecap="round" stroke-dasharray="${c2.toFixed(1)}" stroke-dashoffset="${o2.toFixed(1)}"
        transform="rotate(-90 ${cx} ${cx})"/></svg>`;
  }
  // sparkline draws a 0–100 series as a filled area line. `col` defaults to the
  // teal accent so every existing call site keeps its look; the condensed load
  // lines pass a per-metric colour (e.g. the GPU violet) to stay legible when
  // cpu and gpu are stacked in the same drawer.
  function sparkline(data, w, h, col){
    col = col || "var(--accent)";
    const n = data.length;
    const pts = data.map((v,i)=>{
      const x=(i/(n-1))*w, y=h-3-(v/100)*(h-6);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    });
    const last = pts[pts.length-1].split(",");
    const area = `0,${h} ${pts.join(" ")} ${w},${h}`;
    const gid = "g"+Math.floor(Math.random()*1e6);
    return `<svg class="spark" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none">
      <defs><linearGradient id="${gid}" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stop-color="${col}" stop-opacity=".28"/>
        <stop offset="1" stop-color="${col}" stop-opacity="0"/>
      </linearGradient></defs>
      <polygon points="${area}" fill="url(#${gid})"/>
      <polyline points="${pts.join(" ")}" fill="none" stroke="${col}" stroke-width="1.6"
        stroke-linejoin="round" stroke-linecap="round"/>
      <circle cx="${last[0]}" cy="${last[1]}" r="2.4" fill="${col}"/></svg>`;
  }
  // netSpark draws rx/tx as two overlaid lines sharing one y-axis, scaled to
  // the loudest sample of either series (floored so an idle link isn't drawn
  // as a flat line pinned to the top).
  function netSpark(rx, tx, w, h, cls){
    const peak = Math.max(1024, ...rx, ...tx);
    const line = (data, col) => {
      const n = data.length;
      const pts = data.map((v,i)=>{
        const x=(i/(n-1))*w, y=h-3-(v/peak)*(h-6);
        return `${x.toFixed(1)},${y.toFixed(1)}`;
      });
      const last = pts[pts.length-1].split(",");
      return `<polyline points="${pts.join(" ")}" fill="none" stroke="${col}" stroke-width="1.6"
        stroke-linejoin="round" stroke-linecap="round"/><circle cx="${last[0]}" cy="${last[1]}" r="2.4" fill="${col}"/>`;
    };
    return `<svg class="${cls||"spark"}" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none">
      ${line(tx, "var(--ring2)")}${line(rx, "var(--accent)")}</svg>`;
  }

  /* ---------- render ---------- */
  function render(){
    if(state.view==="machine" && !byId(state.mid)) state.view = "fleet";
    if(state.view==="fleet") renderFleet();
    else renderMachine();
    renderAlertBell();
    // The watch modal lives outside `app`, so the re-render above leaves it
    // untouched — repaint it here so a run finishing mid-watch flips the open
    // modal from "backing up…" to its outcome on the very next poll.
    if(watchOpen) renderBackupWatch();
    // The payphone buddy list is fleet-derived too: keep it current while it's
    // open so a model coming online (or signing off) shows up on the next poll.
    if(payphoneOpen()) refreshBuddies();
  }

  // orderedFleet sorts the grid alphabetically by machine name — the sole
  // sort order. Unlike a posture/severity rank, a name doesn't flip mid-poll,
  // so no pinning is needed to keep cards from reshuffling under a tap.
  function orderedFleet(){
    return [...M].sort((a,b)=> a.id.localeCompare(b.id, undefined, {sensitivity:"base", numeric:true}));
  }

  function renderFleet(){
    // The summary leads with backup posture — this is a backup console, so the
    // headline question is "is the fleet protected?", not "how's CPU". Vitals
    // still colour each card's rings; they're just no longer the fleet verdict.
    const post = M.map(m=>backupPosture(m.id));
    const nProt = post.filter(p=>p==="protected").length;
    const nRisk = post.filter(p=>p==="risk").length;
    const nFail = post.filter(p=>p==="failed").length;
    const nNone = post.filter(p=>p==="none").length;
    const nRun  = post.filter(p=>p==="running").length;
    const nPaused = post.filter(p=>p==="paused").length;
    // Backups are flattened across machines because the question is
    // fleet-shaped ("is everything backed up?"), not per-box.
    const bkAll = CONV_BACKUPS.flatMap(h => (h.backups||[]).map(b => ({host:h.host, b})));
    // "Bad" is what forces the section open and colours the count: a run that
    // finished not-ok, or one that has stalled. A healthy run in flight is
    // explicitly NOT bad — before this, a running backup (no ok field) counted
    // as bad, which is exactly the "in progress looks broken" bug being fixed.
    const bkBad = bkAll.filter(x => bkStalled(x.b) || (!bkRunning(x.b) && !x.b.ok)).length;
    // ...but a healthy run still opens the section, because someone watching a
    // backup wants to see it without hunting for the drawer.
    const bkActive = bkAll.some(x => bkRunning(x.b));
    // Machines that couldn't be asked are named rather than omitted — a backup
    // console that quietly drops a machine is worse than one that says it
    // cannot tell.
    const bkUnreachable = CONV_BACKUPS.filter(h => !h.reachable).map(h => h.host);
    app.innerHTML = `
      <div class="strip">
        <div style="flex:1">
          <div class="h-title">Fleet</div>
        </div>
        <details class="summ-wrap"${state.summLegendOpen?" open":""}>
          <summary class="chip summ">
            <span class="s"><span class="swatch" style="background:var(--good)"></span><b>${nProt}</b></span>
            ${nRun?`<span class="s"><span class="swatch" style="background:var(--accent)"></span><b>${nRun}</b></span>`:""}
            <span class="s"><span class="swatch" style="background:var(--warn)"></span><b>${nRisk}</b></span>
            ${nPaused?`<span class="s" title="backups paused"><span class="swatch" style="background:var(--warn)"></span>⏸<b>${nPaused}</b></span>`:""}
            <span class="s"><span class="swatch" style="background:var(--faint)"></span><b>${nNone}</b></span>
            <span class="s"><span class="swatch" style="background:var(--crit)"></span><b>${nFail}</b></span>
          </summary>
          <div class="summ-legend">
            <span class="s"><span class="swatch" style="background:var(--good)"></span>protected</span>
            ${nRun?`<span class="s"><span class="swatch" style="background:var(--accent)"></span>backing up</span>`:""}
            <span class="s"><span class="swatch" style="background:var(--warn)"></span>at risk</span>
            ${nPaused?`<span class="s"><span class="swatch" style="background:var(--warn)"></span>⏸ paused</span>`:""}
            <span class="s"><span class="swatch" style="background:var(--faint)"></span>unprotected</span>
            <span class="s"><span class="swatch" style="background:var(--crit)"></span>failed</span>
          </div>
        </details>
      </div>
      ${M.length ? `<div class="strip" style="padding-top:0"><div class="fleet-ctl">
        <span class="ctl${state.showRings?" on":""}" id="ctlRings" role="switch" tabindex="0" aria-checked="${state.showRings}"><span class="sw"></span>Rings <b>${state.showRings?"on":"off"}</b></span>
        <span class="ctl${state.showLines?" on":""}" id="ctlLines" role="switch" tabindex="0" aria-checked="${state.showLines}"><span class="sw"></span>Load lines <b>${state.showLines?"on":"off"}</b></span>
        <span class="ctl-note">healthy nodes stay rolled up · hot nodes auto-unroll the bad bit</span>
      </div></div>` : ""}
      <main>
        <div class="grid">
          ${PENDING.map(pendingNodeCard).join("")}
          ${orderedFleet().map(nodeCard).join("")}
        </div>
        ${(M.length+PENDING.length) ? "" : `<p class="note">no machines reporting yet</p>`}

        <details class="rollup"${(bkBad||bkActive)?" open":""}>
          <summary class="sec-title">Backups <span class="n">${bkAll.length}</span></summary>
          <div class="wf-saved">${bkAll.length
            ? `<div class="bk-cards">${bkAll.map(x=>backupCard(x.host, x.b)).join("")}</div>`
            : `<div class="empty">no scheduled backups reporting — see docs/BACKUP-CONVENTION.md</div>`}
            ${bkUnreachable.length ? `<div class="empty">couldn't reach ${esc(bkUnreachable.join(", "))} — backup state unknown</div>` : ""}</div>
        </details>
      </main>`;
    // Legend toggle state is re-applied via state.summLegendOpen above — without
    // this, the next 2.5s poll's re-render would snap it shut on the user.
    const summEl = app.querySelector("details.summ-wrap");
    if(summEl) summEl.addEventListener("toggle", ()=>{ state.summLegendOpen = summEl.open; });
    app.querySelectorAll(".node[data-id]").forEach(el=>{
      const go = e => { if(e.target.closest(".card-drawers")) return; enterMachine(el.dataset.id); };
      el.addEventListener("click", go);
      el.addEventListener("keydown", e=>{
        if((e.key==="Enter"||e.key===" ") && !e.target.closest(".card-drawers")){ e.preventDefault(); enterMachine(el.dataset.id); }
      });
      // Per-card drawer open/close is remembered on `state` so the 2.5s poll
      // re-render doesn't snap a drawer shut while it's being read.
      const sync = (sel, set) => {
        const d = el.querySelector(sel);
        if(d) d.addEventListener("toggle", ()=>{ d.open ? set.add(el.dataset.id) : set.delete(el.dataset.id); });
      };
      sync(".rings-d", state.openRings);
      sync(".lines-d", state.openLines);
    });
    // Global toggles: unroll (or drop) rings / load lines across every card.
    const bindCtl = (id, key) => {
      const el = app.querySelector("#"+id);
      if(!el) return;
      const flip = ()=>{ state[key] = !state[key]; if(state[key]){ /* global open wins */ } render(); };
      el.addEventListener("click", flip);
      el.addEventListener("keydown", e=>{ if(e.key==="Enter"||e.key===" "){ e.preventDefault(); flip(); }});
    };
    bindCtl("ctlRings", "showRings");
    bindCtl("ctlLines", "showLines");
    app.querySelectorAll(".bk-card[data-bkhost]").forEach(el=>{
      el.addEventListener("click", ()=>openBackupWatch(el.dataset.bkhost, el.dataset.bkname));
    });
    JUST_ARRIVED.clear(); // the pop animation above only needs to play once
  }

  // backupTarget pulls the store's hostname out of a repository URL for the
  // "source → target" reading. The URL arrives already credential-stripped —
  // the runner redacts userinfo before it ever reaches a status file.
  function backupTarget(repo){
    const m = String(repo||"").match(/\/\/([^/:@]+)/);
    return m ? m[1] : "";
  }

  // hostMatchesTarget decides whether machine `m` is the box a backup's
  // repository points at. The target is a bare hostname pulled from the repo
  // URL ("nas"); a machine is identified by its agent name (m.id) or tailnet
  // IP (m.ip), either of which may be a MagicDNS name ("nas.tail1234.ts.net").
  // Match leniently — exact id/ip, or a shared first DNS label — so "nas"
  // resolves to the box however it happened to be addressed in the repo string.
  function hostMatchesTarget(m, tgt){
    if(!tgt || !m) return false;
    const t = String(tgt).toLowerCase();
    const id = String(m.id||"").toLowerCase();
    const ip = String(m.ip||"").toLowerCase();
    if(t===id || t===ip) return true;
    const label = s => s.split(".")[0];
    return !!label(id) && label(id)===label(t);
  }

  // backupSourcesFor lists every backup elsewhere in the fleet that ships *to*
  // this machine — the destination side of "source → target" that the map
  // otherwise never draws. A store like the NAS has no backup of its own, so
  // without this it reads as "unprotected" while quietly holding the whole
  // fleet's snapshots. A box's own backups are excluded: receiving from
  // yourself isn't receiving.
  function backupSourcesFor(m){
    const out = [];
    for(const h of CONV_BACKUPS){
      if(h.host===m.id) continue;
      for(const b of (h.backups||[])){
        if(hostMatchesTarget(m, backupTarget(b.repository))) out.push({host:h.host, b});
      }
    }
    return out;
  }

  // bkRunning is true while a run is in flight — the runner writes
  // "state":"running" at the start and overwrites it with the outcome when
  // restic exits. It is checked before ok/incomplete everywhere below because a
  // running run has no outcome yet: its status file carries no ok field, so
  // treating it as a normal run would read the absent ok as a failure.
  function bkRunning(b){ return !!b && b.state === "running"; }

  // bkPaused is true when a backup's timer has been deliberately switched off —
  // the agent sets "paused":true from systemd (is-enabled) when the
  // restic-backup@ timer is disabled or masked. It is the console's cue to stop
  // treating a stopped schedule as trouble: a paused backup no longer ages into
  // "at risk" the way a nightly that silently quit firing does, because someone
  // turned it off on purpose. It never overrides a real fault, though — a paused
  // backup whose last run actually failed is still read as failed below.
  function bkPaused(b){ return !!b && b.paused === true; }

  // bkStalled catches the failure mode a "running" marker introduces: a run
  // killed mid-flight (OOM, reboot, power) leaves "state":"running" on disk
  // forever, and a permanently-"running" backup would otherwise read as
  // healthier than a failed one — inverting the whole point of the console.
  //
  // The tell is that the run overran its own schedule. While a run is in
  // flight systemd already reports the *next* fire (one interval out), so once
  // that time is in the past and the run still hasn't finished, a full interval
  // has elapsed with no snapshot — wedged, not working. When next_run is
  // unknown (no systemd, disabled timer), fall back to a generous fixed ceiling
  // so a hung run still eventually surfaces rather than masquerading as active
  // forever. Both are deliberately conservative: a legitimately long first
  // backup must never be mislabelled stalled.
  const STALL_FALLBACK_MS = 24 * 3600e3;
  function bkStalled(b){
    if(!bkRunning(b)) return false;
    const started = Date.parse(b.started || "") || 0;
    if(!started) return false;
    const next = Date.parse(b.next_run || "") || 0;
    if(next) return Date.now() > next;
    return (Date.now() - started) > STALL_FALLBACK_MS;
  }

  // bkProgress returns the live byte progress of a run in flight, or null when
  // there is none worth drawing.
  //
  // Null covers more than "the field is missing": a runner too old to publish,
  // progress switched off on that box, the seconds before the first sample
  // lands, and — the one that matters — a sample that has gone stale. The agent
  // already refuses to attach progress unless systemd says the unit is running,
  // but systemd can be slower to notice a dead run than a reader is, and a
  // frozen percentage presented as live is worse than no percentage at all.
  // Every one of those falls back to the indeterminate shuttle.
  const PROGRESS_STALE_MS = 120e3;
  function bkProgress(b){
    if(!b || !bkRunning(b) || bkStalled(b)) return null;
    const p = b.progress;
    if(!p || typeof p.percent_done !== "number") return null;
    const updated = Date.parse(p.updated || "");
    if(!isNaN(updated) && Date.now() - updated > PROGRESS_STALE_MS) return null;
    return p;
  }

  // progressScanning reports whether restic is still working out how much there
  // is to do, in which case the percentage is measured against a total that
  // isn't final yet.
  //
  // restic's scanner walks the tree while the upload is already running, so
  // total_bytes climbs for the first stretch of a big backup and the percentage
  // can visibly go backwards. There is no scan-complete flag in the status line
  // to read, so the console watches the total itself and calls the scan done
  // once two consecutive *new* samples report an unchanged one. Comparing per
  // repaint instead would flicker, since the console repaints more often than
  // the runner publishes. Keyed by run (host+name+started) so a new run starts
  // over rather than inheriting the last one's verdict.
  const scanState = new Map();
  function progressScanning(host, b, p){
    if(typeof p.total_bytes !== "number") return false;
    const key = host + " " + (b.name || "");
    let st = scanState.get(key);
    if(!st || st.started !== (b.started || "")){
      st = { started: b.started || "", total: p.total_bytes, updated: p.updated || "", stable: 0 };
      scanState.set(key, st);
      return true;   // first sample of this run — assume the scan is still going
    }
    if(p.updated && p.updated !== st.updated){
      st.stable = (p.total_bytes === st.total) ? st.stable + 1 : 0;
      st.total = p.total_bytes;
      st.updated = p.updated;
    }
    return st.stable < 2;
  }

  // bkSeverity maps a run to one of four states — never two. A run in flight is
  // "run" (or "warn" once it has clearly stalled); otherwise restic's exit 3,
  // which leaves a real snapshot that is missing files, is its own "warn" state
  // between clean "good" and outright "crit" — folding it into either would hide
  // the outcome most likely to be mistaken for success.
  function bkSeverity(b){
    if(bkRunning(b)) return bkStalled(b) ? "warn" : "run";
    return b.ok ? "good" : (b.incomplete ? "warn" : "crit");
  }

  // bkPausedClean is a paused backup whose last real run was fine — the case the
  // per-backup cards render as "paused" (a warn-tinted caution) instead of the
  // green "complete" bkSeverity would give it. A paused backup whose last run
  // failed or came back incomplete deliberately does NOT qualify: pausing the
  // timer doesn't undo a broken snapshot, so those still show their fault. It is
  // kept out of bkSeverity itself because that function also tints a backup's
  // *destination* (receiveLine), and a paused source is not the vault's problem.
  function bkPausedClean(b){ return bkPaused(b) && !bkRunning(b) && !!b.ok; }

  /* ---------- backup posture: the map's primary signal ----------
     hush is a backup console now, so a machine's place on the map is decided by
     "is this box protected?", not by CPU. Posture reads the same /api/backup-status
     feed the Backups section renders (seeded in demo mode by demoConvBackups), so
     the map and the cards can never disagree. One of seven states, worst-wins:
       failed    — a run finished non-zero (restic couldn't write a snapshot)
       none      — reachable, but no backup configured (a total gap)
       risk      — a run was incomplete (exit 3), the newest snapshot is stale,
                    or a run has stalled mid-flight
       paused    — the timer was deliberately stopped (disabled/masked). A caution,
                    not a fault: it outranks a clean/stale/protected read so the
                    box shows "backup paused" instead of aging into "at risk", but
                    yields to a genuine failed/incomplete/stalled last run.
       running   — a run is in flight and healthy — working on it, not a verdict
       protected — every backup ran clean and recently
       unknown   — the box couldn't be asked (backup state genuinely unknown) */
  function hostBackupEntry(id){ return CONV_BACKUPS.find(h=>h.host===id); }
  function backupPosture(id){
    const h = hostBackupEntry(id);
    if(!h || !h.reachable) return "unknown";
    const bks = h.backups || [];
    if(!bks.length) return "none";
    // A stalled run is trouble that hides as activity — surface it, worst of
    // the running cases, before any healthy-run check below can absolve it.
    if(bks.some(b=>bkStalled(b))) return "risk";
    if(bks.some(b=>!bkRunning(b) && !b.ok && !b.incomplete)) return "failed";
    if(bks.some(b=>b.incomplete)) return "risk";
    // A healthy run in flight is "working on it": not failed, but not yet a
    // snapshot either, so it doesn't get to read as protected until it lands.
    // It ranks below the clean/stale checks so a box whose other backups are
    // fine isn't dragged to "running" by one that happens to be mid-run.
    if(bks.some(b=>bkRunning(b))) return "running";
    // Paused: the timer was switched off on purpose. Checked before the stale
    // rule below precisely so a long-paused backup stops nagging as "at risk" —
    // that is the whole point of the state — and before the clean/protected
    // return so an intentionally-stopped backup reads as paused rather than
    // quietly green. It sits *after* the failed/incomplete/stalled checks: a
    // paused backup whose last real run broke is still broken.
    if(bks.some(b=>bkPaused(b))) return "paused";
    // Stale: a nightly that missed a night. Newest run older than ~36h is the
    // cheapest honest "the schedule stopped firing" tell without per-box cadence.
    const newest = bks.reduce((t,b)=>Math.max(t, Date.parse(b.finished||b.started||"")||0), 0);
    if(newest && (Date.now()-newest) > 36*3600e3) return "risk";
    return "protected";
  }
  const POSTURE = {
    failed:    { sev:"crit",  glyph:"✕", label:"backup failed" },
    none:      { sev:"warn",  glyph:"○", label:"no backup" },
    risk:      { sev:"warn",  glyph:"▲", label:"at risk" },
    paused:    { sev:"warn",  glyph:"⏸", label:"backup paused" },
    running:   { sev:"run",   glyph:"⟳", label:"backing up" },
    protected: { sev:"good",  glyph:"✓", label:"protected" },
    unknown:   { sev:"muted", glyph:"·", label:"unknown" },
  };
  // Ordering for "trouble floats to the top" — a failed backup is the loudest,
  // then an outright gap, then an at-risk run, then boxes we couldn't ask. A
  // paused backup sits below unknown (a deliberate stop is less alarming than a
  // box we can't reach) but above a healthy run and protected, so a box someone
  // switched off surfaces instead of hiding among the green. A healthy
  // in-progress run sits just above protected: not trouble, but worth surfacing
  // so someone watching a run can find it without hunting.
  const postureRank = { failed:6, none:5, risk:4, unknown:3, paused:2, running:1, protected:0 };

  // backupPathsFor: every path any of a host's configured backups covers —
  // flattened across all its backup jobs, since a treemap cell's coverage
  // doesn't care which job backs it up. Feeds the disk-usage treemap's
  // per-cell coverage badge (openDu, below).
  function backupPathsFor(id){
    const h = hostBackupEntry(id);
    if(!h) return [];
    return (h.backups||[]).flatMap(b => b.paths||[]);
  }
  // duCoverage: how a treemap cell's absolute path relates to a host's backup
  // paths. "full" when a backup path is the cell itself or an ancestor of it
  // (the whole cell is inside a covered subtree, including a repo backing up
  // "/"); "partial" when a backup path is nested inside the cell without the
  // cell itself being covered (only part of this subtree is backed up);
  // "none" otherwise.
  function duCoverage(path, backupPaths){
    const covers = p => p === "/" || p === path || path.startsWith(p + "/");
    if(backupPaths.some(p => covers(p))) return "full";
    if(backupPaths.some(p => p.startsWith(path === "/" ? "/" : path + "/"))) return "partial";
    return "none";
  }

  // bkRank picks the backup that *defines* a host's posture so the node-card
  // line leads with the run that matters, not just the first. A stalled run is
  // the loudest; then failed, then incomplete, then a healthy in-flight run,
  // then a paused one. A healthy run ranks just above a paused or clean backup,
  // so on a box that's mid-run the line leads with "backing up" rather than
  // yesterday's snapshot; a paused job outranks only a clean one, so a box
  // that's been switched off leads with "paused" — but never outranks a real
  // problem elsewhere on the box.
  function bkRank(b){
    if(bkStalled(b)) return 5;
    if(!bkRunning(b) && !b.ok && !b.incomplete) return 4;
    if(b.incomplete) return 3;
    if(bkRunning(b)) return 2;
    if(bkPaused(b)) return 1;
    return 0;
  }

  // backupLine is the always-on backup readout on every Fleet card: posture
  // glyph + the worst run's outcome, when it ran, and where it ships to.
  function backupLine(m){
    const p = backupPosture(m.id);
    const meta = POSTURE[p];
    const bks = ((hostBackupEntry(m.id)||{}).backups) || [];
    const lead = bks.slice().sort((a,b)=>bkRank(b)-bkRank(a))[0];
    let detail;
    if(p==="none") detail = "no backup configured";
    else if(p==="unknown" || !lead) detail = "backup state unknown";
    else {
      const when = relTimeISO(lead.finished || lead.started || "");
      const tgt = backupTarget(lead.repository);
      // For a run in flight `when` is when it started, not when it finished —
      // "backing up · 4m ago" reads as "started 4m ago, still going".
      const word = p==="failed" ? "backup failed"
        : p==="running" ? "backing up"
        : p==="paused" ? "paused"
        : p==="risk" ? (bkStalled(lead) ? "stalled" : lead.incomplete ? "incomplete" : "stale")
        : "protected";
      detail = `${word}${when?` · ${when}`:""}${tgt?` <span class="ar">→</span> ${esc(tgt)}`:""}`;
    }
    return `<div class="bkline ${meta.sev}"><span class="g">${meta.glyph}</span><span class="t">${detail}</span></div>`;
  }

  // receiveLine is backupLine's mirror on a target's own card: the box others
  // back up to now says so, instead of reading as an unprotected gap. It stays
  // neutral while every incoming run is healthy — this line is about "backups
  // land here", not a verdict on this box — but tints warn/crit when an upload
  // to it has stalled or failed, so trouble is visible on the destination and
  // not only on the source that reported it.
  function receiveLine(m){
    const src = backupSourcesFor(m);
    if(!src.length) return "";
    const sevs = src.map(x=>bkSeverity(x.b));
    const tint = sevs.includes("crit") ? " crit"
      : sevs.includes("warn") ? " warn"
      : sevs.includes("run") ? " run" : "";
    const n = src.length;
    return `<div class="bkline recv${tint}"><span class="g">↙</span><span class="t">${n} ${n===1?"box backs":"boxes back"} up here</span></div>`;
  }

  /* ---------- local inference readout ----------
     A box that serves models is only useful to the fleet if something off-box
     can reach it, and the default for llama-swap and Ollama alike is a loopback
     bind. So the line reports capability and reachability as one verdict rather
     than listing models and letting the reader assume they're callable:

       local    — runtimes are up but bound to loopback: models exist here, and
                  nothing else on the fleet can use them. Yellow, because it's a
                  live capability sitting idle, not a fault.
       serving  — reachable over the tailnet. Green: this box can take work.
       open     — bound to every interface, which for an unauthenticated
                  inference API is wider than the tailnet. Red, on purpose.
       unknown  — the agent couldn't read the listener table, so the scope was
                  never verified. Muted; it must not read as "not exposed". */
  const LLM_SCOPE = {
    open:     { sev:"crit",  glyph:"◉", word:"open to all interfaces" },
    tailnet:  { sev:"good",  glyph:"✓", word:"serving on tailnet" },
    loopback: { sev:"warn",  glyph:"▲", word:"local only" },
    unknown:  { sev:"",      glyph:"·", word:"reach unknown" },
  };
  // Widest scope wins: one runtime open to the world defines the box, even if
  // the others are loopback-bound.
  const llmScopeRank = { open:3, tailnet:2, unknown:1, loopback:0 };

  function llmLine(m){
    // Absent (older agent, or detection off) is not the same claim as "no LLM
    // here", so an unreporting agent gets no line rather than a negative one.
    const rts = (m.llm && m.llm.runtimes) || [];
    if(!rts.length) return "";

    const scope = rts.map(r=>r.exposure||"unknown")
      .reduce((a,b)=> llmScopeRank[b]>llmScopeRank[a] ? b : a, "loopback");
    const meta = LLM_SCOPE[scope] || LLM_SCOPE.unknown;

    const models = [...new Set(rts.flatMap(r=>r.models||[]))].sort();
    const n = models.length;
    // Hover carries the full catalogue and per-runtime detail; the line itself
    // stays one row so a dense fleet grid doesn't grow a paragraph per card.
    const tip = rts.map(r=>`${r.kind==="ollama"?"ollama":"openai"} @ ${r.addr} (${r.exposure||"unknown"})`)
      .concat(models).join("\n");
    return `<div class="bkline llm ${meta.sev}" title="${esc(tip)}">`
      + `<span class="g">${meta.glyph}</span>`
      + `<span class="t">llm <span class="ar">·</span> ${meta.word}`
      + `${n?` <span class="ar">·</span> ${n} model${n===1?"":"s"}`:""}</span></div>`;
  }

  /* ---------- opencode.json from a box's detected runtimes ----------
     A machine serving reachable models is something you'd point a coding agent
     at. These turn what the LLM readout already knows — kind, bind address,
     served model ids — into a ready opencode config the operator copies or
     downloads from the machine view. Pure client-side; no new agent data. */

  // splitAddr breaks a runtime's reported "host:port" (bracketed for IPv6) so a
  // baseURL can be rebuilt from it.
  function splitAddr(addr){
    addr = addr || "";
    if(addr[0]==="["){ const j=addr.indexOf("]"); return { host:addr.slice(1,j), port:addr.slice(j+2) }; }
    const i = addr.lastIndexOf(":");
    return i<0 ? { host:addr, port:"" } : { host:addr.slice(0,i), port:addr.slice(i+1) };
  }

  // ocReachable is the subset of a box's runtimes something off-box can actually
  // call — the same tailnet|open verdict the agent applies. A loopback runtime
  // is bound to 127.0.0.1, so pointing opencode at it from another machine can
  // only fail; it's never offered as an export.
  function ocReachable(m){
    return ((m.llm&&m.llm.runtimes)||[]).filter(r=>r.exposure==="tailnet"||r.exposure==="open");
  }

  // ocConfig builds the opencode.json object for a box's reachable runtimes.
  // Each runtime becomes one openai-compatible provider whose baseURL points at
  // where it actually listens — with one substitution: a wildcard bind
  // (0.0.0.0 / ::, what an "open" runtime reports) is not a usable host, so the
  // box's tailnet IP stands in. cost is pinned to zero because local inference
  // is free, which keeps opencode from inventing dollar figures. A context
  // limit is emitted only when the operator supplies one: the runtime doesn't
  // advertise its configured context, and a wrong guess is worse than letting
  // opencode fall back to its own default.
  function ocConfig(m, ctx){
    const baseKey = (m.id||"local").toLowerCase().replace(/[^a-z0-9-]+/g,"-").replace(/^-+|-+$/g,"") || "local";
    const rts = ocReachable(m);
    const provider = {};
    rts.forEach((r,idx)=>{
      const { host, port } = splitAddr(r.addr);
      let h = host;
      if(h==="0.0.0.0" || h==="::" || h==="") h = m.ip || h;
      const hostPart = h.includes(":") ? `[${h}]` : h;
      const baseURL = `http://${hostPart}${port?":"+port:""}/v1`;
      const key = rts.length>1 ? `${baseKey}-${port||idx+1}` : baseKey;
      const models = {};
      (r.models||[]).forEach(id=>{
        const mdl = { name:id, cost:{ input:0, output:0 } };
        if(ctx>0) mdl.limit = { context:ctx };
        models[id] = mdl;
      });
      provider[key] = {
        npm: "@ai-sdk/openai-compatible",
        name: `${m.id} · ${r.kind==="ollama"?"ollama":"openai-compatible"}`,
        options: { baseURL },
        models,
      };
    });
    return { "$schema":"https://opencode.ai/config.json", provider };
  }

  /* ---------- sessions: running coding agents (opencode / claude) ----------
     A session is a coding-agent process on a box, owned by a Unix user. hush
     only ever *reads* them — the agent's /sessions is a privilege-free /proc
     scan — and never spawns or kills one. Both actions are sudo commands the
     console composes for you to paste over SSH (JuiceSSH from a phone); control
     stays a pure visualisation. See docs/SESSIONS.md. */
  const SESSIONS = {};              // host -> { sessions:[…], loaded:true, err:"" }
  const USERS = {};                 // host -> { users:[…], loaded:true, err:"" }
  const SESSIONS_POLL_MS = 6000;    // sessions/users change slowly; no need to ride the 2.5s fleet cadence
  const USER_RE = /^[a-z_][a-z0-9_-]*$/;   // a conservative POSIX login, to keep a typo out of the generated command

  // OC_SERVER_USER is the one dedicated, unprivileged Unix account every
  // opencode server runs as, on every box — never root, never a personal
  // login. There's no picker for it: the composed Start command provisions
  // the account itself (system user, own group, no shell, no login) the same
  // way install.sh provisions the "hush" agent user, so there's nothing to
  // type or get wrong.
  const OC_SERVER_USER = "opencode";

  function onMachine(id){ return state.view==="machine" && state.mid===id; }

  // fetchSessions refreshes one machine's session list into SESSIONS, then
  // repaints the machine view if it's still the one on screen. Demo mode has no
  // agent to ask, so it fabricates a stable per-box list. An old agent (no
  // /sessions handler) answers 404 — flagged distinctly so the section can say
  // "update the agent" rather than "nothing running".
  async function fetchSessions(id){
    if(MODE==="demo"){ SESSIONS[id] = { sessions:demoSessions(id), installed:demoInstalled(id), loaded:true, err:"" }; if(onMachine(id)) renderMachine(); return; }
    try {
      const r = await fetch(`api/machines/${encodeURIComponent(id)}/sessions`, {cache:"no-store"});
      if(!r.ok){ SESSIONS[id] = { sessions:[], loaded:true, err:r.status===404?"old-agent":"unreach" }; }
      // installed is null when an agent serves /sessions but predates the field
      // (an older new-agent) — the tools strip stays hidden rather than guessing.
      else { const data = await r.json(); SESSIONS[id] = { sessions:(data&&data.sessions)||[], installed:(data&&data.installed)||null, loaded:true, err:"" }; }
    } catch(e){ SESSIONS[id] = { sessions:[], loaded:true, err:"unreach" }; }
    if(onMachine(id)) renderMachine();
  }

  // fetchUsers refreshes one machine's human-account list into USERS, the same
  // shape and cadence as fetchSessions — same demo fallback, same 404-means-
  // "old agent" handling. Kept as its own poll (rather than folded into
  // /sessions) because it reads a different agent endpoint.
  async function fetchUsers(id){
    if(MODE==="demo"){ USERS[id] = { users:demoUsers(id), loaded:true, err:"" }; if(onMachine(id)) renderMachine(); return; }
    try {
      const r = await fetch(`api/machines/${encodeURIComponent(id)}/users`, {cache:"no-store"});
      if(!r.ok){ USERS[id] = { users:[], loaded:true, err:r.status===404?"old-agent":"unreach" }; }
      else { const data = await r.json(); USERS[id] = { users:(data&&data.users)||[], loaded:true, err:"" }; }
    } catch(e){ USERS[id] = { users:[], loaded:true, err:"unreach" }; }
    if(onMachine(id)) renderMachine();
  }

  // userToolsFor returns the distinct coding-agent tools a named user has
  // running on host, from the last /sessions read — the join that lets the
  // Users list show a glyph next to anyone with a live session, without the
  // /users endpoint itself needing to know about sessions.
  function userToolsFor(host, name){
    const list = (SESSIONS[host] && SESSIONS[host].sessions) || [];
    const tools = [];
    list.forEach(s=>{ if(s.user===name && tools.indexOf(s.tool)===-1) tools.push(s.tool); });
    return tools;
  }

  function seGlyph(tool){ return tool==="claude" ? "✳" : "⌁"; }
  function fmtUptime(sec){
    if(!sec || sec<0) return "";
    if(sec<60) return sec+"s";
    const m=Math.round(sec/60); if(m<60) return m+"m";
    const h=Math.floor(m/60); return h<24 ? h+"h" : Math.floor(h/24)+"d";
  }

  // KNOWN_TOOLS are the coding agents the console can install system-wide, each
  // mapped to its npm package. The install and update commands are the same
  // idempotent line — `npm install -g <pkg>@latest` installs when absent and
  // updates when present — so "always offer update" needs no version check.
  const KNOWN_TOOLS = [
    { tool:"opencode", pkg:"opencode-ai" },
    { tool:"claude",   pkg:"@anthropic-ai/claude-code" },
  ];
  function toolInstallCmd(pkg){ return `sudo npm install -g ${pkg}@latest`; }

  // toolInstalledOn reports a tool's installed state on a host from the last
  // /sessions read: true (present), false (reported absent), or null (unknown —
  // agent too old, offline, or not yet loaded). The spawn sheet uses it to warn
  // before you paste a command that would fail on a missing binary.
  function toolInstalledOn(host, tool){
    const st = SESSIONS[host], inst = st && st.installed;
    if(!inst) return null;
    const rec = inst.find(i=>i.tool===tool);
    return rec ? !!rec.present : null;
  }

  // toolsStrip renders, above the running-session list, which known coding
  // agents are installed system-wide on the box — each with an Install (absent)
  // or Update (present) button that opens the one npm command to fix it. It
  // shows only when the agent actually reported installed state (a new-enough
  // agent, online, no read error); otherwise it's silent and the session body
  // below carries any "update the agent" / "unreachable" message on its own.
  function toolsStrip(m){
    const st = SESSIONS[m.id];
    const inst = st && st.installed;      // array (reported) or null/undefined (unknown)
    if(!inst || !inst.length) return "";
    const byTool = {}; inst.forEach(i=>{ byTool[i.tool] = i; });
    const rows = KNOWN_TOOLS.map(kt=>{
      const rec = byTool[kt.tool];
      if(!rec) return "";                 // this agent doesn't track that tool
      const present = !!rec.present;
      return `<div class="tl-row ${present?"on":"off"}">
        <span class="tl-gly">${seGlyph(kt.tool)}</span>
        <div class="tl-main">
          <div class="tl-top"><span class="tl-tool">${esc(kt.tool)}</span>
            <span class="tl-state">${present?"installed":"not installed"}</span></div>
          ${present&&rec.path?`<span class="tl-path">${esc(rec.path)}</span>`:""}
        </div>
        <button class="btn tl-fix" data-tool="${esc(kt.tool)}" data-present="${present?1:0}">${present?"Update":"Install"}</button>
      </div>`;
    }).join("");
    return rows ? `<div class="tl-strip">${rows}</div>` : "";
  }

  // serverSessions returns the detected `opencode serve` processes on a box —
  // the headless servers an opencode mobile client attaches to, as distinct
  // from the interactive sessions above. The agent flags them (session.server)
  // and resolves their reach, so this is a plain filter of the last read.
  function serverSessions(host){
    const list = (SESSIONS[host] && SESSIONS[host].sessions) || [];
    return list.filter(s=>s.server);
  }

  // serverURL turns a reachable server's reported bind address into the URL you
  // paste into opencode mobile's "add server". A wildcard bind (0.0.0.0 / ::,
  // what an "open" server reports) is not a dialable host, so the box's tailnet
  // IP stands in — the same substitution ocConfig makes for a wildcard runtime.
  // A loopback-only or unknown-reach server has no off-box URL and returns "":
  // the console must never hand out an address a phone can't actually reach.
  function serverURL(m, s){
    if(!s || (s.exposure!=="tailnet" && s.exposure!=="open")) return "";
    const { host, port } = splitAddr(s.addr);
    let h = host;
    if(h==="0.0.0.0" || h==="::" || h==="") h = m.ip || h;
    if(!h) return "";
    const hostPart = h.includes(":") ? `[${h}]` : h;
    return `http://${hostPart}${port?":"+port:""}`;
  }

  // rollupSec wraps a machine-view section in a <details class="rollup"> so it
  // starts rolled up (collapsed) and stays that way across the 2.5s poll
  // re-render — same posture as openRings/openLines on the Fleet page, tracked
  // per section key on `state` and reapplied here rather than left to the
  // browser, which would otherwise snap every open section shut on each poll.
  function rollupSec(key, titleHtml, bodyHtml){
    return `<details class="rollup" data-sec="${key}"${state.openSections.has(key)?" open":""}>
      <summary class="sec-title">${titleHtml}</summary>
      ${bodyHtml}
    </details>`;
  }

  // serverSection renders the machine view's opencode-server block, above
  // Sessions. It answers the one question opencode mobile asks — "what URL do I
  // add?" — from what the box already reports: a reachable `opencode serve`
  // shows its URL with a Copy button; a running-but-loopback server is shown as
  // present-but-unreachable (so you know it's there and why you can't add it);
  // and when none is running, a Start button composes the sandboxed launch
  // command (see buildServerCmd). Either running state also gets a Stop button
  // (see openServerStopSheet) — same posture as Sessions' Stop: hush only reads
  // and composes, it never starts or stops the server itself.
  function serverSection(m){
    const st = SESSIONS[m.id];
    const servers = serverSessions(m.id);
    let body;
    if(!m.online && MODE!=="demo"){
      body = `<div class="empty">machine unreachable — bring it online to see its opencode server</div>`;
    } else if(st && st.err==="old-agent"){
      body = `<div class="empty">this agent is too old to report an opencode server — update hush-agent</div>`;
    } else if(servers.length){
      body = servers.map(s=>{
        const url = serverURL(m, s);
        const meta = LLM_SCOPE[s.exposure] || LLM_SCOPE.unknown;
        const up = fmtUptime(s.uptime);
        const stopBtn = `<button class="btn ocs-stop">Stop</button>`;
        if(url){
          return `<div class="oc-rt good">
            <div class="oc-rt-hd">
              <span class="oc-gly">${meta.glyph}</span>
              <span class="oc-kind">opencode server</span>
              <span class="oc-scope">${meta.word}${up?` · ${up}`:""}</span>
            </div>
            <div class="ocs-url"><code class="ocs-url-t">${esc(url)}</code>
              <button class="btn ocs-copy" data-url="${esc(url)}">Copy URL</button>
              ${stopBtn}</div>
            <div class="hint">add this in opencode mobile → Servers → add — it exposes the fleet's models this box was pointed at</div>
          </div>`;
        }
        return `<div class="oc-rt warn">
          <div class="oc-rt-hd">
            <span class="oc-gly">▲</span>
            <span class="oc-kind">opencode server</span>
            <span class="oc-addr">${esc(s.addr||"")}</span>
            <span class="oc-scope">local only${up?` · ${up}`:""}</span>
          </div>
          <div class="hint">running but bound to loopback — a phone can't reach it. Restart it bound to the tailnet (Start below) to get a URL.</div>
          <div class="sheet-actions">${stopBtn}</div>
        </div>`;
      }).join("");
    } else {
      body = `<div class="empty">no opencode server running — start one to add this box in opencode mobile and work against the fleet's models</div>`;
    }
    return rollupSec("server", `opencode server${servers.length?` <span class="n">${servers.length}</span>`:""}`,
      `<div class="oc-sec">
        ${body}
        <div class="sheet-actions">
          <button class="btn primary ocs-start" id="ocs-start">＋ Start opencode server</button>
        </div>
      </div>`);
  }

  // sessionsSection renders the machine view's Sessions block: which coding
  // agents are installed system-wide (the tools strip), the ones running now
  // (each with a Stop button that opens its sudo kill command) and a Spawn
  // button that composes the launch command. Spawn is always offered — the
  // command is text to paste, so it works even against an agent too old to
  // *report* sessions.
  function sessionsSection(m){
    const st = SESSIONS[m.id];
    const list = (st&&st.sessions)||[];
    let body;
    if(!m.online && MODE!=="demo"){
      body = `<div class="empty">machine unreachable — bring it online to see running sessions</div>`;
    } else if(!st || !st.loaded){
      body = `<div class="empty">reading…</div>`;
    } else if(st.err==="old-agent"){
      body = `<div class="empty">this agent is too old to report sessions — update hush-agent to see what's running</div>`;
    } else if(st.err==="unreach"){
      body = `<div class="empty">couldn't read sessions from this machine</div>`;
    } else if(!list.length){
      body = `<div class="empty">no coding-agent sessions running</div>`;
    } else {
      body = list.map(s=>{
        const up = fmtUptime(s.uptime);
        return `<div class="se-row">
          <span class="se-gly">${seGlyph(s.tool)}</span>
          <div class="se-main">
            <div class="se-top"><span class="se-tool">${esc(s.tool)}</span>
              <span class="se-user">${esc(s.user||"?")}</span>
              ${up?`<span class="se-up">${up}</span>`:""}</div>
            ${s.cmd?`<span class="se-cmd">${esc(s.cmd)}</span>`:""}
          </div>
          <button class="btn se-stop" data-pid="${s.pid}" data-user="${esc(s.user||"")}" data-tool="${esc(s.tool)}">Stop</button>
        </div>`;
      }).join("");
    }
    return rollupSec("sessions", `Sessions${list.length?` <span class="n">${list.length}</span>`:""}`,
      `<div class="se-sec">
        ${toolsStrip(m)}
        ${body}
        <div class="sheet-actions">
          <button class="btn primary se-spawn" id="se-spawn">＋ Spawn a session</button>
        </div>
        ${list.length>=3?`<div class="sheet-actions">
          <button class="btn se-stop-all" id="se-stop-all">Stop all ${list.length}</button>
        </div>`:""}
      </div>`);
  }

  // usersSection renders the machine view's Users block, directly under
  // Sessions: the box's human login accounts (from /users), each with a glyph
  // for every coding-agent tool it currently has a session running (the join
  // against SESSIONS — see userToolsFor) so you can see who's active at a
  // glance, and the ＋ Add a user button that opens the create-user command
  // sheet.
  function usersSection(m){
    const st = USERS[m.id];
    const list = (st&&st.users)||[];
    let body;
    if(!m.online && MODE!=="demo"){
      body = `<div class="empty">machine unreachable — bring it online to see its users</div>`;
    } else if(!st || !st.loaded){
      body = `<div class="empty">reading…</div>`;
    } else if(st.err==="old-agent"){
      body = `<div class="empty">this agent is too old to report users — update hush-agent to see who's on this box</div>`;
    } else if(st.err==="unreach"){
      body = `<div class="empty">couldn't read users from this machine</div>`;
    } else if(!list.length){
      body = `<div class="empty">no user accounts found</div>`;
    } else {
      body = list.map(u=>{
        const tools = userToolsFor(m.id, u.name);
        return `<div class="us-row">
          <span class="us-gly">◎</span>
          <div class="us-main">
            <div class="us-top"><span class="us-name">${esc(u.name)}</span>
              <span class="us-uid">${u.uid}</span></div>
            ${u.home?`<span class="us-home">${esc(u.home)}</span>`:""}
          </div>
          ${tools.length?`<div class="us-tools">${tools.map(t=>`<span class="us-tool-gly" title="${esc(t)} running">${seGlyph(t)}</span>`).join("")}</div>`:""}
        </div>`;
      }).join("");
    }
    return rollupSec("users", `Users${list.length?` <span class="n">${list.length}</span>`:""}`,
      `<div class="us-sec">
        ${body}
        <div class="sheet-actions">
          <button class="btn us-adduser" id="us-adduser">＋ Add a user</button>
        </div>
      </div>`);
  }

  /* ---------- spawn / stop command sheets ----------
     The write half of the Session construct is not a hush write at all: it's a
     command you run yourself, as root, on the target box. These sheets compose
     that command and hand it to you to copy into JuiceSSH — the exact posture
     the backup convention uses. */
  const spawnScrim = $("#spawnScrim");
  const spawnBody = $("#spawnSheetBody");
  let spHost=null, spTool="opencode", spUser="", spTarget="";
  // server-sheet state: the box, chosen LLM target, port and the generated
  // password. No run-as-user field — the server always runs as the one fixed
  // OC_SERVER_USER account. Kept beside the spawn state because both sheets
  // share the one scrim/body and the same "compose a command you paste"
  // posture.
  let svTarget="", svPort="4096", svPass="";
  function closeSpawn(){ spawnScrim.classList.remove("open"); }
  spawnScrim.addEventListener("click", e=>{ if(e.target===spawnScrim) closeSpawn(); });

  // spawnTargets are the fleet's LLM boxes something off-box can call — the same
  // tailnet|open reachable set the opencode export offers. Spawning opencode
  // points it at one of these by writing that box's opencode.json.
  function spawnTargets(){ return M.filter(x=>ocReachable(x).length); }

  // buildSpawnCmd composes the one &&-chained sudo command that (for opencode)
  // writes the chosen LLM's opencode.json — base64-piped so the JSON can't
  // collide with the command's own quoting — and launches the tool in a named
  // tmux session you can attach to now and reattach to later. `sudo -u <user>`
  // is the whole user preflight — it fails loudly on its own if the user doesn't
  // exist.
  //
  // The tool is expected to be installed system-wide (via the Sessions tools
  // strip, which composes an `npm install -g` command) so it's already on the
  // box PATH — the spawn command no longer self-installs into the user's home,
  // so there's no per-user install and no `$HOME/.opencode/bin` PATH juggling.
  // If the tool isn't installed the `exec` fails on its own, the same honest way
  // `sudo -u` fails on a missing user; install it once from the strip first.
  //
  // claude launches with --remote-control so the session (typed into from
  // JuiceSSH, same as before) is also steerable from claude.ai/code or the
  // Claude app. It degrades gracefully rather than failing the chain: an
  // account not yet signed in just gets a local session with a Remote Control
  // failure notice. CLAUDE_REMOTE_CONTROL_SESSION_NAME_PREFIX sets the session's
  // auto-generated name to "<fleet machine>.<user>-adjective-noun" — Claude
  // Code's own naming convention, prefixed with the box hush spawned it on
  // (instead of the box's raw OS hostname, which may not match) and the user
  // it's running as, so two operators spawning on the same box get
  // distinguishable titles. A colon would read more like "host:user", but it's
  // outside the sanitized charset below (and awkward in tmux/URL contexts), so
  // a dot stands in for it. It also launches with
  // --dangerously-skip-permissions, since the session already sits behind
  // sudo -u <user> as the whole preflight — the operator chose this box and this
  // user before pasting the command, so a second per-tool-call prompt inside the
  // session is redundant friction, not additional safety.
  function buildSpawnCmd(){
    const u = spUser.trim();
    if(!USER_RE.test(u)) return { cmd:"", err:"enter a Linux username to run as" };
    let lines = "";
    if(spTool==="opencode"){
      const t = spTarget && byId(spTarget);
      if(t){
        const json = JSON.stringify(ocConfig(t, 0), null, 2);
        const b64 = btoa(unescape(encodeURIComponent(json)));
        lines += `  mkdir -p "$HOME/.config/opencode" &&\n`;
        lines += `  printf %s "${b64}" | base64 -d > "$HOME/.config/opencode/opencode.json" &&\n`;
      }
      lines += `  exec tmux new-session -A -s hush-opencode opencode`;
    } else {
      const prefix = `${spHost||""}.${u}`.replace(/[^a-zA-Z0-9._-]/g, "-");
      lines += `  export CLAUDE_REMOTE_CONTROL_SESSION_NAME_PREFIX="${prefix}" &&\n`;
      lines += `  exec tmux new-session -A -s hush-claude claude --remote-control --dangerously-skip-permissions`;
    }
    return { cmd:`sudo -u ${u} -H bash -lc '\n${lines}'`, err:"" };
  }

  function openSpawnSheet(host){
    spHost = host; spTool = "opencode"; spUser = "";
    const ts = spawnTargets();
    // Default the LLM target to this box if it serves one, else the first box on
    // the fleet that does — the common case is "spawn here, point at the GPU box".
    const here = ts.find(x=>x.id===host);
    spTarget = here ? here.id : (ts[0] ? ts[0].id : "");
    renderSpawnSheet();
    spawnScrim.classList.add("open");
  }

  function renderSpawnSheet(){
    const ts = spawnTargets();
    const targetField = spTool==="opencode" ? (ts.length
      ? `<div class="field">
           <label for="spTarget">point opencode at</label>
           <select id="spTarget">${ts.map(x=>`<option value="${esc(x.id)}"${x.id===spTarget?" selected":""}>${esc(x.id)} · ${ocReachable(x).reduce((n,r)=>n+((r.models||[]).length),0)} models</option>`).join("")}</select>
           <div class="hint">writes that box's tailnet LLM into ~/.config/opencode/opencode.json before launching</div>
         </div>`
      : `<div class="field-note">no tailnet-reachable LLM on the fleet — opencode will launch with whatever config already exists on the box. Expose a runtime (see a machine's Inference section) to point it at one.</div>`)
      : `<div class="field-note">claude uses its own Anthropic login (run <span style="font-family:var(--mono)">claude</span> once in the session to sign in) and launches with <span style="font-family:var(--mono)">--remote-control</span>, so once signed in it's also steerable from claude.ai/code or the Claude app — sign-in still happens on the box, hush doesn't hold a credential. Routing claude at a hush-net endpoint is a follow-up.</div>`;
    // Warn (don't block) when the chosen tool is reported absent on this box —
    // spawn no longer self-installs, so the command would fail on the exec.
    const notInstalledNote = toolInstalledOn(spHost, spTool)===false
      ? `<div class="field-note" style="color:var(--warn);border-color:var(--warn-soft)"><span style="font-family:var(--mono)">${esc(spTool)}</span> isn't installed on ${esc(spHost)} — this command will fail until it is. Install it system-wide from the Sessions list first.</div>`
      : "";
    const { cmd, err } = buildSpawnCmd();
    spawnBody.innerHTML = `
      <h3>Spawn a session <span class="sd" style="font-family:var(--mono)">${esc(spHost)}</span></h3>
      <p class="sd">compose the command, then paste it into a root shell on ${esc(spHost)} (JuiceSSH) — hush never runs it for you</p>
      <div class="field">
        <label for="spUser">run as user</label>
        <input id="spUser" type="text" autocapitalize="off" autocomplete="off" spellcheck="false"
          placeholder="e.g. josh" value="${esc(spUser)}">
        <div class="hint">the session runs as this user; the command fails on its own if the user doesn't exist</div>
      </div>
      <div class="field">
        <label>tool</label>
        <div class="se-seg">
          <button type="button" data-tool="opencode" class="${spTool==="opencode"?"on":""}">opencode</button>
          <button type="button" data-tool="claude" class="${spTool==="claude"?"on":""}">claude</button>
        </div>
      </div>
      ${targetField}
      ${notInstalledNote}
      <div class="cmdbox"><code id="spCmd">${esc(cmd||err)}</code><button class="btn" id="spCopy">Copy</button></div>
      <div class="field-note">Launches in a tmux session named <span style="font-family:var(--mono)">hush-${esc(spTool)}</span> — detach with <span style="font-family:var(--mono)">Ctrl-b d</span> and it keeps running (hush will show it); re-run to reattach.${spTool==="claude"?` claude's own Remote Control session (separate from the tmux session) is named <span style="font-family:var(--mono)">${esc(spHost)}.${esc(spUser.trim())}-adjective-noun</span>, matching Claude Code's own auto-naming convention, until you <span style="font-family:var(--mono)">/rename</span> it.`:""}</div>
      <div class="sheet-actions"><button class="btn primary" id="spClose" style="flex:1">Close</button></div>`;

    const code = $("#spCmd"), copy = $("#spCopy");
    const refresh = ()=>{ const r = buildSpawnCmd(); code.textContent = r.cmd || r.err; copy.disabled = !r.cmd; };
    copy.disabled = !cmd;
    const ui = $("#spUser");
    ui.addEventListener("input", ()=>{ spUser = ui.value; refresh(); });
    spawnBody.querySelectorAll(".se-seg button").forEach(b=>{
      b.addEventListener("click", ()=>{ spTool = b.dataset.tool; renderSpawnSheet(); });
    });
    const sel = $("#spTarget");
    if(sel) sel.addEventListener("change", ()=>{ spTarget = sel.value; refresh(); });
    copy.addEventListener("click", async ()=>{
      const r = buildSpawnCmd(); if(!r.cmd) return;
      try { await navigator.clipboard.writeText(r.cmd); toast("copied — paste into JuiceSSH on "+spHost); }
      catch(e){ toast("couldn't copy — select and copy manually"); }
    });
    $("#spClose").addEventListener("click", closeSpawn);
    // keep focus in the user field across the initial paint so you can just type
    if(document.activeElement !== ui) ui.focus();
  }

  /* ---------- start opencode server sheet ----------
     The server is the thing opencode mobile "adds": a headless `opencode serve`
     that exposes, over the tailnet, whatever fleet LLM this box was pointed at.
     This sheet composes the one sudo command that stands it up — sandboxed and
     reboot-surviving — and hands it to you to paste, the same read-only posture
     as spawn: hush never runs it. */

  // genPassword makes a short, URL-safe server password from the browser CSPRNG
  // so a copyable OPENCODE_SERVER_PASSWORD is filled in by default. It's the
  // credential opencode mobile sends on connect; the operator can overwrite it.
  function genPassword(){
    const bytes = new Uint8Array(12);
    (self.crypto||window.crypto).getRandomValues(bytes);
    const abc = "abcdefghijkmnpqrstuvwxyz23456789"; // no look-alikes (0/o, 1/l)
    let out = "";
    for(const b of bytes) out += abc[b % abc.length];
    return out;
  }

  // buildServerCmd composes the single sudo command that starts a hardened
  // opencode server on the box, always as OC_SERVER_USER — the one dedicated,
  // unprivileged account for this job, never root and never whichever user
  // happens to be logged in. Every clause maps to a requirement:
  //
  //   • The account is provisioned inline, the same way install.sh provisions
  //     the "hush" agent user: a system user with its own matching group, no
  //     home directory, no login shell. Idempotent — `getent` skips creation
  //     if it's already there from a previous paste. There's nothing to pick
  //     and nothing that can be mistyped into an existing, more-privileged
  //     account.
  //   • A hush-owned work tree (/var/lib/hush/opencode-server) is the
  //     server's whole world: created up front, and bwrap binds it as the
  //     sandbox's "/" so the served filesystem is that folder and nothing
  //     else on the box. bubblewrap is the jail; the command checks for it
  //     and fails with a clear line if it's absent rather than silently
  //     running unconfined.
  //   • The chosen LLM's opencode.json (the same export the Inference section
  //     builds) is written into that tree's ~/.config/opencode, base64-piped so
  //     the JSON's quotes can't collide with the command's own quoting — so the
  //     moment a phone adds the server it sees the fleet's models.
  //   • A system systemd unit (not a login-shell tmux, not `nohup`) runs
  //     `opencode serve --hostname 0.0.0.0 --port <port>` with the password in
  //     OPENCODE_SERVER_PASSWORD, enabled so it survives a reboot. This is the
  //     "reuse systemd, not a homebrew launcher" the workflow asked for.
  //
  // The command is idempotent: re-pasting it rewrites the unit and restarts the
  // server (systemctl enable --now), so tweaking the port or password is just a
  // re-paste. It binds 0.0.0.0 inside the jail; tailscale on the host is what
  // makes that reachable as the box's tailnet address, which is the URL the
  // server section then shows once detection sees the new bind. Only one
  // server unit exists per box — re-pasting reconfigures it in place rather
  // than standing up a second one.
  function buildServerCmd(){
    const u = OC_SERVER_USER;
    const port = String(parseInt(svPort,10)||4096);
    const pass = svPass || "";
    const work = `/var/lib/hush/opencode-server`;
    const unit = "opencode-server";
    const t = svTarget && byId(svTarget);
    let cfg = "";
    if(t){
      const json = JSON.stringify(ocConfig(t, 0), null, 2);
      const b64 = btoa(unescape(encodeURIComponent(json)));
      cfg =
        `  install -d -o ${u} -g ${u} -m 700 "${work}/.config/opencode" &&\n` +
        `  printf %s "${b64}" | base64 -d > "${work}/.config/opencode/opencode.json" &&\n` +
        `  chown ${u}:${u} "${work}/.config/opencode/opencode.json" &&\n`;
    }
    // A here-doc'd unit file: HOME points opencode at the jailed config tree,
    // and ExecStart wraps `opencode serve` in bwrap so its filesystem root IS
    // the work tree. --dev-bind of the essentials keeps the runtime usable
    // (dns, certs, the opencode binary) without widening the writable root.
    const cmd =
`sudo bash -c '
  command -v bwrap >/dev/null 2>&1 || { echo "bubblewrap (bwrap) is required — install it, then re-run" >&2; exit 1; }
  getent passwd ${u} >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin --user-group ${u} &&
  install -d -o ${u} -g ${u} -m 700 "${work}" &&
${cfg ? cfg.replace(/^/gm,"") : ""}  cat > /etc/systemd/system/${unit}.service <<UNIT &&
[Unit]
Description=opencode server (hush)
After=network-online.target
Wants=network-online.target

[Service]
User=${u}
Environment=HOME=${work}
Environment=OPENCODE_SERVER_PASSWORD=${pass}
ExecStart=/usr/bin/bwrap \\
  --bind ${work} / --dev /dev --proc /proc --tmpfs /tmp \\
  --ro-bind /nix /nix --ro-bind /usr /usr --ro-bind /bin /bin --ro-bind /lib /lib \\
  --ro-bind /etc/resolv.conf /etc/resolv.conf --ro-bind /etc/ssl /etc/ssl \\
  --setenv HOME / --setenv OPENCODE_SERVER_PASSWORD ${pass} \\
  opencode serve --hostname 0.0.0.0 --port ${port}
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload &&
  systemctl enable --now ${unit}.service'`;
    return { cmd, err:"" };
  }

  function openServerSheet(host){
    spHost = host; svPort = "4096"; svPass = genPassword();
    const ts = spawnTargets();
    const here = ts.find(x=>x.id===host);
    svTarget = here ? here.id : (ts[0] ? ts[0].id : "");
    renderServerSheet();
    spawnScrim.classList.add("open");
  }

  function renderServerSheet(){
    const ts = spawnTargets();
    const targetField = ts.length
      ? `<div class="field">
           <label for="svTarget">expose models from</label>
           <select id="svTarget">${ts.map(x=>`<option value="${esc(x.id)}"${x.id===svTarget?" selected":""}>${esc(x.id)} · ${ocReachable(x).reduce((n,r)=>n+((r.models||[]).length),0)} models</option>`).join("")}</select>
           <div class="hint">writes that box's tailnet LLM into the server's opencode.json, so opencode mobile sees those models the moment it connects</div>
         </div>`
      : `<div class="field-note">no tailnet-reachable LLM on the fleet — the server will start with an empty config. Expose a runtime (a machine's Inference section) to give the phone models to talk to.</div>`;
    const { cmd, err } = buildServerCmd();
    spawnBody.innerHTML = `
      <h3>Start opencode server <span class="sd" style="font-family:var(--mono)">${esc(spHost)}</span></h3>
      <p class="sd">compose the command, then paste it into a root shell on ${esc(spHost)} (JuiceSSH) — hush never runs it for you</p>
      <div class="hint">runs as the dedicated <span style="font-family:var(--mono)">${esc(OC_SERVER_USER)}</span> system account — created automatically if it doesn't exist yet — jailed to <span style="font-family:var(--mono)">/var/lib/hush/opencode-server</span> (that folder is its whole filesystem). One server per box; re-pasting reconfigures it in place.</div>
      <div class="field svrow">
        <div style="flex:1"><label for="svPort">port</label>
          <input id="svPort" type="text" inputmode="numeric" autocomplete="off" spellcheck="false" value="${esc(svPort)}"></div>
        <div style="flex:2"><label for="svPass">password</label>
          <input id="svPass" type="text" autocapitalize="off" autocomplete="off" spellcheck="false" value="${esc(svPass)}"></div>
      </div>
      <div class="hint">opencode mobile sends this password on connect — it's set as <span style="font-family:var(--mono)">OPENCODE_SERVER_PASSWORD</span>; hush composes it into the command and never stores it</div>
      ${targetField}
      <div class="cmdbox"><code id="svCmd">${esc(cmd||err)}</code><button class="btn" id="svCopy">Copy</button></div>
      <div class="field-note">Installs a <span style="font-family:var(--mono)">systemd</span> unit (<span style="font-family:var(--mono)">opencode-server.service</span>) — enabled, so it survives a reboot — that runs <span style="font-family:var(--mono)">opencode serve</span> inside a <span style="font-family:var(--mono)">bubblewrap</span> jail rooted at the work folder. Once it's up and bound to the tailnet, this box's URL appears above to add in opencode mobile. Re-paste to change the port or password.</div>
      <div class="sheet-actions"><button class="btn primary" id="svClose" style="flex:1">Close</button></div>`;

    const code = $("#svCmd"), copy = $("#svCopy");
    const refresh = ()=>{ const r = buildServerCmd(); code.textContent = r.cmd || r.err; copy.disabled = !r.cmd; };
    copy.disabled = !cmd;
    const pi = $("#svPort"), pw = $("#svPass");
    pi.addEventListener("input", ()=>{ svPort = pi.value; refresh(); });
    pw.addEventListener("input", ()=>{ svPass = pw.value; refresh(); });
    const sel = $("#svTarget");
    if(sel) sel.addEventListener("change", ()=>{ svTarget = sel.value; refresh(); });
    copy.addEventListener("click", async ()=>{
      const r = buildServerCmd(); if(!r.cmd) return;
      try { await navigator.clipboard.writeText(r.cmd); toast("copied — paste into JuiceSSH on "+spHost); }
      catch(e){ toast("couldn't copy — select and copy manually"); }
    });
    $("#svClose").addEventListener("click", closeSpawn);
    if(document.activeElement !== pi) pi.focus();
  }

  // openStopSheet composes the stop command for one running session: a plain
  // kill of the process hush detected. hush never sends the signal — it hands
  // you the line, same as spawn.
  function openStopSheet(host, s){
    const u = s.user && USER_RE.test(s.user) ? s.user : "";
    const cmd = u ? `sudo -u ${u} kill ${s.pid}` : `sudo kill ${s.pid}`;
    spHost = host;
    spawnBody.innerHTML = `
      <h3>Stop session <span class="sd" style="font-family:var(--mono)">${esc(s.tool)} · pid ${s.pid}</span></h3>
      <p class="sd">run this as root on ${esc(host)} to stop it — hush never kills a process itself</p>
      <div class="cmdbox"><code>${esc(cmd)}</code><button class="btn" id="stCopy">Copy</button></div>
      <div class="field-note">Ends the ${esc(s.tool)} process${u?` owned by ${esc(u)}`:""}; its tmux session closes with it. It drops off the Sessions list on the next refresh.</div>
      <div class="sheet-actions"><button class="btn primary" id="stClose" style="flex:1">Done</button></div>`;
    $("#stCopy").addEventListener("click", async ()=>{
      try { await navigator.clipboard.writeText(cmd); toast("copied — paste into JuiceSSH on "+host); }
      catch(e){ toast("couldn't copy — select and copy manually"); }
    });
    $("#stClose").addEventListener("click", closeSpawn);
    spawnScrim.classList.add("open");
  }

  // openServerStopSheet composes the stop command for the machine's opencode
  // server: because the Start sheet stands it up as a systemd unit (see
  // buildServerCmd), stopping it means disabling that unit, not killing the
  // pid directly — a bare `kill` would just get restarted by the unit's
  // `Restart=on-failure`. Same posture as every other sheet: hush hands you
  // the line, it never runs it.
  function openServerStopSheet(host){
    const cmd = "sudo systemctl disable --now opencode-server";
    spHost = host;
    spawnBody.innerHTML = `
      <h3>Stop opencode server <span class="sd" style="font-family:var(--mono)">${esc(host)}</span></h3>
      <p class="sd">run this as root on ${esc(host)} to stop it — hush never stops a server itself</p>
      <div class="cmdbox"><code>${esc(cmd)}</code><button class="btn" id="ocsStopCopy">Copy</button></div>
      <div class="field-note">Stops and un-enables the <span style="font-family:var(--mono)">opencode-server</span> systemd unit, so it won't come back on reboot either. Re-run the Start command any time to stand it back up. It drops off this section on the next refresh.</div>
      <div class="sheet-actions"><button class="btn primary" id="ocsStopClose" style="flex:1">Done</button></div>`;
    $("#ocsStopCopy").addEventListener("click", async ()=>{
      try { await navigator.clipboard.writeText(cmd); toast("copied — paste into JuiceSSH on "+host); }
      catch(e){ toast("couldn't copy — select and copy manually"); }
    });
    $("#ocsStopClose").addEventListener("click", closeSpawn);
    spawnScrim.classList.add("open");
  }

  // openStopAllSheet composes one chained command that kills every session
  // currently listed for the machine, grouped by user so each sudo -u only
  // names pids that actually belong to it. Same posture as openStopSheet:
  // hush hands you the line, you run it.
  function openStopAllSheet(host, sessions){
    const groups = new Map(); // user ("" = unknown/default) -> pids
    sessions.forEach(s=>{
      const u = s.user && USER_RE.test(s.user) ? s.user : "";
      if(!groups.has(u)) groups.set(u, []);
      groups.get(u).push(s.pid);
    });
    const cmd = [...groups.entries()]
      .map(([u, pids])=> u ? `sudo -u ${u} kill ${pids.join(" ")}` : `sudo kill ${pids.join(" ")}`)
      .join("; ");
    spHost = host;
    spawnBody.innerHTML = `
      <h3>Stop all sessions <span class="sd" style="font-family:var(--mono)">${esc(host)}</span></h3>
      <p class="sd">run this as root on ${esc(host)} to stop all ${sessions.length} sessions — hush never kills a process itself</p>
      <div class="cmdbox"><code>${esc(cmd)}</code><button class="btn" id="stAllCopy">Copy</button></div>
      <div class="field-note">Ends every session listed above; each drops off the list on the next refresh.</div>
      <div class="sheet-actions"><button class="btn primary" id="stAllClose" style="flex:1">Done</button></div>`;
    $("#stAllCopy").addEventListener("click", async ()=>{
      try { await navigator.clipboard.writeText(cmd); toast("copied — paste into JuiceSSH on "+host); }
      catch(e){ toast("couldn't copy — select and copy manually"); }
    });
    $("#stAllClose").addEventListener("click", closeSpawn);
    spawnScrim.classList.add("open");
  }

  // openToolSheet composes the system-wide install/update command for one tool:
  // a single idempotent `sudo npm install -g <pkg>@latest` you paste into a root
  // shell on the box. Same posture as spawn/stop — hush hands you the line, you
  // run it — but it targets the *system* (npm's global prefix, e.g.
  // /usr/local/bin) so one copy serves every user and the unprivileged hush
  // agent can see it, rather than a per-user install tucked inside a home dir.
  function openToolSheet(host, tool, present){
    const kt = KNOWN_TOOLS.find(k=>k.tool===tool); if(!kt) return;
    const cmd = toolInstallCmd(kt.pkg);
    const verb = present ? "Update" : "Install";
    spHost = host;
    spawnBody.innerHTML = `
      <h3>${verb} <span class="sd" style="font-family:var(--mono)">${esc(tool)}</span> on <span class="sd" style="font-family:var(--mono)">${esc(host)}</span></h3>
      <p class="sd">run this as root on ${esc(host)} (JuiceSSH) to ${verb.toLowerCase()} ${esc(tool)} system-wide — hush never runs it for you</p>
      <div class="cmdbox"><code>${esc(cmd)}</code><button class="btn" id="tlCopy">Copy</button></div>
      <div class="field-note">Installs into npm's global prefix (usually <span style="font-family:var(--mono)">/usr/local/bin</span>), so one copy serves every user on the box and hush can see it — a spawned session then just runs it, no per-user install. Needs Node/npm on ${esc(host)}; if it's missing the command says so and you install Node first. Re-run any time — it's idempotent, so the same line updates an existing install.</div>
      <div class="sheet-actions"><button class="btn primary" id="tlClose" style="flex:1">Done</button></div>`;
    $("#tlCopy").addEventListener("click", async ()=>{
      try { await navigator.clipboard.writeText(cmd); toast("copied — paste into JuiceSSH on "+host); }
      catch(e){ toast("couldn't copy — select and copy manually"); }
    });
    $("#tlClose").addEventListener("click", closeSpawn);
    spawnScrim.classList.add("open");
  }

  /* ---------- add-user command sheet ----------
     Same posture as spawn/stop: hush never touches the box. You name a user and
     it composes the one sudo command that creates them with a login shell and no
     password — so `sudo su - <user>` drops you straight in. You copy it into a
     root shell (JuiceSSH) and run it yourself. */
  const userScrim = $("#userScrim");
  const userBody = $("#userSheetBody");
  let usrHost=null, usrName="";
  function closeUser(){ userScrim.classList.remove("open"); }
  userScrim.addEventListener("click", e=>{ if(e.target===userScrim) closeUser(); });

  // buildUserCmd composes the create-user line: useradd -m -s /bin/bash gives a
  // home dir and a real login shell, and `passwd -d` clears the password so the
  // account has none — a locked/no password still lets root in via `su -`, so
  // the workflow it's built for (`sudo su - <user>`) works either way.
  function buildUserCmd(){
    const u = usrName.trim();
    if(!USER_RE.test(u)) return { cmd:"", err:"enter a Linux username to create" };
    return { cmd:`sudo useradd -m -s /bin/bash ${u} && sudo passwd -d ${u}`, err:"" };
  }

  function openUserSheet(host){
    usrHost = host; usrName = "";
    renderUserSheet();
    userScrim.classList.add("open");
  }

  function renderUserSheet(){
    const { cmd, err } = buildUserCmd();
    userBody.innerHTML = `
      <h3>Add a user <span class="sd" style="font-family:var(--mono)">${esc(usrHost)}</span></h3>
      <p class="sd">compose the command, then paste it into a root shell on ${esc(usrHost)} (JuiceSSH) — hush never runs it for you</p>
      <div class="field">
        <label for="usrName">new username</label>
        <input id="usrName" type="text" autocapitalize="off" autocomplete="off" spellcheck="false"
          placeholder="e.g. deploy" value="${esc(usrName)}">
        <div class="hint">lowercase letters, digits, <span style="font-family:var(--mono)">_</span> and <span style="font-family:var(--mono)">-</span>; the command fails on its own if the user already exists</div>
      </div>
      <div class="cmdbox"><code id="usrCmd">${esc(cmd||err)}</code><button class="btn" id="usrCopy">Copy</button></div>
      <div class="field-note">Creates <span style="font-family:var(--mono)">${esc(usrName.trim()||"the user")}</span> with a home directory, a <span style="font-family:var(--mono)">/bin/bash</span> login shell and no password. Then <span style="font-family:var(--mono)">sudo su - ${esc(usrName.trim()||"&lt;user&gt;")}</span> and you're in.</div>
      <div class="sheet-actions"><button class="btn primary" id="usrClose" style="flex:1">Close</button></div>`;

    const code = $("#usrCmd"), copy = $("#usrCopy");
    const refresh = ()=>{ const r = buildUserCmd(); code.textContent = r.cmd || r.err; copy.disabled = !r.cmd;
      const note = userBody.querySelector(".field-note"); if(note){
        const nm = usrName.trim();
        note.innerHTML = `Creates <span style="font-family:var(--mono)">${esc(nm||"the user")}</span> with a home directory, a <span style="font-family:var(--mono)">/bin/bash</span> login shell and no password. Then <span style="font-family:var(--mono)">sudo su - ${esc(nm||"&lt;user&gt;")}</span> and you're in.`;
      } };
    copy.disabled = !cmd;
    const ni = $("#usrName");
    ni.addEventListener("input", ()=>{ usrName = ni.value; refresh(); });
    copy.addEventListener("click", async ()=>{
      const r = buildUserCmd(); if(!r.cmd) return;
      try { await navigator.clipboard.writeText(r.cmd); toast("copied — paste into JuiceSSH on "+usrHost); }
      catch(e){ toast("couldn't copy — select and copy manually"); }
    });
    $("#usrClose").addEventListener("click", closeUser);
    if(document.activeElement !== ni) ni.focus();
  }

  // demoSessions seeds the offline/file:// preview so the Sessions section, the
  // spawn sheet and the stop sheet all render without a live agent.
  function demoSessions(id){
    if(id==="citadel") return [
      // a reachable opencode server (server:true, tailnet) — the preview shows
      // its addable URL; the rest are ordinary interactive sessions.
      { pid:40771, user:"josh", tool:"opencode", cmd:"opencode serve --hostname 0.0.0.0 --port 4096", uptime:9000, server:true, addr:"100.71.8.9:4096", exposure:"tailnet" },
      { pid:48213, user:"josh", tool:"opencode", cmd:"opencode", uptime:5400 },
      { pid:52140, user:"josh", tool:"claude",   cmd:"claude",   uptime:640 },
      { pid:52881, user:"ci",   tool:"opencode", cmd:"opencode run --agent build", uptime:210 },
    ];
    if(id==="forge") return [
      // a loopback-only server: present but not addable from a phone, so the
      // preview shows the "running but local only" state.
      { pid:22030, user:"ci", tool:"opencode", cmd:"opencode serve --port 4096", uptime:800, server:true, addr:"127.0.0.1:4096", exposure:"loopback" },
      { pid:11902, user:"ci", tool:"opencode", cmd:"opencode run --agent build", uptime:120 },
    ];
    return [];
  }

  // demoUsers seeds the Users section in the offline/file:// preview. The
  // names overlap demoSessions' on purpose, so citadel's josh and ci show a
  // running-tool glyph the same way a real box with active sessions would.
  function demoUsers(id){
    if(id==="citadel") return [
      { name:"josh", uid:1000, home:"/home/josh", shell:"/bin/bash" },
      { name:"ci",   uid:1001, home:"/home/ci",   shell:"/bin/bash" },
    ];
    if(id==="forge") return [
      { name:"ci", uid:1000, home:"/home/ci", shell:"/bin/bash" },
    ];
    return [];
  }

  // demoInstalled seeds the tools strip in the offline/file:// preview: citadel
  // has opencode system-wide but not claude (so both the Update and Install
  // states render), forge has both, everything else has neither.
  function demoInstalled(id){
    if(id==="citadel") return [
      { tool:"opencode", present:true, path:"/usr/local/bin/opencode" },
      { tool:"claude",   present:false },
    ];
    if(id==="forge") return [
      { tool:"opencode", present:true, path:"/usr/local/bin/opencode" },
      { tool:"claude",   present:true, path:"/usr/local/bin/claude" },
    ];
    return [
      { tool:"opencode", present:false },
      { tool:"claude",   present:false },
    ];
  }

  /* ---------- alert center: backups that need attention ----------
     A backup console's core job is to tell you when the nightly failed or a box
     drifted out of protection. Posture is aggregated into a ranked alert list,
     surfaced as a header bell you can't miss and a panel that jumps you to the
     machine. In-console today; push/email delivery is the next hop. */
  const alertScrim = $("#alertScrim");
  const alertBody = $("#alertSheetBody");
  const alertBell = $("#alertBell");
  function closeAlerts(){ alertScrim.classList.remove("open"); }
  alertScrim.addEventListener("click", e=>{ if(e.target===alertScrim) closeAlerts(); });
  alertBell.addEventListener("click", openAlerts);

  // fleetAlerts turns each machine's backup posture into an actionable alert —
  // failed, unprotected, or at risk — worst first. Protected and unknown boxes
  // raise nothing.
  function fleetAlerts(){
    const out = [];
    M.forEach(m=>{
      const p = backupPosture(m.id);
      if(p!=="failed" && p!=="none" && p!=="risk") return;
      const bks = ((hostBackupEntry(m.id)||{}).backups) || [];
      const lead = bks.slice().sort((a,b)=>bkRank(b)-bkRank(a))[0];
      const when = lead ? relTimeISO(lead.finished||lead.started||"") : "";
      let msg;
      if(p==="failed") msg = `backup failed${lead&&lead.exit_code?` · exit ${lead.exit_code}`:""}${when?` · ${when}`:""}`;
      else if(p==="none") msg = "no backup configured — this box is unprotected";
      else if(lead && bkStalled(lead)) msg = `backup stalled — running since ${when||"an unknown time"}`;
      else msg = (lead&&lead.incomplete) ? `backup incomplete${when?` · ${when}`:""}` : `no recent snapshot${when?` · last ${when}`:""}`;
      out.push({ id:m.id, posture:p, sev:POSTURE[p].sev, msg, tgt: lead ? backupTarget(lead.repository) : "" });
    });
    out.sort((a,b)=> postureRank[b.posture]-postureRank[a.posture]);
    return out;
  }

  // renderAlertBell keeps the header bell in sync with the current alerts — hidden
  // when all clear, coloured by the worst open alert otherwise. Called by render().
  function renderAlertBell(){
    if(!alertBell) return;
    const alerts = fleetAlerts();
    if(!alerts.length){ alertBell.hidden = true; return; }
    alertBell.hidden = false;
    alertBell.className = "chip alert-bell " + (alerts.some(a=>a.sev==="crit") ? "crit" : "warn");
    alertBell.innerHTML = `⚠ <b>${alerts.length}</b>`;
    alertBell.title = `${alerts.length} backup alert${alerts.length>1?"s":""}`;
  }

  function openAlerts(){
    const alerts = fleetAlerts();
    const rows = alerts.map(a=>`
      <button class="row alert-row ${a.sev}" data-alert="${esc(a.id)}">
        <span class="gly">${POSTURE[a.posture].glyph}</span>
        <span class="nm">${esc(a.id)}<span class="alert-sub">${esc(a.msg)}${a.tgt?` <span class="ar">→</span> ${esc(a.tgt)}`:""}</span></span>
        <span class="rt">${esc(POSTURE[a.posture].label)}</span>
      </button>`).join("");
    alertBody.innerHTML = `
      <h3>Alerts${alerts.length?` <span class="n">${alerts.length}</span>`:""}</h3>
      <p class="sd">backups that need attention — worst first · tap to open the machine</p>
      <div class="list">${rows || `<div class="empty">all clear — every machine is protected</div>`}</div>
      <div class="sheet-actions"><button class="btn primary" id="alClose" style="flex:1">Close</button></div>`;
    $("#alClose").addEventListener("click", closeAlerts);
    alertBody.querySelectorAll("[data-alert]").forEach(el=>{
      el.addEventListener("click", ()=>{ closeAlerts(); enterMachine(el.dataset.alert); });
    });
    alertScrim.classList.add("open");
  }

  // One convention backup (docs/BACKUP-CONVENTION.md) as a card. A backup is a
  // direction, not a machine, so it leads with "source → target".
  //
  // Every field is drawn only when the box actually reported it. next run needs
  // systemd, history needs past runs, paths need a runner new enough to record
  // them — an older agent or a first run simply shows less, rather than the
  // console inventing a plausible number.
  function backupCard(host, b){
    const sev = bkPausedClean(b) ? "warn" : bkSeverity(b);
    const label = bkRunning(b) ? (bkStalled(b) ? "stalled" : "backing up…")
      : bkPausedClean(b) ? "paused"
      : b.ok ? "complete"
      : b.incomplete ? `incomplete · exit ${b.exit_code}`
      : `failed · exit ${b.exit_code}`;
    const target = backupTarget(b.repository);
    const s = b.summary || {};

    const metric = (k, v) => v
      ? `<div class="bk-m"><div class="bk-mk">${k}</div><div class="bk-mv">${esc(v)}</div></div>` : "";
    // A run in flight has no finished time, so this is when it started —
    // labelled "started" rather than "last run" so it doesn't claim to be a
    // completed run's timestamp.
    const last = relTimeISO(b.finished || b.started || "");
    const next = b.next_run ? relTimeISO(b.next_run) : "";
    const protectedSize = (typeof s.total_bytes_processed === "number") ? fmtBytes(s.total_bytes_processed) : "";
    const added = (typeof s.data_added === "number") ? fmtBytes(s.data_added) : "";
    const metrics = [metric(bkRunning(b) ? "started" : "last run", last), metric("next run", next),
                     metric("protected", protectedSize), metric("added", added)].join("");

    // Bar heights are scaled against the largest run in the window, so the
    // strip reads as relative volume. A failed run still gets a visible stub —
    // a zero-height bar would read as "no run happened", which is a different
    // and less alarming thing.
    const hist = Array.isArray(b.history) ? b.history : [];
    const peak = hist.reduce((m,h)=>Math.max(m, (h.summary&&h.summary.data_added)||0), 0);
    const bars = hist.map(h=>{
      const hv = (h.summary && h.summary.data_added) || 0;
      const pct = peak > 0 ? Math.max(18, Math.round(hv/peak*100)) : 40;
      const hs = h.ok ? "" : (h.incomplete ? " warn" : " crit");
      return `<div class="bk-bar${hs}" style="height:${pct}%"></div>`;
    }).join("");
    const bad = hist.filter(h=>!h.ok).length;
    const histBlock = hist.length ? `<div class="bk-hist">
        <div class="bk-histk">last ${hist.length} run${hist.length>1?"s":""}${bad?` · ${bad} not complete`:""}</div>
        <div class="bk-bars">${bars}</div>
      </div>` : "";

    const paths = Array.isArray(b.paths) && b.paths.length
      ? `<div class="bk-paths">${b.paths.map(p=>`<span class="bk-path">${esc(p)}</span>`).join("")}</div>` : "";

    // Rendered as a button: tapping it opens the watch modal, which is the
    // whole point of a run in flight — you want to sit and watch it move.
    // host+name is the stable identity the modal re-looks-up on every poll, so
    // a running run's card and its open modal stay in lockstep as it finishes.
    return `<button type="button" class="bk-card tap${sev==="good"?"":" "+sev}"
        data-bkhost="${esc(host)}" data-bkname="${esc(b.name||"")}">
      <div class="bk-hd">
        <div class="bk-route">${esc(host)}${target?` <span class="ar">→</span> ${esc(target)}`:""}
          <span class="bk-state ${sev}"><span class="d"></span>${esc(label)}</span></div>
        ${b.repository?`<div class="bk-repo">${esc(b.repository)}</div>`:""}
      </div>
      <div class="bk-metrics">${metrics}</div>
      ${histBlock}
      ${paths}
    </button>`;
  }

  /* ---------- backup watch modal ----------
     Tapping a backup card opens this: the same card's facts, room to breathe,
     and — the reason it exists — a live watch for a run in flight. It keys off
     host+name and re-looks-up its backup on every poll (see render()), so a run
     that finishes while you're watching flips from "backing up…" to its outcome
     in place, without you closing and reopening. */
  const watchScrim = $("#watchScrim");
  const watchBody = $("#watchSheetBody");
  let watchOpen = false;         // whether the modal is up — render() repaints it when true
  let watchKey = null;           // {host, name} the modal is bound to
  let watchTimer = null;         // 1s interval that ticks the live "running for" counter

  // watchBackup resolves the modal's {host, name} to the current backup object,
  // freshly each call so a poll's newer data (a run finishing) is what's drawn.
  function watchBackup(){
    if(!watchKey) return null;
    const h = CONV_BACKUPS.find(x=>x.host===watchKey.host);
    if(!h) return null;
    return (h.backups||[]).find(b=>(b.name||"")===watchKey.name) || null;
  }

  // fmtElapsed renders a live run's age as H:MM:SS (or M:SS under an hour) —
  // tabular so the seconds column doesn't jitter as it ticks.
  function fmtElapsed(ms){
    if(!(ms >= 0)) return "—";
    const t = Math.floor(ms/1000), s = t%60, m = Math.floor(t/60)%60, h = Math.floor(t/3600);
    const pad = n => String(n).padStart(2,"0");
    return h ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
  }

  function openBackupWatch(host, name){
    watchKey = { host, name };
    watchOpen = true;
    renderBackupWatch();
    watchScrim.classList.add("open");
    // Tick the elapsed counter every second while a run is in flight. The
    // interval only touches the one span, so it's cheap and never fights the
    // slow backup poll that repaints the rest of the modal.
    if(watchTimer) clearInterval(watchTimer);
    watchTimer = setInterval(tickWatchElapsed, 1000);
  }

  function closeBackupWatch(){
    watchOpen = false;
    watchKey = null;
    if(watchTimer){ clearInterval(watchTimer); watchTimer = null; }
    watchScrim.classList.remove("open");
  }
  watchScrim.addEventListener("click", e=>{ if(e.target===watchScrim) closeBackupWatch(); });

  function tickWatchElapsed(){
    if(!watchOpen) return;
    const el = $("#wtElapsed");
    if(!el) return;
    const b = watchBackup();
    if(!b || !bkRunning(b)) return;   // finished between paints — the repaint will re-label it
    const started = Date.parse(b.started || "");
    if(!isNaN(started)) el.textContent = fmtElapsed(Date.now() - started);
  }

  // renderBackupWatch (re)paints the modal body from the current backup object.
  // Kept idempotent so render() can call it on every poll: it never wires the
  // ticker (open does) and only ever swaps innerHTML.
  function renderBackupWatch(){
    if(!watchOpen) return;
    const b = watchBackup();
    if(!b){
      // The backup stopped reporting (removed, or its box went unreachable)
      // while the modal was open — say so rather than freeze on stale facts.
      watchBody.innerHTML = `<h3>Backup</h3>
        <p class="sd">${esc(watchKey ? watchKey.host : "")}</p>
        <div class="empty">this backup is no longer reporting</div>
        <div class="sheet-actions"><button class="btn primary" id="wtClose" style="flex:1">Close</button></div>`;
      $("#wtClose").addEventListener("click", closeBackupWatch);
      return;
    }
    const host = watchKey.host;
    const paused = bkPausedClean(b);
    const sev = paused ? "warn" : bkSeverity(b);
    // Pause/resume is offered as a command to copy, not a button that acts:
    // hush reads the fleet and never runs anything on it, so the console's job
    // is to compose the exact `systemctl` line and hand it over — the same
    // read-only, copy-into-SSH idiom as spawning a session or updating an agent.
    // The direction follows the timer's actual state (bkPaused), not
    // bkPausedClean, so a paused backup whose last run failed still offers
    // "resume" rather than pretending it isn't paused.
    const bkName = b.name || (watchKey ? watchKey.name : "") || "";
    const bkPausedNow = bkPaused(b);
    const pauseResumeCmd = "sudo systemctl " + (bkPausedNow ? "enable" : "disable") +
      " --now restic-backup@" + bkName + ".timer";
    const running = bkRunning(b), stalled = bkStalled(b);
    const target = backupTarget(b.repository);
    const s = b.summary || {};
    const label = running ? (stalled ? "stalled" : "backing up…")
      : paused ? "paused"
      : b.ok ? "complete"
      : b.incomplete ? `incomplete · exit ${b.exit_code}`
      : `failed · exit ${b.exit_code}`;

    // The live watch for a healthy run in flight: a real percentage when the box
    // publishes one, the indeterminate shuttle when it doesn't, and a ticking
    // elapsed clock under either. A stalled run gets a plain warning instead —
    // it isn't moving, so a "moving" bar would lie.
    const started = Date.parse(b.started || "");
    let watch = "";
    if(running && !stalled){
      const elapsed = isNaN(started) ? "—" : fmtElapsed(Date.now() - started);
      const prog = bkProgress(b);
      if(prog){
        // Clamped because the bar's width is drawn straight from this: one
        // malformed sample must not paint outside the track.
        const frac = Math.max(0, Math.min(1, prog.percent_done));
        const pct = Math.round(frac * 100);
        const scanning = progressScanning(host, b, prog);
        const done = typeof prog.bytes_done === "number" ? fmtBytes(prog.bytes_done) : "";
        const total = typeof prog.total_bytes === "number" ? fmtBytes(prog.total_bytes) : "";
        // The ETA is restic's own, and it is withheld while the scan is still
        // running — an estimate against a total that is still climbing is a
        // number that will be wrong in a way the reader can't see.
        const eta = (!scanning && typeof prog.seconds_remaining === "number" && prog.seconds_remaining > 0)
          ? `${fmtElapsed(prog.seconds_remaining * 1e3)} left` : "";
        const bits = [(done && total) ? `${done} of ${total}` : "",
                      scanning ? "still scanning" : eta].filter(Boolean).join(" · ");
        watch = `<div class="wt-prog det" role="progressbar" aria-label="backing up"
            aria-valuemin="0" aria-valuemax="100" aria-valuenow="${pct}"
            style="--pct:${(frac * 100).toFixed(1)}%"></div>
          <p class="wt-note"><strong>${pct}%</strong>${bits ? " · " + esc(bits) : ""}
            · running for <span class="wt-elapsed" id="wtElapsed">${esc(elapsed)}</span></p>`;
      } else {
        watch = `<div class="wt-prog" role="progressbar" aria-label="backing up"></div>
          <p class="wt-note">running for <span class="wt-elapsed" id="wtElapsed">${esc(elapsed)}</span>
            · this box isn't reporting live byte progress</p>`;
      }
    } else if(running && stalled){
      watch = `<p class="wt-note">this run has overrun its schedule with no snapshot yet — it may have
        been killed mid-flight (a reboot or OOM leaves the "running" marker behind)</p>`;
    }

    const metric = (k, v) => v
      ? `<div class="bk-m"><div class="bk-mk">${k}</div><div class="bk-mv">${esc(v)}</div></div>` : "";
    const startedRel = relTimeISO(b.started || "");
    const finishedRel = relTimeISO(b.finished || "");
    const next = b.next_run ? relTimeISO(b.next_run) : "";
    // fmtElapsed, not fmtDur: a backup runs for minutes-to-hours, and fmtDur's
    // bare-seconds ("21600s") is unreadable at that scale — H:MM:SS isn't.
    const dur = (!running && !isNaN(started) && b.finished)
      ? fmtElapsed(Date.parse(b.finished) - started) : "";
    const protectedSize = (typeof s.total_bytes_processed === "number") ? fmtBytes(s.total_bytes_processed) : "";
    const added = (typeof s.data_added === "number") ? fmtBytes(s.data_added) : "";
    const snap = typeof s.snapshot_id === "string" ? s.snapshot_id.slice(0,8) : "";
    const metrics = [
      metric("started", startedRel),
      running ? "" : metric("finished", finishedRel),
      running ? "" : metric("took", dur),
      metric("next run", next),
      metric("protected", protectedSize),
      metric("added", added),
      metric("snapshot", snap),
    ].join("");

    // History strip — identical shape to the card's, so the modal reads as the
    // same object opened up rather than a different screen.
    const hist = Array.isArray(b.history) ? b.history : [];
    const peak = hist.reduce((m,h)=>Math.max(m, (h.summary&&h.summary.data_added)||0), 0);
    const bars = hist.map(h=>{
      const hv = (h.summary && h.summary.data_added) || 0;
      const pct = peak > 0 ? Math.max(18, Math.round(hv/peak*100)) : 40;
      const hs = h.ok ? "" : (h.incomplete ? " warn" : " crit");
      return `<div class="bk-bar${hs}" style="height:${pct}%"></div>`;
    }).join("");
    const bad = hist.filter(h=>!h.ok).length;
    const histBlock = hist.length ? `<div class="bk-hist" style="border-top:none;padding:0;margin-top:4px">
        <div class="bk-histk">last ${hist.length} run${hist.length>1?"s":""}${bad?` · ${bad} not complete`:""}</div>
        <div class="bk-bars">${bars}</div>
      </div>` : "";

    const paths = Array.isArray(b.paths) && b.paths.length
      ? `<div class="bk-paths" style="padding:0;margin-top:10px">${b.paths.map(p=>`<span class="bk-path">${esc(p)}</span>`).join("")}</div>` : "";

    watchBody.innerHTML = `
      <div class="bk-route" style="margin-bottom:8px">${esc(host)}${target?` <span class="ar">→</span> ${esc(target)}`:""}
        <span class="bk-state ${sev}"><span class="d"></span>${esc(label)}</span></div>
      ${b.repository?`<p class="sd" style="overflow-wrap:anywhere">${esc(b.repository)}</p>`:""}
      ${watch}
      <div class="bk-metrics" style="border-radius:8px;overflow:hidden">${metrics || `<div class="bk-m"><div class="bk-mk">status</div><div class="bk-mv">${esc(label)}</div></div>`}</div>
      ${histBlock}
      ${paths}
      ${bkName ? `<div style="margin-top:16px">
        <div class="bk-mk" style="margin-bottom:6px">${bkPausedNow ? "resume this backup" : "pause this backup"}</div>
        <div class="cmdbox"><code id="wtPauseCmd">${esc(pauseResumeCmd)}</code><button class="btn" id="wtPauseCopy">Copy</button></div>
        <div class="field-note">Run it over SSH as root — hush only reads your fleet, so it hands you the command rather than running it. ${bkPausedNow
          ? "This re-enables the timer; the next scheduled run resumes."
          : "This stops the schedule until you resume; the repository and last snapshot are untouched, so posture reads <b>paused</b> instead of aging into <b>at&nbsp;risk</b>."}</div>
      </div>` : ""}
      <div class="sheet-actions" style="margin-top:16px"><button class="btn primary" id="wtClose" style="flex:1">Close</button></div>`;
    $("#wtClose").addEventListener("click", closeBackupWatch);
    const wtPauseCopy = $("#wtPauseCopy");
    if(wtPauseCopy) wtPauseCopy.addEventListener("click", async ()=>{
      try { await navigator.clipboard.writeText(pauseResumeCmd); toast("copied — paste into JuiceSSH on "+host); }
      catch(e){ toast("couldn't copy — select and copy manually"); }
    });
  }

  // A machine just added through "Add machine", shown until its first real
  // /api/fleet entry shows up (see poll()'s PENDING filter below).
  function pendingNodeCard(p){
    return `
      <div class="node pending">
        <div class="hd">
          <div>
            <div class="host"><span class="aura" style="background:var(--faint)"></span>${esc(p.id)}</div>
            <div class="meta">${esc(p.ip||"")}</div>
          </div>
        </div>
        <div class="rings"><div class="ring"><div class="placeholder-ring">?</div></div></div>
        <div class="foot">
          <span class="cons">waiting for first check-in…</span>
          <span class="badge pending">◌&nbsp;<span class="lbl">pairing</span></span>
        </div>
      </div>`;
  }

  // --- condensed fleet card helpers -----------------------------------------
  // A metric is "hot" once it crosses these; only the hot ones auto-unroll on an
  // elevated card. net is bytes/sec (unbounded), so it has no % threshold and is
  // driven by the alert text instead.
  const HOT = { cpu:80, mem:90, disk:85, gpu:88 };
  const metricHot = (m,k)=> m[k]!=null && HOT[k]!=null && m[k]>=HOT[k];
  const hotMetrics = m => (m.online ? ["cpu","mem","disk","gpu"].filter(k=>metricHot(m,k)) : []);
  const cpuSeries = m => (m.loadHist && m.loadHist.length>1) ? m.loadHist : [m.cpu||0, m.cpu||0];
  const gpuSeries = m => (m.gpuHist && m.gpuHist.length>1) ? m.gpuHist : [m.gpu||0, m.gpu||0];

  // a loadable line (cpu/gpu) — a coloured sparkline with a labelled value
  function loadLine(m, k){
    const col = k==="gpu" ? "var(--ring2)" : "var(--accent)";
    const series = k==="gpu" ? gpuSeries(m) : cpuSeries(m);
    const vcol = metricHot(m,k) ? "var(--sev)" : "var(--muted)";
    return `<div class="line ${k}"><span class="lk"><i></i>${k}</span>
      ${sparkline(series, 200, 22, col)}
      <span class="lv" style="color:${vcol}">${m[k]}%</span></div>`;
  }
  // the network line — rx/tx as a shared-axis dual spark, bytes read out at the end
  function netLine(m){
    const rxHist = (m.netRxHist && m.netRxHist.length>1) ? m.netRxHist : [m.netRx||0, m.netRx||0];
    const txHist = (m.netTxHist && m.netTxHist.length>1) ? m.netTxHist : [m.netTx||0, m.netTx||0];
    return `<div class="line net"><span class="lk"><i></i>net</span>
      ${netSpark(rxHist, txHist, 200, 22, "spark")}
      <span class="lv nettxt"><span style="color:var(--accent)">↓${fmtBytes(m.netRx||0)}</span><span style="color:var(--ring2)">↑${fmtBytes(m.netTx||0)}</span></span></div>`;
  }
  // a point-in-time metric (disk/mem) — no series to plot, so show a sev gauge
  function gaugeLine(m, k){
    return `<div class="line ${k}"><span class="lk">${k}</span>
      <div class="gtrack"><i style="width:${Math.min(100,m[k])}%;background:var(--sev)"></i></div>
      <span class="lv" style="color:var(--sev)">${m[k]}%</span></div>`;
  }

  function nodeCard(m){
    const rolePill = m.role ? `<span class="role">${m.role}</span>` : "";
    let badge;
    if(!m.online) badge = `<span class="badge crit">●&nbsp;<span class="lbl">offline</span></span>`;
    else if(m.status==="crit") badge = `<span class="badge crit">●&nbsp;<span class="lbl">${(m.alert||"critical").split("—")[0].trim()}</span></span>`;
    else if(m.status==="warn") badge = `<span class="badge warn">▲&nbsp;<span class="lbl">${(m.alert||"elevated").split("·")[0].trim()}</span></span>`;
    else badge = `<span class="badge ok">●&nbsp;<span class="lbl">nominal</span></span>`;

    // always-on compact meters
    const meter = k => `<div class="m ${k} ${metricHot(m,k)?"hot":""}">
        <div class="top"><span class="k">${k}</span><span class="v">${m[k]==null?"—":m[k]+"%"}</span></div>
        <div class="bar"><i style="--w:${Math.min(100,m[k]||0)}%"></i></div></div>`;
    const netMeter = `<div class="m net"><div class="top"><span class="k">net</span></div>
        <span class="v"><span style="color:var(--accent)">↓${fmtBytes(m.netRx||0)}</span><span style="color:var(--ring2)">↑${fmtBytes(m.netTx||0)}</span></span></div>`;
    const mini = `<div class="mini">${meter("cpu")}${meter("mem")}${meter("disk")}${m.gpu!=null?meter("gpu"):""}${netMeter}</div>`;

    // strategic unroll: an elevated card opens a focus strip with only its bad
    // bits — a sparkline for hot cpu/gpu, a gauge for hot disk/mem. Healthy
    // cards (and healthy metrics) stay rolled up.
    let focus = "";
    const elevated = !m.online || m.status==="warn" || m.status==="crit";
    if(elevated){
      const hot = hotMetrics(m);
      const why = m.alert || (!m.online ? "offline — last check-in stale"
        : hot.length ? hot.map(k=>`${k} ${m[k]}%`).join(" · ") : "elevated");
      const rows = hot.map(k=> (k==="cpu"||k==="gpu") ? loadLine(m,k) : gaugeLine(m,k)).join("");
      focus = `<div class="focus">
        <div class="fhead">${m.status==="crit"||!m.online?"●":"▲"}&nbsp;why<span class="why">— ${esc(why)}</span></div>
        ${rows?`<div class="flines">${rows}</div>`:""}
      </div>`;
    }

    // roll-up drawers — global toggle OR a per-card open both count
    const ringsOpen = state.showRings || state.openRings.has(m.id);
    const linesOpen = state.showLines || state.openLines.has(m.id);
    const gpuRing = m.gpu!=null ? `
          <div class="ring">${dualRing(m.gpu,m.vram,44)}<span class="v">${m.gpu}%</span><span class="k">gpu</span></div>` : "";

    return `
      <div class="node s-${m.status} ${m.online?"":"offline"}${JUST_ARRIVED.has(m.id)?" new":""}"
           data-id="${m.id}" role="button" tabindex="0">
        <div class="hd">
          <div>
            <div class="host"><span class="aura"></span>${m.id}</div>
            <div class="meta">${m.os||"—"}${m.ip?` · ${m.ip}`:""}${m.gpu!=null&&m.gpuName?` · ${m.gpuName}`:""}</div>
          </div>
          ${rolePill}
        </div>
        ${mini}
        ${focus}
        ${backupLine(m)}
        ${receiveLine(m)}
        ${llmLine(m)}
        <div class="foot">
          ${m.agentUpdateAvailable?`<span class="badge upd">⇧&nbsp;<span class="lbl">update</span></span>`:""}
          ${badge}
        </div>
        <div class="card-drawers">
          <details class="cd rings-d"${ringsOpen?" open":""}>
            <summary><span class="car">▸</span>rings</summary>
            <div class="drawer-body"><div class="rings">
              <div class="ring">${dualRing(m.cpu,m.mem,44)}<span class="v">${m.cpu}%</span><span class="k">cpu</span></div>
              <div class="ring">${ring(m.disk,44)}<span class="v">${m.disk}%</span><span class="k">disk</span></div>
              ${gpuRing}
            </div></div>
          </details>
          <details class="cd lines-d"${linesOpen?" open":""}>
            <summary><span class="car">▸</span>load lines</summary>
            <div class="drawer-body"><div class="alllines">
              ${loadLine(m,"cpu")}
              ${m.gpu!=null?loadLine(m,"gpu"):""}
              ${netLine(m)}
            </div></div>
          </details>
        </div>
      </div>`;
  }

  function enterMachine(id){ state.view="machine"; state.mid=id; state.openSections.clear(); render(); scrollTop();
    // Load this box's sessions and users once on entry; the slow interval
    // refreshes after. Never fetch from within renderMachine — fetchSessions/
    // fetchUsers repaint on completion, which would recurse.
    const m = byId(id); if(m && (m.online || MODE==="demo")) { fetchSessions(id); fetchUsers(id); } }
  function goFleet(){ state.view="fleet"; render(); scrollTop(); }
  function scrollTop(){ window.scrollTo({top:0,behavior:"instant"}); }
  function sevVar(s){ return s==="good"?"good":s==="warn"?"warn":"crit"; }

  function renderMachine(){
    const m = byId(state.mid);
    const sv = sevVar(m.status);
    const alert = m.alert ? `<div class="alertbar ${m.status}">${m.status==="crit"?"●":"▲"} ${m.alert}</div>` : "";
    const hist = m.loadHist && m.loadHist.length>1 ? m.loadHist : [m.cpu||0, m.cpu||0];
    const rxHist = m.netRxHist && m.netRxHist.length>1 ? m.netRxHist : [m.netRx||0, m.netRx||0];
    const txHist = m.netTxHist && m.netTxHist.length>1 ? m.netTxHist : [m.netTx||0, m.netTx||0];
    app.innerHTML = `
      <div class="strip">
        <div class="crumbs"><a id="bk">Fleet</a><span class="sep">/</span><span>${m.id}</span></div>
      </div>
      <main>
        <div class="mhead">
          <div class="top">
            <div>
              <div class="host"><span class="aura" style="--sev:var(--${sv});--sev-soft:var(--${sv}-soft);background:var(--${sv})"></span>${m.id}</div>
              <div class="sub"><span>${m.os||"—"}</span>${m.ip?`<span>${m.ip}</span>`:""}${m.up?`<span>up ${m.up}</span>`:""}${m.gpu!=null&&m.gpuName?`<span style="color:var(--accent)">${m.gpuName}${m.vramText?` · ${m.vramText}`:""}</span>`:""}${m.role?`<span style="color:var(--accent)">${m.role}</span>`:""}${m.agentVersion?(m.agentUpdateAvailable?`<span class="badge upd" id="agentUpdBadge">⇧&nbsp;<span class="lbl">agent ${esc(m.agentVersion)} → ${esc(m.latestVersion)}</span></span>`:`<span>agent ${esc(m.agentVersion)}</span>`):""}</div>
            </div>
            <span class="pill ${m.status}"><span class="d"></span>${!m.online?"offline":m.status==="good"?"healthy":m.status==="warn"?"degraded":"critical"}</span>
          </div>
          ${alert}
          <div class="vitals">
            <div class="bigring topopen" data-top="cpu" title="open live CPU & processes">${dualRing(m.cpu,m.mem,56)}<span class="v">${m.cpu}%</span><span class="k">cpu</span><span class="k2"><i></i>${m.mem}% mem</span></div>
            <div class="bigring">${ring(m.disk,56)}<span class="v">${m.disk}%</span><span class="k">disk</span></div>
            ${m.gpu!=null?`
            <div class="bigring">${dualRing(m.gpu,m.vram,56)}<span class="v">${m.gpu}%</span><span class="k">gpu</span><span class="k2"><i></i>${m.vram}% vram</span></div>`:""}
            <div class="loadbox topopen" data-top="cpu" title="open live CPU & processes">
              <div class="lbl"><span>cpu trend ⤢</span><b>load ${m.load||"—"}</b></div>
              ${sparkline(hist,260,34)}
            </div>
            <div class="loadbox topopen" data-top="net" title="open live network & processes">
              <div class="lbl"><span>network ⤢</span><b><span style="color:var(--accent)">↓ ${fmtBytes(m.netRx||0)}/s</span>&nbsp;&nbsp;<span style="color:var(--ring2)">↑ ${fmtBytes(m.netTx||0)}/s</span></b></div>
              ${netSpark(rxHist,txHist,260,34)}
            </div>
          </div>
        </div>

        ${rollupSec("files", `Files <span class="n">store</span>`,
          `<div class="list">
          <button class="row" id="du-open"${m.online?"":" disabled style=\"opacity:.5;cursor:default\""}>
            <span class="gly">▦</span><span class="nm">Disk usage</span>
            <span class="rt">${m.online?"treemap":"offline"}</span></button>
        </div>`)}

        ${(()=>{
          const rts = (m.llm&&m.llm.runtimes)||[];
          if(!rts.length) return "";
          const rows = rts.map(r=>{
            const meta = LLM_SCOPE[r.exposure] || LLM_SCOPE.unknown;
            const ms = r.models||[];
            return `<div class="oc-rt ${meta.sev}">
              <div class="oc-rt-hd">
                <span class="oc-gly">${meta.glyph}</span>
                <span class="oc-kind">${r.kind==="ollama"?"ollama":"openai-compatible"}</span>
                <span class="oc-addr">${esc(r.addr)}</span>
                <span class="oc-scope">${meta.word}</span>
              </div>
              ${ms.length?`<div class="oc-models">${ms.map(x=>`<span class="oc-m">${esc(x)}</span>`).join("")}</div>`:""}
            </div>`;
          }).join("");
          return rollupSec("inference", `Inference <span class="n">${rts.length}</span>`,
            `<div class="oc-sec">
              ${rows}
              ${ocReachable(m).length
                ? `<button class="btn primary oc-export" id="ocExport">⧉ opencode.json</button>`
                : `<div class="empty">bound to loopback — reachable only on the box itself, so there's nothing off-box to point opencode at</div>`}
            </div>`);
        })()}

        ${serverSection(m)}

        ${sessionsSection(m)}

        ${usersSection(m)}

        ${(()=>{ const h = hostBackupEntry(m.id); const bks = (h&&h.backups)||[];
          return rollupSec("backups", `Backups${bks.length?` <span class="n">${bks.length}</span>`:""}`,
          `<div class="wf-saved">${bks.length
            ? `<div class="bk-cards">${bks.map(b=>backupCard(m.id, b)).join("")}</div>`
            : `<div class="empty">${m.online ? "no scheduled backups reporting — set them up over SSH (docs/BACKUP-CONVENTION.md)" : "machine unreachable"}</div>`}</div>`); })()}

        ${(()=>{ const inc = backupSourcesFor(m);
          // The destination side: every other box that ships its snapshots here.
          // Only drawn when this box actually receives something, so a plain
          // machine's view is unchanged. Each is the source's own backup card,
          // so "citadel → nas" reads the same here as it does on the Fleet page.
          return inc.length
            ? rollupSec("receiving", `Receiving <span class="n">${inc.length}</span>`,
               `<div class="wf-saved"><div class="bk-cards">${inc.map(x=>backupCard(x.host, x.b)).join("")}</div></div>`)
            : ""; })()}
      </main>`;
    $("#bk").addEventListener("click", goFleet);
    // Rollup open/close is remembered on state.openSections so it survives the
    // 2.5s poll re-render — see rollupSec.
    app.querySelectorAll("details.rollup[data-sec]").forEach(d=>{
      d.addEventListener("toggle", ()=>{ d.open ? state.openSections.add(d.dataset.sec) : state.openSections.delete(d.dataset.sec); });
    });
    const agentUpdBadge = $("#agentUpdBadge");
    if(agentUpdBadge) agentUpdBadge.addEventListener("click", ()=>openAgentUpdateSheet(m));
    const du = $("#du-open");
    if(du) du.addEventListener("click", ()=>{ if(m.online) openDu(m.id); });
    const ocx = $("#ocExport");
    if(ocx) ocx.addEventListener("click", ()=>openOcSheet(m.id));
    const spawn = $("#se-spawn");
    if(spawn) spawn.addEventListener("click", ()=>openSpawnSheet(m.id));
    const ocsStart = $("#ocs-start");
    if(ocsStart) ocsStart.addEventListener("click", ()=>openServerSheet(m.id));
    app.querySelectorAll(".ocs-copy").forEach(b=>{
      b.addEventListener("click", async ()=>{
        try { await navigator.clipboard.writeText(b.dataset.url); toast("server URL copied — add it in opencode mobile"); }
        catch(e){ toast("couldn't copy — select and copy manually"); }
      });
    });
    app.querySelectorAll(".ocs-stop").forEach(b=>{
      b.addEventListener("click", ()=>openServerStopSheet(m.id));
    });
    const adduser = $("#us-adduser");
    if(adduser) adduser.addEventListener("click", ()=>openUserSheet(m.id));
    app.querySelectorAll(".se-stop").forEach(b=>{
      b.addEventListener("click", ()=>openStopSheet(m.id, {
        pid: +b.dataset.pid, user: b.dataset.user, tool: b.dataset.tool }));
    });
    app.querySelectorAll(".tl-fix").forEach(b=>{
      b.addEventListener("click", ()=>openToolSheet(m.id, b.dataset.tool, b.dataset.present==="1"));
    });
    const stopAll = $("#se-stop-all");
    if(stopAll) stopAll.addEventListener("click", ()=>{
      const list = (SESSIONS[m.id]&&SESSIONS[m.id].sessions)||[];
      if(list.length) openStopAllSheet(m.id, list);
    });
    // The CPU ring, cpu-trend spark and network box all open the live
    // htop-style panel. Demo mode has no agent, so it opens on synthetic data.
    app.querySelectorAll("[data-top]").forEach(el=>{
      el.addEventListener("click", ()=>{ if(m.online || MODE==="demo") openTop(m.id, el.dataset.top); });
    });
  }

  /* ---------- Disk usage: a windirstat-style treemap over the Store
     construct's recursive /du endpoint. Sizes are computed recursively on the
     agent, but only one level is rendered at a time — clicking a directory's
     box drills into it and fetches that subtree's own children, drawn as
     proportional boxes instead of rows. Lives in its own overlay since a
     treemap wants real screen space. In demo mode there's no agent to ask,
     so demoDu() below fabricates a plausible, stable tree per machine
     instead of hitting the network. */
  const DU_PALETTE = ["#19e3ff","#7c5cff","#2bffa6","#ffd23f","#ff6f3d","#ff3d6e","#3ddcff","#a56bff","#5cffcf","#ffb23d"];
  function duColor(idx, isDir){
    const base = DU_PALETTE[idx % DU_PALETTE.length];
    return `color-mix(in srgb, ${base} ${isDir?58:34}%, var(--panel-2))`;
  }
  // squarify lays out items (each with a .size) into near-square boxes filling
  // the x,y,w,h rect, largest first — the standard "squarified treemap"
  // algorithm (Bruls/Huizing/van Wijk). Returns rects in the same order as
  // items. A floor of 1 on each size keeps a run of zero-byte entries from
  // producing a 0/0 division instead of a (negligible-area) sliver.
  function squarify(items, x, y, w, h){
    const rects = new Array(items.length);
    const sizes = items.map(it => Math.max(it.size, 1));
    const total = sizes.reduce((a,b)=>a+b, 0) || 1;
    const scale = (w*h) / total;
    const scaled = sizes.map(s => s*scale);
    function worst(row, side){
      const sum = row.reduce((a,b)=>a+b, 0);
      if(sum <= 0) return Infinity;
      return Math.max((side*side*Math.max(...row))/(sum*sum), (sum*sum)/(side*side*Math.min(...row)));
    }
    let idx = 0, rx = x, ry = y, rw = w, rh = h;
    while(idx < scaled.length){
      const side = Math.min(rw, rh);
      let row = [scaled[idx]], j = idx+1;
      while(j < scaled.length){
        const candidate = row.concat([scaled[j]]);
        if(worst(candidate, side) <= worst(row, side)){ row = candidate; j++; } else break;
      }
      const rowSum = row.reduce((a,b)=>a+b, 0);
      if(rw >= rh){
        const colW = rowSum / rh;
        let cy = ry;
        for(let k=0;k<row.length;k++){ const cellH = row[k]/colW; rects[idx+k] = {x:rx,y:cy,w:colW,h:cellH}; cy += cellH; }
        rx += colW; rw -= colW;
      } else {
        const rowH = rowSum / rw;
        let cx = rx;
        for(let k=0;k<row.length;k++){ const cellW = row[k]/rowH; rects[idx+k] = {x:cx,y:ry,w:cellW,h:rowH}; cx += cellW; }
        ry += rowH; rh -= rowH;
      }
      idx = j;
    }
    return rects;
  }

  // openDu opens the disk-usage treemap. With opts.select it becomes a path
  // picker for a Backup: tapping a box toggles it into the selection (a dir
  // still drills in via its ⤢ corner), and a footer "Add" hands the chosen
  // absolute paths back through opts.onDone. Without it, it's the read-only
  // treemap as before — a dir click just drills.
  function openDu(id, startPath, opts){
    opts = opts || {};
    const selecting = !!opts.select;
    const selected = new Map();            // absolute path -> size (0 when unknown, e.g. a pre-filled path)
    (opts.initial || []).forEach(p => selected.set(p, 0));
    // Coverage badges only make sense on the read-only treemap (select mode's
    // corner is already spoken for by the ✓ selection badge) and only when
    // this host actually has a backup configured — otherwise every cell would
    // show a hollow "○ not backed up" badge for no reason.
    const backupPaths = selecting ? [] : backupPathsFor(id);
    const ov = document.createElement("div");
    ov.className = "vov";
    ov.innerHTML = `<div class="vsheet">
        <div class="vbar">
          <div class="vname">${selecting ? "Pick paths" : "Disk usage"} · ${esc(id)}</div>
          ${selecting ? "" : `<button class="vbtn duresize" aria-label="Re-size" title="Re-size now, ignoring cached sizes">↻ Re-size</button>`}
          <button class="vbtn vclose" aria-label="Close">✕</button>
        </div>
        <div class="dubody">
          <div class="browsebar" id="duBar"></div>
          ${selecting ? `<div class="dulegend" style="margin:-2px 0 0">tap a box to include it · tap ⤢ to open a folder</div>`
            : backupPaths.length ? `<div class="dulegend" style="margin:-2px 0 0">✓ backed up · ◐ partially backed up · ○ not backed up</div>` : ""}
          <div class="dumap" id="duMap"><div class="empty">sizing…</div></div>
          <div class="dulegend" id="duLegend"></div>
          ${selecting ? `<div class="dufoot" id="duFoot">
            <span class="duselcount" id="duSelCount"></span>
            <button class="btn ghost" id="duCancel">Cancel</button>
            <button class="btn primary" id="duAdd">Add paths</button>
          </div>` : ""}
        </div>
      </div>`;
    document.body.appendChild(ov);
    const alive = () => document.body.contains(ov);
    const close = () => { ov.remove(); document.removeEventListener("keydown", onkey); window.removeEventListener("resize", relayout); };
    // entryPath builds an entry's absolute path from its listing, the same join
    // the drill-in uses ("/" + name off the current dir, minding the root).
    const entryPath = (l, e) => (l.path === "/" ? "" : l.path) + "/" + e.name;
    function updateFoot(){
      if(!selecting) return;
      const cnt = ov.querySelector("#duSelCount");
      const add = ov.querySelector("#duAdd");
      if(!cnt) return;
      let known = 0; selected.forEach(v => { known += v || 0; });
      cnt.textContent = selected.size
        ? `${selected.size} selected${known ? " · " + fmtBytes(known) : ""}`
        : "nothing selected yet";
      if(add) add.disabled = selected.size === 0;
    }
    if(selecting){
      ov.querySelector("#duCancel").addEventListener("click", close);
      ov.querySelector("#duAdd").addEventListener("click", ()=>{
        if(opts.onDone) opts.onDone(Array.from(selected.keys()));
        close();
      });
      updateFoot();
    }
    const onkey = e => { if(e.key==="Escape") close(); };
    document.addEventListener("keydown", onkey);
    ov.querySelector(".vclose").addEventListener("click", close);
    ov.addEventListener("click", e => { if(e.target===ov) close(); });
    // "Re-size" forces a fresh walk of whatever directory is on screen, ignoring
    // the agent's cached sizing; ordinary drill-in/up navigation uses the cache.
    const resizeBtn = ov.querySelector(".duresize");
    if(resizeBtn) resizeBtn.addEventListener("click", ()=> load(curPath, {refresh:true}));

    let lastListing = null;
    let curPath = startPath || "/";
    function drawMap(l){
      lastListing = l;
      const map = ov.querySelector("#duMap");
      if(!map) return;
      if(!l.entries.length){ map.innerHTML = `<div class="empty">empty directory</div>`; return; }
      const rect = map.getBoundingClientRect();
      if(rect.width<=0 || rect.height<=0) return;
      const rects = squarify(l.entries, 0, 0, rect.width, rect.height);
      map.innerHTML = "";
      l.entries.forEach((e,i)=>{
        const r = rects[i];
        const path = entryPath(l, e);
        const isSel = selecting && selected.has(path);
        const cov = backupPaths.length ? duCoverage(path, backupPaths) : null;
        const fitsBadge = r.w>26 && r.h>20;
        const cell = document.createElement("div");
        cell.className = "ducell" + (e.isDir ? " dir" : "") + (selecting ? " selectable" : "") + (isSel ? " sel" : "")
          + (cov && cov!=="none" && !fitsBadge ? ` ring-${cov}` : "");
        cell.style.cssText = `left:${r.x}px;top:${r.y}px;width:${Math.max(r.w-1,0)}px;height:${Math.max(r.h-1,0)}px;background:${duColor(i, e.isDir)}`;
        const covLabel = cov === "full" ? "backed up" : cov === "partial" ? "partially backed up" : cov === "none" ? "not backed up" : "";
        cell.title = `${e.name} — ${fmtBytes(e.size)}` + (covLabel ? ` — ${covLabel}` : "");
        const showText = r.w>34 && r.h>16;
        if(showText){
          cell.innerHTML = `<span class="dn">${esc(e.name)}</span>` + (r.h>30 ? `<span class="ds">${fmtBytes(e.size)}</span>` : "");
        }
        if(cov && fitsBadge){
          const badge = document.createElement("span");
          badge.className = "dubadge " + cov;
          badge.textContent = cov==="full" ? "✓" : cov==="partial" ? "◐" : "○";
          cell.appendChild(badge);
        }
        if(selecting){
          if(isSel){ const badge = document.createElement("span"); badge.className = "duselbadge"; badge.textContent = "✓"; cell.appendChild(badge); }
          // A dir keeps a way to drill in: a corner ⤢ that opens it, so the tap
          // itself is free to mean "include this".
          if(e.isDir && r.w>26 && r.h>20){
            const open = document.createElement("span");
            open.className = "dopen"; open.textContent = "⤢"; open.title = "Open folder";
            open.addEventListener("click", ev=>{ ev.stopPropagation(); load(path); });
            cell.appendChild(open);
          }
          cell.addEventListener("click", ()=>{
            if(selected.has(path)) selected.delete(path);
            else selected.set(path, e.size || 0);
            drawMap(l); updateFoot();
          });
        } else if(e.isDir){
          cell.addEventListener("click", ()=> load(path));
        }
        map.appendChild(cell);
      });
    }
    function relayout(){ if(lastListing) drawMap(lastListing); }
    window.addEventListener("resize", relayout);

    function renderDu(l){
      const bar = ov.querySelector("#duBar");
      const up = l.parent ? `<a data-duup="${esc(l.parent)}">↑ up</a>` : "";
      let shown = l.path;
      if(l.path.length > 40){
        const parts = l.path.split("/").filter(Boolean);
        shown = "/…/" + parts.slice(-2).join("/");
      }
      bar.innerHTML = `<span class="bpath" title="${esc(l.path)}">${esc(shown)}</span>
        <span class="bactions">${up}<a data-duclose>✕ close</a></span>`;
      bar.querySelector("[data-duclose]").addEventListener("click", close);
      const upEl = bar.querySelector("[data-duup]");
      if(upEl) upEl.addEventListener("click", ()=> load(upEl.dataset.duup));

      const legend = ov.querySelector("#duLegend");
      const total = l.entries.reduce((s,e)=>s+e.size, 0);
      // l.computedAt (set by the agent's du cache) is when this sizing actually
      // ran — surfaced so a cached number reads as "as of 14:03", not live.
      let fresh = "";
      if(l.computedAt){
        const d = new Date(l.computedAt);
        if(!isNaN(d)){
          const hh = String(d.getHours()).padStart(2,"0"), mm = String(d.getMinutes()).padStart(2,"0");
          fresh = `as of ${hh}:${mm}`;
        }
      }
      legend.textContent = l.entries.length
        ? `${l.entries.length} item${l.entries.length===1?"":"s"} · ${fmtBytes(total)} total`
          + (l.truncated ? " · truncated — some subtrees were too large to fully size" : "")
          + (fresh ? " · " + fresh : "")
        : (fresh || "");
      drawMap(l);
    }

    let seq = 0;
    function load(p, o){
      o = o || {};
      curPath = p;
      const my = ++seq;
      const stale = () => !alive() || my !== seq;
      const map = ov.querySelector("#duMap");
      map.innerHTML = `<div class="empty">sizing…</div>`;
      const refreshQ = o.refresh ? "&refresh=1" : "";
      const req = MODE === "demo"
        ? Promise.resolve(demoDu(id, p))
        : fetch(`api/machines/${encodeURIComponent(id)}/du?path=${encodeURIComponent(p)}${refreshQ}`, {cache:"no-store"}).then(r=>{
            if(!r.ok){
              const msg = r.status===403 ? "permission denied — the hush user can't read this"
                : r.status===404 ? "no such directory"
                : r.status===400 ? "not a directory"
                : r.status===502 ? "machine unreachable" : `error ${r.status}`;
              return Promise.reject(msg);
            }
            return r.json();
          });
      req.then(l => { if(!stale()) renderDu(l); })
        .catch(e => {
          if(stale()) return;
          const bar = ov.querySelector("#duBar");
          bar.innerHTML = `<span class="bpath">${esc(p)}</span><span class="bactions"><a data-duclose>✕ close</a></span>`;
          bar.querySelector("[data-duclose]").addEventListener("click", close);
          const map2 = ov.querySelector("#duMap");
          if(map2) map2.innerHTML = `<div class="empty">${esc(typeof e==="string"?e:"could not load")}</div>`;
          const legend = ov.querySelector("#duLegend"); if(legend) legend.textContent = "";
        });
    }
    load(startPath || "/");
  }

  // demoDu fabricates a directory-sizing response for MODE="demo" (there's no
  // agent to actually walk). The root of each demo machine is hand-picked to
  // look like that box's role (nas gets a huge /mnt, atlas — the media server
  // — a huge /srv); anything deeper is generated by a seeded PRNG keyed on
  // the exact path, so repeat visits during one session are stable rather
  // than reshuffling, but there's no attempt to make nested totals add up to
  // the parent — it only needs to look plausible, not balance a ledger.
  const DU_ROOTS = {
    nas: [
      {name:"mnt", isDir:true, size:180e9}, {name:"var", isDir:true, size:42e9},
      {name:"usr", isDir:true, size:9.4e9}, {name:"home", isDir:true, size:3.2e9},
      {name:"opt", isDir:true, size:1.1e9}, {name:"boot", isDir:true, size:0.3e9},
      {name:"etc", isDir:true, size:0.08e9},
    ],
    citadel: [
      {name:"var", isDir:true, size:88e9}, {name:"home", isDir:true, size:24e9},
      {name:"usr", isDir:true, size:11e9}, {name:"srv", isDir:true, size:6.4e9},
      {name:"opt", isDir:true, size:2.1e9}, {name:"boot", isDir:true, size:0.3e9},
      {name:"etc", isDir:true, size:0.06e9},
    ],
    forge: [
      {name:"home", isDir:true, size:210e9}, {name:"var", isDir:true, size:96e9},
      {name:"usr", isDir:true, size:14e9}, {name:"opt", isDir:true, size:3.4e9},
      {name:"etc", isDir:true, size:0.05e9},
    ],
    atlas: [
      {name:"srv", isDir:true, size:120e9}, {name:"var", isDir:true, size:28e9},
      {name:"home", isDir:true, size:6e9}, {name:"usr", isDir:true, size:9e9},
      {name:"opt", isDir:true, size:1.8e9},
    ],
    "pi-gate": [
      {name:"var", isDir:true, size:4.2e9}, {name:"usr", isDir:true, size:3.6e9},
      {name:"home", isDir:true, size:0.4e9}, {name:"boot", isDir:true, size:0.2e9},
      {name:"etc", isDir:true, size:0.03e9},
    ],
    "lab-01": [
      {name:"nix", isDir:true, size:42e9}, {name:"home", isDir:true, size:18e9},
      {name:"var", isDir:true, size:9e9}, {name:"usr", isDir:true, size:6e9},
    ],
  };
  function hashStr(s){
    let h = 2166136261;
    for(let i=0;i<s.length;i++){ h ^= s.charCodeAt(i); h = Math.imul(h, 16777619); }
    return h >>> 0;
  }
  function mulberry32(seed){
    return function(){
      seed |= 0; seed = (seed + 0x6D2B79F5) | 0;
      let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
      t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
  }
  function pickN(rand, arr, n){
    const pool = arr.slice(), out = [];
    for(let i=0;i<n && pool.length;i++) out.push(pool.splice(Math.floor(rand()*pool.length), 1)[0]);
    return out;
  }
  function synthDuEntries(seedKey, depth){
    const rand = mulberry32(hashStr(seedKey));
    const n = 3 + Math.floor(rand()*6);
    const dirNames = pickN(rand, ["config","cache","logs","data","src","assets","backups","tmp","modules","build","releases","snapshots"], n);
    const fileNames = pickN(rand, ["access.log","archive.tar.gz","dump.sql","image.iso","notes.txt","dataset.csv","snapshot.qcow2","bundle.js","report.pdf","index.db"], n);
    let remaining = Math.max(20e6, 3e9 / Math.pow(2.6, Math.max(depth-2,0))) * (0.5+rand()*0.9);
    const entries = [];
    for(let i=0;i<n;i++){
      const isDir = dirNames.length && rand()<0.55 && i<n-1;
      const share = i===n-1 ? Math.max(remaining,1024) : remaining*(0.2+rand()*0.35);
      remaining = Math.max(remaining-share, 0);
      entries.push(isDir
        ? {name:dirNames.pop(), isDir:true, size:Math.round(share)}
        : {name:fileNames.length ? fileNames.pop() : `file${i}.dat`, isDir:false, size:Math.round(share)});
    }
    return entries;
  }
  function demoDu(id, path){
    const clean = path === "" ? "/" : path;
    const parts = clean.split("/").filter(Boolean);
    const entries = (clean === "/" && DU_ROOTS[id]) ? DU_ROOTS[id].slice() : synthDuEntries(id+"::"+clean, parts.length);
    entries.sort((a,b)=>b.size-a.size);
    return {
      path: clean,
      parent: clean === "/" ? "" : (parts.length<=1 ? "/" : "/"+parts.slice(0,-1).join("/")),
      entries,
      truncated: false,
      computedAt: new Date().toISOString(),
    };
  }

  // demoTop synthesises an htop-style reading so the live CPU/network panel is
  // legible in demo mode, where there's no hush-control or agent behind it.
  const DEMO_PROC_NAMES = ["hush-agent","tailscaled","systemd","dockerd","postgres","nginx",
    "node","python3","containerd","sshd","redis-server","cron","journald","chronyd","qemu-system"];
  function demoTop(id){
    const m = byId(id) || {cpu:20, mem:40, load:"0.6"};
    const ncpu = 8;
    const cores = [];
    for(let i=0;i<ncpu;i++){ cores.push(Math.max(0, Math.min(100, Math.round(m.cpu + (Math.random()-0.5)*55)))); }
    const totalRam = 16*1024*1024*1024;
    const procs = DEMO_PROC_NAMES.map((n,i)=>{
      const cpu = Math.max(0, (i===0 ? m.cpu*0.4 : 0) + Math.random()*(i<3?55:6));
      const mem = Math.max(0.1, Math.random()*(i<4?8:1.5));
      return {
        pid: 300 + ((i*541)%9000),
        user: i%4===0 ? "root" : "hush",
        command: n,
        cpu: Math.round(cpu*10)/10,
        mem: Math.round(mem*10)/10,
        rss: Math.round(mem/100*totalRam),
      };
    }).sort((a,b)=>b.cpu-a.cpu);
    return { host:id, cpu:m.cpu, mem:m.mem, cores, procs, running: 118 + (Math.random()*44|0) };
  }

  function fmtBytes(n){
    if(n==null) return "";
    if(n<1024) return n+" B";
    const u=["K","M","G","T","P"]; let i=-1, v=n;
    do { v/=1024; i++; } while(v>=1024 && i<u.length-1);
    return (v>=10?Math.round(v):v.toFixed(1))+" "+u[i]+"B";
  }

  /* ---------- Live CPU/network panel: an htop-style read of one box that
     re-polls /top every ~2s. Per-core meters + the busiest processes, plus the
     same live CPU/network sparklines the machine card carries (fed by the fleet
     poll's rolling history, so they keep moving under the overlay). Like the
     file viewer this overlay lives on <body>, so the fleet poll re-rendering
     #app underneath never tears it down. ---------- */
  const TOP_POLL_MS = 2000;
  function meterColor(pct){
    return pct>=88 ? "var(--crit)" : pct>=65 ? "var(--warn)" : "var(--accent)";
  }
  function coreMeters(cores){
    if(!cores || !cores.length) return `<span class="vnote" style="padding:0">no per-core data</span>`;
    return cores.map((c,i)=>`<div class="cmeter"><span class="cl">cpu${i}</span>`
      + `<span class="ctrack"><span class="cfill" style="width:${Math.max(0,Math.min(100,c))}%;background:${meterColor(c)}"></span></span>`
      + `<span class="cv">${c}%</span></div>`).join("");
  }
  function procRows(procs, sortKey){
    const head = `<div class="prow phead"><span class="num">pid</span><span class="u">user</span>`
      + `<span class="num sortable${sortKey==="cpu"?" on":""}" data-sort="cpu">cpu%</span>`
      + `<span class="num sortable${sortKey==="mem"?" on":""}" data-sort="mem">mem%</span>`
      + `<span class="cmd">command</span></div>`;
    if(!procs || !procs.length) return head + `<div class="empty">no processes reported</div>`;
    return head + procs.map(p=>`<div class="prow"><span class="num">${p.pid}</span>`
      + `<span class="u">${esc(p.user||"")}</span>`
      + `<span class="num">${(p.cpu||0).toFixed(1)}</span>`
      + `<span class="num">${(p.mem||0).toFixed(1)}</span>`
      + `<span class="cmd" title="${esc(p.command||"")}">${esc(p.command||"")}</span></div>`).join("");
  }
  function openTop(id, focus){
    const ov = document.createElement("div");
    ov.className = "vov";
    ov.innerHTML = `<div class="vsheet">
        <div class="vbar">
          <div class="vname">${esc(id)} · live</div>
          <div class="vmeta" id="topMeta">reading…</div>
          <button class="vbtn vclose" aria-label="Close">✕</button>
        </div>
        <div class="topbody">
          <div class="topsum" id="topSum"></div>
          <div class="topcores" id="topCores"><span class="vnote" style="padding:0">reading cores…</span></div>
          <div class="ptable" id="topProcs"><div class="empty">reading processes…</div></div>
        </div>
      </div>`;
    document.body.appendChild(ov);
    const alive = () => document.body.contains(ov);
    let sortKey = focus === "net" ? "mem" : "cpu"; // net view defaults to sorting by memory footprint
    let last = null, seq = 0, timer = null;
    const close = () => { if(timer) clearInterval(timer); ov.remove(); document.removeEventListener("keydown", onkey); };
    const onkey = e => { if(e.key==="Escape") close(); };
    document.addEventListener("keydown", onkey);
    ov.querySelector(".vclose").addEventListener("click", close);
    ov.addEventListener("click", e => { if(e.target===ov) close(); });

    function draw(data){
      if(!alive()) return;
      last = data;
      const m = byId(id) || {};
      const hist = (m.loadHist && m.loadHist.length>1) ? m.loadHist : [data.cpu||0, data.cpu||0];
      const rxH = (m.netRxHist && m.netRxHist.length>1) ? m.netRxHist : [m.netRx||0, m.netRx||0];
      const txH = (m.netTxHist && m.netTxHist.length>1) ? m.netTxHist : [m.netTx||0, m.netTx||0];
      const meta = ov.querySelector("#topMeta");
      if(meta) meta.textContent = `${data.running!=null?data.running:(data.procs||[]).length} procs · ${(data.cores||[]).length} cores`;
      const sum = ov.querySelector("#topSum");
      if(sum) sum.innerHTML = `
        <div class="loadbox"><div class="lbl"><span>cpu ${data.cpu!=null?data.cpu+"%":"—"}</span><b>load ${m.load||"—"}</b></div>${sparkline(hist,300,34)}</div>
        <div class="loadbox"><div class="lbl"><span>network</span><b><span style="color:var(--accent)">↓ ${fmtBytes(m.netRx||0)}/s</span>&nbsp;&nbsp;<span style="color:var(--ring2)">↑ ${fmtBytes(m.netTx||0)}/s</span></b></div>${netSpark(rxH,txH,300,34)}</div>`;
      const cores = ov.querySelector("#topCores");
      if(cores) cores.innerHTML = coreMeters(data.cores);
      const procs = (data.procs||[]).slice().sort((a,b)=> sortKey==="mem" ? (b.mem||0)-(a.mem||0) : (b.cpu||0)-(a.cpu||0));
      const tbl = ov.querySelector("#topProcs");
      if(tbl){
        tbl.innerHTML = procRows(procs, sortKey);
        tbl.querySelectorAll(".sortable").forEach(h=>{
          h.addEventListener("click", ()=>{ sortKey = h.dataset.sort; if(last) draw(last); });
        });
      }
    }
    function showErr(msg){
      if(!alive()) return;
      const tbl = ov.querySelector("#topProcs");
      if(tbl) tbl.innerHTML = `<div class="empty">${esc(msg)}</div>`;
    }
    async function tick(){
      const my = ++seq;
      if(MODE === "demo"){ draw(demoTop(id)); return; }
      try {
        const r = await fetch(`api/machines/${encodeURIComponent(id)}/top`, {cache:"no-store"});
        if(!alive() || my!==seq) return;
        if(!r.ok){
          showErr(r.status===404 ? "this agent is too old to report processes — update hush-agent"
            : r.status===502 ? "machine unreachable"
            : `error ${r.status}`);
          return;
        }
        const data = await r.json();
        if(!alive() || my!==seq) return;
        draw(data);
      } catch(e){
        if(alive() && my===seq) showErr("could not reach the machine");
      }
    }
    tick();
    timer = setInterval(tick, TOP_POLL_MS);
  }

  // relTimeISO adapts relTime (which wants an epoch-ms) to the agent's RFC3339
  // lastRun stamps, degrading to "" on anything unparseable.
  function relTimeISO(iso){ const t = Date.parse(iso); return isNaN(t) ? "" : relTime(t); }
  // relTime renders a coarse "how long ago" for ephemeral run records.
  function relTime(ts){
    // Handles both directions: "6h ago" for a past run, "in 18h" for the next
    // scheduled one. A backup's next_run is a future timestamp, and clamping it
    // to "just now" (as this did) hid the one number a backup console exists to
    // show — when the nightly fires next.
    const d = Date.now() - ts;
    const s = Math.round(Math.abs(d) / 1000);
    if(s < 45) return "just now";
    const m = Math.round(s/60);
    const say = m < 60 ? m+"m" : Math.round(m/60)+"h";
    return d < 0 ? "in "+say : say+" ago";
  }

  function machineById(id){ return M.find(m=>m.id===id); }

  let CONV_BACKUPS = [];                       // /api/backup-status: root-run convention backups, per machine

  // Backup status is polled slowly: a nightly backup changes once a day, so
  // there is nothing to gain from riding the 2.5s fleet cadence. A failure to
  // load leaves the last known list in place rather than blanking the section.
  async function pollBackupStatus(){
    try {
      const r = await fetch("api/backup-status", {cache:"no-store"});
      if(!r.ok) return;
      const data = await r.json();
      if(Array.isArray(data)) { CONV_BACKUPS = data; render(); }
    } catch(e){ /* keep the last known state */ }
  }

  /* ---------- data: live + demo ---------- */
  function normalize(m){
    m.backups = m.backups || [];
    m.netRx = m.netRx || 0;
    m.netTx = m.netTx || 0;
    if(m.online===undefined) m.online = true;
    return m;
  }

  function ingestLive(data){
    M = data.map(m=>{
      normalize(m);
      const h = cpuHist[m.id] || Array(HIST).fill(m.cpu);
      h.push(m.cpu); while(h.length>HIST) h.shift();
      cpuHist[m.id] = h;
      m.loadHist = h.slice();
      if(m.gpu!=null){
        const gh = gpuHist[m.id] || Array(HIST).fill(m.gpu);
        gh.push(m.gpu); while(gh.length>HIST) gh.shift();
        gpuHist[m.id] = gh;
        m.gpuHist = gh.slice();
      }
      const rxh = netRxHist[m.id] || Array(HIST).fill(m.netRx);
      rxh.push(m.netRx); while(rxh.length>HIST) rxh.shift();
      netRxHist[m.id] = rxh;
      m.netRxHist = rxh.slice();
      const txh = netTxHist[m.id] || Array(HIST).fill(m.netTx);
      txh.push(m.netTx); while(txh.length>HIST) txh.shift();
      netTxHist[m.id] = txh;
      m.netTxHist = txh.slice();
      return m;
    });
    // a machine added through "Add machine" graduates out of PENDING once its
    // first real /api/fleet entry shows up, with one pop-in animation.
    PENDING = PENDING.filter(p => {
      const arrived = M.some(m => m.id === p.id || (p.ip && m.ip === p.ip));
      if(arrived) JUST_ARRIVED.add(p.id);
      return !arrived;
    });
  }

  function setChip(){
    const chip = $("#statusChip");
    chip.className = "chip" + (MODE==="demo"?" demo":MODE==="live"?"":" down");
    let txt = "connecting…";
    if(MODE==="live"){
      const online = M.filter(m=>m.online).length;
      txt = online===M.length ? "transport ok" : `${online}/${M.length} reachable`;
    } else if(MODE==="demo"){ txt = "demo data"; }
    else if(MODE==="lost"){ txt = "lost connection"; }
    chip.innerHTML = `<span class="live"></span>${txt}`;
    const pairing = PENDING.length ? ` · ${PENDING.length} pairing` : "";
    $("#dockHint").textContent = MODE==="lost"
      ? `showing last known state · ${M.length} machine${M.length===1?"":"s"}`
      : `${M.length} machine${M.length===1?"":"s"}${pairing}`;
  }

  function setLostBanner(on){
    const bar = $("#lostBar");
    bar.hidden = !on;
    document.body.classList.toggle("conn-lost", on);
    document.title = on ? "⛔ DISCONNECTED — hush" : DOC_TITLE;
    if(on){
      const secs = Math.max(0, Math.round((Date.now() - lostSince) / 1000));
      const ago = secs < 60 ? `${secs}s` : `${Math.round(secs/60)}m`;
      $("#lostBarText").textContent = `lost connection to hush-control · reconnecting… · down for ${ago}`;
    }
  }

  async function poll(){
    try {
      const r = await fetch("api/fleet", {cache:"no-store"});
      if(!r.ok) throw new Error("bad status");
      const data = await r.json();
      if(!Array.isArray(data)) throw new Error("bad payload");
      ingestLive(data);
      MODE = "live";
      everLive = true;
      lostSince = 0;
      setLostBanner(false);
    } catch(e){
      if(everLive){
        // we had a real connection and lost it (e.g. hush-control rebooting) —
        // freeze the last known fleet rather than silently swapping in fake
        // demo machines, and make the loss unmissable.
        if(MODE!=="lost"){ lostSince = Date.now(); }
        MODE = "lost";
        setLostBanner(true);
      } else {
        // never connected at all (local preview, file://, no backend running) —
        // this is the legitimate demo-data fallback.
        if(MODE!=="demo"){ M = demoFleet(); CONV_BACKUPS = demoConvBackups(); }
        MODE = "demo";
        jitterDemo();
      }
    }
    setChip();
    render();
  }

  /* ---------- demo fleet (offline / file:// fallback) ---------- */
  function walk(n, base, amp){
    const a=[]; let v=base;
    for(let i=0;i<n;i++){ v+=(Math.random()-0.5)*amp; v=Math.max(2,Math.min(98,v)); a.push(v); }
    return a;
  }
  function walkBytes(n, base, amp){
    const a=[]; let v=base;
    for(let i=0;i<n;i++){ v+=(Math.random()-0.5)*amp; v=Math.max(0,v); a.push(v); }
    return a;
  }
  function demoFleet(){
    const mk = o => {
      o.online=true; o.backups=o.backups||[];
      o.loadHist=walk(HIST,o.cpu,12);
      if(o.gpu!=null) o.gpuHist = walk(HIST, o.gpu, 14);
      o.netRx = Math.round(15000 + Math.random()*220000);
      o.netTx = Math.round(4000 + Math.random()*60000);
      o.netRxHist = walkBytes(HIST, o.netRx, o.netRx*0.35);
      o.netTxHist = walkBytes(HIST, o.netTx, o.netTx*0.35);
      return o;
    };
    return [
      mk({ id:"citadel", agentVersion:"v1.3.0", os:"Debian 12", ip:"100.71.8.9", role:"", status:"good", cpu:23, mem:47, disk:38, up:"41d", load:"0.9",
        gpu:18, vram:47, gpuName:"RTX 3090", vramText:"11.3 / 24 GB",
        llm:{runtimes:[{kind:"openai",addr:"100.71.8.9:8091",exposure:"tailnet",models:["qwen2.5-coder-32b-instruct","llama-3.3-70b-instruct"]}]},
        backups:[{id:"citadel-root",name:"citadel-root",repo:"rest:http://nas:8000/citadel",paths:["/"],oneFileSystem:true,schedule:"0 3 * * *",status:{runs:41,lastCode:0,lastRun:"2026-07-18T03:00:00Z",lastSnapshot:"9f2ab3c1"}}] }),
      mk({ id:"nas", agentVersion:"v1.3.0", os:"TrueNAS · ZFS", ip:"100.71.4.2", role:"store", status:"good", cpu:11, mem:34, disk:81, up:"92d", load:"0.4", gpu:null,
        backup:{enabled:true,restic:"restic 0.16.0",vault:true} }),
      mk({ id:"forge", agentVersion:"v1.2.0", latestVersion:"v1.3.0", agentUpdateAvailable:true, os:"Arch Linux", ip:"100.71.2.5", role:"", status:"warn", cpu:88, mem:71, disk:52, up:"6d", load:"14.2",
        gpu:62, vram:71, gpuName:"RTX 4070", vramText:"8.5 / 12 GB", alert:"load 14.2 · CI backlog building",
        llm:{runtimes:[{kind:"openai",addr:"127.0.0.1:8091",exposure:"loopback",models:["deepseek-coder-v2-lite"]}]},
        backup:{enabled:false,restic:""} }),
      mk({ id:"atlas", agentVersion:"v1.3.0", os:"Ubuntu 24.04", ip:"100.71.9.3", role:"", status:"crit", cpu:5, mem:22, disk:63, up:"2d", load:"0.2",
        gpu:34, vram:41, gpuName:"RTX 3060", vramText:"4.9 / 12 GB", alert:"disk I/O errors — check drive health" }),
      mk({ id:"pi-gate", agentVersion:"v1.3.0", os:"Raspberry Pi OS", ip:"100.71.0.1", role:"gateway", status:"good", cpu:31, mem:58, disk:44, up:"118d", load:"1.1", gpu:null }),
      mk({ id:"lab-01", agentVersion:"v1.3.0", os:"NixOS 24.05", ip:"100.71.5.7", role:"", status:"good", cpu:9, mem:19, disk:12, up:"3d", load:"0.3",
        gpu:88, vram:91, gpuName:"RTX 4090", vramText:"21.8 / 24 GB",
        llm:{runtimes:[{kind:"ollama",addr:"0.0.0.0:11434",exposure:"open",models:["qwen2.5-coder:32b","deepseek-r1:14b"]}]} }),
      mk({ id:"studio", agentVersion:"v1.3.0", os:"Debian 12", ip:"100.71.6.4", role:"", status:"good", cpu:14, mem:31, disk:57, up:"22d", load:"0.6", gpu:null }),
    ];
  }
  // demoConvBackups seeds the backup-first fleet story for MODE="demo": the
  // shape /api/backup-status returns, so the Fleet page's Backups section and
  // every posture signal render exactly as they would against a live fleet.
  // The story is deliberate — most boxes shipping clean nightly snapshots into
  // the nas vault, the vault itself copying offsite (the 3-2-1 second hop), one
  // run in flight right now, one coming back incomplete, and one outright
  // failing:
  //   citadel → nas   protected   (clean nightly)
  //   nas     → offsite protected (the vault's own offsite copy)
  //   pi-gate → nas   RUNNING     (nightly in flight — a run behind it in history)
  //   lab-01  → nas   at risk     (restic exit 3 — files it couldn't read)
  //   atlas   → nas   FAILED      (exit 1 — a healthy history that just broke)
  //   studio  → nas   PAUSED      (timer disabled — a clean run, deliberately stopped)
  //   forge   —       unprotected (reachable, no backup configured)
  function demoConvBackups(){
    const ago = h => new Date(Date.now() - h*3600e3).toISOString();
    const soon = h => new Date(Date.now() + h*3600e3).toISOString();
    const GiB = n => Math.round(n*1073741824);
    // hist builds the history-bar series backupCard draws; each {h} hours ago,
    // {add} GiB added, optionally warn/fail.
    const hist = spec => spec.map(x => ({
      ok: x.fail ? false : true, incomplete: !!x.warn,
      finished: ago(x.h), exit_code: x.fail ? 1 : x.warn ? 3 : 0,
      summary: { data_added: GiB(x.add) },
    }));
    return [
      { host:"citadel", reachable:true, backups:[{
        name:"citadel-root", repository:"rest:http://nas:8000/citadel", paths:["/"],
        started:ago(6.1), finished:ago(6), exit_code:0, ok:true, incomplete:false, next_run:soon(18),
        summary:{ total_bytes_processed:GiB(214), data_added:GiB(1.9) },
        history:hist([{h:150,add:2.4},{h:126,add:1.6},{h:102,add:2.1},{h:78,add:1.3},{h:54,add:2.0},{h:30,add:1.7},{h:6,add:1.9}]),
      }]},
      { host:"nas", reachable:true, backups:[{
        name:"nas-config", repository:"sftp:offsite:/hush/nas", paths:["/etc","/srv/config"],
        started:ago(10.05), finished:ago(10), exit_code:0, ok:true, incomplete:false, next_run:soon(14),
        summary:{ total_bytes_processed:GiB(38), data_added:GiB(0.3) },
        history:hist([{h:154,add:0.5},{h:130,add:0.2},{h:106,add:0.4},{h:82,add:0.3},{h:58,add:0.2},{h:34,add:0.4},{h:10,add:0.3}]),
      }]},
      { host:"pi-gate", reachable:true, backups:[{
        // A run in flight: "state":"running", a recent start, and no outcome
        // fields yet (ok/finished/summary) — exactly the shape the runner
        // writes at the top of a run. next_run is still in the future, so it
        // reads as backing-up, not stalled. History behind it shows the card's
        // strip still draws under a live run.
        name:"pi-gate-etc", repository:"rest:http://nas:8000/pi-gate", paths:["/etc","/var/lib/adguardhome"],
        started:ago(0.11), state:"running", next_run:soon(19),
        // Live byte progress, in the shape the runner publishes beside the
        // status file. jitterDemo advances it on every poll: the bar visibly
        // moves, and the sample stays fresh — the console discards one older
        // than two minutes, so a frozen fixture would decay into the fallback
        // shuttle while the demo page sat open.
        progress:{ percent_done:0.37, bytes_done:GiB(2.6), total_bytes:GiB(7.1),
          files_done:1840, total_files:5200, seconds_remaining:412, updated:ago(0) },
        history:hist([{h:149,add:0.12},{h:125,add:0.06},{h:101,add:0.09},{h:77,add:0.05},{h:53,add:0.11},{h:29,add:0.07},{h:5,add:0.08}]),
      }]},
      { host:"lab-01", reachable:true, backups:[{
        name:"lab-01-home", repository:"rest:http://nas:8000/lab-01", paths:["/home","/srv/models"],
        started:ago(8.2), finished:ago(8), exit_code:3, ok:false, incomplete:true, next_run:soon(16),
        summary:{ total_bytes_processed:GiB(96), data_added:GiB(3.1) },
        history:hist([{h:152,add:2.9},{h:128,add:3.4},{h:104,add:2.7},{h:80,add:3.0},{h:56,add:2.8},{h:32,add:3.2},{h:8,add:3.1,warn:true}]),
      }]},
      { host:"atlas", reachable:true, backups:[{
        name:"atlas-media", repository:"rest:http://nas:8000/atlas", paths:["/var/lib/plex","/srv/media/config"],
        started:ago(2.1), finished:ago(2), exit_code:1, ok:false, incomplete:false, next_run:soon(22),
        summary:{ total_bytes_processed:GiB(0), data_added:GiB(0) },
        history:hist([{h:146,add:1.1},{h:122,add:0.9},{h:98,add:1.3},{h:74,add:1.0},{h:50,add:1.2},{h:26,add:0.8},{h:2,add:0,fail:true}]),
      }]},
      { host:"studio", reachable:true, backups:[{
        // A paused backup: the last run was clean, but the timer has since been
        // switched off (systemctl disable), so the agent reports "paused":true
        // and there is no next_run. The last run is deliberately old — without
        // the paused flag this would age past the 36h stale line and read as "at
        // risk"; paused is exactly the state that keeps a box someone turned off
        // on purpose from nagging as if it had silently broken.
        name:"studio-projects", repository:"rest:http://nas:8000/studio", paths:["/home/studio/projects"],
        started:ago(50.1), finished:ago(50), exit_code:0, ok:true, incomplete:false, paused:true,
        summary:{ total_bytes_processed:GiB(72), data_added:GiB(1.2) },
        history:hist([{h:170,add:1.4},{h:146,add:1.1},{h:122,add:1.3},{h:98,add:1.0},{h:74,add:1.2},{h:50,add:1.2}]),
      }]},
      { host:"forge", reachable:true, backups:[] },
    ];
  }
  function jitterDemo(){
    M.forEach(m=>{
      const j = v => Math.max(2, Math.min(98, Math.round(v + (Math.random()-0.5)*(m.status==="warn"?7:4))));
      m.cpu = j(m.cpu); m.mem = Math.max(2,Math.min(98, m.mem + (((Math.random()-0.5)*2)|0)));
      if(m.gpu!=null){ m.gpu = j(m.gpu); m.vram = Math.max(2,Math.min(99, m.vram + (((Math.random()-0.5)*2)|0)));
        m.gpuHist = (m.gpuHist||Array(HIST).fill(m.gpu)).slice(1).concat([m.gpu]); }
      m.loadHist = m.loadHist.slice(1).concat([m.cpu]);
      m.netRx = Math.max(0, Math.round(m.netRx + (Math.random()-0.5)*Math.max(2000,m.netRx*0.5)));
      m.netTx = Math.max(0, Math.round(m.netTx + (Math.random()-0.5)*Math.max(1000,m.netTx*0.5)));
      m.netRxHist = m.netRxHist.slice(1).concat([m.netRx]);
      m.netTxHist = m.netTxHist.slice(1).concat([m.netTx]);
    });
    // Advance the demo's one in-flight backup. total_bytes deliberately holds
    // still, so the scan-detection settles after a couple of polls and the demo
    // shows both faces of the bar: "still scanning" first, then the ETA.
    for(const h of CONV_BACKUPS){
      for(const b of (h.backups||[])){
        if(!b.progress || !bkRunning(b)) continue;
        const p = b.progress;
        p.percent_done = Math.min(0.995, p.percent_done + 0.004 + Math.random()*0.004);
        p.bytes_done = Math.round(p.total_bytes * p.percent_done);
        p.files_done = Math.round(p.total_files * p.percent_done);
        p.seconds_remaining = Math.max(1, Math.round(650 * (1 - p.percent_done)));
        p.updated = new Date().toISOString();
      }
    }
  }

  /* ---------- build → add a machine ---------- */
  // v2: the dock button does one thing — add a machine — so it opens the
  // add-machine sheet directly instead of a palette of not-yet-built constructs.
  $("#buildBtn").addEventListener("click", ()=>openAddSheet());

  /* ---------- add machine sheet ---------- */
  const addScrim = $("#addScrim");
  const addBody = $("#addSheetBody");
  let addStep = 0;                 // 0 form, 1 verified, 2 done
  let addDraft = { name:"", addr:"", role:"" };
  let addTestResult = null;
  let addBusy = false;
  let addError = "";
  // tailnet discovery state for the "Scan tailnet" affordance.
  let discoverState = { ran:false, loading:false, available:true, error:"", candidates:[] };
  // lastDiscover holds the most recent background poll result, so the badge and
  // the freshly-opened sheet can show discovered agents without waiting on a scan.
  let lastDiscover = null;

  function openAddSheet(){
    addStep = 0; addDraft = { name:"", addr:"", role:"" };
    addTestResult = null; addBusy = false; addError = "";
    // Seed from the last background poll so cached candidates show immediately;
    // "Rescan" then refreshes them live.
    if(lastDiscover){
      discoverState = { ran:true, loading:false, available:lastDiscover.available, error:"", candidates:lastDiscover.candidates.slice() };
    } else {
      discoverState = { ran:false, loading:false, available:true, error:"", candidates:[] };
    }
    addScrim.classList.add("open");
    renderAddSheet();
  }
  function closeAddSheet(){ addScrim.classList.remove("open"); }

  function renderAddSheet(){
    if(addStep === 0){
      addBody.innerHTML = `
        <h3>Add a machine</h3>
        <p class="sd">point hush-control at a new hush-agent on your tailnet</p>
        ${renderScanBlock()}
        <div class="field">
          <label>Name <span class="opt">— optional, falls back to the agent's hostname</span></label>
          <input type="text" id="fName" placeholder="e.g. beacon" value="${esc(addDraft.name)}">
        </div>
        <div class="field">
          <label>Tailnet address</label>
          <input type="text" id="fAddr" placeholder="100.71.x.x:8765" value="${esc(addDraft.addr)}">
          <div class="hint">the agent's tailnet IP or MagicDNS name, port 8765</div>
        </div>
        <div class="field">
          <label>Role <span class="opt">— optional</span></label>
          <input type="text" id="fRole" placeholder="e.g. store, gateway" value="${esc(addDraft.role)}">
        </div>
        <div class="field-note">
          <b>Note —</b> hush-agent has no auth yet, so this just confirms the address answers and saves it. Anything already on your tailnet can reach it.
        </div>
        ${addError ? `<div class="field-note err">${esc(addError)}</div>` : ""}
        <div class="sheet-actions">
          <button class="btn ghost" id="addCancel">Cancel</button>
          <button class="btn primary" id="addTest" ${addBusy?"disabled":""}>${addBusy?"Testing…":"Test connection →"}</button>
        </div>`;
      $("#addCancel").addEventListener("click", closeAddSheet);
      $("#addTest").addEventListener("click", submitTest);
      $("#fName").addEventListener("input", e=>{ addDraft.name = e.target.value; });
      $("#fAddr").addEventListener("input", e=>{ addDraft.addr = e.target.value; });
      $("#fRole").addEventListener("input", e=>{ addDraft.role = e.target.value; });
      attachScanHandlers();
    } else if(addStep === 1){
      const r = addTestResult;
      addBody.innerHTML = `
        <h3>Add a machine</h3>
        <p class="sd">${esc(addDraft.addr)}</p>
        <div class="verify-box">
          <div class="verify-row"><span class="vdot ok"></span>reached the agent</div>
          <div class="verify-row"><span class="vdot ok"></span>/vitals responded in ${r.latencyMs}ms</div>
          <div class="preview">
            <span class="gly">▦</span>
            <div><div class="pn">${esc(addDraft.name || r.host || "—")}</div><div class="pm">${esc(r.os||"—")}</div></div>
          </div>
        </div>
        <div class="sheet-actions">
          <button class="btn ghost" id="addBack">← Back</button>
          <button class="btn primary" id="addSave" ${addBusy?"disabled":""}>${addBusy?"Adding…":"Add to fleet"}</button>
        </div>`;
      $("#addBack").addEventListener("click", ()=>{ addStep=0; renderAddSheet(); });
      $("#addSave").addEventListener("click", submitAdd);
    } else {
      const total = M.length + PENDING.length;
      addBody.innerHTML = `
        <h3>${esc(addDraft.name || (addTestResult&&addTestResult.host) || "Machine")} added ✓</h3>
        <p class="sd">now watching ${total} machine${total===1?"":"s"}</p>
        <div class="field-note"><b>Note —</b> vitals arrive on the next poll (~2.5s). No restart of hush-control needed.</div>
        <div class="sheet-actions">
          <button class="btn primary" id="addDone" style="flex:1">Done</button>
        </div>`;
      $("#addDone").addEventListener("click", closeAddSheet);
    }
  }

  async function submitTest(){
    const addr = ($("#fAddr").value || "").trim();
    addDraft.addr = addr;
    if(!addr){ addError = "enter an address"; renderAddSheet(); return; }
    addBusy = true; addError = ""; renderAddSheet();
    try {
      const r = await fetch("api/agents/test", {
        method: "POST", headers: {"Content-Type":"application/json"},
        body: JSON.stringify({ addr })
      });
      const data = await r.json();
      addBusy = false;
      if(!data.ok){ addError = data.error || "couldn't reach that address"; renderAddSheet(); return; }
      addTestResult = data;
      addStep = 1;
      renderAddSheet();
    } catch(e){
      addBusy = false; addError = "couldn't reach hush-control itself — is it running?";
      renderAddSheet();
    }
  }

  async function submitAdd(){
    addBusy = true; renderAddSheet();
    try {
      const r = await fetch("api/agents", {
        method: "POST", headers: {"Content-Type":"application/json"},
        body: JSON.stringify({ name: addDraft.name, addr: addDraft.addr, role: addDraft.role })
      });
      if(!r.ok){
        addBusy = false; addError = await r.text() || "couldn't add that machine";
        addStep = 0; renderAddSheet(); return;
      }
      const added = await r.json();
      PENDING.push({ id: added.name || (addTestResult&&addTestResult.host) || added.addr, ip: added.ip });
      addBusy = false;
      addStep = 2;
      state.view = "fleet"; // so the pending card is visible behind the sheet
      renderAddSheet();
      render();
      poll(); // fetch fresh vitals now instead of waiting for the next interval tick
      // Drop the just-added machine from the discovery badge without waiting for
      // the next background scan.
      if(lastDiscover){
        lastDiscover.candidates = lastDiscover.candidates.filter(c => c.addr !== addDraft.addr);
        updateAddBadge();
      }
    } catch(e){
      addBusy = false; addError = "couldn't reach hush-control itself — is it running?";
      addStep = 0; renderAddSheet();
    }
  }

  // renderScanBlock draws the "Scan tailnet" control and, once a scan has run,
  // the list of discovered agents not yet in the fleet. Tapping one pre-fills
  // the form and runs the same test → add flow as a hand-typed address.
  function renderScanBlock(){
    const s = discoverState;
    if(s.loading){
      return `<div class="scan-block"><button class="btn ghost" disabled>Scanning tailnet…</button></div>`;
    }
    if(!s.ran){
      return `<div class="scan-block"><button class="btn ghost" id="scanBtn">⟲ Scan tailnet for agents</button></div>`;
    }
    if(!s.available){
      return `<div class="scan-block">
        <button class="btn ghost" id="scanBtn">⟲ Scan tailnet for agents</button>
        <div class="field-note">Tailnet scan needs hush-control running in tsnet mode. Enter the address by hand below.</div></div>`;
    }
    if(s.error){
      return `<div class="scan-block">
        <button class="btn ghost" id="scanBtn">⟲ Rescan tailnet</button>
        <div class="field-note err">${esc(s.error)}</div></div>`;
    }
    if(!s.candidates.length){
      return `<div class="scan-block">
        <button class="btn ghost" id="scanBtn">⟲ Rescan tailnet</button>
        <div class="field-note">No new hush-agents found — everything live on your tailnet is already in the fleet.</div></div>`;
    }
    const rows = s.candidates.map((c,i)=>`
      <button class="disc-row" data-i="${i}">
        <span class="gly">▦</span>
        <div class="disc-meta"><div class="pn">${esc(c.name||c.ip)}</div><div class="pm">${esc(c.ip)}${c.os?" · "+esc(c.os):""}</div></div>
        <span class="disc-add">Add →</span>
      </button>`).join("");
    return `<div class="scan-block">
      <div class="disc-list">${rows}</div>
      <button class="btn ghost" id="scanBtn">⟲ Rescan tailnet</button></div>`;
  }

  function attachScanHandlers(){
    const b = $("#scanBtn");
    if(b) b.addEventListener("click", ()=>scanTailnet(true)); // explicit scan → force fresh
    document.querySelectorAll(".disc-row").forEach(el=>{
      el.addEventListener("click", ()=>{
        const c = discoverState.candidates[+el.dataset.i];
        if(!c) return;
        addDraft.name = c.name || "";
        addDraft.addr = c.addr;
        addError = "";
        const inp = $("#fAddr"); if(inp) inp.value = c.addr;
        const nm = $("#fName"); if(nm) nm.value = c.name || "";
        submitTest(); // reuse the verify → add flow, exactly as a typed address
      });
    });
  }

  async function scanTailnet(fresh){
    discoverState.loading = true; discoverState.error = ""; renderAddSheet();
    try {
      const r = await fetch("api/discover" + (fresh ? "?rescan=1" : ""));
      const data = await r.json();
      discoverState = {
        ran:true, loading:false,
        available: !!data.available,
        error: data.error || "",
        candidates: Array.isArray(data.candidates) ? data.candidates : []
      };
      // keep the badge in sync with what the sheet just learned
      lastDiscover = { available: discoverState.available, candidates: discoverState.candidates };
      updateAddBadge();
    } catch(e){
      discoverState = { ran:true, loading:false, available:true, error:"couldn't reach hush-control — is it running?", candidates:[] };
    }
    renderAddSheet();
  }

  // pollDiscover refreshes the passive badge: how many agents are live on the
  // tailnet but not yet in the fleet. It reads the control plane's cached scan,
  // so it's cheap to call on an interval.
  async function pollDiscover(){
    try {
      const r = await fetch("api/discover");
      const data = await r.json();
      lastDiscover = {
        available: !!data.available,
        candidates: Array.isArray(data.candidates) ? data.candidates : []
      };
      updateAddBadge();
    } catch(e){ /* leave the last known badge in place */ }
  }

  function updateAddBadge(){
    const n = (lastDiscover && lastDiscover.available) ? lastDiscover.candidates.length : 0;
    // The dot rides on the Build (add-machine) dock button.
    const badge = $("#addBadge");
    if(badge){
      if(n > 0){ badge.textContent = n; badge.style.display = ""; }
      else { badge.style.display = "none"; }
    }
    const btn = $("#buildBtn");
    if(btn){
      if(n > 0) btn.title = n + " agent" + (n===1?"":"s") + " found on your tailnet, not yet added";
      else btn.removeAttribute("title");
    }
  }

  addScrim.addEventListener("click", e=>{ if(e.target===addScrim) closeAddSheet(); });

  /* ---------- fleet report download ---------- */
  async function downloadReport(){
    const btn = $("#reportBtn");
    btn.disabled = true;
    try {
      const r = await fetch("api/report", {cache:"no-store"});
      if(!r.ok) throw new Error("bad status");
      const blob = await r.blob();
      const cd = r.headers.get("Content-Disposition") || "";
      const m = /filename="?([^"]+)"?/.exec(cd);
      const name = m ? m[1] : "hush-fleet.json";
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url; a.download = name;
      document.body.appendChild(a); a.click();
      a.remove(); URL.revokeObjectURL(url);
      toast("⬇ fleet report downloaded");
    } catch(e){
      toast("couldn't generate the report — is hush-control running?");
    } finally {
      btn.disabled = false;
    }
  }
  $("#reportBtn").addEventListener("click", downloadReport);

  /* ---------- toast ---------- */
  let toastT;
  function toast(msg){
    const t = $("#toast"); t.textContent = msg; t.classList.add("show");
    clearTimeout(toastT); toastT = setTimeout(()=>t.classList.remove("show"), 2600);
  }

  /* ---------- version / update indicator ---------- */
  const updateScrim = $("#updateScrim");
  const updateBody = $("#updateSheetBody");
  let lastVersion = null;

  async function checkVersion(force){
    const chip = $("#verChip");
    try {
      const r = await fetch(force ? "api/version?force=1" : "api/version", {cache:"no-store"});
      if(!r.ok) throw new Error("bad status");
      const v = await r.json();
      if(!v.current) throw new Error("no version");
      lastVersion = v;
      if(v.updateAvailable && v.latest){
        chip.className = "chip ver avail";
        chip.innerHTML = `<span class="up"></span>${v.current} → ${v.latest}`;
        chip.title = `A newer release (${v.latest}) is available. hush-control auto-updates on its timer; click for a command to update now.`;
      } else {
        chip.className = "chip ver";
        chip.innerHTML = v.current;
        chip.title = v.error ? `Update check failed: ${v.error}` : "Running the latest release. Click to check for updates now.";
      }
      chip.hidden = false;
      return v;
    } catch(e){
      lastVersion = null;
      if(BUILD_SHA){
        // No hush-control here to ask (static demo build) — show the commit
        // this bundle was built from instead of just hiding the chip.
        chip.className = "chip ver build-sha";
        chip.innerHTML = BUILD_SHA;
        chip.title = "No hush-control backend here — static demo build, shown at the commit it was built from.";
        chip.hidden = false;
      } else {
        chip.hidden = true; // no server-side version (demo / file://) — say nothing
      }
      return null;
    }
  }

  $("#verChip").addEventListener("click", ()=>{
    if($("#verChip").classList.contains("build-sha")) return;
    if(lastVersion && lastVersion.updateAvailable && lastVersion.latest){
      openUpdateSheet();
    } else {
      openConfirmCheckSheet();
    }
  });

  function openConfirmCheckSheet(){
    updateBody.innerHTML = `
      <h3>Check for updates now?</h3>
      <p class="sd">This asks GitHub for the latest release immediately instead of waiting for the next scheduled check. GitHub limits unauthenticated API requests per hour, so avoid mashing this button.</p>
      <div class="sheet-actions">
        <button class="btn ghost" id="confCancel" style="flex:1">Cancel</button>
        <button class="btn primary" id="confScan" style="flex:1">Check now</button>
      </div>`;
    $("#confCancel").addEventListener("click", closeUpdateSheet);
    $("#confScan").addEventListener("click", async ()=>{
      const btn = $("#confScan");
      btn.disabled = true;
      btn.textContent = "Checking…";
      const v = await checkVersion(true);
      if(v && v.updateAvailable && v.latest){
        openUpdateSheet();
      } else {
        closeUpdateSheet();
        if(v) toast(v.error ? `update check failed: ${v.error}` : `up to date (${v.current})`);
      }
    });
    updateScrim.classList.add("open");
  }

  function openUpdateSheet(){
    const v = lastVersion;
    const cmd = "sudo systemctl start hush-control-update.service";
    updateBody.innerHTML = `
      <h3>Update available: ${esc(v.latest)}</h3>
      <p class="sd">running ${esc(v.current)} — hush-control auto-updates within its next daily window, or run this now over SSH:</p>
      <div class="cmdbox"><code>${esc(cmd)}</code><button class="btn" id="updCopy">Copy</button></div>
      <div class="field-note">Verifies the release's SHA-256 before swapping the binary, then restarts the service. Safe to run any time.</div>
      <div class="sheet-actions">
        <a class="btn ghost" href="https://github.com/clarkbar-sys/hush/releases/tag/${encodeURIComponent(v.latest)}" target="_blank" rel="noopener" style="text-decoration:none;text-align:center">Release notes</a>
        <button class="btn primary" id="updClose" style="flex:1">Done</button>
      </div>`;
    $("#updCopy").addEventListener("click", async ()=>{
      try { await navigator.clipboard.writeText(cmd); toast("copied to clipboard"); }
      catch(e){ toast("couldn't copy — select and copy manually"); }
    });
    $("#updClose").addEventListener("click", closeUpdateSheet);
    updateScrim.classList.add("open");
  }
  function closeUpdateSheet(){ updateScrim.classList.remove("open"); }
  updateScrim.addEventListener("click", e=>{ if(e.target===updateScrim) closeUpdateSheet(); });

  /* ---------- per-machine agent update popup ---------- */
  function openAgentUpdateSheet(m){
    const isMac = (m.os||"").toLowerCase()==="darwin";
    const cmd = isMac
      ? "go install github.com/clarkbar-sys/hush/cmd/hush-agent@latest"
      : "curl -fsSL https://raw.githubusercontent.com/clarkbar-sys/hush/main/install.sh | sudo sh\nsudo systemctl restart hush-agent";
    const note = isMac
      ? "macOS has no systemd unit to install — this rebuilds the binary with the Go toolchain. Restart the running hush-agent process yourself afterward."
      : "Re-runs the one-line installer to fetch the new binary, then restarts the service so it picks it up.";
    updateBody.innerHTML = `
      <h3>Update available on ${esc(m.id)}: ${esc(m.latestVersion)}</h3>
      <p class="sd">running ${esc(m.agentVersion)} — run this on ${esc(m.id)} over SSH:</p>
      <div class="cmdbox"><code>${esc(cmd)}</code><button class="btn" id="agentUpdCopy">Copy</button></div>
      <div class="field-note">${note}</div>
      <div class="sheet-actions">
        <a class="btn ghost" href="https://github.com/clarkbar-sys/hush/releases/tag/${encodeURIComponent(m.latestVersion)}" target="_blank" rel="noopener" style="text-decoration:none;text-align:center">Release notes</a>
        <button class="btn primary" id="agentUpdClose" style="flex:1">Done</button>
      </div>`;
    $("#agentUpdCopy").addEventListener("click", async ()=>{
      try { await navigator.clipboard.writeText(cmd); toast("copied to clipboard"); }
      catch(e){ toast("couldn't copy — select and copy manually"); }
    });
    $("#agentUpdClose").addEventListener("click", closeUpdateSheet);
    updateScrim.classList.add("open");
  }

  /* ---------- opencode.json export sheet ----------
     Opened from the machine view's Inference section. Shows the generated
     config (see ocConfig) with copy + download, plus the one input the runtime
     can't provide — its configured context limit. Editing the field repaints
     only the <pre>, so the input keeps focus as you type. */
  const ocScrim = $("#ocScrim");
  const ocBody = $("#ocSheetBody");
  let ocMid = null, ocCtx = "";
  function openOcSheet(id){ ocMid = id; ocCtx = ""; renderOcSheet(); ocScrim.classList.add("open"); }
  function closeOcSheet(){ ocScrim.classList.remove("open"); }
  ocScrim.addEventListener("click", e=>{ if(e.target===ocScrim) closeOcSheet(); });
  function ocText(){
    const m = byId(ocMid);
    if(!m) return "";
    const n = parseInt(ocCtx,10);
    return JSON.stringify(ocConfig(m, n>0?n:0), null, 2);
  }
  function renderOcSheet(){
    const m = byId(ocMid);
    if(!m || !ocReachable(m).length){ closeOcSheet(); return; }
    ocBody.innerHTML = `
      <h3>opencode.json <span class="sd" style="font-family:var(--mono)">${esc(m.id)}</span></h3>
      <p class="sd">save to ~/.config/opencode/opencode.json, or .opencode/ inside a project</p>
      <div class="field">
        <label for="ocCtx">context limit <span class="opt">— tokens, optional</span></label>
        <input id="ocCtx" type="number" min="0" inputmode="numeric" value="${esc(ocCtx)}"
          placeholder="blank — let opencode decide">
        <div class="hint">the runtime doesn't report its configured context — set it to the box's -c / num_ctx to avoid silent truncation</div>
      </div>
      <pre class="oc-json"><code id="ocCode">${esc(ocText())}</code></pre>
      <div class="sheet-actions">
        <button class="btn" id="ocCopy">⧉ Copy</button>
        <button class="btn" id="ocDl">⬇ Download</button>
        <button class="btn primary" id="ocClose">Close</button>
      </div>`;
    const ci = $("#ocCtx"), code = $("#ocCode");
    ci.addEventListener("input", ()=>{ ocCtx = ci.value; code.textContent = ocText(); });
    $("#ocClose").addEventListener("click", closeOcSheet);
    $("#ocCopy").addEventListener("click", async ()=>{
      try { await navigator.clipboard.writeText(ocText()); toast("⧉ opencode.json copied"); }
      catch(e){ toast("couldn't copy — select and copy manually"); }
    });
    $("#ocDl").addEventListener("click", ()=>{
      const blob = new Blob([ocText()], {type:"application/json"});
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url; a.download = "opencode.json";
      document.body.appendChild(a); a.click();
      a.remove(); URL.revokeObjectURL(url);
      toast("⬇ opencode.json downloaded");
    });
  }

  /* ---------- go ---------- */
  runBoot(BOOT_LINES, 140);
  // First launch: reveal the home screen behind the fading boot, then let the
  // user tap into the console. Reboots (below) hide straight through to hush.
  Promise.all([poll(), delay(900)]).then(() => { showHome(); hideBoot(); });
  setInterval(poll, 2500);

  // Resuming from the background: a short gap just needs a quiet refresh, but
  // Android can freeze/discard the tab after a longer one, so past a
  // threshold we replay the boot beat while fresh data loads underneath it.
  document.addEventListener("visibilitychange", () => {
    if(document.visibilityState === "hidden"){
      hiddenAt = Date.now();
      return;
    }
    const awayMs = hiddenAt ? Date.now() - hiddenAt : 0;
    if(awayMs > 15000){
      runBoot(REBOOT_LINES, 90);
      Promise.all([poll(), delay(500)]).then(hideBoot);
      pollDiscover();
      pollBackupStatus();
    } else {
      poll();
    }
  });

  checkVersion();
  setInterval(checkVersion, 15*60*1000);
  pollDiscover();
  setInterval(pollDiscover, 20000);
  pollBackupStatus();
  setInterval(pollBackupStatus, 60000);
  // Refresh the open machine's session list on its own slow cadence — sessions
  // change far less often than vitals, and only the machine on screen is polled.
  setInterval(()=>{ const id=state.mid; if(state.view==="machine" && byId(id) && (byId(id).online||MODE==="demo")) { fetchSessions(id); fetchUsers(id); } }, SESSIONS_POLL_MS);

  /* register the service worker so the console is installable and opens
     offline. Only works in a secure context (tsnet HTTPS or localhost);
     silently skipped over plain-HTTP LAN, where the console still runs fine. */
  if ('serviceWorker' in navigator && window.isSecureContext) {
    window.addEventListener('load', () => {
      navigator.serviceWorker.register('/sw.js').catch((err) => {
        console.warn('service worker registration failed:', err);
      });
    });
  }
