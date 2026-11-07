/** A small terminal-cell canvas shared by modern layout and output adapters. */
export function charWidth(c){
 const u=c.codePointAt(0);
 if(u<32||u===127||u>=0x300&&u<=0x36f||u===0x200d||u>=0xfe00&&u<=0xfe0f)return 0;
 return (u>=0x1100&&u<=0x115f||u>=0x2e80&&u<=0xa4cf||u>=0xac00&&u<=0xd7a3||u>=0xf900&&u<=0xfaff||u>=0xfe10&&u<=0xfe6f||u>=0xff01&&u<=0xff60||u>=0xffe0&&u<=0xffe6||u>=0x1f300&&u<=0x1faff)?2:1;
}
export const terminalWidth=s=>[...String(s)].reduce((n,c)=>n+charWidth(c),0);
export const safeText=s=>String(s??'').replace(/[\u0000-\u001f\u007f\u001b]/g,' ').replace(/\s+/g,' ');
// Unlike full-screen widgets, CLI stdout may rely on exact indentation/spacing.
export const safeInlineText=s=>String(s??'').replace(/\t/g,'    ').replace(/[\u0000-\u001f\u007f\u001b]/g,' ');
export function createCanvas(width,height,role='fg'){
 return {width,height,rows:Array.from({length:height},()=>Array.from({length:width},()=>({ch:' ',role}))),clipped:false};
}
export function write(canvas,x,y,value,role='fg',preserveSpacing=false){
 if(y<0||y>=canvas.height){canvas.clipped=true;return;}
 let col=x;
 for(const c of (preserveSpacing?safeInlineText(value):safeText(value))){
  const w=charWidth(c);if(!w)continue;
  if(col<0||col+w>canvas.width){canvas.clipped=true;col+=w;continue;}
  canvas.rows[y][col]={ch:c,role};
  if(w===2)canvas.rows[y][col+1]={ch:'',role,wideTail:true};
  col+=w;
 }
}
export function paste(dst,src,x,y){
 for(let j=0;j<src.height;j++)for(let i=0;i<src.width;i++){
  const a=src.rows[j][i];if(x+i<0||x+i>=dst.width||y+j<0||y+j>=dst.height){if(a.ch.trim())dst.clipped=true;continue;}
  dst.rows[y+j][x+i]={...a};
 }
 dst.clipped ||= src.clipped;
}
export function border(canvas,style='single',role='border'){
 const types={single:['┌','┐','└','┘','─','│'],rounded:['╭','╮','╰','╯','─','│'],double:['╔','╗','╚','╝','═','║']};
 const [tl,tr,bl,br,h,v]=types[style]||types.single;
 if(canvas.width<2||canvas.height<2)return;
 write(canvas,0,0,tl+h.repeat(canvas.width-2)+tr,role);
 write(canvas,0,canvas.height-1,bl+h.repeat(canvas.width-2)+br,role);
 for(let y=1;y<canvas.height-1;y++){write(canvas,0,y,v,role);write(canvas,canvas.width-1,y,v,role);}
}
export function lines(canvas){return canvas.rows.map(row=>row.map(c=>c.ch).join(''));}
export function text(canvas){return lines(canvas).join('\n');}
export function colorRuns(canvas,y){
 const result=[];let active=null;
 for(const c of canvas.rows[y]){
  const role=c.role||'fg';
  if(active?.role!==role){active={role,text:''};result.push(active);}
  active.text+=c.ch;
 }
 return result;
}
