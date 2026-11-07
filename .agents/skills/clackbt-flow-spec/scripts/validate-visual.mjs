/** Validate presentation-only references without changing canonical behavior. */
import fs from 'node:fs';
import path from 'node:path';
import {featureDir} from './generate-html.mjs';
import {validatePresentationConfig} from './mockup/validate-config.mjs';
const read=(d,f)=>{const p=path.join(d,f);if(!fs.existsSync(p))return null;if(fs.lstatSync(p).isSymbolicLink())throw new Error('Symlinked visual file refused: '+p);return JSON.parse(fs.readFileSync(p,'utf8'));};
function nodes(r){return [r,...(r.children||[]).flatMap(nodes)];}
export function validateVisual(repoRoot,feature){
 const dir=featureDir(repoRoot,feature),errors=[],warnings=[];
 const screens=read(dir,'screens.json')?.screens||[],states=read(dir,'machine.json')?.states||[];
 const design=read(dir,'design.json'),scenarios=read(dir,'scenarios.json'),catalog=read(dir,'components.catalog.json');
 errors.push(...validatePresentationConfig(design,scenarios,screens,states));
 for(const [kind,mapping]of Object.entries(design?.componentMappings||{})){
  if(!/^(ink|opentui)\/[a-z0-9][a-z0-9-]*$/.test(mapping))errors.push('design.json: invalid mapping '+mapping);
  else if(!catalog?.components?.some(c=>c.name===mapping))warnings.push(`design.json: ${kind} mapping ${mapping} has no imported schema`);
 }
 const screenById=new Map(screens.map(s=>[s.id,s]));
 const findNode=(screen,id)=>screen?.regions.flatMap(nodes).find(n=>n.id===id);
 function validProp(value,spec){
  if('const' in spec)return value===spec.const;
  if(spec.enum)return spec.enum.includes(value);
  if(spec.anyOf)return spec.anyOf.some(part=>validProp(value,part));
  if(spec.type==='string')return typeof value==='string';
  if(spec.type==='number'||spec.type==='integer')return typeof value==='number'&&Number.isFinite(value)&&(spec.type!=='integer'||Number.isInteger(value));
  if(spec.type==='boolean')return typeof value==='boolean';
  if(spec.type==='null')return value===null;
  if(spec.type==='array')return Array.isArray(value)&&value.every(v=>validProp(v,spec.items||{}));
  if(spec.type==='object')return value!==null&&typeof value==='object'&&!Array.isArray(value)&&Object.entries(value).every(([k,v])=>spec.properties?.[k]?validProp(v,spec.properties[k]):spec.additionalProperties!==false)&&(!spec.required||spec.required.every(k=>k in value));
  return false;
 }
 for(const [target,props]of Object.entries(design?.componentProps||{})){
  const index=target.indexOf('/');const sid=target.slice(0,index),nid=target.slice(index+1),node=findNode(screenById.get(sid),nid);
  if(index<1||!node){errors.push(`design.json: unknown component target ${target}`);continue;}
  const mapping=design?.componentMappings?.[node.kind];
  const entry=catalog?.components?.find(c=>c.name===mapping);
  if(!entry){warnings.push(`design.json: ${target} props cannot be checked until ${node.kind} mapping is imported`);continue;}
  let schema;try{schema=read(dir,entry.schema);}catch(e){errors.push(`design.json: schema unavailable for ${target}: ${e.message}`);continue;}
  for(const [name,value]of Object.entries(props||{})){
   const spec=schema?.properties?.[name];
   if(!spec)errors.push(`design.json: ${target}.${name} is not a supported serializable prop`);
   else if(!validProp(value,spec))errors.push(`design.json: ${target}.${name} fails the imported termcn schema`);
  }
 }
 for(const [name,t]of Object.entries(design?.themes||{})){
  for(const token of ['bg','panel','fg','muted','accent','border','error'])if(!/^#[0-9a-fA-F]{6}$/.test(t[token]||''))errors.push(`design.json: theme ${name} missing valid ${token} color`);
 }
 if(catalog){for(const c of catalog.components||[]){
  if(!/^(ink|opentui)\/[a-z0-9][a-z0-9-]*$/.test(c.name))errors.push('components.catalog.json: invalid component '+c.name);
  if(!/^components\.schemas\/(ink|opentui)--[a-z0-9][a-z0-9-]*\.schema\.json$/.test(c.schema||''))errors.push('components.catalog.json: unsafe schema path '+c.schema);
  else if(!fs.existsSync(path.join(dir,c.schema)))errors.push('components.catalog.json: missing '+c.schema);
 }}
 return {errors,warnings};
}
