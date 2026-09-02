import './style.css';
import { GetSettings, SaveSettings, CaptureBinding, ModelState, DownloadQwen, DownloadParakeet, ParakeetSupported, InputDevices, GetAutostart, SetAutostart, GetVersion, DefaultModels, Recommendations } from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

document.querySelector('#app').innerHTML = `
  <header>
    <div>
      <div class="wordmark">0<span>type</span></div>
      <div class="tagline">no typing allowed</div>
    </div>
    <button id="nav" class="ghost small">Settings</button>
  </header>
  <main>
  <div class="view" id="view-main">
    <label class="field">
      <span>Trigger</span>
      <div class="trigger-row">
        <code id="trigger">Mouse Back</code>
        <button id="rebind" class="ghost">Rebind</button>
      </div>
    </label>

    <label class="field">
      <span>Transcription</span>
      <select id="transcriber">
        <option value="local">Local — Parakeet</option>
        <option value="openrouter">Cloud — OpenRouter</option>
      </select>
    </label>
    <div class="field sub" id="transcriber-fields">
      <input type="text" id="transcription-model" placeholder="Model" autocomplete="off" spellcheck="false" />
      <select id="transcription-pick" class="pick"><option value="">Recommended…</option></select>
      <div class="hint">Any speech-to-text model on OpenRouter. Audio leaves your machine.</div>
    </div>

    <label class="field">
      <span>Cleanup</span>
      <select id="cleaner">
        <option value="local">Local — Qwen3-4B</option>
        <option value="openrouter">Cloud — OpenRouter</option>
      </select>
    </label>
    <div class="field sub" id="cleaner-fields">
      <input type="text" id="cleanup-model" placeholder="Model" autocomplete="off" spellcheck="false" />
      <select id="cleanup-pick" class="pick"><option value="">Recommended…</option></select>
      <div class="hint">Any chat model on OpenRouter. Bigger models catch more misheard words. Transcripts leave your machine.</div>
    </div>
    <div class="field sub keyed" id="rec-source">
      <div class="hint"><span id="rec-status">Loading recommendations…</span> <a href="#" id="rec-refresh">Refresh</a></div>
    </div>

    <div class="field sub keyed" id="openrouter-fields">
      <span>OpenRouter API key</span>
      <input type="password" id="openrouter-key" placeholder="sk-or-…" autocomplete="off" spellcheck="false" />
      <div class="hint">One key for both cloud options. Create one at openrouter.ai/keys.</div>
    </div>

    <label class="field">
      <span>About you <em>— for the cleanup model</em></span>
      <textarea id="notes" rows="3" maxlength="1500" placeholder="Names and words to spell right (people, products, tools), and preferences (keep code words in English, no bullet lists)…"></textarea>
    </label>
  </div>

  <div class="view off" id="view-settings">
    <label class="field">
      <span>Mode</span>
      <select id="mode">
        <option value="hold">Hold to talk</option>
        <option value="toggle">Tap to toggle</option>
      </select>
    </label>
    <label class="field">
      <span>Output</span>
      <select id="output">
        <option value="live">Paste as I speak (at each pause)</option>
        <option value="end">Paste when I stop</option>
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
      <span>Local models <em>— downloaded ones show up as Local on the main screen</em></span>
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

    <div class="field">
      <span>My transcription models <em>— listed first under Recommended</em></span>
      <ul class="mine" id="mine-transcription"></ul>
      <div class="add-row">
        <input type="text" id="add-transcription" placeholder="author/model" autocomplete="off" spellcheck="false" />
        <button class="ghost" id="add-transcription-btn">Add</button>
      </div>
    </div>
    <div class="field">
      <span>My cleanup models <em>— listed first under Recommended</em></span>
      <ul class="mine" id="mine-cleanup"></ul>
      <div class="add-row">
        <input type="text" id="add-cleanup" placeholder="author/model" autocomplete="off" spellcheck="false" />
        <button class="ghost" id="add-cleanup-btn">Add</button>
      </div>
    </div>
    <div class="hint">Slugs are the "author/model" names from openrouter.ai/models.</div>
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
const mine = { transcription: [], cleanup: [] }; // the user's own model slugs
let recs = { transcription: [], cleanup: [] };   // last recommendations from OpenRouter

// Two views in one window: the main screen (what you touch while dictating)
// and Settings for the rest. The header button flips between them; Save
// applies to both.
function showSettings(on) {
  $('view-main').classList.toggle('off', on);
  $('view-settings').classList.toggle('off', !on);
  $('nav').textContent = on ? '← Back' : 'Settings';
}
$('nav').addEventListener('click', () => showSettings($('view-settings').classList.contains('off')));

function human(n) {
  if (n >= 1e9) return (n / 1e9).toFixed(1) + ' GB';
  if (n >= 1e6) return (n / 1e6).toFixed(0) + ' MB';
  return (n / 1e3).toFixed(0) + ' KB';
}

// A "Local" option is offered only once its model is downloaded; until then
// the stage can only be cloud.
function setLocalOption(selectId, available) {
  const sel = $(selectId);
  const opt = sel.querySelector('option[value="local"]');
  opt.hidden = !available; opt.disabled = !available;
  if (!available && sel.value === 'local') sel.value = 'openrouter';
  syncCloudFields();
}

function setQwen(state) {
  const badge = $('qwen-status');
  if (state === 'installed' || state === 'ready') {
    badge.textContent = state; badge.className = 'badge installed';
    $('qwen-dl').textContent = 'Re-download'; $('qwen-dl').disabled = false;
    $('qwen-bar').classList.remove('active');
  } else { badge.textContent = 'not installed'; badge.className = 'badge'; }
  setLocalOption('cleaner', state === 'installed' || state === 'ready');
}

function setParakeet(supported, installed) {
  const badge = $('parakeet-status'); const btn = $('parakeet-dl');
  if (!supported) { badge.textContent = 'not in this build'; badge.className = 'badge soon'; btn.disabled = true; }
  else {
    btn.disabled = false;
    if (installed) { badge.textContent = 'installed'; badge.className = 'badge installed'; btn.textContent = 'Re-download'; }
    else { badge.textContent = 'not installed'; badge.className = 'badge'; }
  }
  setLocalOption('transcriber', supported && installed);
}

// Model fields show under the stage set to cloud; the key field shows when
// either stage is.
function syncCloudFields() {
  const stt = $('transcriber').value === 'openrouter';
  const llm = $('cleaner').value === 'openrouter';
  $('transcriber-fields').classList.toggle('open', stt);
  $('cleaner-fields').classList.toggle('open', llm);
  $('openrouter-fields').classList.toggle('open', stt || llm);
  $('rec-source').classList.toggle('open', stt || llm);
}
$('transcriber').addEventListener('change', syncCloudFields);
$('cleaner').addEventListener('change', syncCloudFields);

// Recommended-model pickers: the user's own models first, then OpenRouter's
// rankings boiled down to a few picks. Choosing one fills the model input.
function fillPicker(kind) {
  const sel = $(kind + '-pick');
  sel.querySelectorAll('optgroup').forEach((g) => g.remove()); // .length = 1 would leave the headers
  const groups = [];
  if (mine[kind].length) groups.push({ title: 'My models', picks: mine[kind].map((m) => ({ model: m, name: m, detail: 'added by you' })) });
  groups.push(...(recs[kind] || []));
  groups.forEach((g) => {
    const og = document.createElement('optgroup');
    og.label = g.title;
    (g.picks || []).forEach((p) => {
      const opt = document.createElement('option');
      opt.value = p.model;
      opt.textContent = p.name + ' — ' + p.detail;
      og.appendChild(opt);
    });
    sel.appendChild(og);
  });
}
function wirePicker(kind, inputId) {
  $(kind + '-pick').addEventListener('change', (e) => {
    if (e.target.value) $(inputId).value = e.target.value;
    e.target.value = '';
  });
}
wirePicker('transcription', 'transcription-model');
wirePicker('cleanup', 'cleanup-model');

async function loadRecommendations(refresh) {
  const st = $('rec-status');
  st.textContent = refresh ? 'Refreshing…' : 'Loading recommendations…';
  try {
    const r = await Recommendations(refresh);
    recs = { transcription: r.transcription || [], cleanup: r.cleanup || [] };
    fillPicker('transcription'); fillPicker('cleanup');
    st.textContent = (r.source || 'Source: OpenRouter rankings') + (r.stale ? ' (offline copy)' : '');
  } catch (e) { st.textContent = 'Recommendations unavailable: ' + e; }
}
$('rec-refresh').addEventListener('click', (e) => { e.preventDefault(); loadRecommendations(true); });

// The user's own models: a list per stage with add/remove.
function renderMine(kind) {
  const ul = $('mine-' + kind);
  ul.innerHTML = '';
  mine[kind].forEach((m, i) => {
    const li = document.createElement('li');
    const code = document.createElement('code'); code.textContent = m;
    const rm = document.createElement('button'); rm.className = 'ghost small'; rm.textContent = 'Remove';
    rm.addEventListener('click', () => { mine[kind].splice(i, 1); renderMine(kind); });
    li.appendChild(code); li.appendChild(rm);
    ul.appendChild(li);
  });
  fillPicker(kind);
}
function wireAdd(kind) {
  const add = () => {
    const input = $('add-' + kind);
    const slug = input.value.trim();
    if (!slug) return;
    if (!/^[\w.-]+\/[\w.:-]+$/.test(slug)) { flash('Use the author/model form, e.g. openai/whisper-large-v3', false); return; }
    if (!mine[kind].includes(slug)) mine[kind].push(slug);
    input.value = '';
    renderMine(kind);
  };
  $('add-' + kind + '-btn').addEventListener('click', add);
  $('add-' + kind).addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); add(); } });
}
wireAdd('transcription');
wireAdd('cleanup');

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
  $('output').value = s.output === 'end' ? 'end' : 'live';
  $('transcriber').value = s.transcriber === 'openrouter' ? 'openrouter' : 'local';
  $('transcription-model').value = s.transcriptionModel || '';
  $('cleaner').value = s.cleaner === 'openrouter' ? 'openrouter' : 'local';
  $('cleanup-model').value = s.cleanupModel || '';
  $('openrouter-key').value = s.openrouterApiKey || '';
  $('notes').value = s.notes || '';
  mine.transcription = (s.myTranscriptionModels || []).slice();
  mine.cleanup = (s.myCleanupModels || []).slice();
  renderMine('transcription'); renderMine('cleanup');
  try {
    const d = await DefaultModels();
    $('transcription-model').placeholder = d.transcription;
    $('cleanup-model').placeholder = d.cleanup;
  } catch (e) { /* keep "Model" */ }
  const m = await ModelState();
  setQwen(m.qwen ? 'installed' : 'missing');
  setParakeet(await ParakeetSupported(), m.parakeet);
  loadRecommendations(false); // in the background; the pickers fill in when it lands
  await loadMics(s.inputDevice);
  try { $('autostart').checked = await GetAutostart(); } catch (e) { /* non-Windows */ }
  try { $('version').textContent = 'v' + (await GetVersion()); } catch (e) { /* ignore */ }
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
    flash('Parakeet ready ✓ — pick Local on the main screen');
  }
});

EventsOn('model-error', (msg) => flash('Model error: ' + msg, false));
EventsOn('notice', (n) => { if (n && n.msg) flash(n.msg, n.kind !== 'error'); });

$('save').addEventListener('click', async () => {
  const s = {
    trigger: binding,
    mode: $('mode').value,
    inputDevice: $('mic').value,
    output: $('output').value,
    transcriber: $('transcriber').value,
    transcriptionModel: $('transcription-model').value.trim(),
    cleaner: $('cleaner').value,
    cleanupModel: $('cleanup-model').value.trim(),
    openrouterApiKey: $('openrouter-key').value.trim(),
    notes: $('notes').value.trim(),
    myTranscriptionModels: mine.transcription.slice(),
    myCleanupModels: mine.cleanup.slice(),
  };
  try {
    await SaveSettings(s);
    flash('Saved ✓');
  } catch (e) { flash('Error: ' + e, false); }
});

load();
