/** Shared terminal-cell layout for the bottom keyboard-help footer. */
import {charWidth,safeText,terminalWidth} from './canvas.mjs';

function shorten(value,width){
 if(width<1)return '';
 let text='',used=0;
 const available=Math.max(0,width-1);
 for(const c of value){const w=charWidth(c);if(used+w>available)break;text+=c;used+=w;}
 return text+'…';
}

/** Keep shortcuts side by side; wrap between shortcuts when necessary. */
export function layoutHelpFooter(keybindings=[],width){
 if(!keybindings.length)return {lines:[],clipped:false};
 const entries=keybindings.map(({key,event})=>safeText(key)+' → '+safeText(event));
 const preferred='  │  ',compact='  ';
 const total=entries.reduce((n,e)=>n+terminalWidth(e),0);
 const separator=total+terminalWidth(preferred)*(entries.length-1)<=width?preferred:compact;
 const lines=[];
 let current='',clipped=false;
 for(const entry of entries){
  const fitted=terminalWidth(entry)>width?shorten(entry,width):entry;
  clipped ||= terminalWidth(entry)>width;
  if(current && terminalWidth(current)+terminalWidth(separator)+terminalWidth(fitted)>width){
   lines.push(current);current='';
  }
  current += (current?separator:'')+fitted;
 }
 if(current)lines.push(current);
 return {lines,clipped};
}

/** Reserve one body row at minimum, even in very small viewports. */
export function footerGeometry(keybindings,width,rows){
 if(!keybindings?.length)return {lines:[],start:rows-1,divider:null,bodyRows:rows-4,clipped:false};
 const footer=layoutHelpFooter(keybindings,width);
 const maxFooterRows=Math.max(0,rows-5);
 const visible=footer.lines.slice(0,maxFooterRows);
 const start=rows-1-visible.length;
 const divider=start>=5?start-1:null;
 return {
  lines:visible,
  start,
  divider,
  bodyRows:start-3-(divider===null?0:1),
  clipped:footer.clipped||footer.lines.length>visible.length
 };
}
