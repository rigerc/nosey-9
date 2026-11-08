#!/usr/bin/env node
// Builds the static registry browser from the official registry's JSON files.
//
//   node site/build.mjs [registryDir] [outDir]
//
// Defaults: registries/default -> dist/site. No dependencies; Node >= 22.
import { execFileSync } from 'node:child_process';
import { cpSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));

const readJSON = (dir, rel) => JSON.parse(readFileSync(join(dir, rel), 'utf8'));

/** Merge the registry documents into a list index and a lazily-loaded detail document. */
export function buildData(reg, meta = {}) {
  const skills = reg.skills.skills;
  const techs = reg.technologies.technologies;
  const repos = reg.repositories.repositories;
  const mappings = reg.mappings.mappings;
  const qualityById = new Map(reg.quality.entries.map((e) => [e.id, e]));
  const scoreById = new Map(reg.scores.entries.map((e) => [e.id, e]));
  const locations = reg.locations.locations;
  const repoScores = reg.manifest.repositoryScores ?? {};

  // technologyId -> recommendations, and skillId -> where it is recommended.
  const techRecs = new Map();
  const skillRecs = new Map();
  for (const m of mappings) {
    const list = techRecs.get(m.technologyId) ?? [];
    for (const r of m.recommendations) {
      list.push({ mappingId: m.id, ...r });
      const s = skillRecs.get(r.skillId) ?? [];
      s.push({ mappingId: m.id, technologyId: m.technologyId, priority: r.priority, reason: r.reason, when: r.when });
      skillRecs.set(r.skillId, s);
    }
    techRecs.set(m.technologyId, list);
  }

  const repoById = new Map(repos.map((r) => [r.id, r]));
  const techCount = new Map();
  const repoCount = new Map();
  const details = {};

  const list = skills.map((s) => {
    const q = qualityById.get(s.id);
    const j = scoreById.get(s.id);
    const loc = locations[s.id];
    const repo = repoById.get(s.repositoryId);
    for (const t of s.technologies) techCount.set(t, (techCount.get(t) ?? 0) + 1);
    repoCount.set(s.repositoryId, (repoCount.get(s.repositoryId) ?? 0) + 1);

    const sourceUrl = loc && repo?.uri
      ? `${repo.uri.replace(/\.git$/, '')}/blob/${loc.commit}/${loc.path}`
      : null;
    details[s.id] = {
      id: s.id,
      source: { ...s.source, path: loc?.path ?? q?.source?.path ?? null, treeSha: q?.source?.tree_sha ?? null, url: sourceUrl },
      assessment: s.assessment,
      jev: j && {
        status: j.status,
        evaluatedAt: j.evaluated_at,
        overall: j.overall_score,
        scores: j.evaluation?.scores ?? {},
      },
      validator: q && {
        status: q.status,
        version: q.validator_version,
        checks: q.checks,
        evaluatedAt: q.evaluated_at,
        counts: q.counts,
        findings: q.findings,
        metrics: q.metrics,
      },
      recommendedFor: skillRecs.get(s.id) ?? [],
    };

    return {
      id: s.id,
      name: s.name,
      repo: s.repositoryId,
      techs: s.technologies,
      quality: s.quality,
      score: s.score,
      status: q?.status ?? null,
      errors: q?.counts?.errors ?? 0,
      warnings: q?.counts?.warnings ?? 0,
      jev: j?.overall_score ?? null,
    };
  });

  const techList = techs.map((t) => ({
    id: t.id,
    name: t.name,
    category: t.category,
    metadataOnly: Boolean(t.metadataOnly),
    skills: techCount.get(t.id) ?? 0,
    rules: t.rules.length,
  }));

  const techDetails = Object.fromEntries(
    techs.map((t) => [t.id, { id: t.id, name: t.name, category: t.category, metadataOnly: Boolean(t.metadataOnly), rules: t.rules, recommendations: techRecs.get(t.id) ?? [] }]),
  );

  const repoList = repos.map((r) => ({
    id: r.id,
    name: r.name,
    kind: r.kind,
    uri: r.uri,
    skills: repoCount.get(r.id) ?? 0,
    averageScore: repoScores[r.id]?.averageScore ?? null,
  }));

  const quality = Object.fromEntries(['excellent', 'good', 'fair', 'poor'].map((k) => [k, list.filter((s) => s.quality === k).length]));
  const q = reg.quality;
  const sc = reg.scores;

  return {
    index: {
      registry: {
        id: reg.manifest.id,
        name: reg.manifest.name,
        commit: meta.commit ?? null,
        builtAt: meta.builtAt ?? null,
        sourceRepo: meta.sourceRepo ?? null,
        counts: { skills: list.length, technologies: techList.length, repositories: repoList.length, mappings: mappings.length },
        quality,
        validator: { ...q.tool, generatedAt: q.generated_at, byStatus: q.summary?.by_status ?? {} },
        jev: { provider: sc.evaluator?.provider, modelRelease: sc.evaluator?.model_release, generatedAt: sc.generated_at, scope: sc.evaluator?.scope, questions: sc.evaluator?.questions },
        files: reg.lock.files,
        generator: reg.lock.generator,
      },
      skills: list,
      technologies: techList,
      repositories: repoList,
    },
    details: { skills: details, technologies: techDetails },
  };
}

export function build(registryDir, outDir, meta = {}) {
  const reg = {
    manifest: readJSON(registryDir, 'nosey.registry.json'),
    lock: readJSON(registryDir, 'nosey.registry.lock.json'),
    skills: readJSON(registryDir, 'skills.json'),
    technologies: readJSON(registryDir, 'technologies.json'),
    repositories: readJSON(registryDir, 'repositories.json'),
    mappings: readJSON(registryDir, 'mappings.json'),
    quality: readJSON(registryDir, 'assessments/quality.json'),
    scores: readJSON(registryDir, 'assessments/scores.json'),
    locations: readJSON(registryDir, 'assessments/locations.json'),
  };
  const { index, details } = buildData(reg, meta);
  rmSync(outDir, { recursive: true, force: true });
  mkdirSync(outDir, { recursive: true });
  cpSync(join(here, 'src'), outDir, { recursive: true });
  writeFileSync(join(outDir, 'data.json'), JSON.stringify(index));
  writeFileSync(join(outDir, 'details.json'), JSON.stringify(details));
  writeFileSync(join(outDir, '.nojekyll'), '');
  return index;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const registryDir = resolve(process.argv[2] ?? 'registries/default');
  const outDir = resolve(process.argv[3] ?? 'dist/site');
  let commit = null;
  try {
    commit = execFileSync('git', ['-C', registryDir, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
  } catch {
    // Not a git checkout; the commit is shown as unknown.
  }
  const index = build(registryDir, outDir, {
    commit,
    builtAt: new Date().toISOString(),
    sourceRepo: process.env.REGISTRY_REPO_URL ?? 'https://github.com/rigerc/nosey-registry',
  });
  console.log(`Built ${index.skills.length} skills, ${index.technologies.length} technologies, ${index.repositories.length} repositories -> ${outDir}`);
}
