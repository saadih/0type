import './style.css';
import { GetSettings, SaveSettings, CaptureBinding } from '../wailsjs/go/main/App';

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
      <span>Language</span>
      <select id="language">
        <option value="auto">Auto-detect</option>
        <option value="en">English</option>
        <option value="sv">Svenska</option>
      </select>
    </label>
    <label class="field">
      <span>Groq API key <em>— cloud transcription</em></span>
      <input id="groq" type="password" placeholder="gsk_..." autocomplete="off"/>
    </label>
    <label class="field">
      <span>Cleanup server URL <em>— local LLM</em></span>
      <input id="cleanup" type="text" placeholder="http://127.0.0.1:8719" autocomplete="off"/>
    </label>
  </main>
  <footer>
    <span id="status"></span>
    <button id="save" class="primary">Save</button>
  </footer>
`;

const $ = (id) => document.getElementById(id);

let binding = { kind: 'mouse', code: 4, name: 'Mouse Back' };

async function load() {
  const s = await GetSettings();
  if (s.trigger && s.trigger.kind) binding = s.trigger;
  $('trigger').textContent = binding.name || 'Mouse Back';
  $('mode').value = s.mode || 'hold';
  $('language').value = s.language || 'auto';
  $('groq').value = s.groqApiKey || '';
  $('cleanup').value = s.cleanupUrl || '';
}

function flash(msg, ok = true) {
  const el = $('status');
  el.textContent = msg;
  el.style.color = ok ? 'var(--accent)' : '#f87171';
  setTimeout(() => { el.textContent = ''; }, 2600);
}

$('rebind').addEventListener('click', async () => {
  const rebind = $('rebind');
  const prev = $('trigger').textContent;
  $('trigger').textContent = 'Press any key or button…';
  rebind.disabled = true;
  try {
    // CaptureBinding blocks until you press something, then applies + saves it.
    const b = await CaptureBinding();
    if (b && b.kind) {
      binding = b;
      $('trigger').textContent = b.name;
      flash('Bound to ' + b.name);
    } else {
      $('trigger').textContent = prev;
    }
  } catch (e) {
    $('trigger').textContent = prev;
    flash('Rebind failed: ' + e, false);
  } finally {
    rebind.disabled = false;
  }
});

$('save').addEventListener('click', async () => {
  const s = {
    trigger: binding,
    mode: $('mode').value,
    language: $('language').value,
    groqApiKey: $('groq').value,
    cleanupUrl: $('cleanup').value,
  };
  try {
    await SaveSettings(s);
    flash('Saved ✓ — restart 0type to apply key/URL changes');
  } catch (e) {
    flash('Error: ' + e, false);
  }
});

load();
