import { Badge, Chips, ConfirmReferences, Diagnostics, EditorActions, Field, List, OpBadge, Select, TextInput, html, useEffect, useMemo, useState } from './components.mjs';
import { ECOSYSTEMS, RULE_KINDS, clone, hrefFor, nextRuleId, normalizeRule, opFor, references, renameTechnology, cascadeDelete, ruleChoice, ruleNeedsSetup, ruleSummary, same, mappingsForTechnology } from './logic.mjs';

function RuleEditor({ rule, onChange, onRemove, detectors }) {
  const kind = RULE_KINDS[rule.kind];
  const choice = ruleChoice(rule);
  const update = (patch, nextChoice = choice) => onChange(normalizeRule({ ...rule, ...patch }, nextChoice));
  const setting = ruleNeedsSetup(rule);
  const text = (field, label, hint, placeholder) => html`<${Field} label=${label} hint=${hint}><${TextInput} value=${rule[field]} placeholder=${placeholder} onInput=${(value) => update({ [field]: value })} /><//>`;
  const swap = (field) => {
    const value = rule[choice] ?? '';
    const patch = { [field]: value };
    if (choice && choice !== field) patch[choice] = undefined;
    update(patch, field);
  };
  return html`<details class="rule">
    <summary><${Badge}>${rule.kind}<//> <code>${rule.id}</code> <span class="muted truncate">${ruleSummary(rule)}</span> ${rule.disabled ? html`<${Badge} tone="fair">disabled<//>` : null}</summary>
    <div class="form-grid">
      <${Field} label="Kind"><${Select} value=${rule.kind} options=${Object.entries(RULE_KINDS).map(([id, def]) => [id, def.label])} onChange=${(value) => onChange({ id: rule.id, kind: value, ...(value === 'dependency' ? { ecosystem: 'npm' } : {}) })} /><//>
      ${text('id', 'Rule id')}
      ${rule.kind === 'dependency' ? html`<${Field} label="Ecosystem"><${Select} value=${rule.ecosystem} options=${ECOSYSTEMS} onChange=${(value) => update({ ecosystem: value })} /><//>` : null}
      ${kind.choices.length ? html`<${Field} label="Match by"><${Select} value=${choice} options=${kind.choices} onChange=${swap} /><//>` : null}
      ${rule.kind === 'content' ? text('glob', 'Files (glob)', null, '**/*.ts') : null}
      ${rule.kind === 'dotenv' ? text('glob', 'Files (glob, optional)', 'Blank means implicit .env files.') : null}
      ${choice ? text(choice, kind.choices.find(([field]) => field === choice)[1]) : null}
      ${rule.kind === 'file' ? html`<${Field} label="Path scope"><${Select} value=${rule.pathScope ?? ''} options=${[['', 'Default'], ['relative', 'Relative path'], ['basename', 'File name only']]} onChange=${(value) => update({ pathScope: value || undefined })} /><//>` : null}
      ${rule.kind === 'content' && choice === 'regex' ? html`<${Field} label="Flags"><${Select} value=${rule.regexFlags ?? ''} options=${[['', 'Case sensitive'], ['i', 'Case insensitive']]} onChange=${(value) => update({ regexFlags: value || undefined })} /><//>` : null}
      ${rule.kind === 'extension' ? text('extension', 'Extension', null, '.ts') : null}
      ${rule.kind === 'mcp_package' ? text('name', 'Package') : null}
      ${rule.kind === 'detector' ? html`<${Field} label="Detector name"><input type="text" list="dl-detectors" value=${rule.detector ?? ''} onInput=${(e) => update({ detector: e.currentTarget.value })} /><//>
        <${Field} label="Arguments (JSON)"><textarea rows="3" value=${JSON.stringify(rule.args ?? {}, null, 2)} onChange=${(e) => { try { update({ args: JSON.parse(e.currentTarget.value) }); } catch { /* keep the last valid arguments */ } }}></textarea><//>
        <datalist id="dl-detectors">${detectors.map((name) => html`<option value=${name} />`)}</datalist>` : null}
      ${text('description', 'Description')}
    </div>
    ${setting ? html`<p class="hint">${setting}</p>` : null}
    <div class="actions">
      <label class="check"><input type="checkbox" checked=${Boolean(rule.disabled)} onChange=${(e) => update({ disabled: e.currentTarget.checked })} /> Disabled</label>
      <span class="spacer"></span><button type="button" class="danger" onClick=${onRemove}>Remove rule</button>
    </div>
  </details>`;
}

function RuleTester({ ctx, technology }) {
  const [path, setPath] = useState('package.json');
  const [content, setContent] = useState('');
  const [folder, setFolder] = useState('');
  const [result, setResult] = useState(null);
  const [error, setError] = useState('');
  const run = async () => {
    setError('');
    try {
      const body = folder.trim() ? { technology, folder: folder.trim() } : { technology, files: [{ path, content }] };
      setResult(await ctx.api('/api/rules/test', body));
    } catch (failure) { setResult(null); setError(failure.message); }
  };
  const tone = { detected: 'excellent', absent: 'poor', unknown: 'fair' }[result?.state];
  return html`<details class="card"><summary><b>Test these rules</b></summary>
    <p class="hint">Runs this form's rules, including unstaged edits, through the real detection engine.</p>
    <div class="form-grid">
      <${Field} label="Sample path"><${TextInput} value=${path} onInput=${setPath} /><//>
      <${Field} label="Or a local folder" hint="Absolute path. Overrides the sample."><${TextInput} value=${folder} onInput=${setFolder} placeholder="/path/to/project" /><//>
    </div>
    <${Field} label="Sample content"><textarea rows="5" value=${content} onInput=${(e) => setContent(e.currentTarget.value)}></textarea><//>
    <div class="actions"><button type="button" onClick=${run}>Run test</button></div>
    ${error ? html`<p class="error">${error}</p>` : null}
    ${result ? html`<div class="result"><p>Result: <${Badge} tone=${tone}>${result.state ?? 'unknown'}<//></p>
      ${result.evidence.length ? html`<ul class="refs">${result.evidence.map((e) => html`<li><code>${e.ruleId}</code> ${e.path} ${e.matchedIdentifier ? html`<span class="muted">${e.matchedIdentifier}</span>` : null}</li>`)}</ul>` : null}
      ${result.gaps.length ? html`<p class="hint">Not evaluated: ${result.gaps.map((g) => `${g.ruleId} (${g.reason})`).join(', ')}</p>` : null}
    </div>` : null}
  </details>`;
}

function TechnologyEditor({ ctx, original, isNew }) {
  const [form, setForm] = useState(() => clone(original));
  const [confirm, setConfirm] = useState(null);
  const [renaming, setRenaming] = useState('');
  useEffect(() => setForm(clone(original)), [original]);
  const set = (patch) => setForm({ ...form, ...patch });
  const dirty = !same(form, original) || isNew;
  const detectors = useMemo(() => [...new Set([...ctx.model.technologies.values()].flatMap((t) => t.rules.filter((r) => r.kind === 'detector').map((r) => r.detector)))].sort(), [ctx.model]);
  const setRule = (index, rule) => set({ rules: form.rules.map((r, i) => (i === index ? rule : r)) });
  const addRule = () => set({ rules: [...form.rules, normalizeRule({ id: nextRuleId(form.rules, 'file'), kind: 'file', glob: '' }, 'glob')] });
  const stage = async () => {
    const entity = clone(form);
    if (entity.metadataOnly) { entity.rules = []; delete entity.disabled; } else delete entity.metadataOnly;
    if (entity.description === '') delete entity.description;
    if (await ctx.stage([{ role: 'technologies', id: entity.id, entity }]) && isNew) location.hash = hrefFor('technologies', entity.id);
  };
  const refs = references(ctx.model, 'technologies', form.id);
  const mappings = isNew ? [] : mappingsForTechnology(ctx.model, form.id);
  return html`<div class="editor">
    <header class="editor-head"><h2>${form.name || 'New technology'}</h2><code>${form.id}</code> <${OpBadge} state=${opFor(ctx.state.ops, 'technologies', original.id)} /></header>
    <${Diagnostics} diagnostics=${ctx.state.diagnostics} subject=${form.id} />
    <div class="form-grid">
      ${isNew ? html`<${Field} label="Id" hint="A stable public identifier."><${TextInput} value=${form.id} onInput=${(id) => set({ id })} /><//>` : null}
      <${Field} label="Name"><${TextInput} value=${form.name} onInput=${(name) => set({ name })} /><//>
      <${Field} label="Category"><input type="text" list="dl-category" value=${form.category ?? ''} onInput=${(e) => set({ category: e.currentTarget.value })} /><//>
      <${Field} label="Description"><${TextInput} value=${form.description} onInput=${(description) => set({ description })} /><//>
    </div>
    <${Chips} label="Tags" values=${form.tags} onChange=${(tags) => set({ tags })} />
    <div class="actions">
      <label class="check"><input type="checkbox" checked=${Boolean(form.metadataOnly)} onChange=${(e) => set({ metadataOnly: e.currentTarget.checked })} /> Metadata only (no detection rules)</label>
      <label class="check"><input type="checkbox" checked=${Boolean(form.disabled)} disabled=${form.metadataOnly} onChange=${(e) => set({ disabled: e.currentTarget.checked })} /> Disabled</label>
    </div>
    ${form.metadataOnly ? html`<p class="hint">Staging removes this technology's rules.</p>` : html`
      <h3 class="section-title">Detection rules (${form.rules.length})</h3>
      <div class="rule-list">${form.rules.map((rule, i) => html`<${RuleEditor} key=${i} rule=${rule} detectors=${detectors} onChange=${(next) => setRule(i, next)} onRemove=${() => set({ rules: form.rules.filter((_, j) => j !== i) })} />`)}</div>
      <div class="actions"><button type="button" onClick=${addRule}>Add rule</button></div>
      <${RuleTester} ctx=${ctx} technology=${form} />`}
    <${EditorActions} dirty=${dirty} busy=${ctx.busy} onStage=${stage} onReset=${() => setForm(clone(original))} onDelete=${isNew ? null : () => setConfirm('delete')} />
    ${!isNew ? html`<details class="card"><summary><b>Rename id</b></summary>
      <p class="hint">Ids are a public contract: overlay registries may reference this one. Rewrites ${refs.length} reference(s).</p>
      <div class="actions"><${TextInput} aria-label="New id" value=${renaming} onInput=${setRenaming} placeholder="new-id" /><button type="button" disabled=${!renaming.trim() || ctx.model.technologies.has(renaming.trim())} onClick=${() => setConfirm('rename')}>Rename…</button></div>
    </details>` : null}
    ${mappings.length ? html`<p class="muted">Recommended through: ${mappings.map((m) => html`<a class="mono" href=${hrefFor('mappings', m.id)}>${m.id}</a> `)}</p>` : null}
    ${confirm === 'delete' ? html`<${ConfirmReferences} title=${`Delete ${form.id}?`} refs=${refs} confirmLabel="Stage deletion" onCancel=${() => setConfirm(null)} onConfirm=${async () => { setConfirm(null); if (await ctx.stage(cascadeDelete(ctx.model, 'technologies', form.id))) location.hash = '#/technologies'; }} />` : null}
    ${confirm === 'rename' ? html`<${ConfirmReferences} title=${`Rename to ${renaming.trim()}?`} refs=${refs} confirmLabel="Stage rename" onCancel=${() => setConfirm(null)} onConfirm=${async () => { const to = renaming.trim(); setConfirm(null); if (await ctx.stage(renameTechnology(ctx.model, form.id, to))) location.hash = hrefFor('technologies', to); }} />` : null}
  </div>`;
}

export function Technologies({ ctx, id }) {
  const [query, setQuery] = useState('');
  const [category, setCategory] = useState('');
  const [mode, setMode] = useState('all');
  const technologies = [...ctx.model.technologies.values()].sort((a, b) => (a.name ?? a.id).localeCompare(b.name ?? b.id));
  const categories = [...new Set(technologies.map((t) => t.category).filter(Boolean))].sort();
  const mapped = new Set([...ctx.model.mappings.values()].map((m) => m.technologyId));
  const needle = query.trim().toLowerCase();
  const visible = technologies.filter((t) => (!needle || `${t.name} ${t.id}`.toLowerCase().includes(needle)) && (!category || t.category === category)
    && (mode === 'all' || (mode === 'metadata' && t.metadataOnly) || (mode === 'mapped' && mapped.has(t.id)) || (mode === 'unmapped' && !mapped.has(t.id))));
  const isNew = id === 'new';
  const selected = isNew ? { id: '', name: '', category: categories[0] ?? 'tool', rules: [] } : ctx.model.technologies.get(id);
  return html`<section class="split">
    <aside class="list-pane">
      <div class="list-head"><h1>Technologies</h1><a class="button" href="#/technologies/new">New</a></div>
      <input type="search" aria-label="Search technologies" placeholder="Search" value=${query} onInput=${(e) => setQuery(e.currentTarget.value)} />
      <div class="filters">
        <${Select} aria-label="Category" value=${category} options=${[['', 'All categories'], ...categories]} onChange=${setCategory} />
        <${Select} aria-label="Coverage" value=${mode} options=${[['all', 'All'], ['mapped', 'Has mapping'], ['unmapped', 'No mapping'], ['metadata', 'Metadata only']]} onChange=${setMode} />
      </div>
      <p class="muted small">${visible.length} shown</p>
      <${List} items=${visible} render=${(t) => html`<a key=${t.id} class=${`row ${t.id === id ? 'active' : ''}`} href=${hrefFor('technologies', t.id)}>
        <span class="row-main">${t.name}<small class="mono">${t.id}</small></span><span class="row-side"><${OpBadge} state=${opFor(ctx.state.ops, 'technologies', t.id)} /> <span class="count">${t.rules.length}</span></span></a>`} />
      <datalist id="dl-category">${categories.map((c) => html`<option value=${c} />`)}</datalist>
    </aside>
    <div class="detail-pane">${selected ? html`<${TechnologyEditor} key=${id} ctx=${ctx} original=${selected} isNew=${isNew} />` : html`<p class="muted pad">Select a technology to edit its detection rules.</p>`}</div>
  </section>`;
}
