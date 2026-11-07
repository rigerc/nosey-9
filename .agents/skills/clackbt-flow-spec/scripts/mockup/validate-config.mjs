/** Structural checks for optional presentation extensions (independent from canonical behavior). */
const object=v=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const name=/^[a-z][a-z0-9-]*$/;
const width=w=>Number.isInteger(w)&&w>=1&&w<=240||typeof w==='string'&&/^([1-9][0-9]?|100)%$/.test(w);
const available={
 direction:['horizontal','vertical','grid','overlay'],border:['none','single','rounded','double'],align:['left','center','right'],valign:['top','center','bottom'],role:['fg','muted','accent','border','error']
};
function keys(value,allow,where,errors){if(!object(value)){errors.push(`${where}: expected object`);return false;}for(const key of Object.keys(value))if(!allow.includes(key))errors.push(`${where}: unsupported property ${key}`);return true;}
function configProps(value,allowed,where,errors){
 if(!keys(value,allowed,where,errors))return;
 for(const [k,v] of Object.entries(value)){
  if(available[k]&&!available[k].includes(v))errors.push(`${where}.${k}: invalid value`);
  else if(k==='width'&&!width(v))errors.push(`${where}.width: invalid dimension`);
  else if(['gap','padding'].includes(k)&&(!Number.isInteger(v)||v<0||v>20))errors.push(`${where}.${k}: expected integer 0-20`);
  else if(k==='columns'&&Array.isArray(v)&&!v.every(x=>typeof x==='string'))errors.push(`${where}.columns: invalid list`);
  else if(k==='columns'&&!Array.isArray(v)&&(!Number.isInteger(v)||v<1||v>12))errors.push(`${where}.columns: invalid count`);
  else if(k==='maxHeight'&&(!Number.isInteger(v)||v<1||v>90))errors.push(`${where}.maxHeight: expected 1-90`);
  else if(k==='maxItems'&&(!Number.isInteger(v)||v<0||v>100))errors.push(`${where}.maxItems: expected 0-100`);
  else if(k==='wrap'&&typeof v!=='boolean')errors.push(`${where}.wrap: expected boolean`);
 }
}
export function validatePresentationConfig(design,scenarios,screens,states){
 const errors=[];
 const screenNodes=new Map(screens.map(s=>[s.id,new Set(s.regions.flatMap(function walk(n){return [n.id,...(n.children||[]).flatMap(walk)];}))]));
 const ids=new Set(states.map(s=>s.id)),screenIds=new Set(screens.map(s=>s.id));
 const allowedLayout=['direction','columns','width','padding','gap','border','align','valign','byViewport'];
 const allowedStyle=['width','padding','border','align','role','wrap','maxHeight','maxItems','columns','byViewport'];
 for(const [type,v]of [['design',design],['scenarios',scenarios]]){
  if(!v)continue;
  const expected=type==='design'?['schema_version','componentProps','componentMappings','themes','preview']:['schema_version','defaults','states','variants','stateVariants'];
  if(!keys(v,expected,`${type}.json`,errors))continue;
  if(!['1.0.0','1.1.0'].includes(v.schema_version))errors.push(`${type}.json: unsupported schema_version`);
  if(v.schema_version==='1.0.0'&&(type==='design'&&v.preview||type==='scenarios'&&(v.variants||v.stateVariants)))errors.push(`${type}.json: extensions require schema_version 1.1.0`);
 }
 if(design?.preview){
  const v=design.preview;
  if(keys(v,['viewports','layouts','styles','outputs'],'design.preview',errors)){
   if(v.viewports!==undefined){
    if(!object(v.viewports)||!Object.keys(v.viewports).length)errors.push('design.preview.viewports: must contain named profiles');
    else for(const [label,item] of Object.entries(v.viewports)){
     if(!name.test(label))errors.push('design.preview.viewports: invalid name '+label);
     if(!keys(item,['cols','rows'],`design.preview.viewports.${label}`,errors))continue;
     if(!Number.isInteger(item.cols)||item.cols<20||item.cols>240||!Number.isInteger(item.rows)||item.rows<8||item.rows>90)errors.push(`design.preview.viewports.${label}: expected cols 20-240, rows 8-90`);
    }
   }
   if(v.outputs!==undefined&&(!Array.isArray(v.outputs)||!v.outputs.length||new Set(v.outputs).size!==v.outputs.length||v.outputs.some(x=>!['txt','svg','ansi','html'].includes(x))))errors.push('design.preview.outputs: unsupported format or duplicate');
   for(const [type,allowed]of [['layouts',allowedLayout],['styles',allowedStyle]])if(v[type]!==undefined){
    if(!object(v[type])){errors.push(`design.preview.${type}: expected object`);continue;}
    for(const [target,props]of Object.entries(v[type])){
     const split=target.indexOf('/'),sid=target.slice(0,split),nid=target.slice(split+1);
     if(split<1||!screenIds.has(sid)||!screenNodes.get(sid)?.has(nid))errors.push(`design.preview.${type}: unknown target ${target}`);
     configProps(props,allowed,`design.preview.${type}.${target}`,errors);
     if(props?.byViewport!==undefined){
      if(!object(props.byViewport))errors.push(`design.preview.${type}.${target}.byViewport: expected object`);
      else for(const [viewport,over]of Object.entries(props.byViewport)){
       if(!name.test(viewport)||!v.viewports?.[viewport])errors.push(`design.preview.${type}.${target}: unknown viewport override ${viewport}`);
       configProps(over,allowed.filter(x=>x!=='byViewport'),`design.preview.${type}.${target}.byViewport.${viewport}`,errors);
      }
     }
    }
   }
  }
 }
 if(scenarios){
  const validFixture=(where,f,screen)=>{
   if(!keys(f,['data','state','components','focus','disabled','status','selectedIndex','suppressDefaultStatus','presentation'],where,errors))return;
   if(f.focus&&screen&&!screenNodes.get(screen)?.has(f.focus))errors.push(`${where}: unknown focus ${f.focus}`);
   if(f.components!==undefined){
    if(!object(f.components)){errors.push(`${where}.components: expected object`);return;}
    for(const [id,props] of Object.entries(f.components)){
     if(screen&&!screenNodes.get(screen)?.has(id))errors.push(`${where}: unknown component ${id}`);
     if(keys(props,['hidden','disabled','checked','value','text','items'],`${where}.components.${id}`,errors)){
      for(const k of ['hidden','disabled','checked'])if(props[k]!==undefined&&typeof props[k]!=='boolean')errors.push(`${where}.components.${id}.${k}: expected boolean`);
      if(props.items!==undefined&&!Array.isArray(props.items))errors.push(`${where}.components.${id}.items: expected array`);
     }
    }
   }
   if(f.presentation&&keys(f.presentation,['statusText','statusType'],`${where}.presentation`,errors)&&f.presentation.statusType&&!['info','error','success','loading'].includes(f.presentation.statusType))errors.push(`${where}.presentation.statusType: invalid`);
  };
  if(scenarios.defaults)validFixture('scenarios.defaults',scenarios.defaults);
  for(const [state,fixture]of Object.entries(scenarios.states||{})){
   if(!ids.has(state))errors.push('scenarios.json: unknown state '+state);
   validFixture(`scenarios.states.${state}`,fixture,states.find(s=>s.id===state)?.screen);
  }
  for(const [variant,fixture]of Object.entries(scenarios.variants||{})){
   if(!name.test(variant)||variant==='default')errors.push('scenarios.variants: invalid variant '+variant);
   validFixture(`scenarios.variants.${variant}`,fixture);
  }
  for(const [state,variants]of Object.entries(scenarios.stateVariants||{})){
   if(!ids.has(state))errors.push('scenarios.stateVariants: unknown state '+state);
   if(!object(variants)){errors.push(`scenarios.stateVariants.${state}: expected object`);continue;}
   for(const [variant,fixture]of Object.entries(variants)){
    if(!name.test(variant)||variant==='default')errors.push(`scenarios.stateVariants.${state}: invalid variant ${variant}`);
    validFixture(`scenarios.stateVariants.${state}.${variant}`,fixture,states.find(s=>s.id===state)?.screen);
   }
  }
 }
 return errors;
}
