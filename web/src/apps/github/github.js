  /* github app — opens a springboard "app" listing the kruddage org's
     public repos; each row hands off to that repo on GitHub in a new tab. The
     list is pulled from the public GitHub API the first time the app opens and
     then cached for the session. Works the same on the live console and on the
     static GitHub Pages build (the service worker ignores cross-origin GETs),
     showing only public repos since the request is unauthenticated. */
  const GH_ORG = "kruddage";
  const githubApp = $("#githubApp");
  const githubList = $("#githubList");
  let ghLoaded = false;            // repos fetched successfully at least once
  let ghLoading = false;

  // a handful of brandish dots for the common languages; everything else falls
  // back to a neutral grey so an unknown language still renders cleanly.
  function ghLangColor(l){
    const m = { Go:"#00add8", JavaScript:"#f1e05a", TypeScript:"#3178c6", HTML:"#e34c26",
      CSS:"#563d7c", Shell:"#89e051", Python:"#3572a5", Rust:"#dea584", Just:"#9aa0a6" };
    return m[l] || "rgba(255,255,255,.5)";
  }
  function ghRow(r){
    const desc = r.description ? `<span class="gh-repo-desc">${esc(r.description)}</span>` : "";
    const lang = r.language
      ? `<span class="gh-repo-lang"><i style="color:${ghLangColor(r.language)}"></i>${esc(r.language)}</span>`
      : "";
    return `<a class="gh-repo" href="${esc(r.html_url)}" target="_blank" rel="noopener">
      <span class="gh-repo-icon" aria-hidden="true">${esc((r.name||"?").charAt(0))}</span>
      <span class="gh-repo-body"><span class="gh-repo-name">${esc(r.name)}</span>${desc}</span>
      ${lang}</a>`;
  }
  const ghFallback = `<p class="gh-note">Couldn't reach GitHub.<br>
    <a href="https://github.com/${GH_ORG}" target="_blank" rel="noopener">Open kruddage on GitHub ↗</a></p>`;

  async function loadGithubRepos(){
    if(ghLoaded || ghLoading) return;
    ghLoading = true;
    githubList.innerHTML = `<span class="gh-spin" aria-hidden="true"></span><p class="gh-note">loading repos…</p>`;
    try {
      const res = await fetch(`https://api.github.com/orgs/${GH_ORG}/repos?per_page=100&sort=updated`,
        { headers: { Accept: "application/vnd.github+json" } });
      if(!res.ok) throw new Error("HTTP " + res.status);
      const repos = (await res.json())
        .filter(r => r && !r.archived && r.name !== ".github")
        .sort((a,b) => String(b.pushed_at||"").localeCompare(String(a.pushed_at||"")));
      if(!repos.length){
        githubList.innerHTML = `<p class="gh-note">No public repositories yet.<br>
          <a href="https://github.com/${GH_ORG}" target="_blank" rel="noopener">Open kruddage on GitHub ↗</a></p>`;
      } else {
        githubList.innerHTML = repos.map(ghRow).join("");
      }
      ghLoaded = true;                 // cache the good render for the session
    } catch(_) {
      githubList.innerHTML = ghFallback;   // leave ghLoaded false so reopening retries
    } finally {
      ghLoading = false;
    }
  }

  function openGithub(){
    githubApp.classList.add("on");
    try { $("#githubBack").focus({ preventScroll:true }); } catch(_){}
    loadGithubRepos();
  }
  function closeGithub(){
    githubApp.classList.remove("on");
    try { $("#homeGithub").focus({ preventScroll:true }); } catch(_){}
  }
  $("#homeGithub").addEventListener("click", openGithub);
  $("#githubBack").addEventListener("click", closeGithub);
  githubApp.addEventListener("keydown", e => { if(e.key === "Escape"){ e.stopPropagation(); closeGithub(); } });

