package preview

const indexHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>dbml preview</title>
<style>
  html,body { margin:0; background:#f8fafc; color:#0f172a;
    font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif; }
  #bar { display:none; position:sticky; top:0; padding:10px 14px;
    background:#fee2e2; color:#991b1b; white-space:pre-wrap; font-family:ui-monospace,monospace;
    font-size:13px; box-shadow:0 2px 8px rgba(0,0,0,.15); }
  #view { padding:24px; }
  #view svg { display:block; }
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
      if (svg.trim()) {
        const view = document.getElementById('view');
        view.innerHTML = svg;
        // D2 SVGs carry only a viewBox; give it an explicit pixel size so it
        // renders at natural size and the browser's own zoom/scroll just work.
        const el = view.querySelector('svg');
        const vb = ((el && el.getAttribute('viewBox')) || '').split(/\s+/).map(Number);
        if (el && vb.length === 4) { el.setAttribute('width', vb[2]); el.setAttribute('height', vb[3]); }
      }
      if (status.title) document.title = 'dbml — ' + status.title.split('/').pop();
      const bar = document.getElementById('bar');
      if (status.error && status.error.trim()) { bar.style.display = 'block'; bar.textContent = status.error; }
      else { bar.style.display = 'none'; }
    } catch (e) { /* server restarting; ignore */ }
  }
  const es = new EventSource('/events');
  es.onmessage = refresh;
  refresh();
</script>
</body>
</html>`
