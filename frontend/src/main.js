import './style.css';
import { GetSettings, SaveSettings, CaptureBinding, ModelState, DownloadQwen, DownloadParakeet, ParakeetSupported, InputDevices, GetAutostart, SetAutostart, GetVersion } from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

document.querySelector('#app').innerHTML = `
  <header>
    <div class="wordmark">0<span>type</span></div>
    <div class="tagline">no typing allowed</div>
  </header>
  <main>
    <label class="field">
      <span>Trigger</span>
      <div class="trigger-row">
        <code id="trigger">Mouse Back</code>
        <button id="rebind" class="ghost">Rebind</button>
      </div>
    </label>
    <label class="field">
      <span>Mode</span>
      <select id="mode">
        <option value="hold">Hold to talk</option>
        <option value="toggle">Tap to toggle</option>
      </select>
    </label>
    <label class="field">
      <span>Microphone</span>
      <select id="mic">
        <option value="">System default</option>
      </select>
    </label>
    <label class="field row-field">
      <span>Start with Windows</span>
      <input type="checkbox" id="autostart" />
    </label>
    <div class="field">
      <span>Models <em>— fully local, downloaded on demand</em></span>
      <div class="model-row">
        <div class="model-info"><b>Parakeet v3</b> · transcription <span id="parakeet-status" class="badge">…</span></div>
        <button id="parakeet-dl" class="ghost" disabled>Download</button>
      </div>
      <div class="bar" id="parakeet-bar"><div class="fill" id="parakeet-fill"></div></div>
      <div class="model-row">
        <div class="model-info"><b>Qwen3-4B-Instruct</b> · cleanup <span id="qwen-status" class="badge">not installed</span></div>
        <button id="qwen-dl" class="ghost">Download</button>
      </div>
      <div class="bar" id="qwen-bar"><div class="fill" id="qwen-fill"></div></div>
    </div>
  </main>
  <footer>
    <span id="status"></span>
    <span id="version" class="version"></span>
    <button id="save" class="primary">Save</button>
  </footer>
`;

const $ = (id) => document.getElementById(id);

let binding = { kind: 'mouse', code: 4, name: 'Mouse Back' };

function human(n) {
  if (n >= 1e9) return (n / 1e9).toFixed(1) + ' GB';
  if (n >= 1e6) return (n / 1e6).toFixed(0) + ' MB';
  return (n / 1e3).toFixed(0) + ' KB';
}

function setQwen(state) {
  const badge = $('qwen-status');
  if (state === 'installed' || state === 'ready') {
    badge.textContent = state; badge.className = 'badge installed';
    $('qwen-dl').textContent = 'Re-download'; $('qwen-dl').disabled = false;
    $('qwen-bar').classList.remove('active');
  } else { badge.textContent = 'not installed'; badge.className = 'badge'; }
}

function setParakeet(supported, installed) {
  const badge = $('parakeet-status'); const btn = $('parakeet-dl');
  if (!supported) { badge.textContent = 'not in this build'; badge.className = 'badge soon'; btn.disabled = true; return; }
  btn.disabled = false;
  if (installed) { badge.textContent = 'installed'; badge.className = 'badge installed'; btn.textContent = 'Re-download'; }
  else { badge.textContent = 'not installed'; badge.className = 'badge'; }
}

async function loadMics(selected) {
  try {
    const devices = await InputDevices();
    const sel = $('mic');
    sel.length = 1; // keep "System default"
    (devices || []).forEach((name) => {
      const opt = document.createElement('option');
      opt.value = name; opt.textContent = name;
      sel.appendChild(opt);
    });
    sel.value = selected || '';
  } catch (e) { /* leave the default option */ }
}

async function load() {
  const s = await GetSettings();
  if (s.trigger && s.trigger.kind) binding = s.trigger;
  $('trigger').textContent = binding.name || 'Mouse Back';
  $('mode').value = s.mode || 'hold';
  await loadMics(s.inputDevice);
  try { $('autostart').checked = await GetAutostart(); } catch (e) { /* non-Windows */ }
  try { $('version').textContent = 'v' + (await GetVersion()); } catch (e) { /* ignore */ }
  const m = await ModelState();
  if (m.qwen) setQwen('installed');
  setParakeet(await ParakeetSupported(), m.parakeet);
}

function flash(msg, ok = true) {
  const el = $('status');
  el.textContent = msg;
  el.style.color = ok ? 'var(--accent)' : '#f87171';
  setTimeout(() => { el.textContent = ''; }, 3200);
}

$('rebind').addEventListener('click', async () => {
  const rebind = $('rebind');
  const prev = $('trigger').textContent;
  $('trigger').textContent = 'Press any key or button…';
  rebind.disabled = true;
  try {
    const b = await CaptureBinding();
    if (b && b.kind) { binding = b; $('trigger').textContent = b.name; flash('Bound to ' + b.name); }
    else { $('trigger').textContent = prev; }
  } catch (e) { $('trigger').textContent = prev; flash('Rebind failed: ' + e, false); }
  finally { rebind.disabled = false; }
});

$('autostart').addEventListener('change', async (e) => {
  const on = e.target.checked;
  try { await SetAutostart(on); flash(on ? 'Will start with Windows ✓' : 'Startup disabled'); }
  catch (err) { e.target.checked = !on; flash('Autostart failed: ' + err, false); }
});

function wireDownload(id, fn) {
  $(id + '-dl').addEventListener('click', async () => {
    const btn = $(id + '-dl');
    btn.disabled = true; btn.textContent = 'Downloading…';
    $(id + '-bar').classList.add('active');
    try { await fn(); }
    catch (e) { flash('Download failed: ' + e, false); btn.textContent = 'Download'; btn.disabled = false; $(id + '-bar').classList.remove('active'); }
  });
}
wireDownload('qwen', DownloadQwen);
wireDownload('parakeet', DownloadParakeet);

EventsOn('download-progress', (p) => {
  if (!p) return;
  const fill = $(p.id + '-fill');
  if (!fill) return;
  const pct = p.total > 0 ? Math.min(100, (p.done / p.total) * 100) : 0;
  fill.style.width = pct + '%';
  const btn = $(p.id + '-dl');
  if (btn) btn.textContent = human(p.done) + (p.total > 0 ? ' / ' + human(p.total) : '');
});

EventsOn('model-ready', (id) => {
  if (id === 'qwen') { setQwen('ready'); flash('Local cleanup is ready ✓'); }
  else if (id === 'parakeet') {
    setParakeet(true, true);
    $('parakeet-bar').classList.remove('active');
    flash('Parakeet ready ✓ — start dictating');
  }
});

EventsOn('model-error', (msg) => flash('Model error: ' + msg, false));
EventsOn('notice', (n) => { if (n && n.msg) flash(n.msg, n.kind !== 'error'); });

$('save').addEventListener('click', async () => {
  const s = { trigger: binding, mode: $('mode').value, inputDevice: $('mic').value };
  try {
    await SaveSettings(s);
    flash('Saved ✓');
  } catch (e) { flash('Error: ' + e, false); }
});

load();
