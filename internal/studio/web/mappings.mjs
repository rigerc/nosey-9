import { Badge, Chips, ConfirmReferences, Diagnostics, EditorActions, Field, List, OpBadge, Select, TextInput, html, useEffect, useState } from './components.mjs';
import { PRIORITIES, bulkEdit, cleanRecommendation, clone, hrefFor, mappingsForTechnology, opFor, recommendationsForSkill, same, slug, suggestSkills } from './logic.mjs';

const hrefTech = (id) => `#/mappings/tech/${encodeURIComponent(id)}`;
const hrefSkill = (id) => `#/mappings/skill/${encodeURIComponent(id)}`;

function RecommendationRow({ rec, onChange, onRemove }) {
  const patch = (next) => onChange({ ...rec, ...next });
  const when = rec.when ?? {};
  const setWhen = (key, value) => patch({ when: { ...when, [key]: value } });
  const count = (when.allTechnologies?.length ?? 0) + (when.anyTechnologies?.length ?? 0) + (when.noneTechnologies?.length ?? 0);
  return html`<div class="rec">
    <div class="rec-main">
      <${Field} label="Skill"><input type="text" list="dl-skills" value=${rec.skillId} onInput=${(e) => patch({ skillId: e.currentTarget.value })} /><//>
      <${Field} label="Priority"><${Select} value=${rec.priority ?? 'optional'} options=${PRIORITIES} onChange=${(priority) => patch({ priority })} /><//>
      <${Field} label="Reason"><${TextInput} value=${rec.reason} onInput=${(reason) => patch({ reason })} /><//>
      <button type="button" class="icon danger" aria-label=${`Remove ${rec.skillId}`} onClick=${onRemove}>×</button>
    </div>
    <details class="conditions"><summary>Conditions${count ? ` (${count})` : ''}</summary>
      <${Chips} label="All of these technologies" list="dl-technologies" values=${when.allTechnologies} onChange=${(v) => setWhen('allTechnologies', v)} />
      <${Chips} label="Any of these technologies" list="dl-technologies" values=${when.anyTechnologies} onChange=${(v) => setWhen('anyTechnologies', v)} />
      <${Chips} label="None of these technologies" list="dl-technologies" values=${when.noneTechnologies} onChange=${(v) => setWhen('noneTechnologies', v)} />
      <${Chips} label="Requires skills" list="dl-skills" values=${rec.requires} onChange=${(requires) => patch({ requires })} />
      <${Chips} label="Conflicts with skills" list="dl-skills" values=${rec.conflictsWith} onChange=${(conflictsWith) => patch({ conflictsWith })} />
    </details>
  </div>`;
}

function MappingEditor({ ctx, mapping, isNew }) {
  const [form, setForm] = useState(() => clone(mapping));
  const [confirm, setConfirm] = useState(false);
  useEffect(() => setForm(clone(mapping)), [mapping]);
  const setRec = (index, rec) => setForm({ ...form, recommendations: form.recommendations.map((r, i) => (i === index ? rec : r)) });
  const entity = { ...form, recommendations: form.recommendations.map(cleanRecommendation) };
  const dirty = isNew || !same(entity, mapping);
  const combination = form.id.startsWith('combination:');
  const addRec = () => {
    const template = form.recommendations[0];
    setForm({ ...form, recommendations: [...form.recommendations, { skillId: '', priority: 'optional', ...(combination && template?.when ? { when: clone(template.when) } : {}) }] });
  };
  return html`<article class="card mapping">
    <header class="editor-head"><code>${form.id}</code> ${combination ? html`<${Badge}>combination<//>` : null} <${OpBadge} state=${opFor(ctx.state.ops, 'mappings', mapping.id)} /></header>
    <${Diagnostics} diagnostics=${ctx.state.diagnostics} subject=${form.id} />
    ${form.recommendations.map((rec, i) => html`<${RecommendationRow} key=${i} rec=${rec} onChange=${(next) => setRec(i, next)} onRemove=${() => setForm({ ...form, recommendations: form.recommendations.filter((_, j) => j !== i) })} />`)}
    <div class="actions"><button type="button" onClick=${addRec}>Add recommendation</button></div>
    <${EditorActions} dirty=${dirty} busy=${ctx.busy} onStage=${() => ctx.stage([{ role: 'mappings', id: entity.id, entity }])} onReset=${() => setForm(clone(mapping))} onDelete=${isNew ? null : () => setConfirm(true)} deleteLabel="Delete mapping" />
    ${confirm ? html`<${ConfirmReferences} title=${`Delete ${form.id}?`} refs=${[]} confirmLabel="Stage deletion" onCancel=${() => setConfirm(false)} onConfirm=${() => { setConfirm(false); ctx.stage([{ role: 'mappings', id: form.id, delete: true }]); }} />` : null}
  </article>`;
}

function TechnologyPivot({ ctx, id }) {
  const technology = ctx.model.technologies.get(id);
  const [partner, setPartner] = useState('');
  const [added, setAdded] = useState([]);
  useEffect(() => setAdded([]), [id]);
  if (!technology) return html`<p class="muted pad">Unknown technology.</p>`;
  const primary = `technology:${id}`;
  const mappings = mappingsForTechnology(ctx.model, id).sort((a, b) => Number(b.id === primary) - Number(a.id === primary));
  const pending = added.filter((m) => !ctx.model.mappings.has(m.id));
  const suggestions = suggestSkills(ctx.model, technology);
  const newRec = (skillId = '') => ({ skillId, priority: 'optional' });
  const startPrimary = (skillId) => setAdded([...added.filter((m) => m.id !== primary), { id: primary, technologyId: id, recommendations: [newRec(skillId)] }]);
  const startCombination = () => {
    const other = partner.trim();
    if (!ctx.model.technologies.has(other)) return;
    setAdded([...added, { id: `combination:${slug(id)}-${slug(other)}`, technologyId: id, recommendations: [{ ...newRec(), when: { allTechnologies: [other] } }] }]);
    setPartner('');
  };
  return html`<div class="editor">
    <header class="editor-head"><h2>${technology.name}</h2><a class="mono" href=${hrefFor('technologies', id)}>${id}</a></header>
    ${[...mappings, ...pending].map((m) => html`<${MappingEditor} key=${m.id} ctx=${ctx} mapping=${m} isNew=${!ctx.model.mappings.has(m.id)} />`)}
    ${!mappings.length && !pending.length ? html`<p class="muted">No mapping yet. Detect-only technologies are valid; add one only when a suitable skill exists.</p>` : null}
    <div class="card">
      <div class="actions">
        ${!mappings.some((m) => m.id === primary) && !pending.some((m) => m.id === primary) ? html`<button type="button" onClick=${() => startPrimary('')}>Add mapping</button>` : null}
        <input type="text" aria-label="Combine with technology" list="dl-technologies" placeholder="Combine with technology" value=${partner} onInput=${(e) => setPartner(e.currentTarget.value)} />
        <button type="button" disabled=${!ctx.model.technologies.has(partner.trim())} onClick=${startCombination}>New combination</button>
      </div>
      <p class="hint">Combinations stay separate mappings so their conditions are not merged away.</p>
      ${suggestions.length ? html`<p class="hint">Possible matches by name (weak signal):</p><div class="chips">${suggestions.map((s) => html`<button type="button" class="chip" title=${s.id} onClick=${() => (mappings.length ? null : startPrimary(s.id))} disabled=${mappings.length > 0}>${s.name}</button>`)}</div>` : null}
    </div>
  </div>`;
}

function SkillPivot({ ctx, id }) {
  const skill = ctx.model.skills.get(id);
  const [technology, setTechnology] = useState('');
  const [priority, setPriority] = useState('optional');
  const [reason, setReason] = useState('');
  if (!skill) return html`<p class="muted pad">Unknown skill.</p>`;
  const found = recommendationsForSkill(ctx.model, id);
  const add = async () => {
    const technologyId = technology.trim();
    const mappingId = `technology:${technologyId}`;
    const rec = cleanRecommendation({ skillId: id, priority, reason });
    const existing = ctx.model.mappings.get(mappingId);
    const entity = existing ? { ...clone(existing), recommendations: [...existing.recommendations, rec] } : { id: mappingId, technologyId, recommendations: [rec] };
    if (await ctx.stage([{ role: 'mappings', id: mappingId, entity }])) { setTechnology(''); setReason(''); }
  };
  const duplicate = ctx.model.mappings.get(`technology:${technology.trim()}`)?.recommendations.some((r) => r.skillId === id);
  return html`<div class="editor">
    <header class="editor-head"><h2>${skill.name}</h2><a class="mono" href=${hrefFor('skills', id)}>${id}</a></header>
    <h3 class="section-title">Recommended for (${found.length})</h3>
    <div class="rows card">${found.length ? found.map(({ mapping, rec }) => html`<a class="row" key=${mapping.id} href=${hrefTech(mapping.technologyId)}>
      <span class="row-main">${mapping.technologyId}<small class="mono">${mapping.id}</small></span><span class="row-side"><${Badge}>${rec.priority ?? 'optional'}<//></span></a>`) : html`<p class="muted pad">Not recommended anywhere.</p>`}</div>
    <div class="card"><h3>Recommend for a technology</h3>
      <div class="form-grid">
        <${Field} label="Technology"><input type="text" list="dl-technologies" value=${technology} onInput=${(e) => setTechnology(e.currentTarget.value)} /><//>
        <${Field} label="Priority"><${Select} value=${priority} options=${PRIORITIES} onChange=${setPriority} /><//>
        <${Field} label="Reason"><${TextInput} value=${reason} onInput=${setReason} /><//>
      </div>
      <div class="actions"><button type="button" class="primary" disabled=${!ctx.model.technologies.has(technology.trim()) || duplicate || ctx.busy} onClick=${add}>Stage</button>${duplicate ? html`<span class="hint">Already recommended there.</span>` : null}</div>
    </div>
  </div>`;
}

function Preview({ ctx }) {
  const [detected, setDetected] = useState([]);
  const [result, setResult] = useState(null);
  const [error, setError] = useState('');
  const run = async () => {
    setError('');
    try { setResult(await ctx.api('/api/preview', { detected })); } catch (failure) { setError(failure.message); }
  };
  return html`<details class="card"><summary><b>Preview recommendations</b></summary>
    <p class="hint">Runs the real recommender over the registry plus your staged changes. Technologies you don't list count as absent.</p>
    <${Chips} label="Detected technologies" list="dl-technologies" values=${detected} onChange=${setDetected} />
    <div class="actions"><button type="button" disabled=${!detected.length} onClick=${run}>Run preview</button></div>
    ${error ? html`<p class="error">${error}</p>` : null}
    ${result?.blocked ? html`<p class="error">Fix validation errors before previewing.</p>` : null}
    ${result && !result.blocked ? html`<div class="result"><p class="muted">${result.skills.length} recommended, ${result.unevaluated.length} unevaluated</p>
      <div class="rows">${result.skills.map((s) => html`<a class="row" key=${s.id} href=${hrefSkill(s.id)}><span class="row-main">${s.name}<small>${s.reasons.join(' · ')}</small></span><span class="row-side"><${Badge}>${s.priority}<//></span></a>`)}</div></div>` : null}
  </details>`;
}

function BulkBar({ ctx, selected, clear }) {
  const [priority, setPriority] = useState('');
  const [reason, setReason] = useState('');
  const apply = async () => {
    const ops = bulkEdit(ctx.model, selected, { priority, reason: reason.trim() });
    if (!ops.length) { ctx.notify('Nothing to change in the selection.'); return; }
    if (await ctx.stage(ops)) { clear(); setPriority(''); setReason(''); }
  };
  return html`<div class="bulk"><b>${selected.length} selected</b>
    <${Select} aria-label="Set priority" value=${priority} options=${[['', 'Keep priority'], ...PRIORITIES]} onChange=${setPriority} />
    <${TextInput} aria-label="Replace templated reasons" placeholder="Replace blank or templated reasons" value=${reason} onInput=${setReason} />
    <button type="button" class="primary" disabled=${(!priority && !reason.trim()) || ctx.busy} onClick=${apply}>Stage</button><button type="button" onClick=${clear}>Clear</button></div>`;
}

export function Mappings({ ctx, pivot, id }) {
  const [query, setQuery] = useState('');
  const [scope, setScope] = useState('mapped');
  const [selected, setSelected] = useState([]);
  let mode = pivot === 'skill' ? 'skill' : 'tech';
  let target = id;
  if (pivot === 'mapping') target = ctx.model.mappings.get(id)?.technologyId;
  const needle = query.trim().toLowerCase();
  const mappingsByTech = new Map();
  for (const mapping of ctx.model.mappings.values()) mappingsByTech.set(mapping.technologyId, [...(mappingsByTech.get(mapping.technologyId) ?? []), mapping]);
  const technologies = [...ctx.model.technologies.values()].filter((t) => (scope === 'all' || mappingsByTech.has(t.id)) && (!needle || `${t.name} ${t.id}`.toLowerCase().includes(needle))).sort((a, b) => a.name.localeCompare(b.name));
  const skills = [...ctx.model.skills.values()].filter((s) => !needle || `${s.name} ${s.id}`.toLowerCase().includes(needle)).sort((a, b) => a.name.localeCompare(b.name));
  const toggle = (ids, on) => setSelected(on ? [...new Set([...selected, ...ids])] : selected.filter((value) => !ids.includes(value)));
  const techRow = (t) => {
    const ids = (mappingsByTech.get(t.id) ?? []).map((m) => m.id);
    const count = (mappingsByTech.get(t.id) ?? []).reduce((sum, m) => sum + m.recommendations.length, 0);
    return html`<div key=${t.id} class=${`row ${t.id === target && mode === 'tech' ? 'active' : ''}`}>
      <input type="checkbox" aria-label=${`Select ${t.name}`} disabled=${!ids.length} checked=${ids.length > 0 && ids.every((value) => selected.includes(value))} onChange=${(e) => toggle(ids, e.currentTarget.checked)} />
      <a class="row-main" href=${hrefTech(t.id)}>${t.name}<small class="mono">${t.id}</small></a>
      <span class="row-side">${ids.map((m) => html`<${OpBadge} key=${m} state=${opFor(ctx.state.ops, 'mappings', m)} />`)} <span class="count">${count}</span></span></div>`;
  };
  const skillRow = (s) => html`<a key=${s.id} class=${`row ${s.id === target && mode === 'skill' ? 'active' : ''}`} href=${hrefSkill(s.id)}><span class="row-main">${s.name}<small class="mono">${s.id}</small></span></a>`;
  return html`<section class="split">
    <aside class="list-pane">
      <div class="list-head"><h1>Mappings</h1></div>
      <div class="segmented" role="group" aria-label="View by">
        <a class=${mode === 'tech' ? 'on' : ''} href="#/mappings">By technology</a><a class=${mode === 'skill' ? 'on' : ''} href="#/mappings/skill">By skill</a>
      </div>
      <input type="search" aria-label="Search" placeholder="Search" value=${query} onInput=${(e) => setQuery(e.currentTarget.value)} />
      ${mode === 'tech' ? html`<${Select} aria-label="Scope" value=${scope} options=${[['mapped', 'With mappings'], ['all', 'All technologies']]} onChange=${setScope} />` : null}
      ${selected.length ? html`<${BulkBar} ctx=${ctx} selected=${selected} clear=${() => setSelected([])} />` : null}
      <${List} items=${mode === 'tech' ? technologies : skills} render=${mode === 'tech' ? techRow : skillRow} />
    </aside>
    <div class="detail-pane">
      ${target && mode === 'tech' ? html`<${TechnologyPivot} key=${target} ctx=${ctx} id=${target} />` : null}
      ${target && mode === 'skill' ? html`<${SkillPivot} key=${target} ctx=${ctx} id=${target} />` : null}
      ${!target ? html`<p class="muted pad">Select a technology or skill to edit what it recommends.</p>` : null}
      <${Preview} ctx=${ctx} />
    </div>
  </section>`;
}
