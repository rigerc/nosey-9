import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {execFileSync} from 'node:child_process';
import vm from 'node:vm';
import {generateMockups,renderSnapshot,registerMockupRenderer} from '../scripts/mockup.mjs';
import {renderFlexibleSnapshot} from '../scripts/mockup/layout.mjs';
import {validateVisual} from '../scripts/validate-visual.mjs';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
function project(t){
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'tui-flex-'));t.after(()=>fs.rmSync(dir,{recursive:true,force:true}));
 const out=path.join(dir,'.work/flow-spec/example-task-tracker');fs.mkdirSync(out,{recursive:true});
 fs.cpSync(path.join(root,'examples/example-task-tracker'),out,{recursive:true});
 return {dir,out};
}
const load=(out,name)=>JSON.parse(fs.readFileSync(path.join(out,name+'.json'),'utf8'));
const save=(out,name,value)=>fs.writeFileSync(path.join(out,name+'.json'),JSON.stringify(value,null,2)+'\n');
function configure(out){
 const d=load(out,'design');d.schema_version='1.1.0';d.preview={
  viewports:{compact:{cols:60,rows:20},wide:{cols:120,rows:32}},
  layouts:{'task-create/root':{direction:'horizontal',padding:1,gap:1,border:'rounded',byViewport:{compact:{direction:'vertical'}}}},
  styles:{'task-create/title-input':{border:'single',width:'100%',wrap:true},'task-list/tasks-list':{role:'accent',maxItems:2}},
  outputs:['txt','svg','ansi','html']
 };save(out,'design',d);
 const s=load(out,'scenarios');s.schema_version='1.1.0';s.variants={empty:{data:{tasks:[]}},error:{presentation:{statusType:'error',statusText:'Fixture error'}}};
 s.stateVariants={'create.edit':{error:{state:{draft_title:'Bad title'},components:{'error-status':{text:'Failure'}}}}};save(out,'scenarios',s);
}
test('committed example snapshots match the current default renderer',t=>{
 const {dir,out}=project(t);generateMockups({repoRoot:dir,feature:'example-task-tracker'});
 for(const state of ['create.edit','create.invalid','list.ready','detail.ready']){
  const expected=fs.readFileSync(path.join(root,'examples/example-task-tracker/mockups',state+'.txt'),'utf8');
  const actual=fs.readFileSync(path.join(out,'mockups',state+'.txt'),'utf8');
  assert.equal(actual,expected);
 }
});
test('all configured viewports and named scenarios generate distinct TXT, SVG, ANSI and offline HTML',t=>{
 const {dir,out}=project(t);configure(out);
 assert.deepEqual(validateVisual(dir,'example-task-tracker'),{errors:[],warnings:[]});
 const r=generateMockups({repoRoot:dir,feature:'example-task-tracker'});
 assert.equal(r.variant_count,54);assert.equal(r.items.length,54);assert.equal(r.state_count,9);
 assert.deepEqual(r.outputs,['txt','svg','ansi','html']);
 const empty=fs.readFileSync(path.join(out,'mockups/list.ready.compact.empty.txt'),'utf8');
 const defaultFile=fs.readFileSync(path.join(out,'mockups/list.ready.compact.default.txt'),'utf8');
 assert.match(empty,/no sample items/);assert.doesNotMatch(defaultFile,/no sample items/);
 const compactLayout=fs.readFileSync(path.join(out,'mockups/create.edit.compact.default.txt'),'utf8');
 const wideLayout=fs.readFileSync(path.join(out,'mockups/create.edit.wide.default.txt'),'utf8');
 assert.equal(compactLayout.split('\n').some(line=>line.includes('Create task')&&line.includes('Save ]')),false);
 assert.ok(wideLayout.split('\n').some(line=>line.includes('Create task')&&line.includes('Save ]')));
 const svg=fs.readFileSync(path.join(out,'mockups/create.edit.wide.default.svg'),'utf8');assert.match(svg,/Create task/);assert.match(svg,/fill="#4fddb7"|fill="#65e8d6"/);
 const ansi=fs.readFileSync(path.join(out,'mockups/create.edit.wide.default.ansi'),'utf8');assert.match(ansi,/\x1b\[38;2;/);
 const html=fs.readFileSync(path.join(out,'storyboard.html'),'utf8');assert.match(html,/id="viewport"/);assert.match(html,/id="scenario"/);assert.match(html,/const setPreview=/);assert.match(html,/Side-by-side comparison/);
 const source=html.match(/<script>([\s\S]*?)<\/script>/)?.[1];assert.ok(source);assert.doesNotThrow(()=>new vm.Script(source));
 assert.equal(html.includes('</script><img src=x'),false);
 const manifest=JSON.parse(fs.readFileSync(path.join(out,'mockups/index.json')));
 assert.equal(manifest.variant_count,54);assert.equal(new Set(manifest.items.map(x=>x.text)).size,54);
 const before=fs.readFileSync(path.join(out,'mockups/create.edit.wide.default.svg'),'utf8');generateMockups({repoRoot:dir,feature:'example-task-tracker'});assert.equal(fs.readFileSync(path.join(out,'mockups/create.edit.wide.default.svg'),'utf8'),before);
});
test('horizontal rendering and nested borders place components on the same row',()=>{
 const screen={id:'sample',title:'Sample',min_size:{cols:20,rows:8},regions:[{id:'root',kind:'container',orientation:'vertical',children:[{id:'left',kind:'text',label:'LEFT'},{id:'right',kind:'button',label:'RIGHT'}]}],keybindings:[]};
 const state={id:'sample.ready',screen:'sample',terminal:false,description:'Ready'};
 const old=renderSnapshot(screen,state,{}, {cols:60,rows:15});
 const modern=renderFlexibleSnapshot(screen,state,{}, {cols:60,rows:15,design:{preview:{layouts:{'sample/root':{direction:'horizontal',gap:3,border:'rounded'}}}}});
 assert.equal(old.lines.some(line=>line.includes('LEFT')&&line.includes('RIGHT')),false);
 assert.equal(modern.lines.some(line=>line.includes('LEFT')&&line.includes('RIGHT')),true);
 assert.ok(modern.lines.some(line=>line.includes('╭')));
 assert.equal(modern.lines[0].length,60);assert.equal(modern.lines.at(-1).length,60);
});
test('help footer is horizontal, bottom-aligned, and consistent in both renderers',()=>{
 const screen={id:'sample',title:'Tasks',min_size:{cols:20,rows:8},regions:[{id:'intro',kind:'text',label:'Tasks'}],keybindings:[
  {key:'enter',event:'task.open'},{key:'n',event:'task.create.start'},{key:'q',event:'app.exit'}
 ]};
 const state={id:'list.ready',terminal:false};
 for(const render of [renderSnapshot,renderFlexibleSnapshot]){
  const standard=render(screen,state,{}, {cols:80,rows:16});
  assert.ok(standard.lines.at(-2).includes('enter → task.open'));
  assert.ok(standard.lines.at(-2).includes('n → task.create.start'));
  assert.ok(standard.lines.at(-2).includes('q → app.exit'));
  assert.equal(standard.lines.slice(2,-2).some(line=>line.includes('→')),false);
  assert.equal(standard.clipped,false);
  const compact=render(screen,state,{}, {cols:40,rows:12});
  assert.ok(compact.lines.slice(-4,-1).some(line=>line.includes('enter → task.open')));
  assert.ok(compact.lines.slice(-4,-1).some(line=>line.includes('q → app.exit')));
  assert.equal(compact.lines.some(line=>line.length!==40),false);
  assert.equal(compact.clipped,false);
  const tight=render({...screen,keybindings:[{key:'really-long-shortcut',event:'extraordinarily.long.command.name'}]},state,{}, {cols:20,rows:8});
  assert.ok(tight.clipped);
  assert.ok(tight.warnings.some(w=>w.includes('clipped')));
  assert.equal(tight.lines.at(-1),'└'+'─'.repeat(18)+'┘');
 }
});
test('built-in adapters and registry render unknown custom components without app runtime',()=>{
 const screen={id:'sample',title:'Forms',min_size:{cols:30,rows:10},regions:[{id:'root',kind:'container',orientation:'vertical',children:[
  {id:'check',kind:'checkbox',label:'Approved',data_ref:'approved'},{id:'pick',kind:'select',label:'Region',data_ref:'region'},
  {id:'rows',kind:'table',label:'Results',data_ref:'rows'},{id:'dialog',kind:'dialog',label:'Confirm'},
  {id:'custom',kind:'meter',label:'Progress'}]}],keybindings:[]};
 const state={id:'sample.ready',screen:'sample',description:'Ready',terminal:false};
 const fixture={data:{approved:true,region:'Europe',rows:[{name:'One',value:42}]},components:{dialog:{text:'Proceed?'}}};
 const restore=registerMockupRenderer('meter',(node)=>[{text:node.label+': 70%',role:'accent'}]);
 try{const preview=renderFlexibleSnapshot(screen,state,fixture,{cols:70,rows:20});
  assert.match(preview.text,/\[x\] Approved/);assert.match(preview.text,/Europe/);assert.match(preview.text,/name │ value/);
  assert.match(preview.text,/Proceed\?/);assert.match(preview.text,/Progress: 70%/);
 }finally{restore();}
 const fallback=renderFlexibleSnapshot(screen,state,fixture,{cols:70,rows:20});assert.match(fallback.text,/unsupported preview/);
 const mapped=registerMockupRenderer('ink/text-input',()=>[{text:'Termcn adapter invoked',role:'accent'}]);
 try{const mappedScreen={...screen,regions:[{id:'input',kind:'input',label:'Name'}]};const mappedPreview=renderFlexibleSnapshot(mappedScreen,state,{}, {cols:70,rows:12,design:{componentMappings:{input:'ink/text-input'}}});assert.match(mappedPreview.text,/Termcn adapter invoked/);}finally{mapped();}
});
test('content clipping preserves bottom frame and emits warning',()=>{
 const node={id:'root',kind:'container',orientation:'vertical',children:Array.from({length:30},(_,i)=>({id:'n'+i,kind:'text',label:'Long '+i}))};
 const s={id:'sample',title:'Test',min_size:{cols:20,rows:8},regions:[node],keybindings:[{key:'q',event:'quit'}]};
 const state={id:'sample.ready',screen:'sample',terminal:false};
 const preview=renderFlexibleSnapshot(s,state,{}, {cols:32,rows:10});
 assert.ok(preview.clipped);assert.ok(preview.warnings.some(w=>w.includes('clipped')));
 assert.equal(preview.lines.length,10);assert.equal(preview.lines.at(-1),'└'+'─'.repeat(30)+'┘');
 assert.match(preview.text,/q → quit/);
});
test('invalid configurations, unsafe targets, unknown scenario and invalid viewport are rejected',t=>{
 const {dir,out}=project(t);configure(out);
 const d=load(out,'design');d.preview.layouts['../other']={direction:'horizontal'};d.preview.viewports.tiny={cols:9,rows:2};save(out,'design',d);
 const failures=validateVisual(dir,'example-task-tracker').errors;assert.ok(failures.some(x=>x.includes('unknown target')));assert.ok(failures.some(x=>x.includes('tiny')));
 assert.throws(()=>generateMockups({repoRoot:dir,feature:'example-task-tracker'}),/Invalid preview configuration/);
 delete d.preview.layouts['../other'];delete d.preview.viewports.tiny;save(out,'design',d);
 assert.throws(()=>generateMockups({repoRoot:dir,feature:'example-task-tracker',scenario:'nonexistent'}),/Unknown scenario/);
 assert.throws(()=>generateMockups({repoRoot:dir,feature:'example-task-tracker',outputs:'exe'}),/Unsupported mockup output/);
});
test('CLI selects one profile, one scenario and limited output formats',t=>{
 const {dir,out}=project(t);configure(out);
 const command=[path.join(root,'scripts/tui.mjs'),'mockup','--repo-root',dir,'--feature','example-task-tracker','--viewport','compact','--scenario','empty','--outputs','txt,html'];
 const result=execFileSync(process.execPath,command,{encoding:'utf8'});assert.match(result,/9 mockup variants/);
 const manifest=load(path.join(out,'mockups'),'index');assert.equal(manifest.variant_count,9);
 assert.deepEqual(manifest.outputs,['txt','html']);assert.ok(fs.existsSync(path.join(out,'storyboard.html')));
 assert.equal(manifest.items.every(x=>x.text&&!x.svg&&!x.ansi),true);
});
