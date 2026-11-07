#!/usr/bin/env node
/** Generate a self-contained interactive HTML viewer from canonical TUI contracts.
 * Node.js 18+; no dependencies. Rendering never executes application source files.
 */
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';

export const skillRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
export const SLUG = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const DATA = ['manifest.json', 'contracts.json', 'screens.json', 'machine.json', 'evidence.json', 'validation-report.json', 'design.json', 'scenarios.json', 'components.catalog.json'];

export function requireFeature(feature) {
  if (!SLUG.test(feature || '')) throw new Error('Invalid feature slug; use lowercase kebab-case (for example: task-browser).');
  return feature;
}
export function resolveRepoRoot(explicit) {
  if (explicit) {
    const root = path.resolve(explicit);
    if (!fs.existsSync(root) || !fs.statSync(root).isDirectory()) throw new Error('Repository root is not a directory: ' + root);
    return fs.realpathSync(root);
  }
  const git = spawnSync('git', ['rev-parse', '--show-toplevel'], { encoding: 'utf8', cwd: process.cwd() });
  if (git.status === 0 && git.stdout.trim()) return fs.realpathSync(git.stdout.trim());
  throw new Error('Cannot determine repository root. Specify --repo-root /absolute/path.');
}
export function featureDir(repoRoot, feature) {
  const root = fs.realpathSync(repoRoot);
  const directory = path.join(root, '.work', 'flow-spec', requireFeature(feature));
  // Prevent symlink redirects anywhere inside the mandated generated-output tree.
  let current = directory;
  while (current !== root && current.startsWith(root + path.sep)) {
    if (fs.existsSync(current) && fs.lstatSync(current).isSymbolicLink()) {
      throw new Error('Symlink not allowed in generated artifact path: ' + current);
    }
    current = path.dirname(current);
  }
  return directory;
}
function readOptional(dir, filename) {
  const filenamePath = path.join(dir, filename);
  if (!fs.existsSync(filenamePath)) return null;
  if (fs.lstatSync(filenamePath).isSymbolicLink()) throw new Error('Refusing symlinked contract input: ' + filenamePath);
  try { return JSON.parse(fs.readFileSync(filenamePath, 'utf8')); }
  catch (e) { throw new Error('Invalid JSON in ' + filenamePath + ': ' + e.message); }
}
export function getBundle(repoRoot, feature) {
  const dir = featureDir(repoRoot, feature);
  if (!fs.existsSync(dir)) throw new Error('Feature folder not found: ' + dir);
  const bundle = {};
  for (const file of DATA) {
    const name = file.split('.')[0] === 'validation-report' ? 'report' : file.split('.')[0];
    bundle[name] = readOptional(dir, file);
  }
  if (!bundle.contracts || !bundle.manifest) throw new Error('manifest.json and contracts.json are required in ' + dir);
  if (bundle.manifest.feature_id !== feature) throw new Error('manifest.feature_id must match the feature directory: ' + feature);
  return bundle;
}
export function renderViewer({ repoRoot, feature, output = 'contracts-viewer.html' }) {
  if (path.basename(output) !== output || output === '.' || output === '..' || !output.endsWith('.html')) {
    throw new Error('HTML output must be a .html filename (not a path) inside the feature directory.');
  }
  const dir = featureDir(repoRoot, feature);
  const data = getBundle(repoRoot, feature);
  const manifestPath = path.join(dir, 'manifest.json');
  const outputPath = path.join(dir, output);
  if (fs.existsSync(outputPath) && fs.lstatSync(outputPath).isSymbolicLink()) throw new Error('Refusing symlinked HTML output: ' + outputPath);
  if (!Array.isArray(data.manifest.files)) throw new Error('manifest.files must be an array.');
  if (!data.manifest.files.includes(output)) {
    data.manifest.files = [...data.manifest.files, output].sort();
    fs.writeFileSync(manifestPath, JSON.stringify(data.manifest, null, 2) + '\n', 'utf8');
  }
  const templatePath = path.join(skillRoot, 'assets', 'viewer', 'index.html');
  const template = fs.readFileSync(templatePath, 'utf8');
  const token = '__TUI_DATA_PAYLOAD__';
  if (template.split(token).length !== 2) throw new Error('Viewer template must contain exactly one payload placeholder.');
  // Escapes prevent source text from becoming executable markup, even when it contains </script>.
  const payload = JSON.stringify(data).replace(/</g, '\\u003c').replace(/>/g, '\\u003e').replace(/\u2028/g, '\\u2028').replace(/\u2029/g, '\\u2029');
  const html = template.replace(token, payload);
  const finalPath = path.join(dir, output);
  fs.writeFileSync(finalPath, html, { encoding: 'utf8', flag: 'w' });
  return { path: finalPath, bytes: Buffer.byteLength(html), sections: Object.keys(data).filter(k => data[k] !== null) };
}
const OPTION_LIKE = /^--[a-z][a-z-]*(=|$)/;
const BOOLEAN_FLAGS = ['all', 'watch', 'strict', 'force', 'help', 'minimal'];
export function parseArgs(argv) {
  const parsed = { _: [] };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (OPTION_LIKE.test(arg)) {
      const eq = arg.indexOf('=');
      const name = arg.slice(2, eq === -1 ? undefined : eq);
      if (eq !== -1) parsed[name] = arg.slice(eq + 1);
      else if (BOOLEAN_FLAGS.includes(name)) parsed[name] = true;
      // A value is anything that is not itself an option; free text such as "--legacy ui" is a value.
      else if (i + 1 < argv.length && !OPTION_LIKE.test(argv[i + 1])) parsed[name] = argv[++i];
      else throw new Error('Missing value for option --' + name);
    } else if (arg.startsWith('--')) throw new Error('Unrecognized option: ' + arg);
    else parsed._.push(arg);
  }
  return parsed;
}
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const args = parseArgs(process.argv.slice(2));
    if (args.help || !args.feature) { console.log('Usage: node scripts/generate-html.mjs --repo-root /path/to/repo --feature <slug> [--output contracts-viewer.html]'); process.exit(args.help ? 0 : 2); }
    const result = renderViewer({repoRoot:resolveRepoRoot(args['repo-root']),feature:args.feature,output:args.output});
    console.log('Generated ' + result.path + ' (' + result.bytes + ' bytes)');
  } catch (error) { console.error('Error: ' + error.message); process.exit(1); }
}
