// Plain JS, no framework. Talks to the Go JSON/PNG API.

async function initHome() {
  const form = document.getElementById('upload-form');
  const msg = document.getElementById('upload-msg');
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const input = document.getElementById('file');
    const files = Array.from(input.files);
    if (!files.length) return;

    const failures = [];
    for (let i = 0; i < files.length; i++) {
      msg.className = 'msg';
      msg.textContent = `Uploading ${i + 1} of ${files.length}: ${files[i].name}…`;
      const fd = new FormData();
      fd.append('file', files[i]);
      try {
        const res = await fetch('/api/upload', { method: 'POST', body: fd });
        if (!res.ok) failures.push(`${files[i].name}: ${(await res.text()).trim()}`);
      } catch (err) {
        failures.push(`${files[i].name}: ${err}`);
      }
      await loadFiles(); // refresh list as each file lands
    }

    const ok = files.length - failures.length;
    if (failures.length) {
      msg.className = 'msg error';
      msg.textContent =
        `Uploaded ${ok} of ${files.length}. Failed:\n` + failures.join('\n');
    } else {
      msg.className = 'msg';
      msg.textContent = `Uploaded ${ok} file${ok === 1 ? '' : 's'}.`;
    }
    input.value = '';
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
  if (meta.status === 'ready') { statusEl.textContent = ''; renderPages(id, meta); }
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

function renderPages(id, meta) {
  const wrap = document.getElementById('pages');
  const total = meta.pages || 0;
  // renderedPages was added later; for older "ready" records that predate it,
  // fall back to the total page count.
  const shown = meta.renderedPages || total;
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
