#!/usr/bin/env node
/** Deterministic semantic terminal mockups and clickable state storyboard.
 * No Go/Bubble Tea runtime is loaded. Previews are approximations of declared UI contracts.
 */
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {getBundle,featureDir,requireFeature,resolveRepoRoot,parseArgs} from './generate-html.mjs';
import {renderFlexibleSnapshot,renderInlineSnapshot} from './mockup/layout.mjs';
import {svgCanvasSnapshot,ansiSnapshot} from './mockup/outputs.mjs';
import {validatePresentationConfig} from './mockup/validate-config.mjs';
import {colorRuns} from './mockup/canvas.mjs';
import {footerGeometry} from './mockup/footer.mjs';
export {registerMockupRenderer} from './mockup/renderers.mjs';

const DEFAULT_THEMES={
 dark:{bg:'#0e1626',panel:'#162338',fg:'#eef4ff',muted:'#91a5c1',accent:'#65e8d6',border:'#3b526e',error:'#ff9a9a'},
 light:{bg:'#f8fafc',panel:'#ffffff',fg:'#1e293b',muted:'#64748b',accent:'#137a72',border:'#b8c7d9',error:'#b42331'}
};
const xml=s=>String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;');
const jsonForScript=x=>JSON.stringify(x).replace(/</g,'\\u003c').replace(/>/g,'\\u003e').replace(/\u2028/g,'\\u2028').replace(/\u2029/g,'\\u2029');
function readMaybe(dir,file){const p=path.join(dir,file);if(!fs.existsSync(p))return null;if(fs.lstatSync(p).isSymbolicLink())throw new Error('Symlinked input refused: '+p);return JSON.parse(fs.readFileSync(p,'utf8'));}
function addManifestFiles(dir,names){
 const p=path.join(dir,'manifest.json');if(!fs.existsSync(p))return;safeOutput(p);const obj=JSON.parse(fs.readFileSync(p,'utf8'));if(!Array.isArray(obj.files))throw new Error('Invalid manifest.files');obj.files=[...new Set([...obj.files,...names])].sort();fs.writeFileSync(p,JSON.stringify(obj,null,2)+'\n');
}
function safeOutput(p){let n=p;while(n!==path.dirname(n)){if(fs.existsSync(n)&&fs.lstatSync(n).isSymbolicLink())throw new Error('Symlinked output refused: '+n);n=path.dirname(n);}}
function cellWidth(s){let n=0;for(const c of String(s)){const u=c.codePointAt(0);if(u<32||u>=0x300&&u<=0x36f||u===0x200d)continue;n+=(u>=0x1100&&u<=0x115f||u>=0x2e80&&u<=0xa4cf||u>=0xac00&&u<=0xd7a3||u>=0xf900&&u<=0xfaff||u>=0xfe10&&u<=0xfe6f||u>=0xff01&&u<=0xff60||u>=0xffe0&&u<=0xffe6||u>=0x1f300&&u<=0x1faff)?2:1;}return n;}
function fit(s,width){let out='',count=0;for(const c of String(s).replace(/[\u0000-\u001f\u007f\u001b]/g,' ').replace(/\s+/g,' ')){const w=cellWidth(c);if(count+w>width)break;out+=c;count+=w;}return out+' '.repeat(Math.max(0,width-count));}
function printable(value){if(value==null)return '';if(typeof value==='object')return Array.isArray(value)?value.map(printable).join(', '):JSON.stringify(value);return String(value);}
function flatten(region){return [region,...(region.children||[]).flatMap(flatten)];}
function lineNodes(node,ctx,depth=0){
 const ind=' '.repeat(Math.min(depth*2,10)),kind=node.kind||'unknown',label=node.label||node.id||kind;
 const override=ctx.fixture.components?.[node.id]||{};
 const designProps=ctx.design?.componentProps?.[ctx.screen.id+'/'+node.id]||{};
 if(override.hidden)return [];
 if(kind==='container'){const children=(node.children||[]).flatMap(n=>lineNodes(n,ctx,depth+(node.id==='root'?0:1)));
   if(node.orientation==='horizontal'&&children.length>1)return [ind+children.map(l=>l.trim()).join('  │  ')];return children;}
 if(kind==='text'){
   const val=override.text??(node.data_ref?ctx.fixture.data?.[node.data_ref]:undefined);
   if(val!==undefined)return [ind+label+': '+printable(val)];
   return [ind+label];}
 if(kind==='list'){
   const items=override.items??ctx.fixture.data?.[node.data_ref]??[];
   const arr=Array.isArray(items)?items:[];
   return [ind+label, ...(arr.length?arr.slice(0,7).map((item,i)=>ind+(i===(ctx.fixture.selectedIndex||0)?'  ▸ ':'    ')+(typeof item==='object'?(item.title||item.label||printable(item)):printable(item))):[ind+'  (no sample items)'])];}
 if(kind==='input'){
   const val=override.value??ctx.fixture.state?.draft_title??designProps.defaultValue??'';
   const disabled=ctx.state.id.endsWith('.saving')||(override.disabled??ctx.fixture.disabled??designProps.disabled??false);
   const focused=ctx.fixture.focus===node.id;
   const shown=printable(val)||(designProps.placeholder?'‹'+printable(designProps.placeholder)+'›':'');
   return [ind+label+': '+(disabled?'[ '+shown+' ] (disabled)':focused?'⟦'+shown+'▌⟧':'[ '+shown+' ]')];}
 if(kind==='button'){
   const disabled=override.disabled??false;
   return [ind+(disabled?'( '+label+' )':'[ '+label+' ]')+(ctx.fixture.focus===node.id?' ◀':'')];}
 if(kind==='status'){
   const message=override.text??(node.id.toLowerCase().includes('error')?ctx.fixture.state?.error_message:undefined)??(node.id.toLowerCase().includes('detail')?ctx.fixture.status:undefined);
   const meaningful=message||(!ctx.fixture.suppressDefaultStatus?ctx.state.description:null);
   return meaningful?[ind+(ctx.state.id.endsWith('.error')||ctx.state.id.endsWith('.invalid')?'! ':'· ')+printable(meaningful)]:[];
 }
 return [ind+'['+kind+': '+label+' — unsupported preview]'];
}
export function renderSnapshot(screen,state,fixture={},options={}){
 const cols=options.cols||80,rows=options.rows||24;
 if(!Number.isInteger(cols)||!Number.isInteger(rows)||cols<20||cols>240||rows<8||rows>90)throw new Error('Viewport must be 20-240 cols and 8-90 rows');
 const violations=[];if(cols<screen.min_size.cols||rows<screen.min_size.rows)violations.push(`Below declared minimum ${screen.min_size.cols}×${screen.min_size.rows}`);
 const inner=cols-4;
 const body=state.terminal?['Flow ended (terminal state).']:screen.regions.flatMap(r=>lineNodes(r,{state,fixture,design:options.design,screen},0));
 const footer=footerGeometry(screen.keybindings,inner,rows);
 let clipped=body.some(s=>cellWidth(s)>inner)||body.length>footer.bodyRows||footer.clipped;
 const content=Array(rows-2).fill('');
 content[0]=` ${screen.title} / ${state.id}${state.terminal?' [TERMINAL]':''}`;
 content[1]='─'.repeat(inner);
 for(let i=0;i<Math.min(body.length,footer.bodyRows);i++)content[i+2]=body[i];
 if(footer.divider!==null)content[footer.divider-1]='─'.repeat(inner);
 for(const [i,line]of footer.lines.entries())content[footer.start+i-1]=line;
 if(cellWidth(content[0])>inner)clipped=true;
 const lines=['┌'+'─'.repeat(cols-2)+'┐',...content.map(s=>'│ '+fit(s,inner)+' │'),'└'+'─'.repeat(cols-2)+'┘'];
 if(clipped)violations.push('Preview content clipped at selected viewport');
 return {text:lines.join('\n'),lines,clipped,warnings:violations};
}
function mergeFixture(a={},b={}){
 return {...a,...b,data:{...(a.data||{}),...(b.data||{})},state:{...(a.state||{}),...(b.state||{})},components:{...(a.components||{}),...(b.components||{})},presentation:{...(a.presentation||{}),...(b.presentation||{})}};
}
function stateFixture(scenarios,state,variant='default'){
 let fixture=mergeFixture(scenarios?.defaults||{},scenarios?.states?.[state.id]||{});
 if(variant!=='default'){
  fixture=mergeFixture(fixture,scenarios?.variants?.[variant]||{});
  fixture=mergeFixture(fixture,scenarios?.stateVariants?.[state.id]?.[variant]||{});
 }
 return fixture;
}
function selections(design,scenarios,options){
 const named=design?.preview?.viewports||{};
 const available=Object.keys(named);
 let viewports;
 if(options.cols!==undefined||options.rows!==undefined){viewports={default:{cols:options.cols??80,rows:options.rows??24}};}
 else if(available.length){viewports=named;}
 else viewports={default:{cols:80,rows:24}};
 if(options.viewport){
  const names=String(options.viewport).split(',').map(x=>x.trim());
  for(const name of names)if(!viewports[name])throw new Error('Unknown viewport: '+name);
  viewports=Object.fromEntries(names.map(name=>[name,viewports[name]]));
 }
 let variants=['default',...new Set([...Object.keys(scenarios?.variants||{}),...Object.values(scenarios?.stateVariants||{}).flatMap(v=>Object.keys(v))])];
 if(options.scenario){variants=String(options.scenario).split(',').map(x=>x.trim());for(const name of variants)if(name!=='default'&&!scenarios?.variants?.[name]&&!Object.values(scenarios?.stateVariants||{}).some(v=>v[name]))throw new Error('Unknown scenario: '+name);}
 const outputs=options.outputs?String(options.outputs).split(',').map(s=>s.trim()):design?.preview?.outputs||['txt','svg','html'];
 if(!outputs.length||outputs.some(x=>!['txt','svg','ansi','html'].includes(x)))throw new Error('Unsupported mockup output format');
 return {viewports,variants:[...new Set(variants)],outputs:[...new Set(outputs)]};
}
function modernLayout(design,screen){
 const prefix=screen.id+'/';return ['layouts','styles'].some(type=>Object.keys(design?.preview?.[type]||{}).some(key=>key.startsWith(prefix)));
}
function validThemes(design){const themes={...DEFAULT_THEMES,...(design?.themes||{})};for(const [k,v] of Object.entries(themes)){
 if(!/^[a-z][\w-]*$/.test(k))throw new Error('Invalid theme id: '+k);
 for(const [key,c]of Object.entries(v))if(!/^(#[0-9a-fA-F]{3,8})$/.test(c))throw new Error('Invalid color token: '+k+'.'+key);
 }return themes;
}
function snapshots(bundle,scenarios,options,selection){
 const screens=bundle.screens?.screens||[],states=bundle.machine?.states||[],transitions=bundle.machine?.transitions||[];
 if(!screens.length||!states.length)throw new Error('Mockups require full screens.json and machine.json; contracts-only features are unsupported');
 const screenIndex=new Map(screens.map(s=>[s.id,s]));const stateIds=new Set(states.map(s=>s.id));
 for(const s of states)if(!screenIndex.has(s.screen))throw new Error('State references missing screen: '+s.id);
 for(const tr of transitions)if(!stateIds.has(tr.from)||!stateIds.has(tr.to))throw new Error('Transition has invalid endpoint: '+tr.id);
 const catalogNames=new Set((options.catalog?.components||[]).map(c=>c.name)),mappings=options.design?.componentMappings||{};
 const mappingWarnings=[];for(const [kind,target]of Object.entries(mappings))if(!catalogNames.has(target))mappingWarnings.push(`Mapping ${kind} → ${target} is not in imported termcn catalog`);
 const allStates=states.map(state=>{
  const screen=screenIndex.get(state.screen),variants=[],modern=modernLayout(options.design,screen);
  for(const [viewport,dimensions]of Object.entries(selection.viewports))for(const scenario of selection.variants){
   const fixture=stateFixture(scenarios,state,scenario);
   const renderer=screen.presentation==='inline'?renderInlineSnapshot:modern?renderFlexibleSnapshot:renderSnapshot;
   const preview=renderer(screen,state,fixture,{...options,...dimensions,viewport});
   variants.push({viewport,scenario,preview,fixtureType:scenarios?.states?.[state.id]||scenarios?.stateVariants?.[state.id]?.[scenario]?'declared sample':'generated fallback'});
  }
  return {...state,screenTitle:screen.title,presentation:screen.presentation||'fullscreen',cli:screen.cli||null,preview:variants[0].preview,variants,fixtureType:variants[0].fixtureType};
 });
 return {screens:screens.map(s=>({id:s.id,title:s.title,presentation:s.presentation||'fullscreen',cli:s.cli||null,states:allStates.filter(t=>t.screen===s.id).map(t=>t.id)})),states:allStates,transitions,initial:bundle.machine.initial,mappingWarnings,viewports:Object.keys(selection.viewports),scenarios:selection.variants};
}
function svgSnapshot(state,theme){const lines=state.preview.lines,cols=lines[0]?cellWidth(lines[0]):80;
 const w=cols*9+40,h=lines.length*19+42;
 const tspans=lines.map((line,i)=>`<text x="20" y="${32+i*19}" xml:space="preserve" fill="${xml(i===1?theme.accent:theme.fg)}">${xml(line)}</text>`).join('');
 return `<svg xmlns="http://www.w3.org/2000/svg" role="img" aria-label="${xml(state.id)} sample terminal mockup" viewBox="0 0 ${w} ${h}" width="${w}" height="${h}"><rect width="100%" height="100%" rx="12" fill="${xml(theme.bg)}"/><g font-family="DejaVu Sans Mono,monospace" font-size="15">${tspans}</g></svg>\n`;
}
function previewRuns(preview){
 return preview.cells?preview.cells.rows.map((_,i)=>colorRuns(preview.cells,i)):
  preview.lines.map((text,i)=>[{text,role:i===1?'accent':'fg'}]);
}
function previewMarkup(preview){
 return previewRuns(preview).map(row=>row.map(run=>`<span class="role-${xml(['fg','border','accent','error','muted'].includes(run.role)?run.role:'fg')}">${xml(run.text)}</span>`).join('')).join('\n');
}
function stylesheet(){return `<style>
:root{font-family:Inter,system-ui,sans-serif;color-scheme:dark;--bg:#0a1020;--fg:#e2e8f0;--sub:#98a9c0;--panel:#131e32;--border:#30425b;--accent:#5eead4}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--fg)}header{position:sticky;top:0;z-index:5;display:flex;justify-content:space-between;align-items:center;gap:12px;padding:15px 24px;background:#111a2b;border-bottom:1px solid #30425b;flex-wrap:wrap}h1{font-size:18px;margin:0}small,.muted{color:var(--sub)}.controls{display:flex;gap:8px;align-items:center;flex-wrap:wrap}select,input{background:#1d2c43;color:var(--fg);border:1px solid #46617f;border-radius:8px;padding:8px;min-width:120px}label{font-size:12px;color:var(--sub)}main{display:grid;grid-template-columns:minmax(0,1fr) 290px;gap:16px;padding:20px;max-width:1800px;margin:auto}.lane{margin-bottom:22px;border:1px solid var(--border);border-radius:12px;background:#0e1728;padding:18px}.lane h2{font-size:15px;display:flex;justify-content:space-between;align-items:center;margin:0 0 14px}.comparison{padding:18px;background:var(--panel);border:1px solid var(--border);border-radius:12px;margin-bottom:18px}.comparison h2{font-size:15px}.comparison .compare-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.comparison .terminal{max-height:320px}.comparison button{margin-top:10px}.lane .states{display:grid;grid-template-columns:repeat(auto-fill,minmax(350px,1fr));gap:12px}.card{background:var(--panel);border:1px solid var(--border);border-radius:10px;padding:12px;min-width:0}.card:focus-within,.card.active{border-color:var(--accent);box-shadow:0 0 0 1px var(--accent)}.card h3{margin:0;font-size:13px;display:flex;justify-content:space-between;gap:8px}.badge{font-size:10px;font-weight:600;color:#92b4cf;background:#1e354a;border-radius:4px;padding:3px 6px}.card p{color:var(--sub);font-size:12px;margin:6px 0 10px}.terminal{background:var(--mock-bg);color:var(--mock-fg);border:1px solid var(--mock-border);padding:10px 12px;border-radius:8px;overflow:auto;max-height:470px}.terminal .role-accent{color:var(--mock-accent)}.terminal .role-muted{color:var(--mock-muted)}.terminal .role-border{color:var(--mock-border)}.terminal .role-error{color:var(--mock-error)}.terminal pre{margin:0;line-height:1.32;font:12px/1.35 'DejaVu Sans Mono',Consolas,monospace;white-space:pre;width:max-content}.edges{padding-top:9px;display:flex;gap:5px;flex-wrap:wrap}button.edge{border:1px solid #375c69;background:#172f3d;color:#9ef0df;border-radius:5px;padding:5px 7px;cursor:pointer;font:11px monospace}button.edge:hover{background:#25475b}aside{position:sticky;top:88px;align-self:start;background:#121e31;border:1px solid var(--border);border-radius:10px;padding:16px;max-height:calc(100vh - 105px);overflow:auto}aside h2{font-size:15px;margin:0 0 12px}aside h3{font-size:13px}aside pre{white-space:pre-wrap;word-break:break-word;font-size:12px;color:#c6d6e9}.warn{font-size:11px;color:#ffbf9a}.note{font-size:11px;color:#92a3b8}.sr{position:absolute;left:-9999px}footer{padding:12px 24px;color:#92a3b8;border-top:1px solid var(--border);font-size:11px}
@media(max-width:970px){main{display:block}.comparison .compare-grid{grid-template-columns:1fr}aside{position:static;margin-bottom:16px;max-height:none}.lane .states{grid-template-columns:repeat(auto-fill,minmax(min(100%,320px),1fr))}}
</style>`;}
function generateHTML(model,themes,chosen){
 const view=model.screens.map(screen=>`<section class="lane" id="screen-${xml(screen.id)}"><h2>${xml(screen.title)} <small>${xml(screen.presentation)} · ${screen.states.length} state${screen.states.length===1?'':'s'}</small></h2><div class="states">${screen.states.map(id=>{
 const s=model.states.find(x=>x.id===id);
 const outgoing=model.transitions.filter(t=>t.from===s.id);
 return `<article class="card" id="state-${xml(s.id)}" data-state="${xml(s.id)}" data-screen="${xml(s.screen)}"><h3><span>${xml(s.id)}</span><span class="badge">${s.terminal?'TERMINAL':s.fixtureType}</span></h3><p>${xml(s.description||'')}</p><div class="terminal"><pre>${previewMarkup(s.preview)}</pre></div><div class="edges">${outgoing.map(t=>`<button type="button" class="edge" data-transition="${xml(t.id)}" title="${xml(t.description||'')}">${xml(t.event)} → ${xml(t.to)}</button>`).join('')||'<span class="note">No outgoing transitions</span>'}<button type="button" class="edge compare-action" data-compare="${xml(s.id)}">Compare</button></div>${s.preview.warnings.map(w=>`<p class="warn">${xml(w)}</p>`).join('')}</article>`;}).join('')}</div></section>`).join('');
 const profiles=Object.keys(themes);const colors={...themes[chosen]};
 const browserModel={...model,states:model.states.map(s=>({...s,preview:{text:s.preview.text,warnings:s.preview.warnings,runs:previewRuns(s.preview)},variants:s.variants.map(v=>({...v,preview:{text:v.preview.text,warnings:v.preview.warnings,runs:previewRuns(v.preview)}}))}))};
 const html=`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Terminal interaction storyboard</title>${stylesheet()}</head><body><header><div><h1>Terminal interaction storyboard</h1><small>${model.screens.length} views · ${model.states.length} states · ${model.transitions.length} transitions · semantic preview, not runtime capture</small></div><div class="controls"><label for="find">Find state</label><input id="find" placeholder="Filter states…"><label for="profile">Theme</label><select id="profile">${profiles.map(p=>`<option value="${xml(p)}" ${p===chosen?'selected':''}>${xml(p)}</option>`).join('')}</select>${model.viewports.length>1?`<label for="viewport">Viewport</label><select id="viewport">${model.viewports.map(v=>`<option value="${xml(v)}">${xml(v)}</option>`).join('')}</select>`:''}${model.scenarios.length>1?`<label for="scenario">Scenario</label><select id="scenario">${model.scenarios.map(v=>`<option value="${xml(v)}">${xml(v)}</option>`).join('')}</select>`:''}</div></header><main><div id="story"><section id="comparison" class="comparison" hidden><h2>Side-by-side comparison</h2><p id="compare-note" class="note">Select a second state to compare.</p><div id="compare-grid" class="compare-grid"></div><button class="edge" id="clear-compare" type="button">Clear comparison</button></section>${view}</div><aside aria-live="polite"><h2>Selection inspector</h2><p class="note">Select a state or transition to inspect the canonical metadata.</p><pre id="details">Initial state: ${xml(model.initial)}</pre><h3>Component adapter status</h3><p class="note">Clack transcript sketches are semantic approximations; they are not live Bubble Tea captures.</p>${model.mappingWarnings.map(w=>`<p class="warn">${xml(w)}</p>`).join('')}</aside></main><footer>Self-contained offline output · No application commands are executed · Sample data is illustrative</footer><script>const MODEL=${jsonForScript(browserModel)};const THEMES=${jsonForScript(themes)};
const applyTheme=name=>{const t=THEMES[name];if(!t)return;document.querySelectorAll('.terminal').forEach(e=>{e.style.setProperty('--mock-bg',t.bg);e.style.setProperty('--mock-fg',t.fg);e.style.setProperty('--mock-border',t.border);e.style.setProperty('--mock-muted',t.muted);e.style.setProperty('--mock-accent',t.accent);e.style.setProperty('--mock-error',t.error);});};
applyTheme(${jsonForScript(chosen)});document.getElementById('profile').addEventListener('change',e=>applyTheme(e.target.value));
const renderRuns=(pre,runs)=>{pre.replaceChildren();for(const [i,row] of runs.entries()){if(i)pre.append(document.createTextNode('\\n'));for(const run of row){const span=document.createElement('span');span.className='role-'+(['fg','border','accent','error','muted'].includes(run.role)?run.role:'fg');span.textContent=run.text;pre.appendChild(span);}}};
const selectedCompare=[];
const variantFor=state=>{const viewport=document.getElementById('viewport')?.value||MODEL.viewports[0],scenario=document.getElementById('scenario')?.value||MODEL.scenarios[0];return state.variants.find(v=>v.viewport===viewport&&v.scenario===scenario);};
const renderComparison=()=>{const target=document.getElementById('comparison'),grid=document.getElementById('compare-grid');target.hidden=!selectedCompare.length;grid.replaceChildren();document.getElementById('compare-note').textContent=selectedCompare.length<2?'Select a second state to compare.':'Comparing selected states in the current viewport and scenario.';
for(const id of selectedCompare){const state=MODEL.states.find(s=>s.id===id),variant=state&&variantFor(state);if(!variant)continue;const box=document.createElement('div');box.className='card';const heading=document.createElement('h3');heading.textContent=id;box.appendChild(heading);const terminal=document.createElement('div');terminal.className='terminal';const pre=document.createElement('pre');renderRuns(pre,variant.preview.runs);terminal.appendChild(pre);box.appendChild(terminal);grid.appendChild(box);}applyTheme(document.getElementById('profile').value);};
const setPreview=()=>{const viewport=document.getElementById('viewport')?.value||MODEL.viewports[0],scenario=document.getElementById('scenario')?.value||MODEL.scenarios[0];document.querySelectorAll('.card').forEach(card=>{const state=MODEL.states.find(s=>s.id===card.dataset.state);const variant=state?.variants.find(v=>v.viewport===viewport&&v.scenario===scenario);if(variant){renderRuns(card.querySelector('.terminal pre'),variant.preview.runs);card.querySelectorAll('.warn[data-variant]').forEach(e=>e.remove());for(const w of variant.preview.warnings){const p=document.createElement('p');p.className='warn';p.dataset.variant='';p.textContent=w;card.appendChild(p);}}});renderComparison();};
for(const id of ['viewport','scenario'])document.getElementById(id)?.addEventListener('change',setPreview);
document.querySelectorAll('[data-compare]').forEach(btn=>btn.addEventListener('click',()=>{const id=btn.dataset.compare;const old=selectedCompare.indexOf(id);if(old>=0)selectedCompare.splice(old,1);else {if(selectedCompare.length===2)selectedCompare.shift();selectedCompare.push(id);}renderComparison();}));document.getElementById('clear-compare').addEventListener('click',()=>{selectedCompare.length=0;renderComparison();});
const details=document.getElementById('details');document.getElementById('find').addEventListener('input',e=>{const q=e.target.value.toLowerCase();document.querySelectorAll('.card').forEach(c=>{const s=MODEL.states.find(x=>x.id===c.dataset.state);c.hidden=!(s.id.toLowerCase().includes(q)||s.description?.toLowerCase().includes(q));});});
const choose=(id)=>{document.querySelectorAll('.card.active').forEach(c=>c.classList.remove('active'));const s=MODEL.states.find(x=>x.id===id);if(!s)return;const el=document.getElementById('state-'+id);el?.classList.add('active');details.textContent=JSON.stringify({state:s.id,screen:s.screen,description:s.description,terminal:s.terminal,exit_code:s.exit_code??null,presentation:s.presentation,cli:s.cli,fixture:s.fixtureType,preview_warnings:s.preview.warnings,outgoing:MODEL.transitions.filter(t=>t.from===id)},null,2);};
document.querySelectorAll('.card').forEach(c=>c.addEventListener('click',e=>{if(!e.target.closest('button.edge'))choose(c.dataset.state)}));
document.querySelectorAll('button.edge').forEach(b=>b.addEventListener('click',()=>{const t=MODEL.transitions.find(x=>x.id===b.dataset.transition);if(!t)return;choose(t.to);details.textContent=JSON.stringify(t,null,2);document.getElementById('state-'+t.to)?.scrollIntoView({behavior:'smooth',block:'center'});}));
choose(MODEL.initial);</script></body></html>`;
 return html;
}
export function generateMockups({repoRoot,feature,cols,rows,theme='dark',viewport,scenario,outputs}){
 const dir=featureDir(repoRoot,requireFeature(feature)),bundle=getBundle(repoRoot,feature),design=readMaybe(dir,'design.json'),scenarios=readMaybe(dir,'scenarios.json'),catalog=readMaybe(dir,'components.catalog.json');
 const configErrors=validatePresentationConfig(design,scenarios,bundle.screens?.screens||[],bundle.machine?.states||[]);if(configErrors.length)throw new Error('Invalid preview configuration: '+configErrors.join('; '));
 const themes=validThemes(design);if(!themes[theme])throw new Error('Unknown theme: '+theme);
 const selection=selections(design,scenarios,{cols,rows,viewport,scenario,outputs});
 const data=snapshots(bundle,scenarios,{catalog,design},selection),outputDir=path.join(dir,'mockups');safeOutput(outputDir);fs.mkdirSync(outputDir,{recursive:true});
 const multiple=Object.keys(selection.viewports).length*selection.variants.length>1;
 const items=[],usedNames=new Set();
 for(const state of data.states)for(const variant of state.variants){
  const name=[state.id,...(multiple?[variant.viewport,variant.scenario]:[])].join('.').replace(/[^a-zA-Z0-9_.-]/g,'_');
  if(usedNames.has(name))throw new Error('Duplicate preview output name: '+name);usedNames.add(name);
  const item={state:state.id,screen:state.screen,presentation:state.presentation,exit_code:state.exit_code??null,viewport:variant.viewport,scenario:variant.scenario,warnings:variant.preview.warnings};
  for(const format of ['txt','svg','ansi'])if(selection.outputs.includes(format)){
   const file=path.join(outputDir,name+'.'+format);safeOutput(file);
   const output=format==='txt'?variant.preview.text+'\n':format==='svg'?(variant.preview.cells?svgCanvasSnapshot(variant.preview,themes[theme],state.id):svgSnapshot({...state,preview:variant.preview},themes[theme])):ansiSnapshot(variant.preview,themes[theme]);
   fs.writeFileSync(file,output);item[format==='txt'?'text':format]='mockups/'+name+'.'+format;
  }
  items.push(item);
 }
 const first=Object.values(selection.viewports)[0];
 const manifest={schema_version:'1.1.0',generator:'semantic-terminal-mockup-v1.6',source:'canonical screens.json + machine.json',note:'Mockups are semantic previews, not live clackbt terminal captures.',viewport:first,viewports:selection.viewports,scenarios:selection.variants,outputs:selection.outputs,theme,components_cataloged:catalog?.components?.length||0,screen_count:data.screens.length,state_count:data.states.length,variant_count:items.length,transition_count:data.transitions.length,presentations:[...new Set(data.screens.map(s=>s.presentation))],items,mapping_warnings:data.mappingWarnings};
 safeOutput(path.join(outputDir,'index.json'));fs.writeFileSync(path.join(outputDir,'index.json'),JSON.stringify(manifest,null,2)+'\n');
 let htmlPath=null;if(selection.outputs.includes('html')){
  htmlPath=path.join(dir,'storyboard.html');safeOutput(htmlPath);fs.writeFileSync(htmlPath,generateHTML(data,themes,theme));addManifestFiles(dir,['storyboard.html']);
 }
 return {html:htmlPath,manifest:path.join(outputDir,'index.json'),...manifest};
}
if(process.argv[1]&&path.resolve(process.argv[1])===fileURLToPath(import.meta.url)){
 try{const argv=parseArgs(process.argv.slice(2));const o=generateMockups({repoRoot:resolveRepoRoot(argv['repo-root']),feature:argv.feature,cols:argv.cols!==undefined?Number(argv.cols):undefined,rows:argv.rows!==undefined?Number(argv.rows):undefined,theme:argv.theme||'dark',viewport:argv.viewport,scenario:argv.scenario,outputs:argv.outputs});console.log(`Generated ${o.variant_count} mockup variants${o.html?' and storyboard: '+o.html:''}`);for(const w of o.mapping_warnings)console.warn('WARN '+w);}
 catch(e){console.error('Error: '+e.message);process.exitCode=1;}
}
