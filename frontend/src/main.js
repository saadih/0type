import './style.css';
import { GetSettings, SaveSettings } from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

document.querySelector('#app').innerHTML = `
  <header>
    <div class="titlebar">
      <div>
        <div class="wordmark">0<span>type</span></div>
        <div class="tagline">no typing allowed</div>
      </div>
      <div class="rec" id="rec"><span class="dot"></span>Recording</div>
    </div>
  </header>
  <main>
    <label class="field">
      <span>Trigger</span>
      <div class="trigger-row">
        <code id="trigger">MouseBack</code>
        <button id="rebind" class="ghost" disabled>Rebind (soon)</button>
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

async function load() {
  const s = await GetSettings();
  $('trigger').textContent = s.trigger || 'MouseBack';
  $('mode').value = s.mode || 'hold';
  $('language').value = s.language || 'auto';
  $('groq').value = s.groqApiKey || '';
  $('cleanup').value = s.cleanupUrl || '';
}

function flash(msg, ok = true) {
  const el = $('status');
  el.textContent = msg;
  el.style.color = ok ? 'var(--accent)' : '#f87171';
  setTimeout(() => { el.textContent = ''; }, 2200);
}

$('save').addEventListener('click', async () => {
  const s = {
    trigger: $('trigger').textContent,
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

// The engine emits a "recording" event on every press/release; light up the dot.
EventsOn('recording', (recording) => {
  $('rec').classList.toggle('active', !!recording);
});

load();
