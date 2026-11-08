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

let data;
let detailsPromise;
const view = { skills: { q: '', quality: '', status: '', sort: 'score', shown: PAGE }, techs: { q: '', category: '', shown: PAGE }, repos: { q: '', shown: PAGE } };

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
const fill = (el, ...kids) => el.replaceChildren(...kids.filter(Boolean));
const link = (href, text, cls) => h('a', { href, class: cls }, text);
const enc = encodeURIComponent;
const skillHref = (id) => `#/skill/${enc(id)}`;
const techHref = (id) => `#/tech/${enc(id)}`;
const repoHref = (id) => `#/repo/${enc(id)}`;
const fmtDate = (s) => (s ? new Date(s).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' }) : '—');
const num = (n) => new Intl.NumberFormat().format(n);
const badge = (cls, text) => h('span', { class: `badge ${cls}` }, text);
const ring = (score, quality, large) => h('div', { class: `ring ${quality ?? ''} ${large ? 'lg' : ''}`, style: `--v:${score ?? 0}`, role: 'img', 'aria-label': `Score ${score ?? 'n/a'} out of 100` }, h('span', null, score ?? '–'));
const short = (s) => (s ? s.slice(0, 7) : '—');

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

/* ---------- shared pieces ---------- */
function searchBox(state, placeholder, rerender) {
  const input = h('input', { class: 'search', type: 'search', placeholder, 'aria-label': placeholder, value: state.q, autocomplete: 'off', enterkeyhint: 'search' });
  input.addEventListener('input', () => {
    state.q = input.value;
    state.shown = PAGE;
    rerender();
  });
  return input;
}

function chipRow(options, current, onPick, label) {
  return h('div', { class: 'chips', role: 'group', 'aria-label': label },
    options.map(([value, text]) => h('button', { class: 'chip', type: 'button', 'aria-pressed': String(current === value), onclick: () => onPick(current === value ? '' : value) }, text)));
}

function showMore(state, total, rerender) {
  if (state.shown >= total) return null;
  return h('button', { class: 'more', type: 'button', onclick: () => { state.shown += PAGE * 2; rerender(); } }, `Show more (${num(total - state.shown)} left)`);
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

const repoIndex = () => (data._repos ??= new Map(data.repositories.map((r) => [r.id, r])));
const techIndex = () => (data._techs ??= new Map(data.technologies.map((t) => [t.id, t])));
const repoName = (id) => repoIndex().get(id)?.name ?? id;
const techName = (id) => techIndex().get(id)?.name ?? id;

function skillMatches(s, q) {
  if (!q) return true;
  const hay = `${s.name} ${s.id} ${repoName(s.repo)} ${s.techs.map(techName).join(' ')}`.toLowerCase();
  return q.toLowerCase().split(/\s+/).every((w) => hay.includes(w));
}

function skillList(items, state, empty) {
  const box = h('div');
  const draw = () => {
    const shown = items.slice(0, state.shown);
    fill(box, 
      shown.length ? h('ul', { class: 'list cols' }, shown.map(skillCard)) : h('p', { class: 'empty' }, empty),
      showMore(state, items.length, draw));
  };
  draw();
  return box;
}

/* ---------- pages ---------- */
function skillsPage() {
  const st = view.skills;
  const c = data.registry.counts;
  const list = h('div');
  const count = h('span');
  const draw = () => {
    const items = data.skills.filter((s) => (!st.quality || s.quality === st.quality) && (!st.status || s.status === st.status) && skillMatches(s, st.q));
    items.sort(sorters[st.sort]);
    count.textContent = `${num(items.length)} skill${items.length === 1 ? '' : 's'}`;
    fill(list, skillList(items, st, 'No skills match these filters.'));
  };
  const sorters = {
    score: (a, b) => b.score - a.score || a.name.localeCompare(b.name),
    name: (a, b) => a.name.localeCompare(b.name),
    issues: (a, b) => b.errors - a.errors || b.warnings - a.warnings,
  };
  const sortSel = h('select', { 'aria-label': 'Sort skills', onchange: (e) => { st.sort = e.target.value; draw(); } },
    [['score', 'Highest score'], ['name', 'Name A–Z'], ['issues', 'Most validator issues']].map(([v, t]) => h('option', { value: v, selected: st.sort === v }, t)));
  const chips = h('div');
  const drawChips = () => fill(chips, 
    chipRow(QUALITIES.map((q) => [q, `${q[0].toUpperCase()}${q.slice(1)} · ${data.registry.quality[q]}`]), st.quality, (v) => { st.quality = v; st.shown = PAGE; drawChips(); draw(); }, 'Filter by quality'),
    chipRow([['pass', 'Validator pass'], ['warn', 'Validator warn'], ['fail', 'Validator fail']], st.status, (v) => { st.status = v; st.shown = PAGE; drawChips(); draw(); }, 'Filter by validator status'));
  drawChips();
  draw();
  return [
    h('section', { class: 'hero' }, h('h1', null, data.registry.name), h('p', null, 'Every skill, technology and quality signal in the official registry.')),
    h('div', { class: 'stats' },
      stat(c.skills, 'skills'), stat(c.technologies, 'technologies'), stat(c.repositories, 'repositories'), stat(c.mappings, 'mappings')),
    h('div', { class: 'controls' }, searchBox(st, 'Search skills, repos, technologies…', draw), chips),
    h('div', { class: 'row-tools' }, count, sortSel),
    list,
  ];
}
const stat = (n, label) => h('div', { class: 'stat' }, h('b', null, num(n)), h('span', null, label));

function technologiesPage() {
  const st = view.techs;
  const cats = [...new Set(data.technologies.map((t) => t.category))].sort();
  const list = h('div');
  const count = h('span');
  const draw = () => {
    const q = st.q.toLowerCase();
    const items = data.technologies
      .filter((t) => (!st.category || t.category === st.category) && (!q || `${t.name} ${t.id} ${t.category}`.toLowerCase().includes(q)))
      .sort((a, b) => b.skills - a.skills || a.name.localeCompare(b.name));
    count.textContent = `${num(items.length)} technologies`;
    const shown = items.slice(0, st.shown);
    fill(list, 
      shown.length ? h('ul', { class: 'list cols' }, shown.map((t) => h('li', null, h('a', { class: 'card', href: techHref(t.id) },
        h('div', { class: 'card-head' }, h('div', null, h('h3', null, t.name), h('div', { class: 'sub' }, t.id)), h('span', { class: 'tag' }, t.category)),
        h('div', { class: 'tags' }, badge(t.skills ? 'q-good' : 'q-fair', `${t.skills} skill${t.skills === 1 ? '' : 's'}`), h('span', { class: 'tag' }, `${t.rules} detection rule${t.rules === 1 ? '' : 's'}`)))))) : h('p', { class: 'empty' }, 'No technologies match.'),
      showMore(st, items.length, draw));
  };
  const chips = h('div');
  const drawChips = () => fill(chips, chipRow(cats.map((c) => [c, c]), st.category, (v) => { st.category = v; st.shown = PAGE; drawChips(); draw(); }, 'Filter by category'));
  drawChips();
  draw();
  return [h('h1', null, 'Technologies'), h('div', { class: 'controls' }, searchBox(st, 'Search technologies…', draw), chips), h('div', { class: 'row-tools' }, count), list];
}

function repositoriesPage() {
  const st = view.repos;
  const list = h('div');
  const draw = () => {
    const q = st.q.toLowerCase();
    const items = data.repositories.filter((r) => !q || `${r.name} ${r.id}`.toLowerCase().includes(q)).sort((a, b) => (b.averageScore ?? -1) - (a.averageScore ?? -1) || b.skills - a.skills);
    const shown = items.slice(0, st.shown);
    fill(list, 
      shown.length ? h('ul', { class: 'list cols' }, shown.map((r) => h('li', null, h('a', { class: 'card', href: repoHref(r.id) },
        h('div', { class: 'card-head' }, h('div', null, h('h3', null, r.name), h('div', { class: 'sub' }, `${r.skills} skill${r.skills === 1 ? '' : 's'}`)), r.averageScore != null && ring(Math.round(r.averageScore), qualityOf(r.averageScore))))))) : h('p', { class: 'empty' }, 'No repositories match.'),
      showMore(st, items.length, draw));
  };
  draw();
  return [h('h1', null, 'Repositories'), h('div', { class: 'controls' }, searchBox(st, 'Search repositories…', draw)), list];
}
const qualityOf = (score) => (score >= 85 ? 'excellent' : score >= 70 ? 'good' : score >= 50 ? 'fair' : 'poor');

function repositoryPage(id) {
  const r = repoIndex().get(id);
  if (!r) return notFound('repository');
  const st = { shown: PAGE };
  const skills = data.skills.filter((s) => s.repo === id).sort((a, b) => b.score - a.score);
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
    skillList(skills, st, 'No skills.'),
  ];
}

function safeLink(url, text) {
  return /^https?:\/\//i.test(url ?? '') ? h('a', { href: url, target: '_blank', rel: 'noopener noreferrer' }, text ?? url) : h('span', null, url ?? '—');
}
const crumb = (href, text) => h('a', { class: 'crumb', href }, `← ${text}`);
const notFound = (what) => [h('p', { class: 'empty' }, `That ${what} is not in this registry.`), link('#/', 'Back to skills', 'btn')];

async function skillPage(id) {
  const s = data.skills.find((x) => x.id === id);
  if (!s) return notFound('skill');
  const details = (await loadDetails()).skills[id];
  const d = details ?? {};
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
    out.push(h('div', { class: 'panel' }, h('h2', null, 'Recommended for'), h('ul', { class: 'findings' },
      d.recommendedFor.map((r) => h('li', null, h('span', null, link(techHref(r.technologyId), techName(r.technologyId)), ` · ${r.priority}`), h('span', { class: 'sub' }, r.reason),
        r.when && h('span', { class: 'sub' }, `when: ${JSON.stringify(r.when)}`))))));
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
  return h('div', { class: 'panel' }, h('h2', null, 'Validator'), h('dl', { class: 'kv' }, kv),
    v.findings?.length ? h('ul', { class: 'findings' }, v.findings.map((f) => h('li', { class: f.level }, h('small', null, `${f.level} · ${f.category}`), h('span', null, f.message)))) : h('p', { class: 'note' }, 'No findings.'));
}

async function techPage(id) {
  const t = techIndex().get(id);
  if (!t) return notFound('technology');
  const d = (await loadDetails()).technologies[id];
  const skills = data.skills.filter((s) => s.techs.includes(id)).sort((a, b) => b.score - a.score);
  const recs = new Map();
  for (const r of d.recommendations) recs.set(r.skillId, r);
  const skillById = new Map(data.skills.map((s) => [s.id, s]));
  const keys = [...new Set(d.rules.flatMap((r) => Object.keys(r)).filter((k) => k !== 'id' && k !== 'kind'))];
  return [
    crumb('#/technologies', 'Technologies'),
    h('h1', null, t.name),
    h('div', { class: 'tags' }, h('span', { class: 'tag' }, t.category), t.metadataOnly && h('span', { class: 'tag' }, 'metadata only'), h('span', { class: 'tag' }, h('code', null, t.id))),
    h('h2', { style: 'margin-top:20px' }, `Skills (${skills.length})`),
    skillList(skills, { shown: PAGE }, 'No skills are tagged with this technology.'),
    recs.size ? h('div', { class: 'panel' }, h('h2', null, `Recommended skills (${recs.size})`), h('ul', { class: 'findings' },
      [...recs.values()].map((r) => h('li', null, h('span', null, skillById.has(r.skillId) ? link(skillHref(r.skillId), skillById.get(r.skillId).name) : r.skillId, ` · ${r.priority}`), h('span', { class: 'sub' }, r.reason), r.when && h('span', { class: 'sub' }, `when: ${JSON.stringify(r.when)}`))))) : null,
    h('div', { class: 'panel' }, h('h2', null, `Detection rules (${d.rules.length})`),
      d.rules.length ? h('div', { class: 'table-wrap' }, h('table', null, h('thead', null, h('tr', null, h('th', null, 'Kind'), keys.map((k) => h('th', null, k)))),
        h('tbody', null, d.rules.map((r) => h('tr', null, h('td', null, r.kind), keys.map((k) => h('td', null, r[k] == null ? '' : typeof r[k] === 'object' ? JSON.stringify(r[k]) : String(r[k])))))))) : h('p', { class: 'note' }, 'No rules; this technology is metadata only.')),
    h('details', { class: 'panel raw' }, h('summary', null, 'All raw data'), h('pre', null, JSON.stringify(d, null, 2))),
  ];
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
  addEventListener('hashchange', route);
  await route();
} catch (err) {
  app.replaceChildren(h('p', { class: 'loading' }, `Could not load registry data: ${err.message}`));
}
