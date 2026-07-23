  /* riff tile — "riff together", a 90s pager that's still warming up. Opens a
     coming-soon panel (reusing the gh-app slide-in shell) whose Try me button
     pages your phone: it asks for notification permission, then fires a local
     notification through the service worker (an installed PWA shows it in the
     system shade, exactly like a real page). This is client-only — no push
     server — so it pages you while the app is open; server-sent push (app
     closed) would need a VAPID backend on control, and the sw.js push handler
     is already stubbed for that day. The green LCD doubles as the status line. */
  const riffApp = $("#riffApp");
  function openRiff(){
    riffApp.classList.add("on");
    try { $("#riffBack").focus({ preventScroll:true }); } catch(_){}
  }
  function closeRiff(){
    riffApp.classList.remove("on");
    try { $("#homeRiff").focus({ preventScroll:true }); } catch(_){}
  }
  $("#homeRiff").addEventListener("click", openRiff);
  $("#riffBack").addEventListener("click", closeRiff);
  riffApp.addEventListener("keydown", e => { if(e.key === "Escape"){ e.stopPropagation(); closeRiff(); } });

  (function(){
    const btn = $("#riffTry"), lcd = $("#riffLcd"), sub = $("#riffSub");
    if(!btn) return;
    const setLcd = (big, small) => { lcd.textContent = big; if(small != null) sub.textContent = small; };

    // Resolve the active service worker registration, but never hang on it: a
    // static preview (or plain-HTTP LAN) has no SW, so race ready against a
    // short timeout and fall back to a page-level Notification.
    async function swReg(){
      if(!("serviceWorker" in navigator)) return null;
      try {
        return await Promise.race([
          navigator.serviceWorker.ready,
          new Promise(res => setTimeout(() => res(null), 1500)),
        ]);
      } catch(_){ return null; }
    }

    // Send the page. Prefer the service worker (required on Android, where the
    // Notification constructor is disallowed); fall back to a page notification
    // on desktop browsers without an active SW.
    async function page(){
      const title = "*RIFF* — page for you";
      const opts = { body:"u up? riff together is almost live — we'll jam soon. 📟",
        icon:"/icon-192.png", badge:"/icon-192.png", tag:"riff-page",
        vibrate:[120,60,120], renotify:true };
      const reg = await swReg();
      if(reg){ try { await reg.showNotification(title, opts); return true; } catch(_){} }
      try { new Notification(title, opts); return true; } catch(_){ return false; }
    }

    btn.addEventListener("click", async () => {
      if(!("Notification" in window)){ setLcd("NO PAGER SIGNAL", "THIS BROWSER CANT PAGE"); return; }
      btn.disabled = true;
      try {
        let perm = Notification.permission;
        if(perm === "default"){
          setLcd("DIALING…", "ALLOW NOTES 2 GET PAGED");
          perm = await Notification.requestPermission();
        }
        if(perm !== "granted"){ setLcd("PAGING BLOCKED", "TURN ON NOTES 4 RIFF"); return; }
        const ok = await page();
        setLcd(ok ? "PAGE SENT" : "PAGE FAILED", ok ? "CHECK UR NOTIF SHADE" : "TRY AGAIN L8R");
      } finally {
        btn.disabled = false;
      }
    });
  })();

