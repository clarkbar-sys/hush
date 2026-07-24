  /* forgecraft app: 90s RTS-style workflow agent launcher. Instances like
     Battle.net channels, subagents like units. Poll cycle syncs agent status,
     render always repaints. State persists UX (selected instance, view mode). */
  const forgeApp = $("#forgeApp");
  const forgeList = $("#forgeList");

  let state = {
    view: "instances",    // "instances" or "warroom"
    selectedInstance: null,
    selectedAgent: null
  };

  let instances = [];
  let agents = new Map(); // agent_id → {id, name, status, progress, cpu, mem, elapsed, tasks}

  function esc(s){
    return String(s || "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
  }

  function openForge(){
    forgeApp.classList.add("on");
    try { $("#forgeBack").focus({ preventScroll:true }); } catch(_){}
    poll();
  }
  function closeForge(){
    forgeApp.classList.remove("on");
    try { $("#homeForge").focus({ preventScroll:true }); } catch(_){}
  }

  // Demo data
  function initDemoInstances(){
    instances = [
      {id: "prod", name: "forgejo-prod", status: "online", workflows: 3, capacity: 16},
      {id: "staging", name: "forgejo-staging", status: "online", workflows: 1, capacity: 16},
      {id: "dev", name: "forgejo-dev", status: "idle", workflows: 0, capacity: 8}
    ];
  }

  function initDemoAgents(){
    agents.set("a1", {
      id: "a1",
      name: "scan-bugs",
      status: "running",
      progress: 50,
      cpu: 45,
      mem: 78,
      elapsed: 270,
      tasks: 12
    });
    agents.set("a2", {
      id: "a2",
      name: "indexing",
      status: "running",
      progress: 72,
      cpu: 12,
      mem: 34,
      elapsed: 135,
      tasks: 8
    });
    agents.set("a3", {
      id: "a3",
      name: "review-queue",
      status: "queued",
      progress: 0,
      cpu: 0,
      mem: 0,
      elapsed: 0,
      tasks: 5
    });
  }

  function tickDemoAgents(){
    agents.forEach(ag => {
      if(ag.status === "running"){
        ag.elapsed += 2;
        if(ag.progress < 100){
          ag.progress = Math.min(100, ag.progress + Math.floor(Math.random() * 4) + 1);
        }
        if(ag.progress >= 100){
          ag.status = "done";
          ag.cpu = 0;
          ag.mem = 0;
        } else {
          ag.cpu = Math.max(5, Math.min(95, ag.cpu + Math.floor(Math.random() * 11) - 5));
          ag.mem = Math.max(10, Math.min(90, ag.mem + Math.floor(Math.random() * 5) - 2));
        }
      }
    });
    const activeCount = Array.from(agents.values()).filter(a => a.status === "running" || a.status === "queued").length;
    const selectedInst = instances.find(i => i.id === (state.selectedInstance || "prod"));
    if(selectedInst){
      selectedInst.workflows = activeCount;
    }
  }

  function poll(){
    // In real app: fetch(/api/forgecraft/instances) and /api/forgecraft/instances/<id>/agents
    tickDemoAgents();
    render();
  }

  function renderInstances(){
    const instanceCards = instances.map(inst => `
      <div class="forge-inst-card" data-id="${esc(inst.id)}">
        <div class="inst-name">${esc(inst.name)}</div>
        <div class="inst-status ${esc(inst.status)}">${esc(inst.status)}</div>
        <div class="inst-bar">
          <div class="inst-fill" style="width: ${Math.min(100, Math.round((inst.workflows / inst.capacity) * 100))}%"></div>
        </div>
        <div class="inst-meta">${inst.workflows}/${inst.capacity} agents</div>
      </div>
    `).join("");

    const totalCpu = Array.from(agents.values()).reduce((a, b) => a + (b.status === "running" ? b.cpu : 0), 0);
    const totalMem = Array.from(agents.values()).reduce((a, b) => a + (b.status === "running" ? b.mem : 0), 0);
    const totalAgents = agents.size;

    const html = `
      <div class="forge-view instances">
        <div class="forge-header">
          <span class="forge-title">FORGECRAFT</span>
          <div class="forge-stats">
            <span class="stat">AGENTS: ${totalAgents}</span>
            <span class="stat">CPU: ${Math.round(totalCpu)}%</span>
            <span class="stat">MEM: ${Math.round(totalMem)}%</span>
          </div>
        </div>
        <div class="forge-label">AVAILABLE INSTANCES</div>
        <div class="forge-instances">
          ${instanceCards}
        </div>
        <div class="forge-actions">
          <button class="forge-btn" id="forgeNewInst">+ NEW INSTANCE</button>
          <button class="forge-btn" id="forgeSettings">⚙ SETTINGS</button>
        </div>
      </div>
    `;
    forgeList.innerHTML = html;
  }

  function renderWarroom(){
    const inst = instances.find(i => i.id === state.selectedInstance);
    if(!inst) { state.view = "instances"; render(); return; }

    const agentCards = Array.from(agents.values()).map(ag => {
      const statusClass = ag.status === "running" ? "active" : ag.status === "done" ? "done" : "paused";
      const healthColor = ag.progress < 33 ? "#ff6666" : ag.progress < 66 ? "#ffdd66" : "#66ff66";
      const icon = ag.status === "running" ? "▶" : ag.status === "done" ? "✓" : "⏸";
      return `
        <div class="forge-unit ${statusClass}">
          <div class="unit-label">${esc(ag.name)}</div>
          <div class="unit-health">
            <div class="health-bar">
              <div class="health-fill" style="width: ${ag.progress}%; background: ${healthColor}; box-shadow: 0 0 6px ${healthColor};"></div>
            </div>
            <span class="health-pct" style="color: ${healthColor}; text-shadow: 0 0 2px ${healthColor};">${ag.progress}%</span>
          </div>
          <div class="unit-stats">
            <span class="stat">CPU: ${ag.cpu}%</span>
            <span class="stat">MEM: ${ag.mem}%</span>
            <span class="stat">${formatElapsed(ag.elapsed)}</span>
          </div>
          <div class="unit-icon">${icon}</div>
        </div>
      `;
    }).join("");

    const totalCpu = Array.from(agents.values()).reduce((a, b) => a + (b.status === "running" ? b.cpu : 0), 0);
    const totalMem = Array.from(agents.values()).reduce((a, b) => a + (b.status === "running" ? b.mem : 0), 0);

    const hasRunning = Array.from(agents.values()).some(a => a.status === "running");
    const pauseBtnText = hasRunning ? "⏸ PAUSE ALL" : "▶ RESUME ALL";

    const html = `
      <div class="forge-view warroom">
        <div class="forge-header">
          <span class="forge-title">${esc(inst.name)}</span>
          <div class="forge-stats">
            <span class="stat">CPU: ${Math.round(totalCpu)}%</span>
            <span class="stat">MEM: ${Math.round(totalMem)}%</span>
          </div>
        </div>
        <div class="forge-label">MISSION STATUS</div>
        <div class="forge-units">
          ${agentCards}
        </div>
        <div class="forge-actions">
          <button class="forge-btn" id="forgeSpawn">+ SPAWN AGENT</button>
          <button class="forge-btn" id="forgePause">${pauseBtnText}</button>
          <button class="forge-btn" id="forgeBack2">← INSTANCES</button>
        </div>
      </div>
    `;
    forgeList.innerHTML = html;
  }

  function formatElapsed(seconds){
    const m = Math.floor(seconds / 60);
    const s = seconds % 60;
    return `${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
  }

  function render(){
    if(state.view === "instances") renderInstances();
    else if(state.view === "warroom") renderWarroom();
  }

  const AGENT_NAMES = ["code-audit", "ci-builder", "docker-pack", "sec-scan", "db-migrate", "test-runner"];
  function spawnAgent(){
    const id = "a" + (agents.size + 1);
    const name = AGENT_NAMES[agents.size % AGENT_NAMES.length];
    agents.set(id, {
      id: id,
      name: name,
      status: "running",
      progress: 0,
      cpu: Math.floor(Math.random() * 30) + 20,
      mem: Math.floor(Math.random() * 40) + 30,
      elapsed: 0,
      tasks: Math.floor(Math.random() * 10) + 1
    });
    render();
  }

  function togglePauseAll(){
    const running = Array.from(agents.values()).some(a => a.status === "running");
    agents.forEach(ag => {
      if(running && ag.status === "running"){
        ag.status = "paused";
        ag.lastCpu = ag.cpu;
        ag.lastMem = ag.mem;
        ag.cpu = 0;
        ag.mem = 0;
      } else if(!running && ag.status === "paused"){
        ag.status = "running";
        ag.cpu = ag.lastCpu || 30;
        ag.mem = ag.lastMem || 40;
      }
    });
    render();
  }

  function spawnInstance(){
    const idx = instances.length + 1;
    instances.push({
      id: "node" + idx,
      name: "forgejo-node-" + idx,
      status: "online",
      workflows: 0,
      capacity: 16
    });
    render();
  }

  forgeList.addEventListener("click", e => {
    const card = e.target.closest(".forge-inst-card");
    if(card){
      const id = card.getAttribute("data-id");
      if(id){
        state.selectedInstance = id;
        state.view = "warroom";
        render();
      }
      return;
    }
    const btn = e.target.closest(".forge-btn");
    if(btn){
      if(btn.id === "forgeBack2"){
        state.view = "instances";
        render();
      } else if(btn.id === "forgeSpawn"){
        spawnAgent();
      } else if(btn.id === "forgePause"){
        togglePauseAll();
      } else if(btn.id === "forgeNewInst"){
        spawnInstance();
      }
    }
  });

  initDemoInstances();
  initDemoAgents();

  $("#homeForge").addEventListener("click", openForge);
  $("#forgeBack").addEventListener("click", closeForge);
  forgeApp.addEventListener("keydown", e => { if(e.key === "Escape"){ e.stopPropagation(); closeForge(); } });

  // Poll every 2.5s like fleet
  setInterval(() => {
    if(forgeApp.classList.contains("on")) poll();
  }, 2500);

