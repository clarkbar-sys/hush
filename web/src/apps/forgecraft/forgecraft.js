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
  let agents = new Map(); // agent_id → {id, name, status, progress, cpu, mem, elapsed}

  function openForge(){
    enterApp();
    forgeApp.classList.add("on");
    try { $("#forgeBack").focus({ preventScroll:true }); } catch(_){}
    poll();
  }
  function closeForge(){
    forgeApp.classList.remove("on");
    exitToHome();
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

  function poll(){
    // In real app: fetch(/api/forgecraft/instances) and /api/forgecraft/instances/<id>/agents
    // For now, demo data with jitter
    if(state.view === "instances") renderInstances();
    else if(state.view === "warroom") renderWarroom();
  }

  function renderInstances(){
    const instanceCards = instances.map(inst => `
      <div class="forge-inst-card" data-id="${inst.id}">
        <div class="inst-name">${inst.name}</div>
        <div class="inst-status ${inst.status}">${inst.status}</div>
        <div class="inst-bar">
          <div class="inst-fill" style="width: ${(inst.workflows / inst.capacity) * 100}%"></div>
        </div>
        <div class="inst-meta">${inst.workflows}/${inst.capacity} agents</div>
      </div>
    `).join("");

    const totalCpu = Array.from(agents.values()).reduce((a, b) => a + b.cpu, 0);
    const totalMem = Array.from(agents.values()).reduce((a, b) => a + b.mem, 0);
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

    // Wire up instance selection
    forgeList.querySelectorAll(".forge-inst-card").forEach(card => {
      card.addEventListener("click", () => {
        const id = card.getAttribute("data-id");
        state.selectedInstance = id;
        state.view = "warroom";
        render();
      });
    });
  }

  function renderWarroom(){
    const inst = instances.find(i => i.id === state.selectedInstance);
    if(!inst) { state.view = "instances"; render(); return; }

    const agentCards = Array.from(agents.values()).map(ag => {
      const statusClass = ag.status === "running" ? "active" : ag.status === "done" ? "victory" : "paused";
      const healthColor = ag.progress < 33 ? "#ff6666" : ag.progress < 66 ? "#ffdd66" : "#66ff66";
      return `
        <div class="forge-unit ${statusClass}">
          <div class="unit-label">${ag.name}</div>
          <div class="unit-health">
            <div class="health-bar">
              <div class="health-fill" style="width: ${ag.progress}%; background: linear-gradient(90deg, #00ff00, #ffff00, #ff6600); box-shadow: 0 0 4px rgba(255, 100, 0, 0.6);"></div>
            </div>
            <span class="health-pct">${ag.progress}%</span>
          </div>
          <div class="unit-stats">
            <span class="stat">CPU: ${ag.cpu}%</span>
            <span class="stat">MEM: ${ag.mem}%</span>
            <span class="stat">${formatElapsed(ag.elapsed)}</span>
          </div>
          <div class="unit-icon">${ag.status === "running" ? "▶" : ag.status === "done" ? "✓" : "⏸"}</div>
        </div>
      `;
    }).join("");

    const totalCpu = Array.from(agents.values()).reduce((a, b) => a + b.cpu, 0);
    const totalMem = Array.from(agents.values()).reduce((a, b) => a + b.mem, 0);

    const html = `
      <div class="forge-view warroom">
        <div class="forge-header">
          <span class="forge-title">${inst.name}</span>
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
          <button class="forge-btn" id="forgePause">⏸ PAUSE ALL</button>
          <button class="forge-btn" id="forgeBack2">← INSTANCES</button>
        </div>
      </div>
    `;
    forgeList.innerHTML = html;

    // Wire up actions
    document.getElementById("forgeSpawn")?.addEventListener("click", () => {
      // TODO: spawn modal
      console.log("Spawn new agent");
    });
    document.getElementById("forgePause")?.addEventListener("click", () => {
      // TODO: pause all agents
      console.log("Pause all agents");
    });
    document.getElementById("forgeBack2")?.addEventListener("click", () => {
      state.view = "instances";
      render();
    });
  }

  function formatElapsed(seconds){
    const m = Math.floor(seconds / 60);
    const s = seconds % 60;
    return `${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
  }

  function render(){
    poll();
  }

  initDemoInstances();
  initDemoAgents();

  $("#homeForge").addEventListener("click", openForge);
  $("#forgeBack").addEventListener("click", closeForge);
  forgeApp.addEventListener("keydown", e => { if(e.key === "Escape"){ e.stopPropagation(); closeForge(); } });

  // Poll every 2.5s like fleet
  setInterval(() => {
    if(forgeApp.classList.contains("on")) poll();
  }, 2500);

