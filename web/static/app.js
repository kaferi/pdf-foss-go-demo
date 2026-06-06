// Plain JS, no framework. Talks to the Go JSON/PNG API.

async function initHome() {
  const form = document.getElementById('upload-form');
  const msg = document.getElementById('upload-msg');
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const input = document.getElementById('file');
    if (!input.files.length) return;
    const fd = new FormData();
    fd.append('file', input.files[0]);
    msg.textContent = 'Uploading…';
    msg.className = 'msg';
    const res = await fetch('/api/upload', { method: 'POST', body: fd });
    if (!res.ok) {
      msg.textContent = 'Upload failed: ' + (await res.text());
      msg.className = 'msg error';
      return;
    }
    msg.textContent = 'Uploaded.';
    input.value = '';
    await loadFiles();
  });
  await loadFiles();
}

async function loadFiles() {
  const list = document.getElementById('file-list');
  const res = await fetch('/api/files');
  const files = await res.json();
  list.innerHTML = '';
  if (!files.length) {
    list.innerHTML = '<li class="empty">No files yet.</li>';
    return;
  }
  for (const f of files) {
    const li = document.createElement('li');
    const pages = f.pages ? ` · ${f.pages} pages` : '';
    li.innerHTML =
      `<a href="/view/${f.id}">${escapeHtml(f.originalName)}</a>` +
      `<span class="badge ${f.status}">${f.status}</span>` +
      `<span class="meta">${(f.size/1024).toFixed(0)} KB${pages}</span>` +
      `<button data-id="${f.id}" class="del">Delete</button>`;
    li.querySelector('.del').addEventListener('click', async () => {
      await fetch('/api/files/' + f.id, { method: 'DELETE' });
      await loadFiles();
    });
    list.appendChild(li);
  }
}

function fileIdFromPath() {
  const m = location.pathname.match(/\/view\/([^/]+)/);
  return m ? m[1] : '';
}

async function initViewer() {
  const id = fileIdFromPath();
  document.getElementById('download').href = `/files/${id}/original.pdf`;
  const statusEl = document.getElementById('status');

  let meta = await (await fetch('/api/files/' + id)).json().catch(() => null);
  if (!meta || !meta.id) { statusEl.textContent = 'File not found.'; return; }
  document.getElementById('title').textContent = meta.originalName;

  if (meta.status === 'uploaded') {
    statusEl.textContent = 'Starting render…';
    fetch('/api/files/' + id + '/render', { method: 'POST' }); // fire and poll
    meta = await pollUntilDone(id, statusEl);
  } else if (meta.status === 'rendering') {
    meta = await pollUntilDone(id, statusEl);
  }

  if (meta.status === 'error') { showError(statusEl, meta); return; }
  if (meta.status === 'ready') { statusEl.textContent = ''; renderPages(id, meta.pages); }
}

async function pollUntilDone(id, statusEl) {
  for (;;) {
    await new Promise(r => setTimeout(r, 700));
    const m = await (await fetch('/api/files/' + id)).json();
    if (m.status === 'rendering' || m.status === 'uploaded') {
      statusEl.textContent = m.pages
        ? `Rendering… (${m.pages} pages)` : 'Rendering…';
      continue;
    }
    return m; // ready or error
  }
}

function renderPages(id, pages) {
  const wrap = document.getElementById('pages');
  for (let n = 1; n <= pages; n++) {
    const img = document.createElement('img');
    img.loading = 'lazy';
    img.className = 'page';
    img.alt = 'Page ' + n;
    img.src = `/files/${id}/pages/${n}`;
    wrap.appendChild(img);
  }
}

function showError(statusEl, meta) {
  const e = meta.error || {};
  statusEl.className = 'status error';
  statusEl.innerHTML =
    `<h2>Render failed</h2>` +
    `<p><b>Stage:</b> ${escapeHtml(e.stage || '?')}` +
    (e.page ? ` · <b>Page:</b> ${e.page}` : '') + `</p>` +
    `<pre>${escapeHtml(e.message || '(no message)')}</pre>` +
    `<p><a href="/files/${meta.id}/original.pdf">Download original PDF</a> to reproduce.</p>`;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, c =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
