package preview

const indexHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>dbml preview</title>
<style>
  html,body { margin:0; height:100%; background:#f8fafc; color:#0f172a;
    font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif; }
  #bar { display:none; position:fixed; top:0; left:0; right:0; padding:10px 14px;
    background:#fee2e2; color:#991b1b; white-space:pre-wrap; font-family:ui-monospace,monospace;
    font-size:13px; z-index:10; box-shadow:0 2px 8px rgba(0,0,0,.15); }
  #view { height:100%; overflow:auto; display:flex; align-items:safe center; justify-content:safe center; padding:24px; box-sizing:border-box; }
  /* readable (natural) size; scroll for large diagrams, safe centering keeps the top reachable */
  #view svg { flex:0 0 auto; }
</style>
</head>
<body>
  <div id="bar"></div>
  <div id="view">loading…</div>
<script>
  async function refresh() {
    try {
      const [svg, status] = await Promise.all([
        fetch('/svg').then(r => r.text()),
        fetch('/status').then(r => r.json()),
      ]);
      if (svg.trim()) document.getElementById('view').innerHTML = svg;
      if (status.title) document.title = 'dbml — ' + status.title.split('/').pop();
      const bar = document.getElementById('bar');
      if (status.error && status.error.trim()) {
        bar.style.display = 'block';
        bar.textContent = status.error;
      } else {
        bar.style.display = 'none';
      }
    } catch (e) { /* server restarting; ignore */ }
  }
  const es = new EventSource('/events');
  es.onmessage = refresh;
  refresh();
</script>
</body>
</html>`
