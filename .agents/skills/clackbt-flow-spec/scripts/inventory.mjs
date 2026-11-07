#!/usr/bin/env node
/** Read-only, heuristic code inventory for existing fullscreen TUIs and inline CLIs. NOT a contract extractor. */
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseArgs, resolveRepoRoot } from './generate-html.mjs';
const BLOCKED=new Set(['.git','.agents','.svn','node_modules','vendor','dist','build','out','target','coverage','.next','.venv','venv','__pycache__','docs','.cache']);
const CODE=new Set(['.go','.rs','.py','.ts','.tsx','.js','.jsx','.mjs','.cjs','.rb','.c','.h','.cpp','.hpp','.swift','.kt','.java','.zig','.cs','.lua']);
const TEST_PATH=/(^|\/)(tests?|__tests__|spec|e2e|fixtures)\/|\.(test|spec)\.[a-z]+$|_test\.[a-z]+$|(^|\/)test_[^/]+$/i;
// Per-signal score weight and cap: one noisy generic keyword (state, update, view...) must not outrank a real TUI framework import.
const WEIGHT={frameworks:[5,25],cli:[4,16],input:[3,15],navigation:[3,12],rendering:[1,2],state:[1,2],effects:[1,2],validation:[1,2]};
const SIGNALS={
  frameworks:/\b(ratatui|crossterm|textual|prompt_toolkit|bubbletea|lipgloss|bubbles|tview|tcell|termbox|termion|ncurses|curses|blessed|urwid|asciimatics|npyscreen|cursive|ftxui|notcurses|gocui|termui|tuirealm|tui-realm|tui-rs)\b|(?:from|require)\s*\(?['"]ink['"]|@shopify\/ink|(?:from|import)\s+rich\b/i,
  // CLI parsers and command registries: source leads, not evidence of flags or behavior.
  cli:/\b(argparse|optparse|typer|questionary|inquirer|enquirer|oclif|commander|yargs|picocli|cobra|urfave\/cli|clap|System\.CommandLine|OptionParser|ArgumentParser|parse_args|parseArgs|parse_argv)\b|\b(?:new Command|@app\.command|@click\.command|@click\.option|cobra\.Command|clap::Parser|derive\(Parser\))\b/i,
  rendering:/\b(render|draw|layout|view|widget|screen|frame|component|viewport)\b/i,
  input:/\b(keydown|keypress|on_key|onkey|keybindings?|key_bindings?|hotkeys?|focus|keyboard|keypressmsg|keyevent|getch|get_wch|getkey|readkey|keymap)\b/i,
  navigation:/\b(navigate|router|routing|transition|go_back|backstack|screen_id)\b/i,
  state:/\b(state|reducer|model|update|store|dispatch|message)\b/i,
  effects:/\b(repository|fetch|query|database|sqlite|api_client|side_effect|command)\b/i,
  validation:/\b(validate|validator|schema|invalid|required|parse_error)\b/i,
};
export function inventory(repoRoot,{limit=2500,top=70}={}) {
  const root=fs.realpathSync(repoRoot);let scanned=0,ignored=0,truncated=false;
  const candidates=[];
  const queue=[root];
  while(queue.length){
    const current=queue.shift();let entries;
    try{entries=fs.readdirSync(current,{withFileTypes:true});}catch{ignored++;continue;}
    for(const entry of entries.sort((a,b)=>a.name.localeCompare(b.name))){
      if(scanned>=limit){truncated=true;break;}
      const file=path.join(current,entry.name);
      if(entry.isSymbolicLink())continue;
      if(entry.isDirectory()){if(!BLOCKED.has(entry.name) && !entry.name.startsWith('.'))queue.push(file);continue;}
      if(!entry.isFile()||!CODE.has(path.extname(entry.name).toLowerCase()))continue;
      let stat;try{stat=fs.statSync(file);}catch{ignored++;continue;}
      if(stat.size>400_000 || stat.size===0){ignored++;continue;}
      scanned++;
      let source;try{source=fs.readFileSync(file,'utf8');}catch{ignored++;continue;}
      if(source.includes('\u0000')){ignored++;continue;}
      const signals={};
      const lines=source.split(/\r?\n/);
      for(let i=0;i<lines.length;i++){
        for(const [name,re] of Object.entries(SIGNALS)){
          if(re.test(lines[i])){
            (signals[name]??=[]).push(i+1);
          }
        }
      }
      let total=0;
      for(const [name,lineNumbers] of Object.entries(signals)){const [weight,cap]=WEIGHT[name];total+=Math.min(lineNumbers.length*weight,cap);}
      if(total){
        for(const signal in signals)signals[signal]=signals[signal].slice(0,12);
        const rel=path.relative(root,file).split(path.sep).join('/');
        const isTest=TEST_PATH.test(rel);
        candidates.push({path:rel,score:isTest?Math.ceil(total/2):total,...(isTest?{is_test:true}:{}),signals});
      }
    }
    if(truncated)break;
  }
  candidates.sort((a,b)=>b.score-a.score||a.path.localeCompare(b.path));
  return {kind:'heuristic-source-inventory',warning:'These are search leads only, not proof of implemented behavior. Open source and verify precise line ranges before citing evidence.',repo_root:root,scanned_files:scanned,ignored_files:ignored,truncated,limit,candidates:candidates.slice(0,top)};
}
if(process.argv[1]&&path.resolve(process.argv[1])===fileURLToPath(import.meta.url)){
  try{
    const a=parseArgs(process.argv.slice(2));const limit=a.limit?Number(a.limit):2500;
    if(!Number.isInteger(limit)||limit<1||limit>20000)throw new Error('limit must be an integer from 1 to 20000');
    console.log(JSON.stringify(inventory(resolveRepoRoot(a['repo-root']),{limit}),null,2));
  }catch(e){console.error('Error: '+e.message);process.exitCode=1;}
}
