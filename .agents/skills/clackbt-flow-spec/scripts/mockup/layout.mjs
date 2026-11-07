/** Configurable layout tree. Geometry is in terminal cells, not pixels. */
import {createCanvas,write,paste,border,lines,terminalWidth} from './canvas.mjs';
import {renderComponent} from './renderers.mjs';
import {footerGeometry} from './footer.mjs';

const clamp=(n,min,max)=>Math.max(min,Math.min(max,n));
const whole=(v,fallback=0)=>Number.isInteger(v)&&v>=0?v:fallback;
function dimension(value,available){
 if(typeof value==='number')return Math.max(1,Math.min(available,Math.floor(value)));
 if(typeof value==='string'&&/^\d{1,3}%$/.test(value))return Math.max(1,Math.min(available,Math.floor(available*parseInt(value,10)/100)));
 return available;
}
function wrapString(value,width,wrap){
 if(width<1)return [];
 const result=[];for(const segment of String(value).split('\n')){
  if(!wrap){result.push(segment);continue;}
  let current='';for(const c of segment){if(terminalWidth(current+c)>width){result.push(current);current='';}current+=c;}result.push(current);
 }
 return result;
}
function config(node,ctx){
 const key=ctx.screen.id+'/'+node.id;
 const a=ctx.preview.layouts?.[key]||{},b=ctx.preview.styles?.[key]||{};
 return {...a,...b,...(a.byViewport?.[ctx.viewport]||{}),...(b.byViewport?.[ctx.viewport]||{})};
}
function pad(block,x,y,inner,padding,hasBorder){paste(block,inner,x+padding+hasBorder,y+padding+hasBorder);}
function finish(inner,width,style){
 const p=whole(style.padding),hasBorder=style.border&&style.border!=='none'?1:0;
 const height=inner.height+2*p+2*hasBorder;
 const output=createCanvas(width,height);pad(output,0,0,inner,p,hasBorder);
 if(hasBorder)border(output,style.border==='true'?'single':style.border);
 return output;
}
function nodeBlock(node,ctx,availableWidth,level=0){
 if(level>30)throw new Error('Layout nesting exceeds 30 levels');
 const style=config(node,ctx),width=dimension(style.width,Math.max(1,availableWidth));
 const p=whole(style.padding),b=style.border&&style.border!=='none'?1:0;
 const contentWidth=Math.max(1,width-2*p-2*b);
 if(node.kind!=='container'){
  const sample=renderComponent(node,ctx);
  const contentRows=sample.flatMap(row=>wrapString(row.text,contentWidth,(style.wrap===true||(style.wrap===undefined&&ctx.screen.presentation==='inline'))).map(text=>({text,role:style.role||row.role})));
  const cutoff=style.maxHeight?Math.max(0,style.maxHeight-2*p-2*b):Infinity;
  const used=contentRows.slice(0,cutoff);
  const content=createCanvas(contentWidth,Math.max(1,used.length));
  for(const [i,row]of used.entries())write(content,alignX(row.text,contentWidth,style.align),i,row.text,row.role,ctx.screen.presentation==='inline');
  if(used.length<contentRows.length)content.clipped=true;
  return finish(content,width,style);
 }
 const visible=(node.children||[]).filter(child=>!ctx.fixture.components?.[child.id]?.hidden);
 const gap=whole(style.gap,0),orientation=style.direction||node.orientation||'vertical';
 const blocks=[];
 if(orientation==='horizontal'||orientation==='grid'){
  const cols=orientation==='grid'?clamp(whole(style.columns,2),1,12):Math.max(1,visible.length);
  const buckets=Array.from({length:cols},()=>[]);
  visible.forEach((child,i)=>buckets[i%cols].push(child));
  const cellGap=gap*Math.max(0,cols-1);
  const widths=Array(cols).fill(Math.max(1,Math.floor((contentWidth-cellGap)/cols)));
  for(let i=0;i<contentWidth-cellGap-widths.reduce((a,n)=>a+n,0);i++)widths[i%cols]++;
  for(const [i,children]of buckets.entries()){
   let offset=0;const items=children.map(child=>nodeBlock(child,ctx,widths[i],level+1));
   const h=items.reduce((n,item)=>n+item.height,0)+Math.max(0,items.length-1)*gap;
   const stack=createCanvas(widths[i],h);
   for(const item of items){paste(stack,item,0,offset);offset+=item.height+gap;}
   blocks.push(stack);
  }
  const maxH=Math.max(0,...blocks.map(block=>block.height));const content=createCanvas(contentWidth,Math.max(1,maxH));
  let x=0;for(const item of blocks){paste(content,item,x,style.valign==='center'?Math.floor((maxH-item.height)/2):style.valign==='bottom'?maxH-item.height:0);x+=item.width+gap;}
  if(x-gap>contentWidth)content.clipped=true;
  return finish(content,width,style);
 }
 for(const child of visible)blocks.push(nodeBlock(child,ctx,contentWidth,level+1));
 const maxH=orientation==='overlay'?Math.max(0,...blocks.map(block=>block.height)):blocks.reduce((n,block)=>n+block.height,0)+Math.max(0,blocks.length-1)*gap;
 const content=createCanvas(contentWidth,Math.max(1,maxH));let y=0;
 for(const item of blocks){
  const x=style.align==='center'?Math.floor((contentWidth-item.width)/2):style.align==='right'?contentWidth-item.width:0;
  if(orientation==='overlay'){
   for(let j=0;j<item.height;j++)for(let i=0;i<item.width;i++){
    const cell=item.rows[j][i];if(cell.ch===' ')continue;
    if(x+i>=0&&x+i<content.width&&j<content.height)content.rows[j][x+i]={...cell};
    else content.clipped=true;
   }
   content.clipped ||= item.clipped;
  }else paste(content,item,Math.max(0,x),y);
  if(orientation!=='overlay')y+=item.height+gap;
 }
 return finish(content,width,style);
}
function alignX(text,width,alignment){return alignment==='center'?Math.max(0,Math.floor((width-terminalWidth(text))/2)):alignment==='right'?Math.max(0,width-terminalWidth(text)):0;}
export function renderFlexibleSnapshot(screen,state,fixture={},options={}){
 const cols=options.cols||80,rows=options.rows||24;
 if(!Number.isInteger(cols)||!Number.isInteger(rows)||cols<20||cols>240||rows<8||rows>90)throw new Error('Viewport must be 20-240 cols and 8-90 rows');
 const violations=[];if(cols<screen.min_size.cols||rows<screen.min_size.rows)violations.push(`Below declared minimum ${screen.min_size.cols}×${screen.min_size.rows}`);
 const inner=cols-4,ctx={screen,state,fixture,design:options.design||{},preview:options.design?.preview||{},viewport:options.viewport||'default'};
 const components=state.terminal?[]:screen.regions.filter(r=>!fixture.components?.[r.id]?.hidden).map(r=>nodeBlock(r,ctx,inner));
 const raw=createCanvas(inner,Math.max(1,components.reduce((h,b)=>h+b.height,0)));
 let cy=0;for(const b of components){paste(raw,b,0,cy);cy+=b.height;}
 const frame=createCanvas(cols,rows);border(frame,'single','border');
 write(frame,2,1,` ${screen.title} / ${state.id}${state.terminal?' [TERMINAL]':''}`,'accent');
 write(frame,2,2,'─'.repeat(inner),'border');
 const footer=footerGeometry(screen.keybindings,inner,rows);
 const bodyHeight=state.terminal?1:raw.height;
 const visible=Math.min(bodyHeight,footer.bodyRows);
 if(state.terminal){if(visible)write(frame,2,3,'Flow ended (terminal state).','muted');}
 else for(let y=0;y<visible;y++)for(let x=0;x<raw.width;x++)frame.rows[y+3][x+2]={...raw.rows[y][x]};
 if(footer.divider!==null)write(frame,2,footer.divider,'─'.repeat(inner),'border');
 for(const [i,line]of footer.lines.entries())write(frame,2,footer.start+i,line,'muted');
 if(frame.clipped||raw.clipped||bodyHeight>visible||footer.clipped)violations.push('Preview content clipped at selected viewport');
 const outputLines=lines(frame);
 return {text:outputLines.join('\n'),lines:outputLines,cells:frame,clipped:violations.some(x=>x.includes('clipped')),warnings:violations};
}

/** Inline CLI transcript: content flows in the scrollback; rows is not a screen limit.
 * No outer border, title bar, pinned key-help, or fake output for terminal states.
 * Each state is a snapshot of the declared output/prompt at that point, not
 * cumulative replay of machine transitions. Exit status stays metadata only.
 */
export function renderInlineSnapshot(screen,state,fixture={},options={}){
 const cols=options.cols??80, rows=options.rows??24;
 if(!Number.isInteger(cols)||cols<20||cols>240||!Number.isInteger(rows)||rows<8||rows>90)throw new Error('Viewport must be 20-240 cols and 8-90 rows');
 const warnings=[];
 if(cols<screen.min_size.cols)warnings.push(`Below declared minimum width ${screen.min_size.cols} columns`);
 const ctx={screen,state,fixture,design:options.design||{},preview:options.design?.preview||{},viewport:options.viewport||'default'};
 const blocks=screen.regions.filter(n=>!fixture.components?.[n.id]?.hidden).map(n=>nodeBlock(n,ctx,cols));
 const content=createCanvas(cols,Math.max(1,blocks.reduce((height,b)=>height+b.height,0)));
 let y=0;for(const block of blocks){paste(content,block,0,y);y+=block.height;}
 if(content.clipped)warnings.push('Preview content clipped at selected width');
 // Only the right-side cell padding is removed: command output does not fill terminal height.
 const outputLines=lines(content).map(line=>line.trimEnd());
 while(outputLines.length>1&&outputLines.at(-1)==='')outputLines.pop();
 // Match the canvas to visible lines for colored SVG/ANSI/HTML serializers.
 content.rows.length=outputLines.length;content.height=outputLines.length;
 return {text:outputLines.join('\n'),lines:outputLines,cells:content,clipped:content.clipped,warnings};
}
