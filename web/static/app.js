// Plain JS, no framework. Single-page master-detail UI over the Go JSON/PNG API.

let currentId = null;     // file currently shown in the detail pane
let pollTimer = null;     // active status poll, cancelled when selection changes

async function initApp() {
  const btn = document.getElementById('upload-btn');
  const input = document.getElementById('file-input');
  btn.addEventListener('click', () => input.click());
  input.addEventListener('change', () => uploadFiles(Array.from(input.files)));

  // Back/forward navigation between / and /view/{id}.
  window.addEventListener('popstate', () => openFromLocation(false));

  await loadFiles();
  openFromLocation(false); // open a deep-linked /view/{id} if present
}

/* ---------------- file list (sidebar) ---------------- */

async function loadFiles() {
  const list = document.getElementById('file-list');
  let files = [];
  try { files = await (await fetch('/api/files')).json(); } catch { /* keep empty */ }

  list.innerHTML = '';
  if (!files.length) {
    list.innerHTML = '<li class="empty">No files yet. Click “+ Upload”.</li>';
    return;
  }
  for (const f of files) {
    const li = document.createElement('li');
    const card = document.createElement('div');
    card.className = 'file-card' + (f.id === currentId ? ' active' : '');
    card.dataset.id = f.id;

    const pages = f.pages ? ` · ${f.pages}p` : '';
    card.innerHTML =
      `<span class="file-name">${escapeHtml(f.originalName)}</span>` +
      `<span class="file-sub">` +
        `<span class="badge ${f.status}">${f.status}</span>` +
        `<span>${(f.size / 1024).toFixed(0)} KB${pages}</span>` +
      `</span>` +
      `<button class="del" title="Delete" aria-label="Delete">×</button>`;

    card.addEventListener('click', (e) => {
      if (e.target.classList.contains('del')) return; // handled below
      selectFile(f.id, true);
    });
    card.querySelector('.del').addEventListener('click', async (e) => {
      e.stopPropagation();
      await fetch('/api/files/' + f.id, { method: 'DELETE' });
      if (f.id === currentId) { currentId = null; showPlaceholder(); navigate('/'); }
      await loadFiles();
    });

    li.appendChild(card);
    list.appendChild(li);
  }
}

function markActive(id) {
  document.querySelectorAll('.file-card').forEach(c =>
    c.classList.toggle('active', c.dataset.id === id));
}

/* ---------------- selection + detail pane ---------------- */

function openFromLocation(push) {
  const m = location.pathname.match(/^\/view\/([^/]+)/);
  if (m) selectFile(m[1], push);
  else { currentId = null; markActive(null); showPlaceholder(); }
}

async function selectFile(id, push) {
  if (pollTimer) { clearTimeout(pollTimer); pollTimer = null; }
  currentId = id;
  markActive(id);
  if (push) navigate('/view/' + id);

  setDownload(id);
  let meta;
  try { meta = await (await fetch('/api/files/' + id)).json(); }
  catch { setTitle('Not found'); showStatus('File not found.'); return; }
  if (!meta || !meta.id) { setTitle('Not found'); showStatus('File not found.'); return; }
  if (id !== currentId) return; // selection changed while awaiting

  setTitle(meta.originalName);

  if (meta.status === 'uploaded') {
    showSpinner('Starting render…');
    fetch('/api/files/' + id + '/render', { method: 'POST' }); // fire, then poll
    pollUntilDone(id);
  } else if (meta.status === 'rendering') {
    showSpinner('Rendering…');
    pollUntilDone(id);
  } else {
    applyTerminal(id, meta);
  }
}

function pollUntilDone(id) {
  pollTimer = setTimeout(async () => {
    if (id !== currentId) return;
    let m;
    try { m = await (await fetch('/api/files/' + id)).json(); } catch { pollUntilDone(id); return; }
    if (id !== currentId) return;
    if (m.status === 'rendering' || m.status === 'uploaded') {
      showSpinner(m.pages ? `Rendering… (${m.pages} pages)` : 'Rendering…');
      pollUntilDone(id);
      return;
    }
    applyTerminal(id, m);
    loadFiles(); // refresh the badge in the list
  }, 700);
}

function applyTerminal(id, meta) {
  if (meta.status === 'error') { showError(meta); return; }
  if (meta.status === 'ready') { renderPages(id, meta); return; }
  showStatus('Unexpected status: ' + meta.status);
}

function renderPages(id, meta) {
  const total = meta.pages || 0;
  // renderedPages was added later; fall back to total for older "ready" records.
  const shown = meta.renderedPages || total;
  const body = document.getElementById('detail-body');
  body.innerHTML = '';
  const wrap = document.createElement('div');
  wrap.className = 'pages';
  for (let n = 1; n <= shown; n++) {
    const img = document.createElement('img');
    img.loading = 'lazy';
    img.className = 'page';
    img.alt = 'Page ' + n;
    img.src = `/files/${id}/pages/${n}`;
    wrap.appendChild(img);
  }
  if (total > shown) {
    const note = document.createElement('p');
    note.className = 'page-note';
    note.textContent = `Showing the first ${shown} of ${total} pages.`;
    wrap.appendChild(note);
  }
  body.appendChild(wrap);
}

function showError(meta) {
  const e = meta.error || {};
  const body = document.getElementById('detail-body');
  body.innerHTML =
    `<div class="error-panel">` +
      `<h2>Render failed</h2>` +
      `<p class="meta-line"><b>Stage:</b> ${escapeHtml(e.stage || '?')}` +
      (e.page ? ` · <b>Page:</b> ${e.page}` : '') + `</p>` +
      `<pre>${escapeHtml(e.message || '(no message)')}</pre>` +
      `<p><a href="/files/${meta.id}/original.pdf" target="_blank" rel="noopener">Open original PDF</a> to reproduce.</p>` +
    `</div>`;
}

/* ---------------- upload ---------------- */

async function uploadFiles(files) {
  if (!files.length) return;
  const msg = document.getElementById('upload-msg');
  const failures = [];
  for (let i = 0; i < files.length; i++) {
    msg.className = 'upload-msg';
    msg.textContent = `Uploading ${i + 1} of ${files.length}: ${files[i].name}…`;
    const fd = new FormData();
    fd.append('file', files[i]);
    try {
      const res = await fetch('/api/upload', { method: 'POST', body: fd });
      if (!res.ok) failures.push(`${files[i].name}: ${(await res.text()).trim()}`);
    } catch (err) {
      failures.push(`${files[i].name}: ${err}`);
    }
    await loadFiles();
  }
  const ok = files.length - failures.length;
  if (failures.length) {
    msg.className = 'upload-msg error';
    msg.textContent = `Uploaded ${ok} of ${files.length}. Failed:\n` + failures.join('\n');
  } else {
    msg.className = 'upload-msg';
    msg.textContent = `Uploaded ${ok} file${ok === 1 ? '' : 's'}.`;
  }
  document.getElementById('file-input').value = '';
}

/* ---------------- small view helpers ---------------- */

function navigate(path) {
  if (location.pathname !== path) history.pushState(null, '', path);
}
function setTitle(t) { document.getElementById('doc-title').textContent = t; }
function setDownload(id) {
  const a = document.getElementById('download');
  a.href = `/files/${id}/original.pdf`;
  a.hidden = false;
}
function showPlaceholder() {
  setTitle('Select a document');
  document.getElementById('download').hidden = true;
  document.getElementById('detail-body').innerHTML =
    `<div class="placeholder"><p>Pick a file on the left, or upload a new PDF to render its pages.</p></div>`;
}
function showStatus(text) {
  document.getElementById('detail-body').innerHTML =
    `<div class="status"><p>${escapeHtml(text)}</p></div>`;
}
function showSpinner(text) {
  document.getElementById('detail-body').innerHTML =
    `<div class="status"><div><div class="spinner"></div><p>${escapeHtml(text)}</p></div></div>`;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, c =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
