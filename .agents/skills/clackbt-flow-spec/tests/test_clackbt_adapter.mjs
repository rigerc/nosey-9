import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {test} from 'node:test';
import {fileURLToPath} from 'node:url';
import {validateClackbt,renderClackbtPreview} from '../scripts/clackbt-adapter.mjs';
const ROOT=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
function fixture(t){const dir=fs.mkdtempSync(path.join(os.tmpdir(),'clackbt-test-'));t.after(()=>fs.rmSync(dir,{recursive:true,force:true}));const out=path.join(dir,'.work/flow-spec/example-clackbt-prompt');fs.mkdirSync(out,{recursive:true});fs.cpSync(path.join(ROOT,'examples/example-clackbt-prompt'),out,{recursive:true});return {dir,out};}
test('validates native 2-step workflow against canonical screens/machine',t=>{const {dir}=fixture(t);assert.deepEqual(validateClackbt(dir,'example-clackbt-prompt'),{exists:true,errors:[],warnings:[]});});
test('rejects callback and mismatched fields',t=>{const {dir,out}=fixture(t);const p=path.join(out,'clackbt.json'),v=JSON.parse(fs.readFileSync(p));v.steps[1].api='Confirm';v.steps[1].field='missing';v.implementation.allow_legacy_steps=true;fs.writeFileSync(p,JSON.stringify(v));const errors=validateClackbt(dir,'example-clackbt-prompt').errors.join('\n');assert.match(errors,/supported native-workflow API|supported/);assert.match(errors,/exported Go struct field/);assert.match(errors,/cannot include Step/);});
test('rejects password example leak',t=>{const {dir,out}=fixture(t);const p=path.join(out,'clackbt.json'),v=JSON.parse(fs.readFileSync(p));v.steps[0].api='PasswordModel';fs.writeFileSync(p,JSON.stringify(v));assert.match(validateClackbt(dir,'example-clackbt-prompt').errors.join('\n'),/must not contain secret/);});
test('generates self-contained escaped preview without code execution',t=>{const {dir,out}=fixture(t);const p=path.join(out,'clackbt.json'),v=JSON.parse(fs.readFileSync(p));v.steps[0].params.Message='<script>alert(1)</script>';fs.writeFileSync(p,JSON.stringify(v));const result=renderClackbtPreview(dir,'example-clackbt-prompt');assert.ok(fs.existsSync(result.html));const html=fs.readFileSync(result.html,'utf8');assert.ok(!html.includes('<script>alert(1)</script>'));assert.match(html,/\\u003cscript/);assert.match(fs.readFileSync(result.text,'utf8'),/COMPLET/);});
