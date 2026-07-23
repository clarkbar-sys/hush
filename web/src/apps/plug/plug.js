  /* plug tile — opens an in-app panel (reusing the github app's slide-in shell)
     previewing plug's three PII-domain tiers: rep (public), crib (personal),
     bag (credential-adjacent). plug is still scoping, so the tiers are read-only
     "coming soon" rows rather than live hand-offs — nothing to fetch or wire. */
  const plugApp = $("#plugApp");
  function openPlug(){
    plugApp.classList.add("on");
    try { $("#plugBack").focus({ preventScroll:true }); } catch(_){}
  }
  function closePlug(){
    plugApp.classList.remove("on");
    try { $("#homePlug").focus({ preventScroll:true }); } catch(_){}
  }
  $("#homePlug").addEventListener("click", openPlug);
  $("#plugBack").addEventListener("click", closePlug);
  plugApp.addEventListener("keydown", e => { if(e.key === "Escape"){ e.stopPropagation(); closePlug(); } });

