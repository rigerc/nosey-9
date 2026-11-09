import { Badge, html, useEffect, useMemo, useState } from './components.mjs';
import { render } from './vendor/htm-preact.mjs';
import { buildModel } from './logic.mjs';
import { Health } from './health.mjs';
import { Technologies } from './technologies.mjs';
import { Mappings } from './mappings.mjs';
import { Skills } from './skills.mjs';
import { Changes } from './changes.mjs';

const TOKEN_KEY = 'nosey-studio-token';

function readStored() {
  try { return sessionStorage.getItem(TOKEN_KEY); } catch { return null; }
}

/** Moves the session token out of the URL so it is not left in history. */
function takeToken() {
  const match = location.hash.match(/^#token=([0-9a-f]+)$/);
  if (!match) return readStored();
  try { sessionStorage.setItem(TOKEN_KEY, match[1]); } catch { /* the in-memory value still works */ }
  history.replaceState(null, '', `${location.pathname}#/health`);
  return match[1];
}

const token = takeToken();

class RequestFailure extends Error {
  constructor(message, status, data) { super(message); this.status = status; this.data = data; }
}

async function api(path, body) {
  const response = await fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    headers: { 'X-Nosey-Studio-Token': token ?? '', 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new RequestFailure(data.error ?? `Request failed (${response.status})`, response.status, data);
  return data;
}

function useRoute() {
  const [hash, setHash] = useState(location.hash || '#/health');
  useEffect(() => {
    const listener = () => setHash(location.hash || '#/health');
    addEventListener('hashchange', listener);
    return () => removeEventListener('hashchange', listener);
  }, []);
  return hash.replace(/^#\//, '').split('/').map(decodeURIComponent);
}

const TABS = [['health', 'Health'], ['mappings', 'Mappings'], ['technologies', 'Technologies'], ['skills', 'Skills'], ['changes', 'Changes']];

function App() {
  const [state, setState] = useState(null);
  const [fatal, setFatal] = useState(token ? '' : 'Open the link printed by nosey registry studio.');
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState(null);
  const route = useRoute();
  const load = async () => {
    try { setState(await api('/api/state')); } catch (failure) { setFatal(failure.message); }
  };
  useEffect(() => { if (token) load(); }, []);
  useEffect(() => {
    if (!toast) return undefined;
    const timer = setTimeout(() => setToast(null), 6000);
    return () => clearTimeout(timer);
  }, [toast]);
  const model = useMemo(() => (state ? buildModel(state.documents, state.ops) : null), [state]);
  if (fatal) return html`<p class="loading error">${fatal}</p>`;
  if (!state) return html`<p class="loading">Loading registry…</p>`;
  const notify = (text, tone = 'ok') => setToast({ text, tone });
  const merge = (result) => setState((current) => ({ ...current, ops: result.ops, diagnostics: result.diagnostics }));
  const ctx = {
    state, model, busy, api, notify,
    /** Sends entity changes; resolves true when the server accepted them all. */
    stage: async (ops) => {
      setBusy(true);
      try {
        merge(await api('/api/draft', { ops }));
        notify(`Staged ${ops.length} change${ops.length === 1 ? '' : 's'}.`);
        return true;
      } catch (failure) { notify(failure.message, 'error'); return false; } finally { setBusy(false); }
    },
    resolveConflict: async (conflict, action) => {
      try { merge(await api('/api/draft/resolve', { role: conflict.role, id: conflict.id, action })); } catch (failure) { notify(failure.message, 'error'); }
    },
    /** Long-running actions; returns conflicts or an error rather than throwing. */
    action: async (path, payload) => {
      setBusy(true);
      try {
        const result = await api(path, payload);
        await load();
        return result;
      } catch (failure) {
        if (failure.status === 409 && failure.data.conflicts) return { conflicts: failure.data.conflicts };
        return { error: failure.message };
      } finally { setBusy(false); }
    },
  };
  const errors = state.diagnostics.filter((d) => d.severity === 'error').length;
  const [page, first, second] = route;
  return html`<div class="shell">
    <header class="top">
      <a class="brand" href="#/health"><span class="logo"></span><span>${state.meta.name}<small>Registry Studio</small></span></a>
      <nav aria-label="Sections">${TABS.map(([id, label]) => html`<a key=${id} class=${page === id ? 'on' : ''} href=${`#/${id}`}>${label}${id === 'changes' && state.ops.length ? html` <span class="count">${state.ops.length}</span>` : null}</a>`)}</nav>
      ${errors ? html`<a href="#/changes"><${Badge} tone="poor">${errors} error${errors === 1 ? '' : 's'}<//></a>` : null}
    </header>
    <main>
      ${page === 'mappings' ? html`<${Mappings} ctx=${ctx} pivot=${first} id=${first === 'skill' || first === 'tech' || first === 'mapping' ? second : undefined} />` : null}
      ${page === 'technologies' ? html`<${Technologies} ctx=${ctx} id=${first} />` : null}
      ${page === 'skills' ? html`<${Skills} ctx=${ctx} id=${first} />` : null}
      ${page === 'changes' ? html`<${Changes} ctx=${ctx} />` : null}
      ${!['mappings', 'technologies', 'skills', 'changes'].includes(page) ? html`<${Health} state=${state} model=${model} />` : null}
    </main>
    ${toast ? html`<div class=${`toast ${toast.tone}`} role="status">${toast.text}</div>` : null}
    <datalist id="dl-technologies">${[...model.technologies.keys()].map((id) => html`<option key=${id} value=${id} />`)}</datalist>
    <datalist id="dl-skills">${[...model.skills.keys()].map((id) => html`<option key=${id} value=${id} />`)}</datalist>
    <datalist id="dl-repositories">${[...model.repositories.keys()].map((id) => html`<option key=${id} value=${id} />`)}</datalist>
  </div>`;
}

render(html`<${App} />`, document.getElementById('app'));
