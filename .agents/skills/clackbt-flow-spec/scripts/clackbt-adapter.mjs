/** clackbt Go/Bubble Tea v2 backend binding and illustrative prompt transcript. */
import fs from 'node:fs';
import path from 'node:path';
import {featureDir} from './generate-html.mjs';

export const MODEL_TYPES = Object.freeze({
  TextModel:'string', PasswordModel:'string', ConfirmModel:'bool',
  SelectModel:'comparable', MultiSelectModel:'slice', GroupMultiSelectModel:'slice',
  SelectKeyModel:'comparable', ReorderModel:'slice', PathModel:'string',
  SelectPathModel:'string', MultiSelectPathModel:'slice',
});
export const ONE_SHOT = Object.freeze(Object.fromEntries(Object.keys(MODEL_TYPES).map(k=>[k.slice(0,-5),MODEL_TYPES[k]])));
const validId = s=>typeof s==='string'&&/^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$/.test(s);
const read=(dir,file)=>{
  const f=path.join(dir,file);
  if(!fs.existsSync(f))return null;
  if(fs.lstatSync(f).isSymbolicLink())throw new Error('Refusing symlinked clackbt contract: '+f);
  return JSON.parse(fs.readFileSync(f,'utf8'));
};
const allNodes=s=>(s.regions||[]).flatMap(function flatten(n){return [n,...(n.children||[]).flatMap(flatten)];});
const isObj=x=>x!==null&&!Array.isArray(x)&&typeof x==='object';
const err=(errors,msg)=>errors.push('clackbt.json: '+msg);
const warn=(warnings,msg)=>warnings.push('clackbt.json: '+msg);
export function validateClackbt(repoRoot, feature){
  const dir=featureDir(repoRoot,feature),errors=[],warnings=[];
  const cfg=read(dir,'clackbt.json');
  if(!cfg)return {errors,warnings,exists:false};
  const screens=read(dir,'screens.json')?.screens||[];
  const machine=read(dir,'machine.json')?.states||[];
  const screenMap=new Map(screens.map(s=>[s.id,s]));
  const machineIds=new Set(machine.map(s=>s.id));
  if(cfg.schema_version!=='1.0.0')err(errors,'schema_version must be 1.0.0');
  if(cfg.backend!=='clackbt-go')err(errors,'backend must be clackbt-go');
  if(cfg.presentation!=='inline')err(errors,'clackbt prompts must use presentation inline (use internal/ui for fullscreen views)');
  if(!['native-workflow','single-prompt','output-only'].includes(cfg.interaction))err(errors,'invalid interaction');
  if(typeof cfg.command!=='string'||!cfg.command.trim())err(errors,'command is required');
  if(typeof cfg.go_module!=='string'||!cfg.go_module.trim())err(errors,'go_module is required; read current go.mod');
  const interactive=cfg.interaction!=='output-only';
  if(!isObj(cfg.tty)||typeof cfg.tty.required!=='boolean'||!['error','skip','flags','noninteractive'].includes(cfg.tty.on_missing))err(errors,'tty.required and tty.on_missing (error|skip|flags|noninteractive) are required');
  if(interactive&&cfg.tty?.required!==true)err(errors,'interactive Clack models require an explicitly documented TTY gate');
  if(!isObj(cfg.io)||!['human','machine','none'].includes(cfg.io.stdout)||!['diagnostics','human','none'].includes(cfg.io.stderr))err(errors,'io.stdout and io.stderr must specify stream contracts');
  if(cfg.io?.stdout==='machine'&&interactive)warn(warnings,'machine-readable stdout alongside prompts requires a deliberate separation strategy');
  if(!isObj(cfg.cancellation)||!['core.ErrCancelPrompt','not-applicable'].includes(cfg.cancellation.error)||typeof cfg.cancellation.on_interrupt!=='string')err(errors,'cancellation.error and cancellation.on_interrupt must be specified');
  if(interactive&&cfg.cancellation?.error!=='core.ErrCancelPrompt')err(errors,'interactive models signal cancellation via core.ErrCancelPrompt');
  if(interactive&&cfg.cancellation?.handler!=='prompts.IsCancel')err(errors,'handle cancellation via prompts.IsCancel at command boundary');
  if(cfg.cancellation?.exit_code!==undefined&&cfg.cancellation.exit_code!==null&&(!Number.isInteger(cfg.cancellation.exit_code)||cfg.cancellation.exit_code<0||cfg.cancellation.exit_code>255))err(errors,'cancellation.exit_code must be null or 0..255');
  if(!Array.isArray(cfg.steps))err(errors,'steps must be an array');
  const steps=Array.isArray(cfg.steps)?cfg.steps:[];
  if(interactive&&!steps.length)err(errors,'interactive mode requires at least one prompt step');
  if(cfg.interaction==='single-prompt'&&steps.length!==1)err(errors,'single-prompt mode requires exactly one step');
  if(cfg.interaction==='output-only'&&steps.length)err(errors,'output-only mode cannot include interactive steps');
  if(!isObj(cfg.result_struct))err(errors,'result_struct must be a mapping of exported Go fields to types');
  const ids=new Set(),fields=new Set();
  steps.forEach((step,index)=>{
    const base=`steps[${index}]`;
    if(!isObj(step)){err(errors,base+' must be object');return;}
    if(!validId(step.id)||ids.has(step.id))err(errors,base+'.id is invalid/duplicate');
    ids.add(step.id);
    if(typeof step.field!=='string'||!/^[A-Z][a-zA-Z0-9]*$/.test(step.field))err(errors,base+'.field must be an exported Go struct field');
    if(fields.has(step.field))err(errors,base+'.field is duplicated');
    fields.add(step.field);
    if(!isObj(cfg.result_struct)||typeof cfg.result_struct[step.field]!=='string')err(errors,base+'.field missing in result_struct');
    if(!validId(step.screen_id)||!screenMap.has(step.screen_id))err(errors,base+'.screen_id is not a known screen');
    const screen=screenMap.get(step.screen_id);
    if(screen&&(screen.presentation!=='inline'||screen.cli?.interaction!=='prompted'||screen.cli?.stdin!=='tty'))err(errors,base+'.screen_id must refer to an inline, prompted, tty screen');
    if(screen&&!allNodes(screen).some(n=>n.kind==='prompt'||n.kind==='clack-prompt'))err(errors,base+'.screen_id needs a prompt or clack-prompt node');
    const allowed=cfg.interaction==='single-prompt'?ONE_SHOT:MODEL_TYPES;
    if(!Object.hasOwn(allowed,step.api))err(errors,base+'.api is not a supported '+cfg.interaction+' API');
    const expected=allowed[step.api],goType=cfg.result_struct?.[step.field];
    if(expected==='string'&&goType!=='string')err(errors,base+'.field must have Go type string');
    if(expected==='bool'&&goType!=='bool')err(errors,base+'.field must have Go type bool');
    if(expected==='slice'&&!/^\[\][\w.\[\]*]+$/.test(goType||''))err(errors,base+'.field must have a Go slice type');
    if(expected==='comparable'&&/^\[\]|^map\[|^func\(/.test(goType||''))err(errors,base+'.field must have comparable Go value type');
    if(!Array.isArray(step.states)||!step.states.length||step.states.some(x=>!machineIds.has(x)))err(errors,base+'.states must reference machine states');
    if(step.states?.some(x=>machine.find(y=>y.id===x)?.screen!==step.screen_id))err(errors,base+'.states must belong to step.screen_id');
    if(!isObj(step.params)||typeof step.params.Message!=='string'||!step.params.Message.trim())err(errors,base+'.params.Message is required');
    if(step.conditional===true&&(!step.condition||typeof step.condition!=='string'))err(errors,base+'.conditional requires a documented condition');
    if(step.api==='Password'||step.api==='PasswordModel'){
      if(step.preview?.answer!==undefined||step.preview?.value!==undefined||step.preview?.options!==undefined||step.preview?.input!==undefined)err(errors,base+'.preview must not contain secret input');
    }
    if(/^(?:Select|MultiSelect|GroupMultiSelect|Reorder)/.test(step.api)&&!Array.isArray(step.params?.Options))warn(warnings,base+'.params.Options must be checked against the current Go API and source');
    const options=step.params?.Options;
    if(Array.isArray(options)){
      if(options.length===0)err(errors,base+'.params.Options cannot be empty');
      if(options.some(o=>!isObj(o)||typeof o.Label!=='string'||!('Value' in o)))warn(warnings,base+'.params.Options may not match the selected prompt option type');
    }
  });
  if(cfg.interaction==='native-workflow'&&steps.length===1)warn(warnings,'native-workflow with one step is valid but a one-shot helper may be simpler');
  if(cfg.interaction==='native-workflow'&&cfg.implementation?.runner!=='RunNative')err(errors,'native-workflow requires implementation.runner = RunNative');
  if(cfg.interaction==='native-workflow'&&cfg.implementation?.allow_legacy_steps!==false)err(errors,'native-workflow cannot include Step/ConditionalStep callbacks or nested Run calls');
  if(cfg.interaction==='single-prompt'&&cfg.implementation?.runner!=='one-shot')err(errors,'single-prompt requires implementation.runner = one-shot');
  if(cfg.interaction==='native-workflow'&&steps.some(s=>s.api?.endsWith('Model')===false))err(errors,'native-workflow steps must be composable model constructors');
  if(!isObj(cfg.terminal)||typeof cfg.terminal.success_exit_code!=='number'||!Number.isInteger(cfg.terminal.success_exit_code)||cfg.terminal.success_exit_code<0||cfg.terminal.success_exit_code>255)err(errors,'terminal.success_exit_code must be an explicit 0..255 value');
  const full=read(dir,'manifest.json')?.artifact_scope==='full';
  if(full&&screenMap.size&&steps.length){
    const terminalCodes=new Set(machine.filter(s=>s.terminal).map(s=>s.exit_code));
    if(cfg.terminal?.success_exit_code!==undefined&&!terminalCodes.has(cfg.terminal.success_exit_code))warn(warnings,'success exit code is not represented by any terminal machine state');
  }
  return {exists:true,errors,warnings};
}
const esc=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const redact=(step,x)=>step.api?.startsWith('Password')?'••••••':String(x??'');
const frame=(step,mode)=>{
 const message=step.params?.Message||step.id,preview=step.preview||{},kind=step.api||'';
 if(mode==='completed')return ['◇  '+message,'│  '+redact(step,preview.answer??(kind.startsWith('Confirm')?'Yes':'(example answer)')),'│'];
 if(mode==='cancelled')return ['■  '+message,'│  Cancelled','└'];
 if(mode==='invalid')return ['◆  '+message,'│  '+(kind.startsWith('Password')?'••••••':redact(step,preview.input??'')),'│  ! '+String(preview.error||'Validation failed'),'└'];
 const out=['◆  '+message];
 if(Array.isArray(preview.options)&&preview.options.length){
  preview.options.slice(0,8).forEach((v,i)=>out.push('│  '+(i===0?'●':'○')+' '+String(v)));
 } else if(kind.startsWith('Confirm'))out.push('│  ● Yes  ○ No');
 else out.push('│  '+(kind.startsWith('Password')?'••••••':String(preview.placeholder??step.params?.Placeholder??'‹enter value›'))+' ▌');
 out.push('└');return out;
};
export function renderClackbtPreview(repoRoot,feature){
 const dir=featureDir(repoRoot,feature),cfg=read(dir,'clackbt.json');if(!cfg)return null;
 const rows=[];
 for(let i=0;i<(cfg.steps||[]).length;i++){
  const step=cfg.steps[i];rows.push('STAGE '+(i+1)+' — '+step.id+' ('+step.api+')');
  for(const previous of cfg.steps.slice(0,i))rows.push(...frame(previous,'completed'));
  rows.push(...frame(step,'active'),'');
 }
 if(cfg.steps?.length){rows.push('COMPLETE');for(const step of cfg.steps)rows.push(...frame(step,'completed'));rows.push('');}
 rows.push('CANCELLATION (illustrative)',...(cfg.steps?.length?[...frame(cfg.steps[0],'cancelled')]:['(No prompts)']));
 rows.push('');rows.push('Synthetic transcript only; NOT runtime output, interaction testing, or exact ANSI/byte fidelity.');
 const content=rows.join('\n')+'\n';
 const outfile=path.join(dir,'clackbt-transcript.txt');if(fs.existsSync(outfile)&&fs.lstatSync(outfile).isSymbolicLink())throw new Error('Refusing symlinked clackbt transcript output: '+outfile);fs.writeFileSync(outfile,content,'utf8');
 const groups=content.split(/(?=^STAGE \d|^COMPLETE$|^CANCELLATION \(illustrative\)$)/m).filter(Boolean);
 const html=`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>clackbt prompt preview — ${esc(feature)}</title><style>body{font:16px system-ui,sans-serif;background:#101319;color:#e5e7eb;margin:0 auto;padding:32px;max-width:920px}h1{font-size:24px}p{color:#aab4c5}nav{display:flex;gap:8px;flex-wrap:wrap}button{background:#202a38;border:1px solid #516073;border-radius:8px;padding:9px 14px;color:#eee;cursor:pointer}button[aria-pressed="true"]{background:#3a5272}pre{font:15px/1.65 ui-monospace,SFMono-Regular,Consolas,monospace;white-space:pre-wrap;overflow-wrap:anywhere;border-radius:12px;background:#171e29;border:1px solid #526174;padding:24px;min-height:140px}@media(max-width:600px){body{padding:14px}pre{font-size:13px;padding:14px}}</style><h1>${esc(feature)} · clackbt transcript</h1><p>Spec-driven approximation. No Go application is executed; illustrations are not byte-for-byte terminal output.</p><nav id="tabs"></nav><pre id="screen" aria-live="polite"></pre><script>const G=${JSON.stringify(groups).replace(/</g,'\\u003c')};const tabs=document.getElementById('tabs'),view=document.getElementById('screen');G.forEach((g,i)=>{let b=document.createElement('button');b.textContent=g.split('\\n')[0];b.onclick=()=>{view.textContent=g;for(const x of tabs.children)x.setAttribute('aria-pressed',String(x===b))};tabs.append(b)});if(tabs.firstChild)tabs.firstChild.click();</script></html>`;
 const htmlfile=path.join(dir,'clackbt-preview.html');if(fs.existsSync(htmlfile)&&fs.lstatSync(htmlfile).isSymbolicLink())throw new Error('Refusing symlinked clackbt preview output: '+htmlfile);fs.writeFileSync(htmlfile,html,'utf8');
 return {text:outfile,html:htmlfile,stages:cfg.steps?.length||0};
}
