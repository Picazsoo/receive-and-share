// Check rejected requests and a disposable hidden book on the real FAT volume.
// No existing hidden book content is read. Requires an active Share all session.
import {readFileSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import {randomBytes} from 'node:crypto';
import {once} from 'node:events';
import net from 'node:net';
import path from 'node:path';
import assert from 'node:assert/strict';
const state=path.resolve(process.argv[2]??'../.local-ssh');
const address=readFileSync(path.join(state,'device-ip.txt'),'utf8').trim();
assert.match(address,/^\d+\.\d+\.\d+\.\d+$/);
const sshArgs=['-T','-p','2222','-i',path.join(state,'kobo_ed25519'),'-o','BatchMode=yes','-o','IdentitiesOnly=yes','-o','StrictHostKeyChecking=yes','-o',`UserKnownHostsFile=${path.join(state,'known_hosts')}`,'-o','HostKeyAlias=kobo-libra2','-o','ConnectTimeout=8',`root@${address}`];
const ssh=command=>execFileSync('ssh.exe',[...sshArgs,command],{encoding:'utf8',windowsHide:true,timeout:20000});
const port=Number(ssh('cat /mnt/onboard/.adds/kobo-receive/active-port 2>/dev/null || echo 8080').trim());
assert.ok(Number.isInteger(port)&&port>=1&&port<=65535);
const base=port===80?`http://${address}/`:`http://${address}:${port}/`;
const socket=net.connect(port,address);
socket.on('error',()=>{});
await once(socket,'connect');
socket.setTimeout(3000,()=>socket.destroy(Error('Rejected request did not close')));
let response='';socket.on('data',data=>response+=data);
const closed=once(socket,'close');
socket.write('POST / HTTP/1.1\r\nHost: rejected.example\r\nContent-Length: 1\r\n\r\n');
await closed;
assert.match(response,/403 Forbidden/);
assert.equal((await fetch(base)).status,200);
let screen=ssh(`wget -qO- http://127.0.0.1:${port}/screen`);
const token=screen.match(/name="token" value="([a-f0-9]+)"/)[1];
screen=ssh(`wget -qO- --post-data='token=${token}' http://127.0.0.1:${port}/renew >/dev/null 2>&1; wget -qO- http://127.0.0.1:${port}/screen`);
const code=screen.match(/class="code">([0-9]{6})</)[1];
const challenge=await fetch(base),nonce=(await challenge.text()).match(/name="nonce" value="([a-f0-9]+)"/)[1];
const paired=await fetch(base+'pair',{method:'POST',headers:{Cookie:challenge.headers.get('set-cookie').split(';')[0]},body:new URLSearchParams({nonce,code}),redirect:'manual'});
assert.equal(paired.status,303);
const Cookie=paired.headers.get('set-cookie').split(';')[0];
const session=await(await fetch(base+'api/session',{headers:{Cookie}})).json();
assert.equal(session.shareAll,true,'Run this check only when Share all was already enabled');
assert.equal(ssh('test -d /mnt/onboard/ADDS~1 && echo yes').trim(),'yes','Kobo short alias exists');
const name='hidden-regression-'+randomBytes(6).toString('hex')+'.pdf';
const hidden='/mnt/onboard/.adds/kobo-receive/tmp/'+name;
try{
 ssh(`printf '%s' '%PDF-1.7 disposable alias regression' > '${hidden}'`);
 const alias='ADDS~1/kobo-receive/tmp/'+name;
 const listed=await(await fetch(base+'api/files',{headers:{Cookie}})).json();
 assert.ok(!listed.some(f=>f.path.includes(name)));
 for(const file of ['.adds/kobo-receive/tmp/'+name,alias]){
  assert.equal((await fetch(base+'download?file='+encodeURIComponent(file),{headers:{Cookie}})).status,404);
  for(const route of ['review','zip']){
   const r=await fetch(base+route,{method:'POST',headers:{Cookie},body:new URLSearchParams({csrf:session.csrf,file})});
   assert.notEqual(r.status,200,route+' accepted hidden alias');
  }
 }
 console.log('PASS on Kobo: rejected Host with missing body closes promptly; hidden FAT alias is absent from listing and blocked by download, review and ZIP.');
}finally{
 assert.match(name,/^hidden-regression-[a-f0-9]{12}\.pdf$/);
 ssh(`rm -f '${hidden}'`);
}
