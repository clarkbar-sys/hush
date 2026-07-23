  /* tally tile — a plain hand-off to the tally node on the tailnet, no
     in-app overlay. hush and tally each join the tailnet as their own tsnet
     node ("hush", "tally"), so swapping the leading label of hush's own
     hostname gives tally's MagicDNS name without hardcoding the tailnet.
     Falls back to the bare "tally" node name when hush isn't served from a
     "hush.…" host (local dev, GitHub Pages preview). */
  (function(){
    const host = location.hostname;
    const tallyHost = host.startsWith("hush.") ? "tally." + host.slice("hush.".length) : "tally";
    $("#homeTally").href = `https://${tallyHost}/`;
  })();

