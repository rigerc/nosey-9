// Nosey registry browser. All registry text is rendered with textContent only.
const app = document.getElementById('app');
const PAGE = 30;
const QUALITIES = ['excellent', 'good', 'fair', 'poor'];
const JEV_LABELS = {
  trigger_clarity: 'Trigger clarity',
  instruction_quality: 'Instruction quality',
  correctness: 'Correctness',
  safety: 'Safety',
  context_efficiency: 'Context efficiency',
};
const LEVEL_RANK = { error: 3, warning: 2, info: 1 };

let data;
let detailsPromise;
// List state survives navigation, so filters are still there after going back.
const view = {};

const h = (tag, attrs, ...kids) => {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs ?? {})) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const kid of kids.flat()) if (kid != null && kid !== false) el.append(kid);
  return el;
};
// replaceChildren stringifies null, so drop empty slots first.
const fill = (el, ...kids) => el.replaceChildren(...kids.flat().filter(Boolean));
const link = (href, text, cls) => h('a', { href, class: cls }, text);
const enc = encodeURIComponent;
const skillHref = (id) => `#/skill/${enc(id)}`;
const techHref = (id) => `#/tech/${enc(id)}`;
const repoHref = (id) => `#/repo/${enc(id)}`;
const fmtDate = (s) => (s ? new Date(s).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' }) : '—');
const num = (n) => new Intl.NumberFormat().format(n);
const plural = (noun, n) => (n === 1 ? noun : noun.endsWith('y') ? `${noun.slice(0, -1)}ies` : `${noun}s`);
const cap = (s) => `${s[0].toUpperCase()}${s.slice(1)}`;
const badge = (cls, text) => h('span', { class: `badge ${cls}` }, text);
const ring = (score, quality, large) => h('div', { class: `ring ${quality ?? ''} ${large ? 'lg' : ''}`, style: `--v:${score ?? 0}`, role: 'img', 'aria-label': `Score ${score ?? 'n/a'} out of 100` }, h('span', null, score ?? '–'));
const short = (s) => (s ? s.slice(0, 7) : '—');
const qualityOf = (score) => (score >= 85 ? 'excellent' : score >= 70 ? 'good' : score >= 50 ? 'fair' : 'poor');
const avg = (xs) => (xs.length ? xs.reduce((a, b) => a + b, 0) / xs.length : null);

async function loadDetails() {
  detailsPromise ??= fetch('details.json').then((r) => {
    if (!r.ok) throw new Error(`details.json: ${r.status}`);
    return r.json();
  });
  return detailsPromise;
}

function mount(...nodes) {
  app.replaceChildren(...nodes);
  window.scrollTo(0, 0);
}

/* ---------- derived data ---------- */
const repoIndex = () => (data._repos ??= new Map(data.repositories.map((r) => [r.id, r])));
const techIndex = () => (data._techs ??= new Map(data.technologies.map((t) => [t.id, t])));
const repoName = (id) => repoIndex().get(id)?.name ?? id;
const techName = (id) => techIndex().get(id)?.name ?? id;
const owner = (repo) => repo.name.split('/')[0];

function prepare() {
  const techs = techIndex();
  const repos = repoIndex();
  for (const t of data.technologies) Object.assign(t, { _scores: [], _issues: 0 });
  for (const r of data.repositories) Object.assign(r, { _q: Object.fromEntries(QUALITIES.map((q) => [q, 0])), _errors: 0, _warnings: 0, owner: owner(r) });
  for (const s of data.skills) {
    s.categories = [...new Set(s.techs.map((t) => techs.get(t)?.category).filter(Boolean))];
    for (const id of s.techs) {
      const t = techs.get(id);
      if (t) {
        t._scores.push(s.score);
        t._issues += s.errors;
      }
    }
    const r = repos.get(s.repo);
    if (r) {
      r._q[s.quality] += 1;
      r._errors += s.errors;
      r._warnings += s.warnings;
    }
  }
  for (const t of data.technologies) t.avgScore = avg(t._scores);
}

/* ---------- generic browser: search + filters + sort + paging ---------- */
// cfg: items, state, noun, placeholder, searchText(item), sorts{key:{label,val,desc}},
//      filters[{key,label,type:'chips'|'pick'|'min',options?,match(item,value),max?,step?,unit?}],
//      render(slice) -> Node, empty, inline (filters shown in-line instead of a sheet)
const newState = (sort) => ({ q: '', sort, dir: null, sel: {}, shown: PAGE });

function compareBy(sort, dir) {
  return (a, b) => {
    const x = sort.val(a);
    const y = sort.val(b);
    const tie = () => String(a.name ?? a.id ?? '').localeCompare(String(b.name ?? b.id ?? ''));
    if (x == null && y == null) return tie();
    if (x == null) return 1;
    if (y == null) return -1;
    const c = typeof x === 'string' ? x.localeCompare(y) : x - y;
    return c ? c * dir : tie();
  };
}

function browser(cfg) {
  const { items, state, noun = 'item', sorts, searchText, render, empty = 'Nothing matches these filters.', inline = false } = cfg;
  const sortKeys = Object.keys(sorts);
  if (!sorts[state.sort]) state.sort = sortKeys[0];
  state.dir ??= sorts[state.sort].desc ? -1 : 1;

  // Resolve options once; facets with a single choice are not worth showing.
  const filters = (cfg.filters ?? [])
    .map((f) => ({ ...f, opts: f.type === 'min' ? [] : f.options().map(([value, label]) => ({ value, label, count: items.filter((it) => f.match(it, value)).length })) }))
    .filter((f) => f.type === 'min' || f.opts.length > 1);
  const textCache = new Map();
  const textOf = (it) => {
    let t = textCache.get(it);
    if (t == null) textCache.set(it, (t = searchText(it).toLowerCase()));
    return t;
  };

  const root = h('div');
  const body = h('div');
  const count = h('span', { 'aria-live': 'polite' });
  const activeBox = h('div', { class: 'active' });
  const filterBtn = h('button', { class: 'btn-tool', type: 'button', onclick: () => openSheet() });
  const dirBtn = h('button', { class: 'btn-tool dir', type: 'button', onclick: () => { state.dir = -state.dir; changed(); } });
  const sortSel = h('select', { 'aria-label': `Sort ${plural(noun, 2)}`, onchange: (e) => { state.sort = e.target.value; state.dir = sorts[state.sort].desc ? -1 : 1; changed(); } },
    sortKeys.map((k) => h('option', { value: k }, sorts[k].label)));
  const inlineBox = h('div', { class: 'inline-filters' });
  const input = h('input', { class: 'search', type: 'search', placeholder: cfg.placeholder ?? `Search ${plural(noun, 2)}…`, 'aria-label': cfg.placeholder ?? `Search ${plural(noun, 2)}`, value: state.q, autocomplete: 'off', enterkeyhint: 'search' });
  input.addEventListener('input', () => { state.q = input.value; changed(); });

  const isSet = (f) => (f.type === 'min' ? (state.sel[f.key] ?? 0) > 0 : (state.sel[f.key]?.size ?? 0) > 0);
  const activeCount = () => filters.filter(isSet).length;

  function visible() {
    const words = state.q.trim().toLowerCase().split(/\s+/).filter(Boolean);
    const out = items.filter((it) => (!words.length || words.every((w) => textOf(it).includes(w)))
      && filters.every((f) => !isSet(f) || (f.type === 'min' ? f.match(it, state.sel[f.key]) : [...state.sel[f.key]].some((v) => f.match(it, v)))));
    return out.sort(compareBy(sorts[state.sort], state.dir));
  }

  const toggle = (f, value) => {
    const set = (state.sel[f.key] ??= new Set());
    if (set.has(value)) set.delete(value);
    else set.add(value);
    changed();
  };
  const clearAll = () => { state.sel = {}; changed(); };

  function chipGroup(f) {
    return h('div', { class: 'chips', role: 'group', 'aria-label': f.label }, f.opts.map((o) =>
      h('button', { class: 'chip', type: 'button', 'aria-pressed': String(Boolean(state.sel[f.key]?.has(o.value))), onclick: () => toggle(f, o.value) }, `${o.label} · ${o.count}`)));
  }
  function pickGroup(f) {
    const filterIn = h('input', { class: 'mini-search', type: 'search', placeholder: `Find ${f.label.toLowerCase()}…`, 'aria-label': `Find ${f.label}` });
    const list = h('div', { class: 'pick-list' });
    const drawList = () => {
      const q = filterIn.value.trim().toLowerCase();
      const sel = state.sel[f.key] ?? new Set();
      const opts = f.opts.filter((o) => !q || o.label.toLowerCase().includes(q)).sort((a, b) => (sel.has(b.value) - sel.has(a.value)) || b.count - a.count || a.label.localeCompare(b.label)).slice(0, 80);
      fill(list, opts.length ? opts.map((o) => h('label', { class: 'pick' },
        h('input', { type: 'checkbox', checked: sel.has(o.value), onchange: () => { toggleQuiet(f, o.value); } }), h('span', null, o.label), h('small', null, String(o.count)))) : h('p', { class: 'note' }, 'No matches.'));
    };
    filterIn.addEventListener('input', drawList);
    drawList();
    return h('div', null, filterIn, list);
  }
  // Toggling inside a pick list keeps the list (and its scroll position) in place.
  const toggleQuiet = (f, value) => {
    const set = (state.sel[f.key] ??= new Set());
    if (set.has(value)) set.delete(value);
    else set.add(value);
    state.shown = PAGE;
    refreshList();
    refreshSummary();
  };
  function minGroup(f) {
    const out = h('output', null, `${state.sel[f.key] ?? 0}${f.unit ?? ''}`);
    const range = h('input', { type: 'range', min: 0, max: f.max, step: f.step ?? 1, value: state.sel[f.key] ?? 0, 'aria-label': f.label });
    range.addEventListener('input', () => { state.sel[f.key] = Number(range.value); out.textContent = `${range.value}${f.unit ?? ''}`; state.shown = PAGE; refreshList(); refreshSummary(); });
    return h('div', { class: 'range' }, range, out);
  }
  const groupFor = (f) => (f.type === 'pick' ? pickGroup(f) : f.type === 'min' ? minGroup(f) : chipGroup(f));

  function activeChips() {
    const chips = [];
    for (const f of filters) {
      if (!isSet(f)) continue;
      if (f.type === 'min') chips.push(h('button', { class: 'chip active-chip', type: 'button', onclick: () => { delete state.sel[f.key]; changed(); } }, `${f.label} ≥ ${state.sel[f.key]}${f.unit ?? ''} ×`));
      else for (const v of state.sel[f.key]) chips.push(h('button', { class: 'chip active-chip', type: 'button', onclick: () => toggle(f, v) }, `${f.label}: ${f.opts.find((o) => o.value === v)?.label ?? v} ×`));
    }
    if (chips.length) chips.push(h('button', { class: 'linklike', type: 'button', onclick: clearAll }, 'Clear all'));
    return chips;
  }

  function refreshSummary() {
    const n = activeCount();
    filterBtn.textContent = n ? `Filters · ${n}` : 'Filters';
    filterBtn.classList.toggle('on', n > 0);
    dirBtn.textContent = state.dir > 0 ? '↑' : '↓';
    dirBtn.setAttribute('aria-label', state.dir > 0 ? 'Ascending, tap to reverse' : 'Descending, tap to reverse');
    sortSel.value = state.sort;
    if (!inline) fill(activeBox, activeChips());
  }

  let last = [];
  function refreshList() {
    last = visible();
    count.textContent = `${num(last.length)} of ${num(items.length)} ${plural(noun, items.length)}`;
    const slice = last.slice(0, state.shown);
    fill(body, slice.length ? render(slice) : h('p', { class: 'empty' }, empty),
      last.length > state.shown && h('button', { class: 'more', type: 'button', onclick: () => { state.shown += PAGE * 2; refreshList(); } }, `Show more (${num(last.length - state.shown)} left)`));
    if (sheet) fill(sheetDone, `Show ${num(last.length)} result${last.length === 1 ? '' : 's'}`);
  }
  function changed() {
    state.shown = PAGE;
    refreshSummary();
    if (inline) drawInline();
    if (sheet) drawSheet();
    refreshList();
  }

  function drawInline() {
    fill(inlineBox, filters.map((f) => (f.type === 'chips' ? h('div', { class: 'inline-group' }, h('span', { class: 'inline-label' }, f.label), chipGroup(f)) : h('div', { class: 'inline-group' }, h('span', { class: 'inline-label' }, f.label), groupFor(f)))));
  }

  // Filter sheet (bottom sheet on phones, dialog on desktop).
  let sheet = null;
  let sheetBody;
  let sheetDone;
  function drawSheet() {
    const scroll = sheetBody.scrollTop;
    fill(sheetBody, filters.map((f) => h('fieldset', { class: 'facet' }, h('legend', null, f.label), groupFor(f))));
    sheetBody.scrollTop = scroll;
  }
  function openSheet() {
    sheetBody = h('div', { class: 'sheet-body' });
    sheetDone = h('button', { class: 'btn', type: 'button', onclick: () => sheet.close() });
    sheet = h('dialog', { class: 'sheet', 'aria-label': 'Filters' },
      h('div', { class: 'sheet-inner' },
        h('div', { class: 'sheet-head' }, h('h2', null, `Filter ${plural(noun, 2)}`), h('button', { class: 'linklike', type: 'button', onclick: clearAll }, 'Clear all')),
        sheetBody,
        h('div', { class: 'sheet-foot' }, sheetDone)));
    sheet.addEventListener('click', (e) => { if (e.target === sheet) sheet.close(); });
    sheet.addEventListener('close', () => { sheet.remove(); sheet = null; });
    document.body.append(sheet);
    drawSheet();
    refreshList();
    sheet.showModal();
  }

  const tools = inline
    ? h('div', { class: 'tools' }, input, h('div', { class: 'sortrow' }, sortSel, dirBtn))
    : h('div', { class: 'tools' }, input, h('div', { class: 'sortrow' }, filters.length ? filterBtn : null, sortSel, dirBtn));
  root.append(h('div', { class: inline ? 'controls inline' : 'controls' }, tools), inline ? inlineBox : activeBox, h('div', { class: 'row-tools' }, count), body);
  refreshSummary();
  if (inline) drawInline();
  refreshList();
  return root;
}

const state = (key, sort) => (view[key] ??= newState(sort));
const fresh = (sort) => newState(sort);

/* ---------- skills ---------- */
const skillSorts = () => ({
  score: { label: 'Score', val: (s) => s.score, desc: true },
  jev: { label: 'Open JEV overall', val: (s) => s.jev, desc: true },
  name: { label: 'Name', val: (s) => s.name.toLowerCase() },
  repo: { label: 'Repository', val: (s) => repoName(s.repo).toLowerCase() },
  errors: { label: 'Validator errors', val: (s) => s.errors, desc: true },
  warnings: { label: 'Validator warnings', val: (s) => s.warnings, desc: true },
  tokens: { label: 'SKILL.md tokens', val: (s) => s.tokens, desc: true },
  other: { label: 'Other-file tokens', val: (s) => s.otherTokens, desc: true },
  words: { label: 'Word count', val: (s) => s.words, desc: true },
  techs: { label: 'Technology count', val: (s) => s.techs.length, desc: true },
  ...Object.fromEntries(Object.entries(JEV_LABELS).map(([k, label]) => [`dim_${k}`, { label: `JEV · ${label}`, val: (s) => s.dims?.[k], desc: true }])),
});

function skillFilters(skip = []) {
  const withSkills = () => data.technologies.filter((t) => t.skills > 0).sort((a, b) => a.name.localeCompare(b.name));
  const all = [
    { key: 'quality', label: 'Quality', type: 'chips', options: () => QUALITIES.map((q) => [q, cap(q)]), match: (s, v) => s.quality === v },
    { key: 'status', label: 'Validator', type: 'chips', options: () => ['pass', 'warn', 'fail'].map((v) => [v, cap(v)]), match: (s, v) => s.status === v },
    { key: 'contamination', label: 'Contamination', type: 'chips', options: () => ['none', 'low', 'medium', 'high'].filter((l) => data.skills.some((s) => s.contamination === l)).map((l) => [l, cap(l)]), match: (s, v) => s.contamination === v },
    { key: 'minScore', label: 'Min score', type: 'min', max: 100, step: 5, match: (s, n) => s.score >= n },
    { key: 'minJev', label: 'Min Open JEV', type: 'min', max: 5, step: 0.5, match: (s, n) => (s.jev ?? 0) >= n },
    { key: 'category', label: 'Category', type: 'pick', options: () => [...new Set(data.technologies.map((t) => t.category))].sort().map((c) => [c, c]), match: (s, v) => s.categories.includes(v) },
    { key: 'tech', label: 'Technology', type: 'pick', options: () => withSkills().map((t) => [t.id, t.name]), match: (s, v) => s.techs.includes(v) },
    { key: 'repo', label: 'Repository', type: 'pick', options: () => data.repositories.map((r) => [r.id, r.name]), match: (s, v) => s.repo === v },
  ];
  return all.filter((f) => !skip.includes(f.key));
}

function skillCard(s) {
  return h('li', null, h('a', { class: 'card', href: skillHref(s.id) },
    h('div', { class: 'card-head' },
      h('div', null, h('h3', null, s.name), h('div', { class: 'sub' }, repoName(s.repo))),
      ring(s.score, s.quality)),
    h('div', { class: 'tags' },
      badge(`q-${s.quality}`, s.quality),
      s.status && badge(`s-${s.status}`, `validator ${s.status}`),
      s.techs.slice(0, 4).map((t) => h('span', { class: 'tag' }, techName(t))),
      s.techs.length > 4 && h('span', { class: 'tag' }, `+${s.techs.length - 4}`))));
}

// Used on the home page and, scoped, on technology and repository pages.
function skillBrowser(items, st, skip = []) {
  return browser({
    items,
    state: st,
    noun: 'skill',
    placeholder: 'Search skills, repos, technologies…',
    searchText: (s) => `${s.name} ${s.id} ${repoName(s.repo)} ${s.techs.map(techName).join(' ')} ${s.categories.join(' ')}`,
    sorts: skillSorts(),
    filters: skillFilters(skip),
    render: (slice) => h('ul', { class: 'list cols' }, slice.map(skillCard)),
    empty: 'No skills match these filters.',
  });
}

function skillsPage() {
  const c = data.registry.counts;
  return [
    h('section', { class: 'hero' }, h('h1', null, data.registry.name), h('p', null, 'Every skill, technology and quality signal in the official registry.')),
    h('div', { class: 'stats' }, stat(c.skills, 'skills'), stat(c.technologies, 'technologies'), stat(c.repositories, 'repositories'), stat(c.mappings, 'mappings')),
    skillBrowser(data.skills, state('skills', 'score')),
  ];
}
const stat = (n, label) => h('div', { class: 'stat' }, h('b', null, num(n)), h('span', null, label));

/* ---------- technologies ---------- */
function technologiesPage() {
  const kinds = [...new Set(data.technologies.flatMap((t) => t.ruleKinds))].sort();
  return [h('h1', null, 'Technologies'), browser({
    items: data.technologies,
    state: state('techs', 'skills'),
    noun: 'technology',
    placeholder: 'Search technologies…',
    searchText: (t) => `${t.name} ${t.id} ${t.category} ${t.ruleKinds.join(' ')}`,
    sorts: {
      skills: { label: 'Skill count', val: (t) => t.skills, desc: true },
      name: { label: 'Name', val: (t) => t.name.toLowerCase() },
      category: { label: 'Category', val: (t) => t.category },
      rules: { label: 'Detection rules', val: (t) => t.rules, desc: true },
      avg: { label: 'Average skill score', val: (t) => t.avgScore, desc: true },
      issues: { label: 'Skill validator errors', val: (t) => t._issues, desc: true },
    },
    filters: [
      { key: 'has', label: 'Skills', type: 'chips', options: () => [['with', 'With skills'], ['without', 'Without skills']], match: (t, v) => (v === 'with') === t.skills > 0 },
      { key: 'category', label: 'Category', type: 'pick', options: () => [...new Set(data.technologies.map((t) => t.category))].sort().map((c) => [c, c]), match: (t, v) => t.category === v },
      { key: 'kind', label: 'Detection rule', type: 'chips', options: () => kinds.map((k) => [k, k]), match: (t, v) => t.ruleKinds.includes(v) },
      { key: 'meta', label: 'Type', type: 'chips', options: () => [['detect', 'Detectable'], ['meta', 'Metadata only']], match: (t, v) => (v === 'meta') === t.metadataOnly },
      { key: 'minSkills', label: 'Min skills', type: 'min', max: 30, match: (t, n) => t.skills >= n },
      { key: 'minAvg', label: 'Min avg score', type: 'min', max: 100, step: 5, match: (t, n) => (t.avgScore ?? 0) >= n },
    ],
    render: (slice) => h('ul', { class: 'list cols' }, slice.map((t) => h('li', null, h('a', { class: 'card', href: techHref(t.id) },
      h('div', { class: 'card-head' }, h('div', null, h('h3', null, t.name), h('div', { class: 'sub' }, t.id)), t.avgScore != null ? ring(Math.round(t.avgScore), qualityOf(t.avgScore)) : h('span', { class: 'tag' }, t.category)),
      h('div', { class: 'tags' }, badge(t.skills ? 'q-good' : 'q-fair', `${t.skills} skill${t.skills === 1 ? '' : 's'}`), h('span', { class: 'tag' }, t.category), h('span', { class: 'tag' }, `${t.rules} rule${t.rules === 1 ? '' : 's'}`)))))),
    empty: 'No technologies match these filters.',
  })];
}

async function techPage(id) {
  const t = techIndex().get(id);
  if (!t) return notFound('technology');
  const d = (await loadDetails()).technologies[id];
  const skills = data.skills.filter((s) => s.techs.includes(id));
  const skillById = new Map(data.skills.map((s) => [s.id, s]));
  const keys = [...new Set(d.rules.flatMap((r) => Object.keys(r)).filter((k) => k !== 'id' && k !== 'kind'))];
  const cell = (v) => (v == null ? '' : typeof v === 'object' ? JSON.stringify(v) : String(v));
  const recs = d.recommendations.map((r) => ({ ...r, id: r.skillId, name: skillById.get(r.skillId)?.name ?? r.skillId }));
  return [
    crumb('#/technologies', 'Technologies'),
    h('div', { class: 'detail-head' }, h('h1', null, t.name), t.avgScore != null && ring(Math.round(t.avgScore), qualityOf(t.avgScore), true)),
    h('div', { class: 'tags' }, h('span', { class: 'tag' }, t.category), t.metadataOnly && h('span', { class: 'tag' }, 'metadata only'), h('span', { class: 'tag' }, h('code', null, t.id))),
    h('h2', { style: 'margin-top:20px' }, `Skills (${skills.length})`),
    skills.length ? skillBrowser(skills, fresh('score'), ['tech']) : h('p', { class: 'empty' }, 'No skills are tagged with this technology.'),
    recs.length ? h('div', { class: 'panel' }, h('h2', null, `Recommended skills (${recs.length})`), browser({
      items: recs,
      state: fresh('name'),
      noun: 'recommendation',
      inline: true,
      searchText: (r) => `${r.name} ${r.skillId} ${r.reason} ${r.priority}`,
      sorts: {
        name: { label: 'Skill name', val: (r) => r.name.toLowerCase() },
        score: { label: 'Skill score', val: (r) => skillById.get(r.skillId)?.score, desc: true },
        priority: { label: 'Priority', val: (r) => r.priority },
      },
      filters: [
        { key: 'priority', label: 'Priority', type: 'chips', options: () => [...new Set(recs.map((r) => r.priority))].map((p) => [p, p]), match: (r, v) => r.priority === v },
        { key: 'cond', label: 'Condition', type: 'chips', options: () => [['yes', 'Conditional'], ['no', 'Always']], match: (r, v) => (v === 'yes') === Boolean(r.when) },
      ],
      render: (slice) => h('ul', { class: 'findings' }, slice.map((r) => h('li', null,
        h('span', null, skillById.has(r.skillId) ? link(skillHref(r.skillId), r.name) : r.skillId, ` · ${r.priority}`),
        h('span', { class: 'sub' }, r.reason), r.when && h('span', { class: 'sub' }, `when: ${JSON.stringify(r.when)}`)))),
    })) : null,
    h('div', { class: 'panel' }, h('h2', null, `Detection rules (${d.rules.length})`),
      d.rules.length ? browser({
        items: d.rules,
        state: fresh('kind'),
        noun: 'rule',
        inline: true,
        searchText: (r) => `${r.kind} ${keys.map((k) => cell(r[k])).join(' ')}`,
        sorts: { kind: { label: 'Kind', val: (r) => r.kind }, ...Object.fromEntries(keys.map((k) => [k, { label: cap(k), val: (r) => (r[k] == null ? null : cell(r[k]).toLowerCase()) }])) },
        filters: [{ key: 'kind', label: 'Kind', type: 'chips', options: () => [...new Set(d.rules.map((r) => r.kind))].map((k) => [k, k]), match: (r, v) => r.kind === v }],
        render: (slice) => h('div', { class: 'table-wrap' }, h('table', null, h('thead', null, h('tr', null, h('th', null, 'Kind'), keys.map((k) => h('th', null, k)))),
          h('tbody', null, slice.map((r) => h('tr', null, h('td', null, r.kind), keys.map((k) => h('td', null, cell(r[k])))))))),
      }) : h('p', { class: 'note' }, 'No rules; this technology is metadata only.')),
    h('details', { class: 'panel raw' }, h('summary', null, 'All raw data'), h('pre', null, JSON.stringify(d, null, 2))),
  ];
}

/* ---------- repositories ---------- */
function repositoriesPage() {
  return [h('h1', null, 'Repositories'), browser({
    items: data.repositories,
    state: state('repos', 'avg'),
    noun: 'repository',
    placeholder: 'Search repositories…',
    searchText: (r) => `${r.name} ${r.id} ${r.owner}`,
    sorts: {
      avg: { label: 'Average score', val: (r) => r.averageScore, desc: true },
      skills: { label: 'Skill count', val: (r) => r.skills, desc: true },
      name: { label: 'Name', val: (r) => r.name.toLowerCase() },
      owner: { label: 'Owner', val: (r) => r.owner.toLowerCase() },
      excellent: { label: 'Excellent skills', val: (r) => r._q.excellent, desc: true },
      poor: { label: 'Poor skills', val: (r) => r._q.poor, desc: true },
      errors: { label: 'Validator errors', val: (r) => r._errors, desc: true },
    },
    filters: [
      { key: 'has', label: 'Contains', type: 'chips', options: () => QUALITIES.map((q) => [q, `${cap(q)} skills`]), match: (r, v) => r._q[v] > 0 },
      { key: 'owner', label: 'Owner', type: 'pick', options: () => [...new Set(data.repositories.map((r) => r.owner))].sort().map((o) => [o, o]), match: (r, v) => r.owner === v },
      { key: 'minAvg', label: 'Min avg score', type: 'min', max: 100, step: 5, match: (r, n) => (r.averageScore ?? 0) >= n },
      { key: 'minSkills', label: 'Min skills', type: 'min', max: 50, match: (r, n) => r.skills >= n },
    ],
    render: (slice) => h('ul', { class: 'list cols' }, slice.map((r) => h('li', null, h('a', { class: 'card', href: repoHref(r.id) },
      h('div', { class: 'card-head' }, h('div', null, h('h3', null, r.name), h('div', { class: 'sub' }, `${r.skills} skill${r.skills === 1 ? '' : 's'}`)), r.averageScore != null && ring(Math.round(r.averageScore), qualityOf(r.averageScore))),
      h('div', { class: 'tags' }, QUALITIES.filter((q) => r._q[q]).map((q) => badge(`q-${q}`, `${r._q[q]} ${q}`))))))),
    empty: 'No repositories match these filters.',
  })];
}

function repositoryPage(id) {
  const r = repoIndex().get(id);
  if (!r) return notFound('repository');
  const skills = data.skills.filter((s) => s.repo === id);
  return [
    crumb('#/repositories', 'Repositories'),
    h('div', { class: 'detail-head' }, h('h1', null, r.name), r.averageScore != null && ring(Math.round(r.averageScore), qualityOf(r.averageScore), true)),
    h('div', { class: 'panel' }, h('dl', { class: 'kv' },
      h('dt', null, 'ID'), h('dd', null, h('code', null, r.id)),
      h('dt', null, 'Kind'), h('dd', null, r.kind),
      h('dt', null, 'Source'), h('dd', null, safeLink(r.uri)),
      h('dt', null, 'Skills'), h('dd', null, String(r.skills)),
      h('dt', null, 'Average score'), h('dd', null, r.averageScore != null ? String(r.averageScore) : '—'))),
    h('h2', null, 'Skills'),
    skillBrowser(skills, fresh('score'), ['repo']),
  ];
}

function safeLink(url, text) {
  return /^https?:\/\//i.test(url ?? '') ? h('a', { href: url, target: '_blank', rel: 'noopener noreferrer' }, text ?? url) : h('span', null, url ?? '—');
}
const crumb = (href, text) => h('a', { class: 'crumb', href }, `← ${text}`);
const notFound = (what) => [h('p', { class: 'empty' }, `That ${what} is not in this registry.`), link('#/', 'Back to skills', 'btn')];

/* ---------- skill detail ---------- */
async function skillPage(id) {
  const s = data.skills.find((x) => x.id === id);
  if (!s) return notFound('skill');
  const d = (await loadDetails()).skills[id] ?? {};
  const out = [
    crumb('#/', 'Skills'),
    h('div', { class: 'detail-head' }, h('div', null, h('h1', null, s.name), h('div', { class: 'sub' }, h('a', { href: repoHref(s.repo) }, repoName(s.repo)))), ring(s.score, s.quality, true)),
    h('div', { class: 'tags' }, badge(`q-${s.quality}`, s.quality), s.status && badge(`s-${s.status}`, `validator ${s.status}`), s.techs.map((t) => link(techHref(t), techName(t), 'tag'))),
  ];
  if (d.source?.url) out.push(h('p', null, safeLink(d.source.url, 'View SKILL.md on GitHub →')));
  out.push(h('div', { class: 'panel' }, h('h2', null, 'Source'), h('dl', { class: 'kv' },
    h('dt', null, 'ID'), h('dd', null, h('code', null, s.id)),
    h('dt', null, 'Repository'), h('dd', null, safeLink(d.source?.uri)),
    h('dt', null, 'Skill name'), h('dd', null, d.source?.skillName ?? s.name),
    h('dt', null, 'Path'), h('dd', null, h('code', null, d.source?.path ?? '—')),
    h('dt', null, 'Pinned ref'), h('dd', null, h('code', null, d.source?.ref ?? '—')),
    h('dt', null, 'Tree'), h('dd', null, h('code', null, short(d.source?.treeSha))),
    h('dt', null, 'Assessed'), h('dd', null, `${fmtDate(d.assessment?.assessedAt)} · ${d.assessment?.method ?? ''}`),
    h('dt', null, 'Assessor'), h('dd', null, d.assessment?.assessor ?? '—'))));
  if (d.jev) out.push(jevPanel(d.jev));
  if (d.validator) out.push(validatorPanel(d.validator));
  if (d.recommendedFor?.length) {
    out.push(h('div', { class: 'panel' }, h('h2', null, `Recommended for (${d.recommendedFor.length})`), browser({
      items: d.recommendedFor.map((r) => ({ ...r, id: r.mappingId, name: techName(r.technologyId) })),
      state: fresh('name'),
      noun: 'recommendation',
      inline: true,
      searchText: (r) => `${r.name} ${r.reason} ${r.priority}`,
      sorts: { name: { label: 'Technology', val: (r) => r.name.toLowerCase() }, priority: { label: 'Priority', val: (r) => r.priority } },
      filters: [
        { key: 'priority', label: 'Priority', type: 'chips', options: () => [...new Set(d.recommendedFor.map((r) => r.priority))].map((p) => [p, p]), match: (r, v) => r.priority === v },
        { key: 'cond', label: 'Condition', type: 'chips', options: () => [['yes', 'Conditional'], ['no', 'Always']], match: (r, v) => (v === 'yes') === Boolean(r.when) },
      ],
      render: (slice) => h('ul', { class: 'findings' }, slice.map((r) => h('li', null, h('span', null, link(techHref(r.technologyId), r.name), ` · ${r.priority}`), h('span', { class: 'sub' }, r.reason),
        r.when && h('span', { class: 'sub' }, `when: ${JSON.stringify(r.when)}`)))),
    })));
  }
  out.push(h('details', { class: 'panel raw' }, h('summary', null, 'All raw data'), h('pre', null, JSON.stringify({ summary: s, ...d }, null, 2))));
  return out;
}

function jevPanel(j) {
  const rows = Object.entries(j.scores);
  return h('div', { class: 'panel' }, h('h2', null, `Open JEV score · ${j.overall ?? '–'} / 5`),
    h('div', { class: 'bars' }, rows.map(([k, v]) => h('div', null,
      h('div', { class: 'bar-top' }, h('span', null, JEV_LABELS[k] ?? k), h('span', null, `${v.score} / 5 · ${Math.round((v.confidence ?? 0) * 100)}% confidence`)),
      h('div', { class: 'bar', role: 'img', 'aria-label': `${JEV_LABELS[k] ?? k} ${v.score} of 5` }, h('i', { style: `width:${(v.score / 5) * 100}%` }))))),
    h('p', { class: 'note' }, `Evaluated ${fmtDate(j.evaluatedAt)}. Only the first 4,000 characters of SKILL.md are reviewed; scores are model-generated and low-confidence.`));
}

function validatorPanel(v) {
  const m = v.metrics ?? {};
  const kv = [];
  const add = (k, val) => val != null && kv.push(h('dt', null, k), h('dd', null, String(val)));
  add('Status', v.status);
  add('Errors / warnings / info', `${v.counts?.errors ?? 0} / ${v.counts?.warnings ?? 0} / ${v.counts?.infos ?? 0}`);
  add('Tokens (SKILL.md)', m.tokens && num(m.tokens.skill_md));
  add('Tokens (references)', m.tokens && num(m.tokens.references));
  add('Tokens (other files)', m.tokens && num(m.tokens.other));
  add('Words', m.content && num(m.content.word_count));
  add('Sections', m.content?.section_count);
  add('Code blocks', m.content?.code_block_count);
  add('Code languages', m.content?.code_languages?.join(', ') || null);
  add('Information density', m.content?.information_density);
  add('Instruction specificity', m.content?.instruction_specificity);
  add('Contamination', m.contamination && `${m.contamination.level} (${m.contamination.score})`);
  add('Primary category', m.contamination?.primary_category);
  add('Checks', v.checks?.join(', '));
  add('Validator', `${v.version ?? ''} · ${fmtDate(v.evaluatedAt)}`);
  const findings = (v.findings ?? []).map((f, i) => ({ ...f, id: String(i), name: f.message }));
  return h('div', { class: 'panel' }, h('h2', null, `Validator · ${findings.length} finding${findings.length === 1 ? '' : 's'}`), h('dl', { class: 'kv' }, kv),
    findings.length ? browser({
      items: findings,
      state: fresh('severity'),
      noun: 'finding',
      inline: true,
      searchText: (f) => `${f.level} ${f.category} ${f.message}`,
      sorts: {
        severity: { label: 'Severity', val: (f) => LEVEL_RANK[f.level] ?? 0, desc: true },
        category: { label: 'Category', val: (f) => f.category },
        length: { label: 'Message length', val: (f) => f.message.length, desc: true },
      },
      filters: [
        { key: 'level', label: 'Level', type: 'chips', options: () => ['error', 'warning', 'info'].filter((l) => findings.some((f) => f.level === l)).map((l) => [l, cap(l)]), match: (f, val) => f.level === val },
        { key: 'category', label: 'Category', type: 'chips', options: () => [...new Set(findings.map((f) => f.category))].sort().map((c) => [c, c]), match: (f, val) => f.category === val },
      ],
      render: (slice) => h('ul', { class: 'findings' }, slice.map((f) => h('li', { class: f.level }, h('small', null, `${f.level} · ${f.category}`), h('span', null, f.message)))),
    }) : h('p', { class: 'note' }, 'No findings.'));
}

function aboutPage() {
  const r = data.registry;
  const row = (k, v) => [h('dt', null, k), h('dd', null, v)];
  return [
    h('h1', null, 'About this registry'),
    h('div', { class: 'panel' }, h('dl', { class: 'kv' },
      row('Registry', r.name), row('ID', h('code', null, r.id)),
      row('Source', r.sourceRepo ? safeLink(r.sourceRepo) : '—'),
      row('Revision', r.commit ? h('code', null, short(r.commit)) : '—'),
      row('Site built', fmtDate(r.builtAt)),
      row('Generator', r.generator ?? '—'))),
    h('div', { class: 'panel' }, h('h2', null, 'Quality labels'), h('div', { class: 'tags' }, QUALITIES.map((q) => badge(`q-${q}`, `${q} · ${r.quality[q]}`))),
      h('p', { class: 'note' }, 'Each skill is labelled from the structural validator and the Open JEV score. Scores run 0–100.')),
    h('div', { class: 'panel' }, h('h2', null, 'skill-validator'), h('dl', { class: 'kv' },
      row('Version', r.validator.version ?? '—'), row('Checks', (r.validator.checks ?? []).join(', ')), row('Generated', fmtDate(r.validator.generatedAt)),
      Object.entries(r.validator.byStatus).flatMap(([k, v]) => row(k, String(v))))),
    h('div', { class: 'panel' }, h('h2', null, 'Open JEV'), h('dl', { class: 'kv' },
      row('Provider', r.jev.provider ?? '—'), row('Model', r.jev.modelRelease ?? '—'), row('Generated', fmtDate(r.jev.generatedAt))),
      r.jev.scope && h('p', { class: 'note' }, r.jev.scope),
      r.jev.questions && h('details', { class: 'raw' }, h('summary', null, 'Rubric'), h('pre', null, JSON.stringify(r.jev.questions, null, 2)))),
    h('div', { class: 'panel' }, h('h2', null, 'Registry files'), h('dl', { class: 'kv' },
      Object.entries(r.files ?? {}).flatMap(([k, v]) => row(k, h('code', null, short(v.sha256)))))),
  ];
}

/* ---------- router ---------- */
async function route() {
  const path = location.hash.replace(/^#\/?/, '');
  const [kind, ...rest] = path.split('/');
  const id = rest.length ? decodeURIComponent(rest.join('/')) : '';
  const tab = { tech: 'technologies', technologies: 'technologies', repo: 'repositories', repositories: 'repositories', about: 'about' }[kind] ?? 'skills';
  for (const a of document.querySelectorAll('#tabs a')) {
    if (a.dataset.tab === tab) a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
  }
  try {
    let nodes;
    if (kind === 'skill') nodes = await skillPage(id);
    else if (kind === 'tech') nodes = await techPage(id);
    else if (kind === 'repo') nodes = repositoryPage(id);
    else if (kind === 'technologies') nodes = technologiesPage();
    else if (kind === 'repositories') nodes = repositoriesPage();
    else if (kind === 'about') nodes = aboutPage();
    else nodes = skillsPage();
    mount(...nodes.filter(Boolean));
    document.title = `${kind === 'skill' || kind === 'tech' || kind === 'repo' ? `${id.split(':').pop()} · ` : ''}Nosey Registry`;
  } catch (err) {
    mount(h('p', { class: 'empty' }, `Something went wrong: ${err.message}`));
  }
}

try {
  data = await (await fetch('data.json')).json();
  prepare();
  addEventListener('hashchange', route);
  await route();
} catch (err) {
  app.replaceChildren(h('p', { class: 'loading' }, `Could not load registry data: ${err.message}`));
}
