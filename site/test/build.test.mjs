import assert from 'node:assert/strict';
import { existsSync, mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { build, buildData } from '../build.mjs';

const fixture = () => ({
  manifest: { id: 'x:y', name: 'X', repositoryScores: { 'o:r': { averageScore: 80 } } },
  lock: { files: {}, generator: 'g' },
  skills: { skills: [{ id: 'o:r:s', name: 's', technologies: ['t'], source: { kind: 'git', uri: 'https://github.com/o/r', ref: 'abc', skillName: 's' }, repositoryId: 'o:r', quality: 'good', score: 72, assessment: { method: 'automated' } }] },
  technologies: { technologies: [{ id: 't', name: 'T', category: 'tool', rules: [{ id: 'r1', kind: 'file', path: 'a' }] }, { id: 'u', name: 'U', category: 'tool', rules: [], metadataOnly: true }] },
  repositories: { repositories: [{ id: 'o:r', name: 'o/r', kind: 'git', uri: 'https://github.com/o/r' }] },
  mappings: { mappings: [{ id: 'technology:t', technologyId: 't', recommendations: [{ skillId: 'o:r:s', priority: 'optional', reason: 'why' }] }] },
  quality: { tool: { name: 'v' }, generated_at: 'now', summary: { by_status: { pass: 1 } }, entries: [{ id: 'o:r:s', source: { path: 'p/SKILL.md', tree_sha: 'tt' }, status: 'pass', counts: { errors: 0, warnings: 2 }, findings: [], metrics: {} }] },
  scores: { evaluator: { provider: 'p' }, generated_at: 'now', entries: [{ id: 'o:r:s', status: 'evaluated', overall_score: 3.6, evaluation: { scores: { safety: { score: 4 } } } }] },
  locations: { locations: { 'o:r:s': { commit: 'abc', path: 'p/SKILL.md' } } },
});

test('merges every document into list and detail records', () => {
  const { index, details } = buildData(fixture(), { commit: 'c1' });
  assert.equal(index.registry.counts.skills, 1);
  assert.deepEqual(index.skills[0], { id: 'o:r:s', name: 's', repo: 'o:r', techs: ['t'], quality: 'good', score: 72, status: 'pass', errors: 0, warnings: 2, jev: 3.6, dims: { safety: 4 }, tokens: null, otherTokens: null, words: null, contamination: null });
  assert.deepEqual(index.technologies.find((t) => t.id === 't').ruleKinds, ['file']);
  assert.equal(index.technologies.find((t) => t.id === 't').skills, 1);
  assert.equal(index.technologies.find((t) => t.id === 'u').metadataOnly, true);
  assert.equal(index.repositories[0].averageScore, 80);
  const d = details.skills['o:r:s'];
  assert.equal(d.source.url, 'https://github.com/o/r/blob/abc/p/SKILL.md');
  assert.equal(d.recommendedFor[0].technologyId, 't');
  assert.equal(details.technologies.t.recommendations[0].skillId, 'o:r:s');
});

test('tolerates skills without assessments', () => {
  const f = fixture();
  f.quality.entries = [];
  f.scores.entries = [];
  f.locations.locations = {};
  const { index, details } = buildData(f);
  assert.equal(index.skills[0].jev, null);
  assert.equal(details.skills['o:r:s'].source.url, null);
});

test('builds the official registry when it is checked out', (t) => {
  const dir = 'registries/default';
  if (!existsSync(join(dir, 'skills.json'))) return t.skip('registry submodule not checked out');
  const out = mkdtempSync(join(tmpdir(), 'site-'));
  const index = build(dir, out);
  assert.ok(index.skills.length > 0);
  for (const f of ['index.html', 'app.js', 'style.css', 'data.json', 'details.json']) assert.ok(existsSync(join(out, f)), f);
  assert.equal(JSON.parse(readFileSync(join(out, 'data.json'), 'utf8')).skills.length, index.skills.length);
});
