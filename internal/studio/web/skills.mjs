import { Badge, Chips, ConfirmReferences, Diagnostics, EditorActions, Field, List, OpBadge, Select, TextInput, html, useEffect, useState } from './components.mjs';
import { PRIORITIES, QUALITIES, addSkillOps, cascadeDelete, clone, hrefFor, opFor, recommendationsForSkill, references, same, skillEditOps, skillIdFor } from './logic.mjs';

const today = () => new Date().toISOString().slice(0, 10);

function AutomationPanel({ skill, assessments, automated }) {
  const rows = [
    ['Source', skill.source?.uri ?? skill.source?.path], ['Upstream name', skill.source?.skillName], ['Pinned commit', skill.source?.ref],
    ['Repository', skill.repositoryId], ['Quality', skill.quality], ['Score', skill.score], ['Assessed', skill.assessment && `${skill.assessment.assessedAt} · ${skill.assessment.method}`],
    ['Validator', assessments.status[skill.id]], ['Quarantine', skill.quarantine && `${skill.quarantine.reason} since ${skill.quarantine.since}`],
  ].filter(([, value]) => value !== undefined && value !== null && value !== '');
  return html`<aside class="card readonly"><h3>${automated ? 'Owned by automation' : 'Source'}</h3>
    <dl>${rows.map(([label, value]) => html`<dt>${label}</dt><dd class="mono">${String(value)}</dd>`)}</dl>
    ${automated ? html`<p class="hint">Pins and scores come from registry workflows and cannot be edited here.</p>` : null}</aside>`;
}

function ManualAssessment({ form, set }) {
  const manual = form.assessment && form.assessment.method !== 'automated';
  const enable = () => set({ quality: form.quality ?? 'good', assessment: { method: 'manual-review', assessedAt: today() } });
  if (!manual) return html`<div class="actions"><button type="button" onClick=${enable}>Add manual assessment</button></div>`;
  return html`<div class="form-grid">
    <${Field} label="Quality"><${Select} value=${form.quality} options=${QUALITIES} onChange=${(quality) => set({ quality })} /><//>
    <${Field} label="Score (0–100)"><input type="number" min="0" max="100" value=${form.score ?? ''} onInput=${(e) => set({ score: e.currentTarget.value === '' ? undefined : Number(e.currentTarget.value) })} /><//>
    <${Field} label="Assessor"><${TextInput} value=${form.assessment.assessor} onInput=${(assessor) => set({ assessment: { ...form.assessment, assessor } })} /><//>
  </div>`;
}

function SkillEditor({ ctx, original }) {
  const [form, setForm] = useState(() => clone(original));
  const [confirm, setConfirm] = useState(false);
  useEffect(() => setForm(clone(original)), [original]);
  const set = (patch) => setForm({ ...form, ...patch });
  const { automated, registryChecks } = ctx.state.meta;
  const elevated = !automated || registryChecks;
  const clean = () => {
    const entity = clone(form);
    for (const key of ['description', 'tags', 'requires', 'conflictsWith']) if (!entity[key]?.length) delete entity[key];
    if (entity.score === undefined) delete entity.score;
    return entity;
  };
  const dirty = !same(clean(), original);
  const refs = references(ctx.model, 'skills', form.id);
  const blocked = automated && !registryChecks;
  const placements = recommendationsForSkill(ctx.model, form.id);
  return html`<div class="editor">
    <header class="editor-head"><h2>${form.name}</h2><code>${form.id}</code> <${OpBadge} state=${opFor(ctx.state.ops, 'skills', form.id)} /> ${form.quarantine ? html`<${Badge} tone="poor">quarantined<//>` : null}</header>
    <${Diagnostics} diagnostics=${ctx.state.diagnostics} subject=${form.id} />
    <div class="form-grid">
      <${Field} label="Name"><${TextInput} value=${form.name} onInput=${(name) => set({ name })} /><//>
      ${elevated ? html`<${Field} label="Repository"><input type="text" list="dl-repositories" value=${form.repositoryId ?? ''} onInput=${(e) => set({ repositoryId: e.currentTarget.value })} /><//>` : null}
    </div>
    <${Field} label="Description"><textarea rows="2" value=${form.description ?? ''} onInput=${(e) => set({ description: e.currentTarget.value })}></textarea><//>
    <${Chips} label="Tags" values=${form.tags} onChange=${(tags) => set({ tags })} />
    <${Chips} label="Technologies" list="dl-technologies" values=${form.technologies} onChange=${(technologies) => set({ technologies })} hint="Declares relevance. Recommendations come from mappings." />
    <${Chips} label="Requires skills" list="dl-skills" values=${form.requires} onChange=${(requires) => set({ requires })} />
    <${Chips} label="Conflicts with skills" list="dl-skills" values=${form.conflictsWith} onChange=${(conflictsWith) => set({ conflictsWith })} hint="Also written on the other skill." />
    ${elevated ? html`<${ManualAssessment} form=${form} set=${set} />` : null}
    <${EditorActions} dirty=${dirty} busy=${ctx.busy} onStage=${() => ctx.stage(skillEditOps(ctx.model, clean()))} onReset=${() => setForm(clone(original))}
      onDelete=${blocked ? null : () => setConfirm(true)} />
    ${blocked ? html`<p class="hint">Deleting a skill rewrites its assessments. Restart the studio with <code>--registry-checks</code> to enable it.</p>` : null}
    ${confirm ? html`<${ConfirmReferences} title=${`Delete ${form.id}?`} refs=${refs} confirmLabel="Stage deletion" onCancel=${() => setConfirm(false)} onConfirm=${async () => { setConfirm(false); if (await ctx.stage(cascadeDelete(ctx.model, 'skills', form.id))) location.hash = '#/skills'; }} />` : null}
    <p class="muted"><a href=${`#/mappings/skill/${encodeURIComponent(form.id)}`}>Recommended for ${placements.length} ${placements.length === 1 ? 'technology' : 'technologies'}</a></p>
    <${AutomationPanel} skill=${form} assessments=${ctx.state.assessments} automated=${automated} />
  </div>`;
}

function AddSkill({ ctx }) {
  const [uri, setUri] = useState('');
  const [resolution, setResolution] = useState(null);
  const [name, setName] = useState('');
  const [selected, setSelected] = useState(null);
  const [technologies, setTechnologies] = useState([]);
  const [priority, setPriority] = useState('optional');
  const [reason, setReason] = useState('');
  const [map, setMap] = useState(true);
  const [error, setError] = useState('');
  const [working, setWorking] = useState(false);
  const call = async (body) => {
    setError(''); setWorking(true);
    try { return await ctx.api('/api/skills/resolve', body); } catch (failure) { setError(failure.message); return null; } finally { setWorking(false); }
  };
  const resolve = async () => { const result = await call({ uri }); setResolution(result); setName(''); setSelected(null); };
  const pick = async (skillName) => {
    setName(skillName);
    const result = await call({ uri, skillName });
    setSelected(result?.selected ?? null);
  };
  const existing = resolution && name ? ctx.model.skills.has(skillIdFor(resolution.repository, name)) : false;
  const canAdd = selected?.trackable && !existing && !ctx.busy && (!map || technologies.length > 0);
  const add = async () => {
    const normalized = uri.trim().replace(/\.git$/, '');
    const ops = addSkillOps(ctx.model, { repository: resolution.repository, uri: normalized, commit: resolution.commit, path: selected.path, name, description: selected.description, technologies, priority, reason, map });
    if (await ctx.stage(ops)) location.hash = hrefFor('skills', skillIdFor(resolution.repository, name));
  };
  return html`<div class="editor">
    <header class="editor-head"><h2>Add a skill</h2></header>
    <p class="muted">The registry stores a reference pinned to one commit, never the skill itself. Quality and score arrive from automation after merge.</p>
    <div class="actions"><${TextInput} aria-label="Repository URL" value=${uri} onInput=${setUri} placeholder="https://github.com/owner/repo" /><button type="button" class="primary" disabled=${!uri.trim() || working} onClick=${resolve}>${working ? 'Working…' : 'Find skills'}</button></div>
    ${error ? html`<p class="error">${error}</p>` : null}
    ${resolution ? html`<div class="card">
      <p><b>${resolution.repository}</b> at <code>${resolution.commit.slice(0, 12)}</code> · ${resolution.skills.length} SKILL.md found</p>
      <div class="rows">${resolution.skills.map((skill) => html`<button type="button" key=${skill.path} class=${`row ${skill.name === name ? 'active' : ''}`} disabled=${!skill.trackable} title=${skill.reason ?? ''} onClick=${() => pick(skill.name)}>
        <span class="row-main">${skill.name}<small class="mono">${skill.path}</small></span>${skill.trackable ? null : html`<${Badge} tone="poor">untrackable<//>`}</button>`)}</div>
    </div>` : null}
    ${selected ? html`<div class="card">
      ${selected.trackable ? html`<p>${selected.description || html`<span class="muted">No description in frontmatter.</span>`}</p>` : html`<p class="error">${selected.reason}</p>`}
      ${existing ? html`<p class="error">This skill is already in the registry.</p>` : null}
      <${Chips} label="Technologies" list="dl-technologies" values=${technologies} onChange=${setTechnologies} />
      <label class="check"><input type="checkbox" checked=${map} onChange=${(e) => setMap(e.currentTarget.checked)} /> Recommend for these technologies</label>
      ${map ? html`<div class="form-grid"><${Field} label="Priority"><${Select} value=${priority} options=${PRIORITIES} onChange=${setPriority} /><//><${Field} label="Reason"><${TextInput} value=${reason} onInput=${setReason} /><//></div>` : null}
      <div class="actions"><button type="button" class="primary" disabled=${!canAdd} onClick=${add}>Stage skill</button></div>
    </div>` : null}
  </div>`;
}

export function Skills({ ctx, id }) {
  const [query, setQuery] = useState('');
  const [quality, setQuality] = useState('');
  const [repository, setRepository] = useState('');
  const skills = [...ctx.model.skills.values()].sort((a, b) => a.name.localeCompare(b.name));
  const needle = query.trim().toLowerCase();
  const visible = skills.filter((s) => (!needle || `${s.name} ${s.id}`.toLowerCase().includes(needle)) && (!quality || s.quality === quality) && (!repository || s.repositoryId === repository));
  const repositories = [...new Set(skills.map((s) => s.repositoryId).filter(Boolean))].sort();
  const selected = ctx.model.skills.get(id);
  const tones = { excellent: 'excellent', good: 'good', fair: 'fair', poor: 'poor' };
  return html`<section class="split">
    <aside class="list-pane">
      <div class="list-head"><h1>Skills</h1><a class="button" href="#/skills/new">Add</a></div>
      <input type="search" aria-label="Search skills" placeholder="Search" value=${query} onInput=${(e) => setQuery(e.currentTarget.value)} />
      <div class="filters">
        <${Select} aria-label="Quality" value=${quality} options=${[['', 'Any quality'], ...QUALITIES]} onChange=${setQuality} />
        <${Select} aria-label="Repository" value=${repository} options=${[['', 'All repositories'], ...repositories]} onChange=${setRepository} />
      </div>
      <p class="muted small">${visible.length} shown</p>
      <${List} items=${visible} render=${(s) => html`<a key=${s.id} class=${`row ${s.id === id ? 'active' : ''}`} href=${hrefFor('skills', s.id)}>
        <span class="row-main">${s.name}<small class="mono">${s.repositoryId ?? s.id}</small></span>
        <span class="row-side"><${OpBadge} state=${opFor(ctx.state.ops, 'skills', s.id)} />${s.quality ? html`<${Badge} tone=${tones[s.quality]}>${s.quality}<//>` : null}</span></a>`} />
    </aside>
    <div class="detail-pane">
      ${id === 'new' ? html`<${AddSkill} ctx=${ctx} />` : null}
      ${selected ? html`<${SkillEditor} key=${id} ctx=${ctx} original=${selected} />` : null}
      ${!selected && id !== 'new' ? html`<p class="muted pad">Select a skill to edit it, or add one from GitHub.</p>` : null}
    </div>
  </section>`;
}
