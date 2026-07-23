  /* ---------- launcher (home screen) ---------- */
  // A GTA-IV phone home screen shown once per page load. The console loads
  // underneath; tapping the lone hush icon zooms into it. Resumes from the
  // background replay the boot beat but skip this — it's a first-launch beat.
  const homeScreen = $("#homeScreen");
  const homeClock = $("#homeClock");
  let homeShown = false;
  let homeClockTimer = null;

  function tickHomeClock(){
    const d = new Date();
    homeClock.textContent =
      String(d.getHours()).padStart(2,"0") + ":" + String(d.getMinutes()).padStart(2,"0");
  }

  function showHome(){
    if(homeShown) return;
    homeShown = true;
    homeScreen.hidden = false;
    tickHomeClock();
    homeClockTimer = setInterval(tickHomeClock, 15000);
    try { $("#homeHush").focus({ preventScroll:true }); } catch(_){}
  }

  function enterApp(){
    clearInterval(homeClockTimer);
    homeScreen.classList.add("launching");
    setTimeout(() => { homeScreen.hidden = true; }, 420);
  }

  function exitToHome(){
    homeScreen.classList.add("launching");
    homeScreen.hidden = false;
    tickHomeClock();
    clearInterval(homeClockTimer);
    homeClockTimer = setInterval(tickHomeClock, 15000);
    void homeScreen.offsetWidth; // force reflow so the zoom-back transition plays
    requestAnimationFrame(() => homeScreen.classList.remove("launching"));
  }

  $("#homeHush").addEventListener("click", enterApp);
  $("#backBtn").addEventListener("click", exitToHome);

