// Real Kobo conversion check. Creates/removes only uniquely named sample books.
import {readFileSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import {randomBytes} from 'node:crypto';
import {crc32,inflateRawSync} from 'node:zlib';
import path from 'node:path';
import assert from 'node:assert/strict';

const state=path.resolve(process.argv[2]??'../.local-ssh');
const address=readFileSync(path.join(state,'device-ip.txt'),'utf8').trim();
assert.match(address,/^\d+\.\d+\.\d+\.\d+$/);
const args=['-T','-p','2222','-i',path.join(state,'kobo_ed25519'),'-o','BatchMode=yes','-o','IdentitiesOnly=yes','-o','StrictHostKeyChecking=yes','-o',`UserKnownHostsFile=${path.join(state,'known_hosts')}`,'-o','HostKeyAlias=kobo-libra2','-o','ConnectTimeout=8',`root@${address}`];
const ssh=command=>execFileSync('ssh.exe',[...args,command],{encoding:'utf8',windowsHide:true,timeout:20000});
const port=Number(ssh('cat /mnt/onboard/.adds/kobo-receive/active-port 2>/dev/null || echo 8080').trim());
assert.ok(Number.isInteger(port)&&port>=1&&port<=65535);
const base=port===80?`http://${address}/`:`http://${address}:${port}/`;
const screen=ssh(`wget -qO- http://127.0.0.1:${port}/screen`);
const token=screen.match(/name="token" value="([a-f0-9]+)"/)[1];
const refreshed=ssh(`wget -qO- --post-data='token=${token}' http://127.0.0.1:${port}/renew >/dev/null 2>&1; wget -qO- http://127.0.0.1:${port}/screen`);
const code=refreshed.match(/class="code">([0-9]{6})</)[1];
const challenge=await fetch(base),nonce=(await challenge.text()).match(/name="nonce" value="([a-f0-9]+)"/)[1];
const paired=await fetch(base+'pair',{method:'POST',headers:{Cookie:challenge.headers.get('set-cookie').split(';')[0]},body:new URLSearchParams({nonce,code}),redirect:'manual'});
assert.equal(paired.status,303);
const Cookie=paired.headers.get('set-cookie').split(';')[0];
const session=await(await fetch(base+'api/session',{headers:{Cookie}})).json();
assert.match(await(await fetch(base,{headers:{Cookie}})).text(),/id="convert-epub"/);
assert.match(await(await fetch(base+'basic',{headers:{Cookie}})).text(),/name="convert"/);

function zip(entries){
 const locals=[],central=[];let offset=0;
 for(const [name,text] of entries){const n=Buffer.from(name),data=Buffer.from(text),local=Buffer.alloc(30),head=Buffer.alloc(46),crc=crc32(data);
  local.writeUInt32LE(0x04034b50);local.writeUInt16LE(20,4);local.writeUInt32LE(crc,14);local.writeUInt32LE(data.length,18);local.writeUInt32LE(data.length,22);local.writeUInt16LE(n.length,26);
  head.writeUInt32LE(0x02014b50);head.writeUInt16LE(20,4);head.writeUInt16LE(20,6);head.writeUInt32LE(crc,16);head.writeUInt32LE(data.length,20);head.writeUInt32LE(data.length,24);head.writeUInt16LE(n.length,28);head.writeUInt32LE(offset,42);
  locals.push(local,n,data);central.push(head,n);offset+=local.length+n.length+data.length;
 }
 const directory=Buffer.concat(central),end=Buffer.alloc(22);end.writeUInt32LE(0x06054b50);end.writeUInt16LE(entries.length,8);end.writeUInt16LE(entries.length,10);end.writeUInt32LE(directory.length,12);end.writeUInt32LE(offset,16);return Buffer.concat([...locals,directory,end]);
}
function unzip(bytes){const end=bytes.lastIndexOf(Buffer.from([0x50,0x4b,0x05,0x06]));assert.ok(end>=0);let at=bytes.readUInt32LE(end+16);const result=new Map();for(let i=0;i<bytes.readUInt16LE(end+10);i++){assert.equal(bytes.readUInt32LE(at),0x02014b50);const method=bytes.readUInt16LE(at+10),length=bytes.readUInt32LE(at+20),nl=bytes.readUInt16LE(at+28),el=bytes.readUInt16LE(at+30),cl=bytes.readUInt16LE(at+32),offset=bytes.readUInt32LE(at+42),name=bytes.subarray(at+46,at+46+nl).toString(),start=offset+30+bytes.readUInt16LE(offset+26)+bytes.readUInt16LE(offset+28),body=bytes.subarray(start,start+length);assert.ok(method===0||method===8);result.set(name,method===8?inflateRawSync(body):body);at+=46+nl+el+cl;}return result;}
const chapters=Array.from({length:20},(_,i)=>[`chapter${i}.xhtml`,`<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Chapter ${i}</title></head><body>${'<p>This is a disposable conversion test. Another sentence for Kobo reading spans.</p>'.repeat(100)}</body></html>`]);
const source=zip([['mimetype','application/epub+zip'],['META-INF/container.xml','<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>'],['content.opf',`<package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="id"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Disposable conversion test</dc:title><dc:identifier id="id">conversion-test</dc:identifier><dc:language>en</dc:language></metadata><manifest>${chapters.map(([name],i)=>`<item id="c${i}" href="${name}" media-type="application/xhtml+xml"/>`).join('')}</manifest><spine>${chapters.map((_,i)=>`<itemref idref="c${i}"/>`).join('')}</spine></package>`],...chapters]);
const stem='ReceiveShareConversionTest-'+randomBytes(6).toString('hex'),name=stem+'.epub',saved=stem+'.kepub.epub',bookPath='Received Books/'+saved,id=randomBytes(16).toString('hex');
const prior=ssh(`wget -qO- http://127.0.0.1:${port}/share-settings`);
const mode=/name="mode" value="all" checked/.test(prior)?'all':'selected',remember=/name="remember" value="1" checked/.test(prior);
const allowDelete=/name="allow-delete" value="1" checked/.test(prior);
function sharing(include){const form=new URLSearchParams({token,mode,shown:bookPath});if(remember)form.set('remember','1');if(allowDelete)form.set('allow-delete','1');if(include)form.set('file',bookPath);ssh(`wget -qO- --post-data='${form}' http://127.0.0.1:${port}/share-settings >/dev/null 2>&1; echo updated`);}
const started=performance.now();
try {
 for(let i=0;i<2;i++){
  const body=new FormData();body.set('book',new Blob([source]),name);
  const response=await fetch(base+'api/upload',{method:'POST',headers:{Cookie,'X-CSRF-Token':session.csrf,'X-Upload-ID':id,'X-Upload-Name':encodeURIComponent(name),'X-Upload-Size':String(source.length),'X-Convert-EPUB':'1'},body});
  const text=await response.text();assert.equal(response.status,200,text);assert.equal(JSON.parse(text).name,saved);
 }
 const receipt=await(await fetch(base+'api/upload-status?id='+id,{headers:{Cookie}})).json();assert.equal(receipt.status,'done');assert.equal(receipt.convert,true);
 sharing(true);
 const response=await fetch(base+'download?file='+encodeURIComponent(bookPath),{headers:{Cookie}});assert.equal(response.status,200);
 const output=Buffer.from(await response.arrayBuffer()),entries=unzip(output);
 assert.equal(entries.get('mimetype').toString(),'application/epub+zip');assert.match(entries.get('chapter0.xhtml').toString(),/koboSpan/);
 const listing=await(await fetch(base+'api/files',{headers:{Cookie}})).json();const own=listing.filter(f=>f.name.startsWith(stem));assert.equal(own.length,1);assert.equal(own[0].name,saved);assert.equal(own[0].size,output.length);
 console.log(`PASS on Kobo: 20-chapter EPUB converted (${source.length} to ${output.length} bytes), koboSpan verified, retry created no duplicate, modern/basic controls present; ${(performance.now()-started).toFixed(0)} ms including verification.`);
} finally {
 sharing(false);
 assert.match(saved,/^ReceiveShareConversionTest-[a-f0-9]{12}\.kepub\.epub$/);
 ssh(`rm -f '/mnt/onboard/Received Books/${saved}'`);
}
