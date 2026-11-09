import { Badge, Diagnostics, Field, OpBadge, TextInput, html, useState } from './components.mjs';
import { ROLE_LABELS, hrefFor, lineDiff, opFor } from './logic.mjs';

function OpRow({ ctx, op }) {
  const revert = () => ctx.stage([op.base === null ? { role: op.role, id: op.id, delete: true } : { role: op.role, id: op.id, entity: op.base }]);
  return html`<details class="op"><summary><${OpBadge} state=${opFor(ctx.state.ops, op.role, op.id)} /> <code>${op.id}</code></summary>
    <pre class="diff">${lineDiff(op.base, op.entity).filter((line, i, all) => line.type !== 'same' || all[i - 1]?.type !== 'same' || all[i + 1]?.type !== 'same').map((line) => html`<span class=${line.type}>${line.type === 'add' ? '+ ' : line.type === 'del' ? '- ' : '  '}${line.text}\n</span>`)}</pre>
    <div class="actions">${op.entity !== null ? html`<a class="button" href=${hrefFor(op.role, op.id)}>Open</a>` : null}<button type="button" onClick=${revert}>Revert</button></div>
  </details>`;
}

function Conflicts({ ctx, conflicts, onResolved }) {
  const resolve = async (conflict, action) => {
    await ctx.resolveConflict(conflict, action);
    onResolved(conflicts.filter((item) => item !== conflict));
  };
  return html`<div class="card conflicts"><h3>Conflicts with upstream</h3>
    <p class="hint">These entities changed on the remote since you edited them. Keep your version or drop your edit.</p>
    ${conflicts.map((conflict) => html`<div class="row" key=${conflict.id}><span class="row-main"><code>${conflict.id}</code><small>${conflict.reason}</small></span>
      <span class="row-side"><button type="button" onClick=${() => resolve(conflict, 'keep')}>Keep mine</button><button type="button" onClick=${() => resolve(conflict, 'discard')}>Discard</button></span></div>`)}
  </div>`;
}

export function Changes({ ctx }) {
  const { ops, diagnostics, meta, published } = ctx.state;
  const [title, setTitle] = useState('');
  const [body, setBody] = useState('');
  const [conflicts, setConflicts] = useState([]);
  const [message, setMessage] = useState(null);
  const errors = diagnostics.filter((d) => d.severity === 'error').length;
  const roles = [...new Set(ops.map((op) => op.role))].sort();
  const missingChecks = meta.automated && !meta.registryChecks;
  const act = async (path, payload, done) => {
    setMessage(null);
    const result = await ctx.action(path, payload);
    if (result.conflicts) { setConflicts(result.conflicts); return; }
    if (result.error) { setMessage({ tone: 'error', text: result.error }); return; }
    setConflicts([]);
    setMessage({ tone: 'ok', text: done(result) });
  };
  const publish = () => act('/api/publish', { title, body }, (r) => (r.updated ? `Updated ${r.published.url}` : `Opened ${r.published.url}`));
  const blocked = !ops.length || errors > 0 || ctx.busy;
  return html`<section class="page">
    <h1>Changes</h1>
    <p class="muted">${ops.length} staged · ${errors} error${errors === 1 ? '' : 's'} · checkout <code>${meta.git.commit ? meta.git.commit.slice(0, 7) : 'not a Git repository'}</code></p>
    <${Diagnostics} diagnostics=${diagnostics} />
    ${conflicts.length ? html`<${Conflicts} ctx=${ctx} conflicts=${conflicts} onResolved=${setConflicts} />` : null}
    ${roles.map((role) => html`<div class="card" key=${role}><h3>${ROLE_LABELS[role]}</h3>${ops.filter((op) => op.role === role).map((op) => html`<${OpRow} key=${op.id} ctx=${ctx} op=${op} />`)}</div>`)}
    ${!ops.length ? html`<p class="card pad">Nothing staged yet.</p>` : null}
    ${missingChecks ? html`<p class="notice">The registry's own Node checks are off, so CI could still reject something they cover. Start the studio with <code>--registry-checks</code> to run them first.</p>` : null}
    ${published ? html`<p class="notice">Pull request: <a href=${published.url} rel="noreferrer" target="_blank">${published.url}</a> on <code>${published.branch}</code></p>` : null}
    ${message ? html`<p class=${message.tone === 'error' ? 'error' : 'notice'} role="status">${message.text}</p>` : null}
    <div class="card">
      <h3>${published ? 'Update the pull request' : 'Publish'}</h3>
      ${meta.git.publishable ? html`
        <${Field} label="Title"><${TextInput} value=${title} onInput=${setTitle} placeholder="Describe the curation change" /><//>
        ${published ? null : html`<${Field} label="Description"><textarea rows="4" value=${body} onInput=${(e) => setBody(e.currentTarget.value)}></textarea><//>`}
        <div class="actions"><button type="button" class="primary" disabled=${blocked || !title.trim()} onClick=${publish}>${published ? 'Update pull request' : 'Open pull request'}</button></div>
        <p class="hint">Replays your changes on the latest remote default branch in a temporary worktree. Your checkout is not touched.</p>` : html`<p class="hint">Publishing needs a Git checkout with an origin remote.</p>`}
      <div class="actions">
        <button type="button" disabled=${blocked} onClick=${() => act('/api/apply', {}, (r) => `Applied ${r.applied} change(s) to the working tree.`)}>Apply to working tree</button>
        <span class="spacer"></span>
        <button type="button" class="danger" disabled=${!ops.length || ctx.busy} onClick=${() => { if (confirm('Discard every staged change?')) act('/api/draft/clear', {}, () => 'Draft cleared.'); }}>${published ? 'Finish and clear draft' : 'Discard all'}</button>
      </div>
    </div>
  </section>`;
}
