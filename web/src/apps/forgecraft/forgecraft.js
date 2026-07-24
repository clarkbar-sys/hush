  /* forgecraft tile — opens an in-app panel (reusing the github app's slide-in
     shell) showing a coming-soon crest. forgecraft is a stub with nothing to
     wire yet, so this is just the open/close plumbing, mirroring plug/riff. */
  const forgeApp = $("#forgeApp");
  function openForge(){
    forgeApp.classList.add("on");
    try { $("#forgeBack").focus({ preventScroll:true }); } catch(_){}
  }
  function closeForge(){
    forgeApp.classList.remove("on");
    try { $("#homeForge").focus({ preventScroll:true }); } catch(_){}
  }
  $("#homeForge").addEventListener("click", openForge);
  $("#forgeBack").addEventListener("click", closeForge);
  forgeApp.addEventListener("keydown", e => { if(e.key === "Escape"){ e.stopPropagation(); closeForge(); } });

