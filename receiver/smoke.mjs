import {spawn} from 'node:child_process';
import {mkdtemp,readFile,writeFile,access,mkdir} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import path from 'node:path';
import assert from 'node:assert/strict';

const root = await mkdtemp(path.join(tmpdir(),'kobo-receive-test-'));
const dest=path.join(root,'books'), temp=path.join(root,'tmp');
await mkdir(dest);await mkdir(temp);
const server=spawn(path.resolve('../dist/kobo-receive-host.exe'),['-root',root,'-ip','127.0.0.1','-port','18080','-dest',dest,'-temp',temp,'-ttl','8s'],{stdio:['ignore','ignore','pipe'],windowsHide:true});
let log='';server.stderr.on('data',b=>log+=b);
const exited=new Promise((resolve,reject)=>{server.on('exit',resolve);server.on('error',reject)});
try {
 let response;
 for(let n=0;n<40;n++){try{response=await fetch('http://127.0.0.1:18080/screen');break;}catch{await new Promise(r=>setTimeout(r,50));}}
 assert.equal(response?.status,200,log);
 const screen=await response.text();
 const url='http://127.0.0.1:18080/';assert.ok(screen.includes(url),'short address on QR screen');
 const sourceToken=screen.match(/name="token" value="([a-f0-9]+)"/)?.[1];assert.ok(sourceToken);
 assert.equal((await fetch(url+'share-settings',{method:'POST',body:new URLSearchParams({token:sourceToken,mode:'all'}),redirect:'manual'})).status,303);
 const code=screen.match(/class="code">([0-9]{6})</)?.[1];assert.ok(code,'pairing PIN on local screen');
 const png=Buffer.from(await(await fetch('http://127.0.0.1:18080/qr.png')).arrayBuffer());
 assert.equal(png.subarray(1,4).toString(),'PNG');
 await writeFile('../dist/qr-preview.png',png);
 const pairingForm=await fetch(url);assert.equal(pairingForm.status,200);
 const pairCookie=pairingForm.headers.get('set-cookie').split(';')[0];const nonce=(await pairingForm.text()).match(/name="nonce" value="([a-f0-9]+)"/)?.[1];assert.ok(nonce);
 assert.equal((await fetch(url+'api/files')).status,401);
 const paired=await fetch(url+'pair',{method:'POST',body:new URLSearchParams({code,nonce}),headers:{Cookie:pairCookie},redirect:'manual'});assert.equal(paired.status,303);
 const Cookie=paired.headers.get('set-cookie').split(';')[0];assert.ok(Cookie);
 const session=await(await fetch(url+'api/session',{headers:{Cookie}})).json();assert.ok(session.csrf);
 const form=new FormData();form.set('book',new Blob(['%PDF-1.7\nsmoke test']),'smoke.pdf');
 const sent=await fetch(url+'api/upload',{method:'POST',body:form,headers:{Cookie,'X-CSRF-Token':session.csrf}});assert.equal(sent.status,200);assert.equal((await sent.json()).name,'smoke.pdf');
 assert.equal(await readFile(path.join(dest,'smoke.pdf'),'utf8'),'%PDF-1.7\nsmoke test');
 const files=await(await fetch(url+'api/files',{headers:{Cookie}})).json();assert.equal(files.length,1);assert.equal(files[0].path,'books/smoke.pdf');
 assert.equal(await(await fetch(url+'download?file=books%2Fsmoke.pdf',{headers:{Cookie}})).text(),'%PDF-1.7\nsmoke test');
 const zip=await fetch(url+'zip',{method:'POST',headers:{Cookie},body:new URLSearchParams({csrf:session.csrf,file:files[0].path})});assert.equal(zip.status,200);assert.equal(Buffer.from(await zip.arrayBuffer()).subarray(0,2).toString(),'PK');
 const basic=await(await fetch(url+'basic',{headers:{Cookie}})).text();assert.match(basic,/books\/smoke.pdf/);assert.ok(!basic.includes('<script'));
 assert.equal((await fetch('http://127.0.0.1:18080/incorrect/')).status,404);
 await exited;
 await assert.rejects(fetch(url));
 console.log('PASS: live QR, PIN pairing, cookie/CSRF, upload, listing, downloads, streamed ZIP, basic view, and session expiry.');
} finally {server.kill();}
