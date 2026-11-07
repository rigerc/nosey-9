import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {generateMockups} from '../scripts/mockup.mjs';

const skillRoot=path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const entry=path.join(skillRoot,'scripts/tui.mjs');
const sample=path.join(skillRoot,'examples/example-inline-cli');
function project(t){
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'inline-cli-'));
 t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
 const folder=path.join(root,'.work/flow-spec/example-inline-cli');
 fs.mkdirSync(path.dirname(folder),{recursive:true});fs.cpSync(sample,folder,{recursive:true});
 return {root,folder};
}
const read=(p)=>JSON.parse(fs.readFileSync(p,'utf8'));
const file=(folder,name)=>path.join(folder,name);
function cli(root,...args){return spawnSync(process.execPath,[entry,...args,'--repo-root',root],{encoding:'utf8'});}

test('inline example passes strict canonical + visual validation',t=>{
 const {root,folder}=project(t);
 const p=cli(root,'build','--feature','example-inline-cli','--strict');
 assert.equal(p.status,0,p.stderr+p.stdout);
 const report=read(file(folder,'validation-report.json'));
 assert.equal(report.error_count,0);
 const index=read(file(folder,'mockups/index.json'));
 assert.deepEqual(index.presentations,['inline']);
 assert.equal(index.state_count,5);
 assert.equal(index.variant_count,10);
 assert.ok(fs.existsSync(file(folder,'storyboard.html')));
 assert.match(fs.readFileSync(file(folder,'storyboard.html'),'utf8'),/inline ·/);
});

test('inline snapshots are unframed, variable height and preserve terminal output',t=>{
 const {root,folder}=project(t);generateMockups({repoRoot:root,feature:'example-inline-cli'});
 const txt=(state)=>fs.readFileSync(file(folder,`mockups/${state}.compact.default.txt`),'utf8');
 const prompt=txt('confirm.ready');
 assert.match(prompt,/^\$ shipctl deploy --env staging\n\? Deploy to staging\? \[y\/N\] ▌\n$/);
 assert.doesNotMatch(prompt,/[┌┐└┘│]/);
 assert.doesNotMatch(prompt,/deploy\.confirm|deploy\.cancel|FLOW ENDED|exit 0/);
 const running=txt('deploy.running');
 assert.match(running,/Uploading release archive/);
 assert.match(running,/Upload \[.+\] 37%/);
 const done=txt('deploy.done');
 assert.match(done,/Published release 8f4c2 to staging/);
 assert.match(done,/URL: https:\/\/staging\.example\.test/);
 assert.doesNotMatch(done,/Flow ended|exit_code|\[TERMINAL\]/);
 assert.match(txt('deploy.failed'),/Error: deployment API returned 503/);
 assert.match(txt('deploy.cancelled'),/Cancelled/);
 const index=read(file(folder,'mockups/index.json'));
 assert.equal(index.items.find(item=>item.state==='deploy.done').exit_code,0);
 assert.equal(index.items.find(item=>item.state==='deploy.failed').exit_code,1);
 assert.equal(index.items.find(item=>item.state==='deploy.cancelled').exit_code,130);
});

test('inline scrollback is never clipped to fixed terminal rows',t=>{
 const {root,folder}=project(t);
 const scenarios=read(file(folder,'scenarios.json'));
 const lines=Array.from({length:45},(_,i)=>`  line ${String(i).padStart(2,'0')}  value`);
 scenarios.states['deploy.done'].components.result.text=lines.join('\n');
 fs.writeFileSync(file(folder,'scenarios.json'),JSON.stringify(scenarios));
 generateMockups({repoRoot:root,feature:'example-inline-cli',cols:45,rows:8,outputs:'txt,html'});
 const txt=fs.readFileSync(file(folder,'mockups/deploy.done.txt'),'utf8');
 assert.equal(txt.split('\n').filter(Boolean).length,46);
 assert.match(txt,/  line 00  value/);
 assert.match(txt,/  line 44  value/);
 assert.doesNotMatch(txt,/[┌┐└┘]/);
 const item=read(file(folder,'mockups/index.json')).items.find(item=>item.state==='deploy.done');
 assert.deepEqual(item.warnings,[]);
});

test('inline stdout, stderr and command-echo are safely rendered across output adapters',t=>{
 const {root,folder}=project(t);generateMockups({repoRoot:root,feature:'example-inline-cli'});
 const failed=fs.readFileSync(file(folder,'mockups/deploy.failed.compact.default.ansi'),'utf8');
 assert.match(failed,/\x1b\[38;2;255;154;154mError:/);
 const svg=fs.readFileSync(file(folder,'mockups/deploy.failed.compact.default.svg'),'utf8');
 assert.match(svg,/<svg/);
 assert.match(svg,/Error: deployment API returned 503/);
 const html=fs.readFileSync(file(folder,'storyboard.html'),'utf8');
 assert.match(html,/Terminal interaction storyboard/);
 assert.match(html,/exit_code/);
 assert.match(html,/role-error/);
});

test('scaffold --surface inline creates a prompted draft; fullscreen remains explicitly selectable',t=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'inline-scaffold-'));
 t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
 let result=cli(root,'scaffold','--feature','sample-inline','--surface','inline');
 assert.equal(result.status,0,result.stderr);
 const dir=path.join(root,'.work/flow-spec/sample-inline');
 assert.equal(read(file(dir,'screens.json')).screens[0].presentation,'inline');
 assert.equal(read(file(dir,'screens.json')).screens[0].cli.stdin,'tty');
 assert.equal(read(file(dir,'machine.json')).states[0].exit_code,0);
 result=cli(root,'scaffold','--feature','sample-tui','--surface','fullscreen');
 assert.equal(result.status,0,result.stderr);
 const legacy=path.join(root,'.work/flow-spec/sample-tui');
 assert.equal(read(file(legacy,'screens.json')).screens[0].presentation,undefined);
 assert.equal(read(file(legacy,'machine.json')).states[0].exit_code,undefined);
 result=cli(root,'scaffold','--feature','bad','--surface','inline','--mode','existing');
 assert.notEqual(result.status,0);
 assert.match(result.stderr,/Inline scaffold requires full scope/);
});

test('strict validation rejects invalid CLI semantics and malformed inline exits',t=>{
 const {root,folder}=project(t);
 const originalScreens=read(file(folder,'screens.json'));
 const originalMachine=read(file(folder,'machine.json'));
 const run=()=>cli(root,'validate','--feature','example-inline-cli');
 const invalid=(change,pattern)=>{
  const screens=structuredClone(originalScreens),machine=structuredClone(originalMachine);
  change(screens,machine);
  fs.writeFileSync(file(folder,'screens.json'),JSON.stringify(screens));
  fs.writeFileSync(file(folder,'machine.json'),JSON.stringify(machine));
  const result=run();assert.notEqual(result.status,0,'validator unexpectedly accepted invalid CLI');
  assert.match(result.stderr,pattern);
 };
 invalid((screens)=>{screens.screens[0].cli.stdin='pipe';},/prompted interaction requires tty/);
 invalid((screens)=>{delete screens.screens[0].cli;},/inline presentation requires cli/);
 invalid((_,machine)=>{delete machine.states.find(x=>x.id==='deploy.done').exit_code;},/requires exit_code/);
 invalid((_,machine)=>{machine.states.find(x=>x.id==='deploy.done').exit_code=256;},/requires exit_code/);
 invalid((screens)=>{screens.screens[1].regions[0].children.find(x=>x.id==='upload').stream='stderr';},/stream only supports output/);
 invalid((screens)=>{screens.screens[1].regions[0].children.push({id:'bad-prompt',kind:'prompt'});},/prompt requires prompted interaction/);
 invalid((screens)=>{screens.screens[0].presentation='fullscreen';},/cli metadata requires inline|CLI node/);
});

test('read-only source inventory recognizes CLI parsers without treating them as proof',async t=>{
 const {inventory}=await import('../scripts/inventory.mjs');
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'cli-inventory-'));
 t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
 fs.writeFileSync(file(root,'main.py'),'import argparse\np = argparse.ArgumentParser()\np.add_argument("--env")\nargs = p.parse_args()\n');
 fs.writeFileSync(file(root,'index.js'),'import { Command } from "commander";\nconst program = new Command();\nprogram.option("--yes");\n');
 fs.writeFileSync(file(root,'unrelated.ts'),'export const x = 1;\n');
 const found=inventory(root).candidates;
 assert.deepEqual(new Set(found.map(x=>x.path)),new Set(['main.py','index.js']));
 assert.ok(found.every(x=>x.signals.cli?.length));
});
