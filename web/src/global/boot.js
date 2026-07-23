  /* ---------- boot overlay ----------
     Shown on first load and again, briefly, whenever the tab regains
     visibility after being backgrounded long enough that Android may have
     throttled or frozen it — masks that stale-frame gap with a deliberate
     "rebooting" beat instead of letting it read as broken. */
  const bootScreen = $("#bootScreen");
  const bootLog = $("#bootLog");
  const bootBarFill = $("#bootBarFill");
  const BOOT_LINES = ["KERNEL ................ OK", "TAILNET UPLINK ......... OK",
    "AUTH HANDSHAKE ......... OK", "FLEET POLL ............. ▮"];
  const REBOOT_LINES = ["REINITIALIZING ......... OK", "FLEET POLL ............. ▮"];
  let bootStepTimer = null;
  let hiddenAt = 0;
  const delay = ms => new Promise(r => setTimeout(r, ms));

  function runBoot(lines, stepMs){
    clearTimeout(bootStepTimer);
    bootScreen.classList.remove("hide");
    bootScreen.setAttribute("aria-hidden", "false");
    bootLog.textContent = "";
    bootBarFill.style.width = "0%";
    const step = i => {
      if(i >= lines.length) return;
      const row = document.createElement("div");
      row.className = "boot-line";
      row.textContent = "> " + lines[i];
      bootLog.appendChild(row);
      bootBarFill.style.width = `${Math.round(((i + 1) / lines.length) * 100)}%`;
      bootStepTimer = setTimeout(() => step(i + 1), stepMs);
    };
    step(0);
  }

  function hideBoot(){
    clearTimeout(bootStepTimer);
    bootScreen.classList.add("hide");
    bootScreen.setAttribute("aria-hidden", "true");
  }

