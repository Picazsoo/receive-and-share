// Exercise real networking/files without reading existing book contents.
// Usage: node tools/smoke-device.mjs [path-to-local-ssh-state]
import {readFileSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import path from 'node:path';
import assert from 'node:assert/strict';
import {randomBytes} from 'node:crypto';
const state=path.resolve(process.argv[2]??'../.local-ssh');
const address=readFileSync(path.join(state,'device-ip.txt'),'utf8').trim();
if(!/^\d+\.\d+\.\d+\.\d+$/.test(address))throw Error('Invalid device address');
const args=['-T','-p','2222','-i',path.join(state,'kobo_ed25519'),'-o','BatchMode=yes','-o','IdentitiesOnly=yes','-o','StrictHostKeyChecking=yes','-o',`UserKnownHostsFile=${path.join(state,'known_hosts')}`,'-o','HostKeyAlias=kobo-libra2','-o','ConnectTimeout=8',`root@${address}`];
const ssh=command=>execFileSync('ssh.exe',[...args,command],{encoding:'utf8',windowsHide:true,timeout:20000});
const base=`http://${address}:8080/`;
let screen=ssh('wget -qO- http://127.0.0.1:8080/screen');
const localToken=screen.match(/name="token" value="([a-f0-9]+)"/)?.[1];assert.ok(localToken);
screen=ssh(`wget -qO- --post-data='token=${localToken}' http://127.0.0.1:8080/renew >/dev/null 2>&1; wget -qO- http://127.0.0.1:8080/screen`);
const code=screen.match(/class="code">([0-9]{6})</)?.[1];assert.ok(code,'fresh pairing code');
const pairPage=await fetch(base);assert.equal(pairPage.status,200);const pairCookie=pairPage.headers.get('set-cookie').split(';')[0];const nonce=(await pairPage.text()).match(/name="nonce" value="([a-f0-9]+)"/)?.[1];assert.ok(nonce);
const paired=await fetch(base+'pair',{method:'POST',body:new URLSearchParams({code,nonce,basic:'1'}),headers:{Cookie:pairCookie,Origin:'http://www.kobo.com'},redirect:'manual'});assert.equal(paired.status,303);assert.equal(paired.headers.get('location'),'/basic');
const Cookie=paired.headers.get('set-cookie').split(';')[0];const session=await(await fetch(base+'api/session',{headers:{Cookie}})).json();assert.ok(session.csrf);
assert.equal((await fetch(base+'screen')).status,404);assert.equal((await fetch(base+'api/files')).status,401);
const previousSharing=ssh('wget -qO- http://127.0.0.1:8080/share-settings');
 const originalMode=/name="mode" value="all" checked/.test(previousSharing)?'all':'selected';
 const originalRemember=/name="remember" value="1" checked/.test(previousSharing);
 const setSharing=(mode,remember,files)=>{const form=new URLSearchParams({token:localToken,mode});if(remember)form.set('remember','1');names.forEach(n=>form.append('shown','Received Books/'+n));files.forEach(n=>form.append('file','Received Books/'+n));ssh(`wget -qO- --post-data='${form.toString()}' http://127.0.0.1:8080/share-settings >/dev/null 2>&1; echo updated`);};
 const names=[`Receive & Share test ${randomBytes(6).toString('hex')}.pdf`,`Receive & Share test ${randomBytes(6).toString('hex')}.pdf`];
const bytes=Buffer.from('%PDF-1.7\nReceive & Share disposable integration test\n%%EOF\n');
try {
 for(let i=0;i<names.length;i++) {const data=new FormData();if(i>0)data.set('csrf',session.csrf);data.set('book',new Blob([bytes]),names[i]);const response=await fetch(base+(i===0?'api/upload':'upload'),{method:'POST',headers:{Cookie,'X-CSRF-Token':session.csrf,Origin:i===0?base.slice(0,-1):'http://www.kobo.com'},body:data});assert.equal(response.status,200,await response.text());}
 setSharing('selected',false,names);
 const listing=await(await fetch(base+'api/files',{headers:{Cookie}})).json();assert.ok(listing.every(f=>!f.path.split('/').some(p=>p.startsWith('.'))));
 const selected=names.map(n=>listing.find(f=>f.path==='Received Books/'+n));assert.ok(selected.every(Boolean));
 for(const f of selected){const response=await fetch(base+'download?file='+encodeURIComponent(f.path),{headers:{Cookie}});assert.equal(response.status,200);assert.deepEqual(Buffer.from(await response.arrayBuffer()),bytes);}
 const form=new URLSearchParams({csrf:session.csrf});selected.forEach(f=>form.append('file',f.path));
 const review=await fetch(base+'review',{method:'POST',body:form,headers:{Cookie,Origin:'http://www.kobo.com'}});assert.equal(review.status,200);assert.match(await review.text(),/2 books selected/);
 const archive=await fetch(base+'zip',{method:'POST',body:form,headers:{Cookie,Origin:'http://www.kobo.com'}});assert.equal(archive.status,200);const zip=Buffer.from(await archive.arrayBuffer());
 // Read ZIP central-directory entries, then verify STORE payloads byte-for-byte.
 const end=zip.lastIndexOf(Buffer.from([0x50,0x4b,0x05,0x06]));assert.ok(end>=0);assert.equal(zip.readUInt16LE(end+10),2);let at=zip.readUInt32LE(end+16);
 for(let i=0;i<2;i++){assert.equal(zip.readUInt32LE(at),0x02014b50);assert.equal(zip.readUInt16LE(at+10),0);const length=zip.readUInt32LE(at+20),nameLength=zip.readUInt16LE(at+28),extra=zip.readUInt16LE(at+30),comment=zip.readUInt16LE(at+32),offset=zip.readUInt32LE(at+42);assert.equal(zip.subarray(at+46,at+46+nameLength).toString(),selected[i].path);const start=offset+30+zip.readUInt16LE(offset+26)+zip.readUInt16LE(offset+28);assert.deepEqual(zip.subarray(start,start+length),bytes);at+=46+nameLength+extra+comment;}
 const basic=await(await fetch(base+'basic',{headers:{Cookie}})).text();assert.ok(!basic.includes('<script'));assert.ok(basic.includes(names[0].replaceAll('&','&amp;')));
 console.log(`PASS on Kobo: native ARM server, pairing, basic/modern uploads, listing (${listing.length} books), downloads, ZIP contents, and native-browser form headers.`);
} finally {
 setSharing(originalMode,originalRemember,[]);
 // Only the two unique test names generated by this run are removed.
 for(const name of names){assert.match(name,/^Receive & Share test [a-f0-9]{12}\.pdf$/);ssh(`rm -f '/mnt/onboard/Received Books/${name}'`);}
}
