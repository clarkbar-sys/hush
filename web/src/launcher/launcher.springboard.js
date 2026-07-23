  /* ---- launcher layout: a reorderable springboard. Long-press any tile to
     drop the home screen into iPhone-OS jiggle mode, drag a tile to a new slot,
     then tap the wallpaper (or press Escape) to settle. The order is saved to
     IndexedDB immediately — an instant, offline-safe first paint — and mirrored
     up to hush-control (PUT /api/launcher/layout) as the durable copy that
     survives a cache wipe and follows you to another browser. Tile identity is
     the data-tile slug on each .home-app; we move the real button nodes (never
     re-render), so every tile keeps the click handler wired to it above. */
  (function(){
    const grid = $(".home-grid");
    if(!grid) return;
    const screen = grid.closest(".home-screen") || grid;

    // --- IndexedDB: a tiny key/value store for console prefs. Named "hush" so
    //     it reads plainly in devtools; the launcher order lives under one key.
    const DB_NAME = "hush", DB_VERSION = 1, STORE = "prefs", ORDER_KEY = "launcherOrder";
    let dbPromise = null;
    function idb(){
      if(dbPromise) return dbPromise;
      dbPromise = new Promise((resolve, reject) => {
        if(typeof indexedDB === "undefined"){ reject(new Error("no indexedDB")); return; }
        let req;
        try { req = indexedDB.open(DB_NAME, DB_VERSION); }
        catch(e){ reject(e); return; }
        req.onupgradeneeded = () => {
          const db = req.result;
          if(!db.objectStoreNames.contains(STORE)) db.createObjectStore(STORE);
        };
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error);
      });
      return dbPromise;
    }
    function idbGet(key){
      return idb().then(db => new Promise((resolve, reject) => {
        const rq = db.transaction(STORE, "readonly").objectStore(STORE).get(key);
        rq.onsuccess = () => resolve(rq.result);
        rq.onerror = () => reject(rq.error);
      }));
    }
    function idbPut(key, val){
      return idb().then(db => new Promise((resolve, reject) => {
        const tx = db.transaction(STORE, "readwrite");
        tx.objectStore(STORE).put(val, key);
        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error);
      }));
    }

    // --- order helpers: an order is a list of data-tile slugs. We only ever
    //     rearrange the tiles actually present, so a saved order that names a
    //     since-removed tile (or omits a newly shipped one) still lands sanely.
    const tiles = () => Array.from(grid.querySelectorAll(".home-app"));
    const tileId = el => el.getAttribute("data-tile");
    const currentOrder = () => tiles().map(tileId);

    function applyOrder(order){
      if(!Array.isArray(order) || !order.length) return;
      const byId = new Map(tiles().map(el => [tileId(el), el]));
      const seen = new Set();
      // Saved ids first, in saved order…
      order.forEach(id => {
        const el = byId.get(id);
        if(el){ grid.appendChild(el); seen.add(id); }
      });
      // …then any tile the save didn't mention (a new tile), kept at the end.
      tiles().forEach(el => { if(!seen.has(tileId(el))) grid.appendChild(el); });
    }

    // Persist: write IndexedDB immediately (the next paint reads from it), then
    // best-effort PUT to the server. A server hiccup never blocks the local save.
    function persist(order){
      idbPut(ORDER_KEY, order).catch(() => {});
      fetch("/api/launcher/layout", {
        method:"PUT",
        headers:{ "Content-Type":"application/json" },
        body: JSON.stringify({ order }),
      }).catch(() => {});
    }

    // Load: paint from IndexedDB first (instant), then reconcile with the server
    // as the durable cross-device copy — if it differs, the server wins and the
    // local cache is refreshed to match.
    (async function load(){
      let localOrder = null;
      try { localOrder = await idbGet(ORDER_KEY); } catch(_){}
      if(Array.isArray(localOrder) && localOrder.length) applyOrder(localOrder);
      try {
        const res = await fetch("/api/launcher/layout", { headers:{ "Accept":"application/json" } });
        if(!res.ok) return;
        const data = await res.json();
        const server = data && data.order;
        if(Array.isArray(server) && server.length){
          applyOrder(server);
          if(JSON.stringify(server) !== JSON.stringify(localOrder || [])) idbPut(ORDER_KEY, server).catch(() => {});
        }
      } catch(_){ /* offline or a static preview with no backend — local order stands */ }
    })();

    // --- edit mode + drag-to-reorder ----------------------------------------
    // Reorder is snap-to-slot: the held tile hops into whichever slot is nearest
    // the pointer as you move, staying in grid flow. Simpler and far more robust
    // than free-follow translation, and it reads the same on a phone.
    let editing = false, drag = null, pressTimer = 0, press = null;

    function setEditing(on){
      if(editing === on) return;
      editing = on;
      grid.classList.toggle("editing", on);
      screen.classList.toggle("editing", on);
      if(!on) endDrag(false);
    }

    // Swallow a tile's own click while editing (capture beats the per-tile
    // handlers) so a drag or an edit-mode tap never opens an app.
    grid.addEventListener("click", e => {
      if(editing){ e.stopPropagation(); e.preventDefault(); }
    }, true);

    const cancelPress = () => { clearTimeout(pressTimer); pressTimer = 0; press = null; };

    // Long-press to enter edit mode; if the finger is still down, that same
    // press begins dragging the tile it started on.
    grid.addEventListener("pointerdown", e => {
      const tile = e.target.closest(".home-app");
      if(!tile) return;
      if(editing){ startDrag(tile, e); return; }
      press = { x:e.clientX, y:e.clientY };
      clearTimeout(pressTimer);
      pressTimer = setTimeout(() => { pressTimer = 0; press = null; setEditing(true); startDrag(tile, e); }, 450);
    });
    grid.addEventListener("pointermove", e => {
      if(drag){ moveDrag(e); return; }
      // A slip past a few px before the long-press fires is a scroll, not intent.
      if(pressTimer && press && Math.hypot(e.clientX - press.x, e.clientY - press.y) > 10) cancelPress();
    });
    grid.addEventListener("pointerup", () => { cancelPress(); if(drag) endDrag(true); });
    grid.addEventListener("pointercancel", () => { cancelPress(); endDrag(false); });

    // Tap the wallpaper (anywhere on the springboard that isn't a tile) to settle.
    screen.addEventListener("pointerdown", e => {
      if(editing && !e.target.closest(".home-app")) setEditing(false);
    });
    // Escape settles edit mode before any other Escape handler runs.
    document.addEventListener("keydown", e => {
      if(editing && e.key === "Escape"){ e.stopPropagation(); setEditing(false); }
    }, true);

    function startDrag(tile, e){
      if(drag) return;
      try { tile.setPointerCapture(e.pointerId); } catch(_){}
      drag = { tile, id:e.pointerId };
      tile.classList.add("dragging");
    }
    function moveDrag(e){
      if(e.pointerId !== drag.id) return;
      const ref = insertionRef(e.clientX, e.clientY);
      if(ref !== drag.tile && ref !== drag.tile.nextElementSibling){
        if(ref) grid.insertBefore(drag.tile, ref);
        else grid.appendChild(drag.tile);
      }
    }
    function endDrag(commit){
      if(!drag) return;
      const tile = drag.tile;
      try { tile.releasePointerCapture(drag.id); } catch(_){}
      tile.classList.remove("dragging");
      drag = null;
      if(commit) persist(currentOrder());
    }

    // Nearest-center pick: the sibling closest to the pointer decides the slot,
    // and reading order (left-or-above its center) decides before vs after it.
    function insertionRef(x, y){
      let best = null, bestDist = Infinity, before = true;
      for(const el of tiles()){
        if(el === drag.tile) continue;
        const r = el.getBoundingClientRect();
        const cx = r.left + r.width/2, cy = r.top + r.height/2;
        const d = Math.hypot(x - cx, y - cy);
        if(d < bestDist){
          bestDist = d; best = el;
          before = (y < cy - r.height/2) || (Math.abs(y - cy) <= r.height/2 && x < cx);
        }
      }
      if(!best) return null;
      return before ? best : best.nextElementSibling;
    }
  })();

