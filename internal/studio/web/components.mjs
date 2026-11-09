import { html, useEffect, useMemo, useState } from './vendor/htm-preact.mjs';

export { html, useEffect, useMemo, useState };

export const PAGE_SIZE = 150;

export function Field({ label, hint, children }) {
  return html`<label class="field"><span class="field-label">${label}</span>${children}${hint ? html`<span class="hint">${hint}</span>` : null}</label>`;
}

export function TextInput({ value, onInput, ...rest }) {
  return html`<input type="text" value=${value ?? ''} onInput=${(e) => onInput(e.currentTarget.value)} ...${rest} />`;
}

export function Select({ value, onChange, options, ...rest }) {
  return html`<select value=${value ?? ''} onChange=${(e) => onChange(e.currentTarget.value)} ...${rest}>
    ${options.map((o) => (Array.isArray(o) ? html`<option value=${o[0]}>${o[1]}</option>` : html`<option value=${o}>${o}</option>`))}
  </select>`;
}

/** Token input backed by a shared datalist; values must be added explicitly. */
export function Chips({ label, values, onChange, list, placeholder, hint }) {
  const [text, setText] = useState('');
  const items = values ?? [];
  const add = () => {
    const value = text.trim();
    if (value && !items.includes(value)) onChange([...items, value]);
    setText('');
  };
  const onKey = (e) => {
    if (e.key === 'Enter' || e.key === ',') { e.preventDefault(); add(); }
    if (e.key === 'Backspace' && !text && items.length) onChange(items.slice(0, -1));
  };
  return html`<div class="field">
    <span class="field-label">${label}</span>
    <div class="chips">
      ${items.map((value) => html`<span class="chip" key=${value}>${value}<button type="button" aria-label=${`Remove ${value}`} onClick=${() => onChange(items.filter((v) => v !== value))}>×</button></span>`)}
      <input aria-label=${label} list=${list} value=${text} placeholder=${placeholder ?? 'Type and press Enter'} onInput=${(e) => setText(e.currentTarget.value)} onKeyDown=${onKey} onBlur=${add} />
    </div>
    ${hint ? html`<span class="hint">${hint}</span>` : null}
  </div>`;
}

export const Badge = ({ tone, children }) => html`<span class=${`badge ${tone ?? ''}`}>${children}</span>`;

export function OpBadge({ state }) {
  if (!state) return null;
  const tone = { new: 'good', edited: 'accent', deleted: 'poor' }[state];
  return html`<${Badge} tone=${tone}>${state}<//>`;
}

export function List({ items, render, empty }) {
  const [limit, setLimit] = useState(PAGE_SIZE);
  if (!items.length) return html`<p class="muted pad">${empty ?? 'Nothing matches.'}</p>`;
  return html`<div class="rows">${items.slice(0, limit).map(render)}
    ${items.length > limit ? html`<button type="button" class="more" onClick=${() => setLimit(limit + PAGE_SIZE)}>Show more (${items.length - limit} left)</button>` : null}
  </div>`;
}

export function Diagnostics({ diagnostics, subject }) {
  const relevant = diagnostics.filter((d) => d.severity !== 'info' && (!subject || d.subject === subject));
  if (!relevant.length) return null;
  return html`<ul class="diagnostics" role="status">${relevant.map((d) => html`<li class=${d.severity}><b>${d.code}</b> ${subject ? null : html`<code>${d.subject}</code> `}${d.message}</li>`)}</ul>`;
}

/** Stage, revert and delete controls shared by every entity editor. */
export function EditorActions({ dirty, busy, onStage, onReset, onDelete, deleteLabel, deleteHint }) {
  return html`<div class="actions">
    <button type="button" class="primary" disabled=${!dirty || busy} onClick=${onStage}>Stage change</button>
    <button type="button" disabled=${!dirty || busy} onClick=${onReset}>Reset form</button>
    <span class="spacer"></span>
    ${onDelete ? html`<button type="button" class="danger" disabled=${busy} title=${deleteHint ?? ''} onClick=${onDelete}>${deleteLabel ?? 'Delete'}</button>` : null}
  </div>`;
}

/** Two-step confirmation that lists what a destructive edit will touch. */
export function ConfirmReferences({ title, refs, onConfirm, onCancel, confirmLabel }) {
  return html`<div class="confirm" role="alertdialog" aria-label=${title}>
    <h3>${title}</h3>
    ${refs.length ? html`<p>This also updates ${refs.length} referencing ${refs.length === 1 ? 'entity' : 'entities'}:</p>
      <ul class="refs">${refs.slice(0, 40).map((r) => html`<li><code>${r.id}</code> <span class="muted">${r.field}</span></li>`)}${refs.length > 40 ? html`<li class="muted">and ${refs.length - 40} more</li>` : null}</ul>` : html`<p>Nothing else references it.</p>`}
    <div class="actions"><button type="button" class="danger" onClick=${onConfirm}>${confirmLabel ?? 'Confirm'}</button><button type="button" onClick=${onCancel}>Cancel</button></div>
  </div>`;
}
