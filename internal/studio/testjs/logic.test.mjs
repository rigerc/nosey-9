import assert from 'node:assert/strict';
import test from 'node:test';
import * as logic from '../web/logic.mjs';

const fixture = () => logic.buildModel({
  technologies: [{ id: 'react', name: 'React', category: 'ui', rules: [] }, { id: 'vue', name: 'Vue', category: 'ui', rules: [] }],
  skills: [
    { id: 'o:r:a', name: 'a', technologies: ['react'], source: {}, conflictsWith: ['o:r:b'] },
    { id: 'o:r:b', name: 'b', technologies: [], source: {}, conflictsWith: ['o:r:a'] },
    { id: 'o:r:c', name: 'react-tips', technologies: [], source: {} },
  ],
  mappings: [
    { id: 'technology:react', technologyId: 'react', recommendations: [{ skillId: 'o:r:a', priority: 'optional', reason: 'Catalog recommendation: x' }] },
    { id: 'combination:vue-react', technologyId: 'vue', recommendations: [{ skillId: 'o:r:b', priority: 'optional', when: { allTechnologies: ['react'] } }] },
  ],
  repositories: [{ id: 'o:r', name: 'o/r', kind: 'git', uri: 'https://github.com/o/r' }],
}, []);

test('normalizeRule keeps only fields the kind allows', () => {
  const rule = { id: 'r', kind: 'file', glob: '**/a', regex: 'x', ecosystem: 'npm', name: 'n', args: {} };
  assert.deepEqual(logic.normalizeRule(rule), { id: 'r', kind: 'file', glob: '**/a' });
  const dep = logic.normalizeRule({ id: 'd', kind: 'dependency', ecosystem: 'npm', name: 'x', nameRegex: 'y', glob: 'z' }, 'nameRegex');
  assert.deepEqual(dep, { id: 'd', kind: 'dependency', ecosystem: 'npm', nameRegex: 'y' });
  const content = logic.normalizeRule({ id: 'c', kind: 'content', glob: '*.js', regex: 'x', regexFlags: 'i', contains: 'y' }, 'regex');
  assert.deepEqual(content, { id: 'c', kind: 'content', glob: '*.js', regex: 'x', regexFlags: 'i' });
  assert.equal(logic.normalizeRule({ id: 'c', kind: 'content', glob: '*', contains: 'y', regexFlags: 'i' }, 'contains').regexFlags, undefined);
  assert.deepEqual(logic.normalizeRule({ id: 'm', kind: 'mcp_package', name: 'p', glob: 'z' }), { id: 'm', kind: 'mcp_package', name: 'p' });
  assert.deepEqual(logic.normalizeRule({ id: 'x', kind: 'detector', detector: 'd', args: { a: 1 } }), { id: 'x', kind: 'detector', detector: 'd', args: { a: 1 } });
});

test('nextRuleId finds an unused sequential id', () => {
  assert.equal(logic.nextRuleId([{ id: 'file-0001' }], 'file'), 'file-0002');
  assert.equal(logic.nextRuleId([], 'mcp_package'), 'mcp-package-0001');
});

test('detector and mcp rules are flagged as needing setup', () => {
  assert.match(logic.ruleNeedsSetup({ kind: 'detector' }), /adapter/);
  assert.match(logic.ruleNeedsSetup({ kind: 'mcp_package' }), /MCP/);
  assert.equal(logic.ruleNeedsSetup({ kind: 'file' }), null);
});

test('buildModel overlays upserts and deletions', () => {
  const model = logic.buildModel({ skills: [{ id: 'a', name: 'old' }, { id: 'b', name: 'b' }] }, [
    { role: 'skills', id: 'a', entity: { id: 'a', name: 'new' }, base: { id: 'a', name: 'old' } },
    { role: 'skills', id: 'b', entity: null, base: { id: 'b' } },
  ]);
  assert.equal(model.skills.get('a').name, 'new');
  assert.equal(model.skills.has('b'), false);
});

test('references finds technology use across skills, mappings and conditions', () => {
  const refs = logic.references(fixture(), 'technologies', 'react').map((r) => `${r.role}:${r.field}`);
  assert.deepEqual(refs.sort(), ['mappings:technologyId', 'mappings:when', 'skills:technologies']);
});

test('cascadeDelete of a skill clears both conflict sides and empties mappings', () => {
  const ops = logic.cascadeDelete(fixture(), 'skills', 'o:r:a');
  const peer = ops.find((op) => op.id === 'o:r:b');
  assert.equal('conflictsWith' in peer.entity, false);
  assert.equal(ops.find((op) => op.id === 'technology:react').delete, true);
});

test('cascadeDelete of a technology removes conditional recommendations', () => {
  const ops = logic.cascadeDelete(fixture(), 'technologies', 'react');
  assert.deepEqual(ops.find((op) => op.id === 'o:r:a').entity.technologies, []);
  assert.equal(ops.find((op) => op.id === 'combination:vue-react').delete, true);
});

test('renameTechnology rewrites references and the primary mapping id', () => {
  const ops = logic.renameTechnology(fixture(), 'react', 'reactjs');
  assert.deepEqual(ops.find((op) => op.id === 'o:r:a').entity.technologies, ['reactjs']);
  assert.equal(ops.find((op) => op.id === 'technology:react').delete, true);
  assert.equal(ops.find((op) => op.id === 'technology:reactjs').entity.technologyId, 'reactjs');
  assert.deepEqual(ops.find((op) => op.id === 'combination:vue-react').entity.recommendations[0].when.allTechnologies, ['reactjs']);
});

test('cleanRecommendation orders fields and drops blanks', () => {
  const rec = logic.cleanRecommendation({ conflictsWith: [], reason: ' ', when: { allTechnologies: [], noneTechnologies: ['x'] }, priority: '', skillId: 's' });
  assert.deepEqual(Object.keys(rec), ['skillId', 'priority', 'when']);
  assert.deepEqual(rec.when, { noneTechnologies: ['x'] });
  assert.equal(rec.priority, 'optional');
});

test('skillEditOps writes conflicts symmetrically in both directions', () => {
  const model = fixture();
  const added = logic.skillEditOps(model, { ...model.skills.get('o:r:c'), conflictsWith: ['o:r:a'] });
  assert.deepEqual(added.find((op) => op.id === 'o:r:a').entity.conflictsWith, ['o:r:b', 'o:r:c']);
  const removed = logic.skillEditOps(model, { ...model.skills.get('o:r:a'), conflictsWith: [] });
  assert.equal('conflictsWith' in removed.find((op) => op.id === 'o:r:b').entity, false);
});

test('addSkillOps creates repository, skill and extends existing mappings', () => {
  const model = fixture();
  const ops = logic.addSkillOps(model, { repository: 'New/Repo', uri: 'https://github.com/New/Repo', commit: 'a'.repeat(40), path: 'skills/s/SKILL.md', name: 's', technologies: ['react', 'vue'], priority: 'core', reason: 'why', map: true });
  assert.equal(ops[0].id, 'new:repo');
  const skill = ops[1].entity;
  assert.equal(skill.id, 'New:Repo:s');
  assert.equal(skill.repositoryId, 'new:repo');
  assert.equal('quality' in skill, false);
  assert.deepEqual(ops[1].location, { commit: 'a'.repeat(40), path: 'skills/s/SKILL.md' });
  assert.equal(ops.find((op) => op.id === 'technology:react').entity.recommendations.length, 2);
  assert.equal(ops.find((op) => op.id === 'technology:vue').entity.technologyId, 'vue');
});

test('bulkEdit only replaces templated reasons and skips unchanged mappings', () => {
  const model = fixture();
  const ops = logic.bulkEdit(model, ['technology:react', 'combination:vue-react'], { reason: 'Because' });
  assert.equal(ops.find((op) => op.id === 'technology:react').entity.recommendations[0].reason, 'Because');
  assert.equal(ops.find((op) => op.id === 'combination:vue-react').entity.recommendations[0].reason, 'Because');
  assert.equal(logic.bulkEdit(model, ['technology:react'], { priority: 'optional' }).length, 0);
});

test('suggestSkills falls back to weak name matching', () => {
  const model = fixture();
  assert.deepEqual(logic.suggestSkills(model, model.technologies.get('react')).map((s) => s.id), ['o:r:c']);
});

test('healthQueue separates real problems from the optional backlog', () => {
  const model = fixture();
  model.skills.get('o:r:a').quality = 'poor';
  const state = { diagnostics: [{ severity: 'error', code: 'E', message: 'm', subject: 'o:r:a' }], assessments: { available: false, status: {}, located: {}, openMiss: {} } };
  const { sections, backlog } = logic.healthQueue(state, model);
  assert.deepEqual(sections.map((s) => s.id), ['errors', 'poor']);
  assert.equal(sections[0].items[0].href, '#/skills/o%3Ar%3Aa');
  assert.deepEqual(backlog.map((s) => s.id), ['priorities', 'reasons', 'skillsUnmapped']);
});

test('healthQueue treats a staged location as located', () => {
  const skills = [{ id: 'o:r:a', name: 'a', technologies: [], source: { kind: 'git', ref: 'a'.repeat(40) } }];
  const model = logic.buildModel({ skills, technologies: [], mappings: [], repositories: [] }, []);
  const assessments = { available: true, status: {}, located: {}, openMiss: {} };
  assert.ok(logic.healthQueue({ diagnostics: [], ops: [], assessments }, model).sections.some((s) => s.id === 'unlocated'));
  const ops = [{ id: 'o:r:a', location: { commit: 'a'.repeat(40) } }];
  assert.ok(!logic.healthQueue({ diagnostics: [], ops, assessments }, model).sections.some((s) => s.id === 'unlocated'));
});

test('lineDiff marks additions and removals', () => {
  const types = logic.lineDiff({ a: 1 }, { a: 2 }).map((line) => line.type);
  assert.ok(types.includes('add') && types.includes('del') && types.includes('same'));
  assert.ok(logic.lineDiff(null, { a: 1 }).every((line) => line.type === 'add'));
});
