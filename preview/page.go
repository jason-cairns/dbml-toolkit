package preview

const indexHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>dbml preview</title>
<style>
  html,body { margin:0; height:100%; background:#0b1020; color:#e2e8f0;
    font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif; }
  #bar { display:none; position:fixed; top:0; left:0; right:0; padding:10px 14px;
    background:#7f1d1d; color:#fff; white-space:pre-wrap; font-family:ui-monospace,monospace;
    font-size:13px; z-index:10; box-shadow:0 2px 8px rgba(0,0,0,.4); }
  #view { height:100%; overflow:auto; display:flex; align-items:center; justify-content:center; }
  #view svg { max-width:98%; height:auto; }
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
