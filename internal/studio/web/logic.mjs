// Pure editor logic: everything here is free of DOM and network access so it
// can be unit-tested under Node.

export const PRIORITIES = ['core', 'recommended', 'optional'];
export const QUALITIES = ['excellent', 'good', 'fair', 'poor'];
export const ECOSYSTEMS = ['npm', 'python', 'cargo', 'go', 'ruby', 'composer', 'docker', 'terraform', 'terraform.resource', 'githubAction', 'deno', 'hex'];
export const ROLE_LABELS = { technologies: 'Technologies', skills: 'Skills', mappings: 'Mappings', repositories: 'Repositories' };
const TEMPLATED_REASON = /^Catalog (recommendation|combination)/;

export const clone = (value) => (value === undefined ? undefined : JSON.parse(JSON.stringify(value)));
export const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
export const slug = (text) => text.toLowerCase().replace(/[^a-z0-9._-]+/g, '-').replace(/^-+|-+$/g, '');

/** Rule kinds and the exact, schema-defined fields each one may carry. */
export const RULE_KINDS = {
  file: { label: 'File path', choices: [['glob', 'Glob'], ['pathRegex', 'Path regex']], fields: ['pathScope'] },
  dependency: { label: 'Package dependency', choices: [['name', 'Exact name'], ['nameRegex', 'Name regex']], fields: ['ecosystem'] },
  content: { label: 'File content', choices: [['contains', 'Contains text'], ['regex', 'Regex']], fields: ['glob', 'regexFlags'] },
  extension: { label: 'File extension', choices: [], fields: ['extension'] },
  dotenv: { label: 'Dotenv key', choices: [['key', 'Exact key'], ['keyRegex', 'Key regex']], fields: ['glob'] },
  detector: { label: 'Trusted detector', choices: [], fields: ['detector', 'args'] },
  mcp_package: { label: 'MCP package', choices: [], fields: ['name'] },
};

/** The matcher field in use on a rule, for kinds offering a choice. */
export function ruleChoice(rule) {
  const options = RULE_KINDS[rule.kind]?.choices ?? [];
  return options.find(([field]) => rule[field] !== undefined)?.[0] ?? options[0]?.[0] ?? null;
}

/** Strips every field the schema forbids for the rule's kind, preserving id-first order. */
export function normalizeRule(rule, choice = ruleChoice(rule)) {
  const kind = RULE_KINDS[rule.kind];
  const out = { id: rule.id, kind: rule.kind };
  if (rule.description) out.description = rule.description;
  if (rule.disabled) out.disabled = true;
  const keep = (field) => {
    const value = rule[field];
    if (value !== undefined && value !== '' && value !== null) out[field] = value;
  };
  if (rule.kind === 'content' || rule.kind === 'dotenv') keep('glob');
  if (rule.kind === 'dependency') keep('ecosystem');
  if (choice) keep(choice);
  for (const field of kind.fields) if (!(field in out) && field !== 'regexFlags' && field !== 'pathScope') keep(field);
  if (rule.kind === 'file' && rule.pathScope && (choice === 'pathRegex' || rule.glob !== undefined)) keep('pathScope');
  if (rule.kind === 'content' && choice === 'regex') keep('regexFlags');
  return out;
}

export function nextRuleId(rules, kind) {
  const used = new Set(rules.map((rule) => rule.id));
  for (let n = 1; ; n += 1) {
    const id = `${kind.replace(/_/g, '-')}-${String(n).padStart(4, '0')}`;
    if (!used.has(id)) return id;
  }
}

export function ruleSummary(rule) {
  const matcher = rule.glob ?? rule.pathRegex ?? rule.name ?? rule.nameRegex ?? rule.extension ?? rule.key ?? rule.keyRegex ?? rule.detector ?? '';
  const text = rule.contains ?? rule.regex;
  return [rule.ecosystem, matcher, text && `→ ${text}`].filter(Boolean).join(' ');
}

/** Whether a rule can run in a default scan, which the tester must report honestly. */
export function ruleNeedsSetup(rule) {
  if (rule.kind === 'detector') return 'Needs a trusted adapter in settings; a default scan reports it as not evaluated.';
  if (rule.kind === 'mcp_package') return 'Needs MCP inspection opted in; a default scan reports it as not evaluated.';
  return null;
}

/** Applies draft ops over the on-disk documents. */
export function buildModel(documents, ops) {
  const model = {};
  for (const role of Object.keys(documents)) model[role] = new Map(documents[role].map((entity) => [entity.id, entity]));
  for (const op of ops) {
    if (!model[op.role]) continue;
    if (op.entity === null) model[op.role].delete(op.id);
    else model[op.role].set(op.id, op.entity);
  }
  for (const role of ['technologies', 'skills', 'mappings', 'repositories']) model[role] ??= new Map();
  return model;
}

export function opFor(ops, role, id) {
  const op = ops.find((candidate) => candidate.role === role && candidate.id === id);
  if (!op) return null;
  if (op.entity === null) return 'deleted';
  return op.base === null ? 'new' : 'edited';
}

const whenTechnologies = (rec) => [...(rec.when?.allTechnologies ?? []), ...(rec.when?.anyTechnologies ?? []), ...(rec.when?.noneTechnologies ?? [])];

export const mappingsForTechnology = (model, id) => [...model.mappings.values()].filter((m) => m.technologyId === id);

export function recommendationsForSkill(model, skillId) {
  const found = [];
  for (const mapping of model.mappings.values()) {
    for (const rec of mapping.recommendations) if (rec.skillId === skillId) found.push({ mapping, rec });
  }
  return found;
}

/** Every entity that names role/id, so destructive edits can show their reach. */
export function references(model, role, id) {
  const found = [];
  if (role === 'technologies') {
    for (const skill of model.skills.values()) if ((skill.technologies ?? []).includes(id)) found.push({ role: 'skills', id: skill.id, field: 'technologies' });
    for (const mapping of model.mappings.values()) {
      if (mapping.technologyId === id) found.push({ role: 'mappings', id: mapping.id, field: 'technologyId' });
      else if (mapping.recommendations.some((rec) => whenTechnologies(rec).includes(id))) found.push({ role: 'mappings', id: mapping.id, field: 'when' });
    }
  }
  if (role === 'skills') {
    for (const skill of model.skills.values()) {
      if (skill.id === id) continue;
      if ((skill.requires ?? []).includes(id)) found.push({ role: 'skills', id: skill.id, field: 'requires' });
      if ((skill.conflictsWith ?? []).includes(id)) found.push({ role: 'skills', id: skill.id, field: 'conflictsWith' });
    }
    for (const mapping of model.mappings.values()) {
      if (mapping.recommendations.some((rec) => rec.skillId === id || (rec.requires ?? []).includes(id) || (rec.conflictsWith ?? []).includes(id))) found.push({ role: 'mappings', id: mapping.id, field: 'recommendations' });
    }
  }
  if (role === 'repositories') {
    for (const skill of model.skills.values()) if (skill.repositoryId === id) found.push({ role: 'skills', id: skill.id, field: 'repositoryId' });
  }
  return found;
}

const without = (list, id) => (list ?? []).filter((value) => value !== id);
const setOrDrop = (target, key, list) => { if (list.length) target[key] = list; else delete target[key]; };

function stripRecommendation(rec, skillId) {
  const next = clone(rec);
  setOrDrop(next, 'requires', without(next.requires, skillId));
  setOrDrop(next, 'conflictsWith', without(next.conflictsWith, skillId));
  return next;
}

/** Ops removing an entity and every reference to it. */
export function cascadeDelete(model, role, id) {
  const ops = [{ role, id, delete: true }];
  if (role === 'skills') {
    for (const skill of model.skills.values()) {
      if (skill.id === id || !((skill.requires ?? []).includes(id) || (skill.conflictsWith ?? []).includes(id))) continue;
      const next = clone(skill);
      setOrDrop(next, 'requires', without(next.requires, id));
      setOrDrop(next, 'conflictsWith', without(next.conflictsWith, id));
      ops.push({ role, id: skill.id, entity: next });
    }
    for (const mapping of model.mappings.values()) {
      if (!references(model, 'skills', id).some((ref) => ref.role === 'mappings' && ref.id === mapping.id)) continue;
      const recommendations = mapping.recommendations.filter((rec) => rec.skillId !== id).map((rec) => stripRecommendation(rec, id));
      ops.push(recommendations.length ? { role: 'mappings', id: mapping.id, entity: { ...clone(mapping), recommendations } } : { role: 'mappings', id: mapping.id, delete: true });
    }
  }
  if (role === 'technologies') {
    for (const skill of model.skills.values()) {
      if ((skill.technologies ?? []).includes(id)) ops.push({ role: 'skills', id: skill.id, entity: { ...clone(skill), technologies: without(skill.technologies, id) } });
    }
    for (const mapping of model.mappings.values()) {
      if (mapping.technologyId === id) { ops.push({ role: 'mappings', id: mapping.id, delete: true }); continue; }
      if (!mapping.recommendations.some((rec) => whenTechnologies(rec).includes(id))) continue;
      const recommendations = mapping.recommendations.filter((rec) => !whenTechnologies(rec).includes(id));
      ops.push(recommendations.length ? { role: 'mappings', id: mapping.id, entity: { ...clone(mapping), recommendations } } : { role: 'mappings', id: mapping.id, delete: true });
    }
  }
  return ops;
}

const swap = (list, from, to) => (list ?? []).map((value) => (value === from ? to : value));

function renameInWhen(rec, from, to) {
  if (!rec.when) return rec;
  const when = {};
  for (const [key, list] of Object.entries(rec.when)) when[key] = swap(list, from, to);
  return { ...rec, when };
}

/** Ops renaming a technology and rewriting everything that references it. */
export function renameTechnology(model, from, to) {
  const ops = [{ role: 'technologies', id: from, delete: true }, { role: 'technologies', id: to, entity: { ...clone(model.technologies.get(from)), id: to } }];
  for (const skill of model.skills.values()) {
    if ((skill.technologies ?? []).includes(from)) ops.push({ role: 'skills', id: skill.id, entity: { ...clone(skill), technologies: swap(skill.technologies, from, to) } });
  }
  for (const mapping of model.mappings.values()) {
    const touches = mapping.technologyId === from || mapping.recommendations.some((rec) => whenTechnologies(rec).includes(from));
    if (!touches) continue;
    const next = { ...clone(mapping), technologyId: mapping.technologyId === from ? to : mapping.technologyId, recommendations: mapping.recommendations.map((rec) => renameInWhen(clone(rec), from, to)) };
    if (mapping.id === `technology:${from}`) {
      next.id = `technology:${to}`;
      ops.push({ role: 'mappings', id: mapping.id, delete: true });
    }
    ops.push({ role: 'mappings', id: next.id, entity: next });
  }
  return ops;
}

/** Builds a recommendation with the schema's field order, dropping blanks. */
export function cleanRecommendation(rec) {
  const out = { skillId: rec.skillId, priority: rec.priority || 'optional' };
  if (rec.reason?.trim()) out.reason = rec.reason.trim();
  const when = {};
  for (const key of ['allTechnologies', 'anyTechnologies', 'noneTechnologies']) if (rec.when?.[key]?.length) when[key] = rec.when[key];
  if (Object.keys(when).length) out.when = when;
  if (rec.requires?.length) out.requires = rec.requires;
  if (rec.conflictsWith?.length) out.conflictsWith = rec.conflictsWith;
  return out;
}

/** Ops for a skill edit, writing conflictsWith on both sides as the loader requires. */
export function skillEditOps(model, skill) {
  const previous = model.skills.get(skill.id);
  const before = new Set(previous?.conflictsWith ?? []);
  const after = new Set(skill.conflictsWith ?? []);
  const ops = [{ role: 'skills', id: skill.id, entity: skill }];
  const adjust = (otherId, change) => {
    const other = model.skills.get(otherId);
    if (!other) return;
    const next = clone(other);
    setOrDrop(next, 'conflictsWith', change(next.conflictsWith ?? []));
    ops.push({ role: 'skills', id: otherId, entity: next });
  };
  for (const id of after) if (!before.has(id)) adjust(id, (list) => (list.includes(skill.id) ? list : [...list, skill.id]));
  for (const id of before) if (!after.has(id)) adjust(id, (list) => without(list, skill.id));
  return ops;
}

export const repositoryIdFor = (repository) => repository.toLowerCase().replace('/', ':');
export const skillIdFor = (repository, name) => `${repository.replace('/', ':')}:${name}`;

/** Ops adding one upstream skill with its repository and optional mappings. */
export function addSkillOps(model, { repository, uri, commit, path, name, description, technologies, priority, reason, map }) {
  const repositoryId = repositoryIdFor(repository);
  const skillId = skillIdFor(repository, name);
  const ops = [];
  if (!model.repositories.has(repositoryId)) ops.push({ role: 'repositories', id: repositoryId, entity: { id: repositoryId, name: repository, kind: 'git', uri } });
  const skill = { id: skillId, name };
  if (description) skill.description = description;
  skill.technologies = technologies;
  skill.source = { kind: 'git', uri, ref: commit, skillName: name };
  skill.repositoryId = repositoryId;
  ops.push({ role: 'skills', id: skillId, entity: skill, location: { commit, path } });
  if (!map) return ops;
  for (const technologyId of technologies) {
    const mappingId = `technology:${technologyId}`;
    const rec = cleanRecommendation({ skillId, priority, reason });
    const existing = model.mappings.get(mappingId);
    const entity = existing ? { ...clone(existing), recommendations: [...existing.recommendations, rec] } : { id: mappingId, technologyId, recommendations: [rec] };
    ops.push({ role: 'mappings', id: mappingId, entity });
  }
  return ops;
}

/** Bulk edits over selected mappings; returns ops only for entities that changed. */
export function bulkEdit(model, mappingIds, { priority, reason }) {
  const ops = [];
  for (const id of mappingIds) {
    const mapping = model.mappings.get(id);
    if (!mapping) continue;
    const recommendations = mapping.recommendations.map((rec) => {
      const next = clone(rec);
      if (priority) next.priority = priority;
      if (reason && (!next.reason || TEMPLATED_REASON.test(next.reason))) next.reason = reason;
      return next;
    });
    if (!same(recommendations, mapping.recommendations)) ops.push({ role: 'mappings', id, entity: { ...clone(mapping), recommendations } });
  }
  return ops;
}

/** Skills whose declared technology has no mapping, matched by name otherwise. */
export function suggestSkills(model, technology, limit = 12) {
  const needle = slug(technology.name || technology.id);
  const mapped = new Set(mappingsForTechnology(model, technology.id).flatMap((m) => m.recommendations.map((rec) => rec.skillId)));
  return [...model.skills.values()]
    .filter((skill) => !mapped.has(skill.id) && (skill.technologies?.includes(technology.id) || (needle.length > 2 && slug(`${skill.name} ${skill.id}`).includes(needle))))
    .slice(0, limit);
}

export function hrefFor(role, id) {
  const encoded = encodeURIComponent(id);
  if (role === 'technologies') return `#/technologies/${encoded}`;
  if (role === 'skills') return `#/skills/${encoded}`;
  if (role === 'mappings') return `#/mappings/mapping/${encoded}`;
  return '#/changes';
}

function hrefForSubject(model, subject) {
  for (const role of ['skills', 'technologies', 'mappings']) if (model[role].has(subject)) return hrefFor(role, subject);
  return null;
}

/**
 * The curation work queue. Real problems come first; the backlog is optional
 * editorial work and deliberately never counted as an error, because detect-only
 * technologies and skills without mappings are valid registry states.
 */
export function healthQueue(state, model) {
  const { diagnostics } = state;
  const assessments = { ...state.assessments, located: { ...state.assessments.located } };
  for (const op of state.ops ?? []) if (op.location) assessments.located[op.id] = op.location.commit;
  const skills = [...model.skills.values()];
  const recommended = new Set([...model.mappings.values()].flatMap((m) => m.recommendations.map((rec) => rec.skillId)));
  const skillItem = (skill, detail) => ({ label: skill.name, sub: skill.id, detail, href: hrefFor('skills', skill.id) });
  const diagnosticItem = (d) => ({ label: d.subject || d.code, sub: d.code, detail: d.message, href: hrefForSubject(model, d.subject) });
  const pinned = skills.filter((skill) => skill.source?.kind === 'git' && /^[0-9a-f]{40}$/.test(skill.source.ref ?? ''));
  const sections = [
    { id: 'errors', title: 'Validation errors', tone: 'poor', items: diagnostics.filter((d) => d.severity === 'error').map(diagnosticItem) },
    { id: 'warnings', title: 'Validation warnings', tone: 'fair', items: diagnostics.filter((d) => d.severity === 'warning').map(diagnosticItem) },
    { id: 'upstream', title: 'Upstream problems', tone: 'poor', hint: 'Quarantined skills and open misses from the pin workflow.', items: [
      ...skills.filter((s) => s.quarantine).map((s) => skillItem(s, `Quarantined: ${s.quarantine.reason} since ${s.quarantine.since}`)),
      ...skills.filter((s) => assessments.openMiss[s.id]).map((s) => skillItem(s, `Missed upstream: ${assessments.openMiss[s.id]}`)),
    ] },
  ];
  if (assessments.available) {
    sections.push(
      { id: 'unlocated', title: 'Not yet located', tone: 'fair', hint: 'The quality refresh locates these after merge.', items: pinned.filter((s) => !s.quarantine && assessments.located[s.id] !== s.source.ref).map((s) => skillItem(s, 'No SKILL.md location at this pin')) },
      { id: 'validator', title: 'Failing skill validation', tone: 'poor', items: skills.filter((s) => ['fail', 'not_evaluated'].includes(assessments.status[s.id])).map((s) => skillItem(s, `Validator status: ${assessments.status[s.id]}`)) },
    );
  }
  sections.push({ id: 'poor', title: 'Poor quality but recommended', tone: 'fair', items: skills.filter((s) => s.quality === 'poor' && recommended.has(s.id)).map((s) => skillItem(s, `Score ${s.score ?? 'n/a'}`)) });
  const mappings = [...model.mappings.values()];
  const mappingItem = (m, detail) => ({ label: m.technologyId, sub: m.id, detail, href: hrefFor('mappings', m.id) });
  const backlog = [
    { id: 'priorities', title: 'All recommendations optional', items: mappings.filter((m) => m.recommendations.length && m.recommendations.every((r) => (r.priority ?? 'optional') === 'optional')).map((m) => mappingItem(m, `${m.recommendations.length} recommendations`)) },
    { id: 'reasons', title: 'Templated reasons', items: mappings.filter((m) => m.recommendations.some((r) => TEMPLATED_REASON.test(r.reason ?? ''))).map((m) => mappingItem(m, 'Uses generated catalog text')) },
    { id: 'skillsUnmapped', title: 'Skills never recommended', hint: 'Some are intentionally cross-cutting.', items: skills.filter((s) => !recommended.has(s.id) && !s.quarantine).map((s) => skillItem(s, 'No mapping recommends this skill')) },
    { id: 'detectOnly', title: 'Technologies with no mapping', hint: 'Detect-only technologies are valid; map one only when a suitable skill exists.', items: [...model.technologies.values()].filter((t) => !mappings.some((m) => m.technologyId === t.id)).map((t) => ({ label: t.name, sub: t.id, detail: t.category, href: hrefFor('technologies', t.id) })) },
  ];
  return { sections: sections.filter((section) => section.items.length), backlog: backlog.filter((section) => section.items.length) };
}

/** What the header says about the data being edited, and anything to flag. */
export function sourceSummary(source) {
  const commit = source.commit ? source.commit.slice(0, 7) : '';
  const behind = source.mode === 'remote' && source.behind > 0 ? ` · checkout ${source.behind} behind` : '';
  const label = source.mode === 'remote' ? `${source.ref} · ${commit}${behind}` : `Local checkout${commit ? ` · ${commit}` : ''}`;
  const notices = [];
  if (source.warning) notices.push(source.warning);
  if (source.mode === 'remote') {
    if (source.ahead > 0) notices.push(`Your checkout has ${source.ahead} commit${source.ahead === 1 ? '' : 's'} not on ${source.ref}. They are not shown here; use --local to edit them.`);
    if (source.dirty) notices.push('Your checkout has uncommitted changes. They are not shown here; use --local to edit them.');
  }
  return { label, notices };
}

/** Line diff for the changes screen (LCS; entities are small). */
export function lineDiff(before, after) {
  const a = before === null ? [] : JSON.stringify(before, null, 2).split('\n');
  const b = after === null ? [] : JSON.stringify(after, null, 2).split('\n');
  const table = Array.from({ length: a.length + 1 }, () => new Array(b.length + 1).fill(0));
  for (let i = a.length - 1; i >= 0; i -= 1) {
    for (let j = b.length - 1; j >= 0; j -= 1) {
      table[i][j] = a[i] === b[j] ? table[i + 1][j + 1] + 1 : Math.max(table[i + 1][j], table[i][j + 1]);
    }
  }
  const lines = [];
  let i = 0;
  let j = 0;
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) { lines.push({ type: 'same', text: a[i] }); i += 1; j += 1; }
    else if (j < b.length && (i === a.length || table[i][j + 1] >= table[i + 1][j])) { lines.push({ type: 'add', text: b[j] }); j += 1; }
    else { lines.push({ type: 'del', text: a[i] }); i += 1; }
  }
  return lines;
}
