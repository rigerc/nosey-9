import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import vm from 'node:vm';
import { spawnSync } from 'node:child_process';
import { skillRoot, renderViewer, featureDir, parseArgs } from '../scripts/generate-html.mjs';
import { inventory } from '../scripts/inventory.mjs';

const cli = path.join(skillRoot,'scripts','tui.mjs');
function tmpRepo(){const r=fs.mkdtempSync(path.join(os.tmpdir(),'tui-contracts-'));fs.mkdirSync(path.join(r,'.work/flow-spec'),{recursive:true});return r;}
function copyFeature(root,feature){fs.cpSync(path.join(skillRoot,'examples',feature),featureDir(root,feature),{recursive:true});}
function exec(root,...args){return spawnSync(process.execPath,[cli,...args,'--repo-root',root],{encoding:'utf8'});}
function htmlScript(html){const matches=[...html.matchAll(/<script(?:\s[^>]*)?>([\s\S]*?)<\/script>/g)];return matches.at(-1)?.[1];}
class DomNode {
  constructor(tag='div'){this.tag=tag;this.children=[];this.attributes={};this.style={};this.dataset={};this.listeners={};this.value='';this._text='';}
  set className(v){this.classes=v;}get className(){return this.classes||'';}
  set textContent(v){this._text=String(v);this.children=[];}get textContent(){return this._text+this.children.map(x=>x.textContent||'').join('');}
  append(...children){this.children.push(...children);}appendChild(c){this.append(c);}
  replaceChildren(...children){this.children=children;this._text='';}
  setAttribute(k,v){this.attributes[k]=v;}
  addEventListener(type,fn){this.listeners[type]=fn;}
  querySelectorAll(){return [];} remove(){} click(){this.listeners.click?.({target:this});}
  get parentNode(){return this._parent;}
}
function browserSmoke(html){
  const script=htmlScript(html);assert.ok(script,'Viewer includes executable renderer');new vm.Script(script);
  const fields={};const ids=['contract-data','navigation','search','rail-title','rail-meta','breadcrumbs','eyebrow','heading','description','badges','content','theme','download'];for(const id of ids)fields[id]=new DomNode(id);
  const payload=html.match(/<script id="contract-data" type="application\/json">([\s\S]*?)<\/script>/)?.[1];assert.ok(payload);
  fields['contract-data'].textContent=payload;
  const document={ getElementById(id){return fields[id];},createElement(tag){return new DomNode(tag);},createElementNS(_,tag){return new DomNode(tag);},documentElement:new DomNode('html'),body:new DomNode('body'),title:''};
  const context=vm.createContext({document,console,setTimeout(){},JSON,URL,Blob,navigator:{clipboard:{writeText:async()=>{}}}});
  new vm.Script(script).runInContext(context,{timeout:5000});
  assert.ok(fields.content.children.length>0,'Overview was rendered');
  const buttons=fields.navigation.children.filter(x=>x.tag==='button');
  assert.ok(buttons.length>=8);
  const eventsButton=buttons.find(b=>b.textContent.includes('Events'));assert.ok(eventsButton);eventsButton.click();
  assert.equal(fields.heading.textContent,'Events');assert.ok(fields.content.children.length>0,'Event list rendered');
  fields.search.value='task.open';fields.search.listeners.input({target:fields.search});
  assert.equal(fields.heading.textContent,'Events');
  fields.theme.click();assert.equal(document.documentElement.dataset.theme,'light');
  const flowButton=fields.navigation.children.find(b=>b.tag==='button'&&b.textContent.includes('Flows'));
  flowButton.click();assert.equal(fields.heading.textContent,'Flows');
  return document;
}

test('portable skill root follows metadata and packaging conventions',()=>{
  assert.equal(path.basename(skillRoot),'clackbt-flow-spec');
  const skill=fs.readFileSync(path.join(skillRoot,'SKILL.md'),'utf8');
  assert.match(skill,/^---\nname: clackbt-flow-spec/m);
  assert.match(skill,/version: "2\.0\.0"/);
  assert.ok(fs.existsSync(path.join(skillRoot,'README.md')));
  assert.equal(fs.readdirSync(path.join(skillRoot,'.agents','prompts')).length,4);
});
test('skill enforces an explicit plan, approval and clackbt-native runner',()=>{
  const read=rel=>fs.readFileSync(path.join(skillRoot,rel),'utf8');
  const skill=read('SKILL.md');
  assert.match(skill,/Provisional plan/);
  assert.match(skill,/explicit approval/i);
  assert.match(skill,/RunNative/);
  assert.match(skill,/clackbt\.json/);
  for(const prompt of ['clackbt-plan.md','clackbt-extract.md','clackbt-validate.md','clackbt-revise.md']) {
    const body=read(`.agents/prompts/${prompt}`);
    assert.match(body,/clackbt|Clack/);
  }
});
test('full screen/flow HTML is interactive and has no remote resources',()=>{
  const root=tmpRepo();try{
    copyFeature(root,'example-task-tracker');
    const result=renderViewer({repoRoot:root,feature:'example-task-tracker'});
    const html=fs.readFileSync(result.path,'utf8');
    assert.match(html,/Interactive state graph/);
    assert.match(html,/contract-data/);
    assert.doesNotMatch(html,/\b(?:src|href)\s*=\s*["']https?:\/\//);
    const doc=browserSmoke(html);assert.match(doc.title,/Contract Explorer/);
    const manifest=JSON.parse(fs.readFileSync(path.join(path.dirname(result.path),'manifest.json')));
    assert.ok(manifest.files.includes('contracts-viewer.html'));
  }finally{fs.rmSync(root,{recursive:true,force:true});}
});
test('contracts-only output works without screen and flow artifacts',()=>{
  const root=tmpRepo();try{
    copyFeature(root,'example-existing-app');
    const result=renderViewer({repoRoot:root,feature:'example-existing-app'});
    const html=fs.readFileSync(result.path,'utf8');
    assert.match(html,/evidence/);
    const payload=JSON.parse(html.match(/<script id="contract-data" type="application\/json">([\s\S]*?)<\/script>/)[1]);
    assert.equal(payload.screens,null);assert.equal(payload.machine,null);
    browserSmoke(html);
  }finally{fs.rmSync(root,{recursive:true,force:true});}
});
test('HTML payloads cannot break out of the non-executing JSON script',()=>{
  const root=tmpRepo();try{
    copyFeature(root,'example-task-tracker');const dir=featureDir(root,'example-task-tracker');
    const manifestPath=path.join(dir,'manifest.json');const manifest=JSON.parse(fs.readFileSync(manifestPath));
    manifest.source_brief='unsafe </script><script>alert(1)</script> <img src=x onerror=alert(1)>';
    fs.writeFileSync(manifestPath,JSON.stringify(manifest));
    const html=fs.readFileSync(renderViewer({repoRoot:root,feature:'example-task-tracker'}).path,'utf8');
    assert.doesNotMatch(html,/unsafe <\/script><script>/);
    const embedded=html.match(/<script id="contract-data" type="application\/json">([\s\S]*?)<\/script>/)[1];
    assert.ok(!embedded.includes('<'));
    assert.equal(JSON.parse(embedded).manifest.source_brief,manifest.source_brief);
    browserSmoke(html);
  }finally{fs.rmSync(root,{recursive:true,force:true});}
});
test('output is constrained to the feature directory and symlinks are rejected',()=>{
  const root=tmpRepo();try{
    copyFeature(root,'example-task-tracker');
    assert.throws(()=>renderViewer({repoRoot:root,feature:'../escape'}),/slug/);
    assert.throws(()=>renderViewer({repoRoot:root,feature:'example-task-tracker',output:'../../other.html'}),/filename/);
    const link=path.join(root,'.work/flow-spec','symlink-test');fs.symlinkSync('/tmp',link);
    assert.throws(()=>featureDir(root,'symlink-test'),/Symlink/);
  }finally{fs.rmSync(root,{recursive:true,force:true});}
});
test('installer and automated builds support fullscreen, inline and contracts-only examples',()=>{
  const root=tmpRepo();try{
    const install=exec(root,'install');assert.equal(install.status,0,install.stderr);
    assert.ok(fs.existsSync(path.join(root,'.agents/skills/clackbt-flow-spec/SKILL.md')));
    assert.ok(fs.existsSync(path.join(root,'.agents/prompts/clackbt-extract.md')));
    assert.ok(fs.existsSync(path.join(root,'.agents/skills/clackbt-flow-spec/.agents/prompts/clackbt-extract.md')));
    assert.notEqual(exec(root,'install').status,0,'install must not overwrite without --force');
    assert.equal(exec(root,'demo').status,0);
    const build=exec(root,'build','--all','--strict');assert.equal(build.status,0,build.stdout+'\n'+build.stderr);
    for(const f of ['example-clackbt-prompt','example-existing-app','example-inline-cli']){
      assert.ok(fs.existsSync(path.join(featureDir(root,f),'contracts-viewer.html')));
      const report=JSON.parse(fs.readFileSync(path.join(featureDir(root,f),'validation-report.json')));
      assert.equal(report.status,'pass');
    }
  }finally{fs.rmSync(root,{recursive:true,force:true});}
});
test('scaffold produces draft artifacts with appropriate mode and without overwriting',()=>{
  const root=tmpRepo();try{
    assert.equal(exec(root,'scaffold','--feature','new-feature','--mode','greenfield','--title','New Feature').status,0);
    const manifest=JSON.parse(fs.readFileSync(path.join(featureDir(root,'new-feature'),'manifest.json')));
    assert.equal(manifest.status,'draft');assert.equal(manifest.entry_screen,'new-feature-main');
    assert.ok(manifest.files.includes('contracts-viewer.html'));
    assert.equal(exec(root,'scaffold','--feature','existing-contracts','--mode','existing').status,0);
    const dir=featureDir(root,'existing-contracts');
    const existing=JSON.parse(fs.readFileSync(path.join(dir,'manifest.json')));
    assert.equal(existing.artifact_scope,'contracts-only');assert.equal(existing.source_mode,'existing-app');
    assert.ok(!fs.existsSync(path.join(dir,'machine.json')));
    assert.equal(exec(root,'scaffold','--feature','existing-full','--mode','existing-full').status,0);
    assert.ok(fs.existsSync(path.join(featureDir(root,'existing-full'),'machine.json')));
    assert.notEqual(exec(root,'scaffold','--feature','new-feature').status,0);
  }finally{fs.rmSync(root,{recursive:true,force:true});}
});

test('read-only inventory detects source candidates without creating files',()=>{
  const root=tmpRepo();try{
    const f=path.join(root,'my_tui.py');
    fs.writeFileSync(f,'import textual\nclass Screen:\n  def on_key(self, event): pass\n  def render(self): pass\n');
    const scan=inventory(root);
    assert.equal(scan.kind,'heuristic-source-inventory');
    assert.ok(scan.candidates.some(c=>c.path==='my_tui.py'&&c.signals.input));
    assert.equal(scan.scanned_files,1);
    assert.equal(fs.readdirSync(root).filter(x=>x==='inventory.json').length,0);
  }finally{fs.rmSync(root,{recursive:true,force:true});}
});

test('parseArgs accepts free text beginning with dashes and rejects genuinely missing values',()=>{
  assert.equal(parseArgs(['scaffold','--brief','--legacy ui','--strict']).brief,'--legacy ui');
  assert.equal(parseArgs(['--feature=a-b']).feature,'a-b');
  assert.equal(parseArgs(['--minimal']).minimal,true);
  assert.throws(()=>parseArgs(['--feature']),/Missing value/);
  assert.throws(()=>parseArgs(['--feature','--strict']),/Missing value/);
  assert.throws(()=>parseArgs(['--Bad']),/Unrecognized/);
});

test('CLI argument errors are one-line messages, not stack traces',()=>{
  const root=tmpRepo();
  const run=exec(root,'scaffold','--feature');
  assert.notEqual(run.status,0);
  assert.match(run.stderr,/^Error: Missing value for option --feature/m);
  assert.doesNotMatch(run.stderr,/\n\s+at /);
});

test('inventory ranks a curses app above generic keyword noise and discounts tests',()=>{
  const root=fs.mkdtempSync(path.join(os.tmpdir(),'tui-inv-'));
  fs.mkdirSync(path.join(root,'app'));fs.mkdirSync(path.join(root,'web'));fs.mkdirSync(path.join(root,'tests'));
  fs.writeFileSync(path.join(root,'app','main.py'),'import curses\ndef main(stdscr):\n    stdscr.getch()\ncurses.wrapper(main)\n');
  const noise='export const store = {state:{}, dispatch(message){ /* update model */ }, validate(schema){ return required }}\nfunction render(){ view(); update(); }\n';
  for(const n of [1,2,3])fs.writeFileSync(path.join(root,'web',`store${n}.ts`),noise);
  fs.writeFileSync(path.join(root,'tests','test_curses.py'),'import curses\ncurses.getch()\n');
  const scan=inventory(root);
  assert.equal(scan.candidates[0].path,'app/main.py');
  assert.ok(scan.candidates[0].signals.frameworks);
  const t=scan.candidates.find(c=>c.path==='tests/test_curses.py');
  assert.equal(t.is_test,true);
  assert.ok(t.score<scan.candidates[0].score);
});

test('install --minimal leaves out examples and tests but keeps the validator',()=>{
  const root=tmpRepo();
  const run=exec(root,'install','--minimal');
  assert.equal(run.status,0,run.stderr);
  const dest=path.join(root,'.agents','skills','clackbt-flow-spec');
  assert.ok(fs.existsSync(path.join(dest,'scripts','validate.py')));
  assert.ok(fs.existsSync(path.join(dest,'SKILL.md')));
  assert.equal(fs.existsSync(path.join(dest,'examples')),false);
  assert.equal(fs.existsSync(path.join(dest,'tests')),false);
  assert.ok(fs.existsSync(path.join(root,'.agents','prompts','clackbt-plan.md')));
});

test('validate refuses a symlinked output tree just like build',()=>{
  const root=tmpRepo();
  const outside=fs.mkdtempSync(path.join(os.tmpdir(),'tui-outside-'));
  copyFeature(root,'example-task-tracker');
  fs.rmSync(path.join(root,'.work'),{recursive:true});
  fs.cpSync(path.join(skillRoot,'examples','example-task-tracker'),path.join(outside,'flow-spec','example-task-tracker'),{recursive:true});
  try{fs.symlinkSync(outside,path.join(root,'.work'));}catch{return;}
  const run=exec(root,'validate','--feature','example-task-tracker');
  assert.notEqual(run.status,0);
  assert.match(run.stderr,/[Ss]ymlink/);
});
