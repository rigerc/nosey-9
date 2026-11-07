/** Color-aware serializers for the shared terminal-cell canvas. */
import {colorRuns,charWidth} from './canvas.mjs';
const esc=s=>String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;').replace(/'/g,'&#39;');
const hexRgb=s=>{const h=s.replace('#','');return h.length===3?[...h].map(x=>parseInt(x+x,16)):[0,2,4].map(i=>parseInt(h.slice(i,i+2),16));};
function roleColor(theme,role){return theme[role]||theme.fg;}
export function svgCanvasSnapshot(preview,theme,id){
 const canvas=preview.cells,w=canvas.width*9+40,h=canvas.height*19+42;
 const body=[];
 for(let y=0;y<canvas.height;y++){
  let x=0;for(const run of colorRuns(canvas,y)){
   body.push(`<text x="${20+x*9}" y="${32+y*19}" xml:space="preserve" fill="${esc(roleColor(theme,run.role))}">${esc(run.text)}</text>`);
   x+=[...run.text].reduce((total,c)=>total+charWidth(c),0);
  }
 }
 return `<svg xmlns="http://www.w3.org/2000/svg" role="img" aria-label="${esc(id)} sample terminal mockup" viewBox="0 0 ${w} ${h}" width="${w}" height="${h}"><rect width="100%" height="100%" rx="12" fill="${esc(theme.bg)}"/><g font-family="DejaVu Sans Mono,monospace" font-size="15">${body.join('')}</g></svg>\n`;
}
export function ansiSnapshot(preview,theme){
 if(!preview.cells){
  const [r,g,b]=hexRgb(theme.fg),[ar,ag,ab]=hexRgb(theme.accent);
  return preview.lines.map((line,i)=>`\u001b[38;2;${i===1?ar:r};${i===1?ag:g};${i===1?ab:b}m${line}\u001b[0m`).join('\n')+'\n';
 }
 const lines=[];
 for(let y=0;y<preview.cells.height;y++){
  let line='';for(const run of colorRuns(preview.cells,y)){
   const [r,g,b]=hexRgb(roleColor(theme,run.role));line+=`\u001b[38;2;${r};${g};${b}m`+run.text;
  }lines.push(line+'\u001b[0m');
 }
 return lines.join('\n')+'\n';
}
