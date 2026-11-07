#!/usr/bin/env node
/** Standalone zero-dependency orchestration for TUI contracts, Node.js 18+. */
import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { renderViewer, skillRoot, featureDir, requireFeature, resolveRepoRoot, parseArgs } from './generate-html.mjs';
import { inventory } from './inventory.mjs';
import { validateClackbt, renderClackbtPreview } from './clackbt-adapter.mjs';
import { generateMockups } from './mockup.mjs';
import { validateVisual } from './validate-visual.mjs';

let argv = { _: [] };
let command = 'help';
const sourceTemplates = path.join(skillRoot, 'assets', 'templates');
const OUTPUTS = ['CONTRACTS.md', 'DECISIONS.md', 'EVIDENCE.md', 'FLOWS.md', 'SPEC.md'];
const commandText = () => `Usage: node scripts/clackbt.mjs <command> [options]\n\nCommands:\n  install    --repo-root PATH [--force] [--minimal]  Install skill + .agents/prompts (--minimal omits examples/ and tests/)\n  inventory  --repo-root PATH [--limit N]     Read-only source discovery hints for existing apps\n  scaffold   --repo-root PATH --feature SLUG [--mode greenfield|existing|existing-full] [--surface fullscreen|inline] [--title TITLE] [--brief TEXT]\n  validate   --repo-root PATH --feature SLUG   Validate JSON references and write a report\n  visualize  --repo-root PATH --feature SLUG   Generate standalone interactive HTML\n  clack-preview --repo-root PATH --feature SLUG   Create illustrative Clack prompt HTML/TXT transcript\n  mockup     --repo-root PATH --feature SLUG [--cols N --rows N --viewport NAME[,NAME] --scenario NAME[,NAME] --outputs txt,svg,ansi,html --theme dark]\n  build      --repo-root PATH --feature SLUG   Validate and generate interactive HTML\n  build      --repo-root PATH --all            Validate/render all features\n  watch      --repo-root PATH --all            Rebuild when source contracts change\n  demo       --repo-root PATH [--force]       Copy bundled examples into output path\n  test                                        Run Python and Node regression tests\n\nFlags: --strict fails build if validation fails; --output NAME sets HTML filename; --force allows overwrites for install/demo.\nNo application code is executed by validation, HTML generation, scaffold, or watch.\n`;
function assertDestinationSafe(destination, root) {
  const resolvedRoot = path.resolve(root);
  let cursor = path.resolve(destination);
  if (cursor !== resolvedRoot && !cursor.startsWith(resolvedRoot + path.sep)) throw new Error('Install path escapes repository: ' + cursor);
  while (cursor !== resolvedRoot) {
    if (fs.existsSync(cursor) && fs.lstatSync(cursor).isSymbolicLink()) throw new Error('Refusing symlink in install destination: ' + cursor);
    cursor = path.dirname(cursor);
  }
}
function copyDir(src,dst,force=false,exclude = new Set(),root=dst) {
  assertDestinationSafe(dst, root);
  if (!fs.existsSync(dst)) fs.mkdirSync(dst,{recursive:true});
  for(const dirent of fs.readdirSync(src,{withFileTypes:true})) {
    if (exclude.has(dirent.name)) continue;
    const from=path.join(src,dirent.name), to=path.join(dst,dirent.name);
    if(dirent.isSymbolicLink()) throw new Error('Refusing to install symlink: '+from);
    assertDestinationSafe(to, root);
    if(dirent.isDirectory()) copyDir(from,to,force,new Set(),root);
    else if(dirent.isFile()) {
      if(fs.existsSync(to) && !force) throw new Error('File already exists: '+to+' (use --force to update)');
      fs.copyFileSync(from,to);
    }
  }
}
function install(root) {
  const destination=path.join(root,'.agents','skills','clackbt-flow-spec');
  const promptDest=path.join(root,'.agents','prompts');
  const skillAlreadyInstalled = fs.existsSync(destination) && fs.realpathSync(destination)===fs.realpathSync(skillRoot);
  // Check collisions first to avoid a half-updated install.
  if (!argv.force && !skillAlreadyInstalled && fs.existsSync(destination) && fs.readdirSync(destination).length) throw new Error('Skill already installed; rerun with --force to update.');
  for(const filename of fs.readdirSync(path.join(skillRoot,'.agents','prompts'))){
    const to=path.join(promptDest,filename);
    if(!argv.force && fs.existsSync(to))throw new Error('Prompt already exists: '+to+' (use --force to update)');
  }
  const skipped=new Set(['node_modules','.git',...(argv.minimal?['examples','tests']:[])]);
  if(!skillAlreadyInstalled)copyDir(skillRoot,destination,!!argv.force,skipped,root);
  copyDir(path.join(skillRoot,'.agents','prompts'),promptDest,!!argv.force,new Set(),root);
  console.log('Installed skill: '+destination+'\nInstalled workflow prompts: '+promptDest);
}
function writeJSON(location,data){fs.writeFileSync(location,JSON.stringify(data,null,2)+'\n','utf8');}
function scaffold(root){
  const feature=requireFeature(argv.feature);
  const mode=argv.mode||'greenfield';
  const surface=argv.surface||(mode==='existing'?'fullscreen':'inline');
  if(!['fullscreen','inline'].includes(surface))throw new Error('Unknown presentation surface: '+surface);
  if(mode==='existing'&&surface==='inline')throw new Error('Inline scaffold requires full scope (greenfield or existing-full)');
  if(!['greenfield','existing','existing-full'].includes(mode))throw new Error('Unknown scaffold mode: '+mode);
  const dir=featureDir(root,feature);
  if(fs.existsSync(dir))throw new Error('Feature folder already exists; refusing to overwrite: '+dir);
  fs.mkdirSync(dir,{recursive:true});
  const existing=mode!=='greenfield',full=mode!=='existing';
  const wanted=['CONTRACTS.md','DECISIONS.md','contracts.json','manifest.json'];
  if(full)wanted.push('clackbt.json');
  if(existing)wanted.push('EVIDENCE.md','evidence.json');
  if(full)wanted.push('SPEC.md','FLOWS.md','screens.json','machine.json','design.json','scenarios.json');
  for(const filename of wanted){
    const base=filename==='clackbt.json' ? 'clackbt.template.json' : filename==='manifest.json' && existing && !full ? 'manifest.existing-app.template.json' : filename.replace(/(\.md|\.json)$/i,'.template$1');
    fs.copyFileSync(path.join(sourceTemplates,base),path.join(dir,filename));
  }
  const manifest=JSON.parse(fs.readFileSync(path.join(dir,'manifest.json'),'utf8'));
  manifest.feature_id=feature;
  manifest.title=argv.title||feature.replace(/-/g,' ').replace(/\b\w/g,c=>c.toUpperCase());
  manifest.source_brief=argv.brief||'Brief or inspected source scope not yet documented';
  manifest.source_mode=existing?'existing-app':'brief';
  manifest.artifact_scope=full?'full':'contracts-only';
  if(full)manifest.entry_screen=feature+'-main';else delete manifest.entry_screen;
  manifest.status='draft';
  manifest.open_questions=['Agent must inspect the brief or source and resolve material questions before completing this specification.'];
  manifest.files=[...wanted,'validation-report.json','contracts-viewer.html',...(surface==='inline'?['clackbt-preview.html','clackbt-transcript.txt']:[])].sort();
  writeJSON(path.join(dir,'manifest.json'),manifest);
  if(full){
    const binding=JSON.parse(fs.readFileSync(path.join(dir,'clackbt.json'),'utf8'));binding.command=feature;binding.steps[0].screen_id=manifest.entry_screen;binding.steps[0].states=[feature+'.initial'];writeJSON(path.join(dir,'clackbt.json'),binding);
    const screens=JSON.parse(fs.readFileSync(path.join(dir,'screens.json'),'utf8'));
    screens.screens[0].id=manifest.entry_screen;screens.screens[0].title=manifest.title;screens.screens[0].states=[feature+'.initial'];
    if(surface==='inline'){
      const screen=screens.screens[0];
      screen.presentation='inline';
      screen.cli={invocation:feature,interaction:'prompted',stdin:'tty'};
      screen.min_size={cols:40,rows:1};
      screen.regions[0].children=[{id:'question',kind:'clack-prompt',label:'What should we call you?'}];
    }
    writeJSON(path.join(dir,'screens.json'),screens);
    const machine=JSON.parse(fs.readFileSync(path.join(dir,'machine.json'),'utf8'));
    machine.initial=feature+'.initial';machine.states[0].id=machine.initial;machine.states[0].screen=manifest.entry_screen;
    if(surface==='inline')machine.states[0].exit_code=0;
    writeJSON(path.join(dir,'machine.json'),machine);
  }
  console.log('Scaffolded draft (not a complete specification): '+dir);
  console.log('Resolve clarification questions and replace template placeholders before marking ready.');
}
function features(root){
  const dir=path.join(root,'.work','flow-spec');
  if(!fs.existsSync(dir))return [];
  return fs.readdirSync(dir,{withFileTypes:true}).filter(d=>d.isDirectory()&&/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(d.name) && fs.existsSync(path.join(dir,d.name,'manifest.json'))).map(d=>d.name).sort();
}
function validate(root,feature){
  featureDir(root,feature); // rejects symlinks anywhere in the generated-output path before anything is read or written
  const script=path.join(skillRoot,'scripts','validate.py');
  let python=process.env.PYTHON||'python3';
  let result=spawnSync(python,[script,'--repo-root',root,'--feature',feature,'--write-report'],{encoding:'utf8'});
  if(result.error?.code==='ENOENT' && !process.env.PYTHON){python='python';result=spawnSync(python,[script,'--repo-root',root,'--feature',feature,'--write-report'],{encoding:'utf8'});}
  if(result.error)throw new Error('Unable to run Python validator: '+result.error.message);
  let report;
  try{report=JSON.parse(result.stdout);}catch{throw new Error('Validator error: '+(result.stderr||result.stdout||'unknown error').trim());}
  const additional=validateVisual(root,feature);
  const backend=validateClackbt(root,feature);
  additional.errors.push(...backend.errors);additional.warnings.push(...backend.warnings);
  report.errors=[...(report.errors||[]),...additional.errors];report.warnings=[...(report.warnings||[]),...additional.warnings];
  report.error_count=report.errors.length;report.warning_count=report.warnings.length;report.status=report.error_count?'invalid':report.status;
  if(additional.errors.length||additional.warnings.length){
    const rp=path.join(featureDir(root,feature),'validation-report.json');
    if(fs.existsSync(rp)&&fs.lstatSync(rp).isSymbolicLink())throw new Error('Refusing symlink report');
    fs.writeFileSync(rp,JSON.stringify(report,null,2)+'\n');
  }
  console.log(`${feature}: ${report.status} (${report.error_count} errors, ${report.warning_count} warnings)`);
  for(const e of report.errors||[])console.error('  ERROR '+e);
  for(const e of report.warnings||[])console.warn('  WARN '+e);
  return result.status===0 && additional.errors.length===0;
}
function runBuild(root,feature,strict){
  const passed=validate(root,feature);
  const output=renderViewer({repoRoot:root,feature,output:argv.output||'contracts-viewer.html'});
  console.log('  HTML: '+path.relative(root,output.path));
  if(fs.existsSync(path.join(featureDir(root,feature),'screens.json')) && fs.existsSync(path.join(featureDir(root,feature),'machine.json'))){
    const mock=generateMockups({repoRoot:root,feature,cols:argv.cols!==undefined?Number(argv.cols):undefined,rows:argv.rows!==undefined?Number(argv.rows):undefined,viewport:argv.viewport,scenario:argv.scenario,outputs:argv.outputs,theme:argv.theme||'dark'});
    console.log('  MOCKUPS: '+path.relative(root,mock.manifest)+' ('+mock.variant_count+' variants)');
    if(mock.html)console.log('  STORYBOARD: '+path.relative(root,mock.html)+' ('+mock.state_count+' states)');
  }
  const clack=renderClackbtPreview(root,feature);
  if(clack)console.log('  CLACK PREVIEW: '+path.relative(root,clack.html)+' ('+clack.stages+' steps)');
  if(strict&&!passed)process.exitCode=1;
  return passed;
}
function copyExample(root) {
  if(!fs.existsSync(path.join(skillRoot,'examples')))throw new Error('Bundled examples are not available (skill was installed with --minimal).');
  for(const feature of ['example-clackbt-prompt','example-existing-app','example-inline-cli']){
    const dst=featureDir(root,feature),src=path.join(skillRoot,'examples',feature);
    if(fs.existsSync(dst)&&!argv.force)throw new Error('Example already exists: '+dst+'; --force to overwrite');
    copyDir(src,dst,!!argv.force,new Set(),root);
    console.log('Installed example: '+path.relative(root,dst));
  }
}
function changeKey(root) {
  const checksum=[];
  for(const feat of features(root)) {
    const dir=featureDir(root,feat);
    for(const filename of ['manifest.json','contracts.json','screens.json','machine.json','evidence.json','design.json','scenarios.json','clackbt.json']){
      const f=path.join(dir,filename);
      if(fs.existsSync(f)){const s=fs.statSync(f);checksum.push([feat,filename,s.size,s.mtimeMs]);}
    }
  }
  return JSON.stringify(checksum);
}
async function main(){
  argv=parseArgs(process.argv.slice(2));
  command=argv._[0]||'help';
  if(command==='help'||argv.help){console.log(commandText());return;}
  if(command==='test'){
    const results=[];
    results.push(spawnSync(process.env.PYTHON||'python3',['-m','unittest','discover','-s',path.join(skillRoot,'tests'),'-p','test_*.py','-v'],{stdio:'inherit'}));
    results.push(spawnSync(process.execPath,['--test',path.join(skillRoot,'tests','test_tools.mjs'),path.join(skillRoot,'tests','test_clackbt_adapter.mjs'),path.join(skillRoot,'tests','test_mockup_flex.mjs'),path.join(skillRoot,'tests','test_inline_cli.mjs')],{stdio:'inherit'}));
    if(results.some(r=>r.status!==0))process.exitCode=1;
    return;
  }
  const root=resolveRepoRoot(argv['repo-root']);
  if(command==='install')return install(root);
  if(command==='inventory'){const limit=argv.limit?Number(argv.limit):2500;if(!Number.isInteger(limit)||limit<1||limit>20000)throw new Error('limit must be an integer from 1 to 20000');console.log(JSON.stringify(inventory(root,{limit}),null,2));return;}
  if(command==='scaffold')return scaffold(root);
  if(command==='demo')return copyExample(root);
  const fspec=()=>{if(!argv.feature)throw new Error('Missing --feature SLUG');return requireFeature(argv.feature);};
  if(command==='validate'){const ok=validate(root,fspec());if(!ok)process.exitCode=1;return;}
  if(command==='clack-preview'){const r=renderClackbtPreview(root,fspec());if(!r)throw new Error('No clackbt.json backend binding in feature');console.log(r.html);return;}
  if(command==='mockup'){const r=generateMockups({repoRoot:root,feature:fspec(),cols:argv.cols!==undefined?Number(argv.cols):undefined,rows:argv.rows!==undefined?Number(argv.rows):undefined,viewport:argv.viewport,scenario:argv.scenario,outputs:argv.outputs,theme:argv.theme||'dark'});console.log('Generated '+r.variant_count+' mockup variants'+(r.html?' and '+r.html:''));return;}
  if(command==='visualize'){const o=renderViewer({repoRoot:root,feature:fspec(),output:argv.output||'contracts-viewer.html'});console.log('Generated '+o.path);return;}
  if(command==='build'||command==='watch'){
    const all=!!argv.all;
    if(!all&&!argv.feature)throw new Error('Provide --feature SLUG or --all.');
    const target=()=>all?features(root):[fspec()];
    const work=()=>{for(const feature of target()){try{runBuild(root,feature,!!argv.strict);}catch(err){console.error(feature+': '+err.message);process.exitCode=1;}}};
    work();
    if(command==='watch'){
      console.log('Watching canonical JSON for changes (Ctrl+C to exit).');
      let previous=changeKey(root),busy=false;
      setInterval(()=>{if(busy)return;const now=changeKey(root);if(now!==previous){previous=now;busy=true;try{work();}finally{busy=false;}}},1100);
    }
    return;
  }
  throw new Error('Unknown command: '+command+'\n'+commandText());
}
try{await main();}catch(e){console.error('Error: '+e.message);process.exitCode=1;}
