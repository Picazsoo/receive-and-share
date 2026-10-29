import http from 'node:http';
import {readFile} from 'node:fs/promises';
import path from 'node:path';
import {createRequire} from 'node:module';
const {zipStore}=createRequire(import.meta.url)('./zip.js');
const files={'/':'index.html','/index.html':'index.html','/styles.css':'styles.css','/app.js':'app.js','/zip.js':'zip.js','/basic.html':'basic.html','/basic.css':'basic.css','/pair.html':'pair.html','/pair-source.html':'pair-source.html'};
const types={'.html':'text/html; charset=utf-8','.css':'text/css; charset=utf-8','.js':'text/javascript; charset=utf-8'};
const port=Number(process.env.PORT??4173);
const basicFiles=[{id:'1',name:'Letters from elsewhere.epub',folder:'Received Books',type:'EPUB',size:'1.6 MiB'},{id:'2',name:'The small book of gardens.pdf',folder:'Reading list',type:'PDF',size:'3.4 MiB'},{id:'3',name:'Collected essays.epub',folder:'',type:'EPUB',size:'1.1 MiB'}];
const escape=value=>String(value).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const filePath=f=>`/${f.folder?f.folder+'/':''}${f.name}`;
const sample=f=>new Blob([`Receive & Share — local basic-view prototype\n\n${filePath(f)}\n${f.type} · ${f.size}\n\nThis is a text preview, not a real book.\n`],{type:'text/plain;charset=utf-8'});
const chosen=url=>basicFiles.filter(f=>url.searchParams.getAll('file').includes(f.id));
const wrap=body=>`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Receive & Share</title><link rel="stylesheet" href="basic.css"></head><body><div class="page">${body}</div></body></html>`;
http.createServer(async(req,res)=>{
 const url=new URL(req.url,'http://localhost');
 if(url.pathname==='/pair'&&req.method==='POST'){
  let body='';for await(const chunk of req){body+=chunk;if(body.length>1024){res.writeHead(413);res.end();return;}}
  const ok=new URLSearchParams(body).get('code')==='482713';res.writeHead(303,{Location:ok?'basic.html':'pair.html?error=1'});res.end();return;
 }
 if(url.pathname==='/basic-review'&&req.method==='GET'){
  const selected=chosen(url),total=selected.reduce((n,f)=>n+parseFloat(f.size),0).toFixed(1);
  const rows=selected.map(f=>`<div class="selected-item"><strong>${escape(f.name)}</strong><div class="path">${escape(filePath(f))}</div><div class="meta">${f.type} · ${f.size}</div></div>`).join('');
  res.writeHead(200,{'Content-Type':'text/html; charset=utf-8','Cache-Control':'no-store'});res.end(wrap(`<nav><a href="basic.html">Back to files</a></nav><h1>Download selected files</h1><div class="summary"><strong>${selected.length} files selected</strong><span>Total: ${total} MiB</span></div><div class="selected-list">${rows||'<p>No files selected.</p>'}</div><form method="get" action="basic-zip">${selected.map(f=>`<input type="hidden" name="file" value="${f.id}">`).join('')}<button type="submit" class="primary" ${selected.length?'':'disabled'}>Download ZIP</button></form><p class="prototype-note">Sample files download as text previews. No JavaScript is used.</p>`));return;
 }
 if(url.pathname==='/basic-zip'&&req.method==='GET'){
  const selected=chosen(url);if(!selected.length){res.writeHead(400);res.end('Select files first.');return;}
  const zip=await zipStore(selected.map(f=>({name:`${f.folder?f.folder+'/':''}${f.name}.preview.txt`,blob:sample(f)})));
  res.writeHead(200,{'Content-Type':'application/zip','Content-Disposition':'attachment; filename="Receive & Share books.zip"'});res.end(Buffer.from(await zip.arrayBuffer()));return;
 }
 if(url.pathname==='/basic-download'&&req.method==='GET'){
  const item=basicFiles.find(f=>f.id===url.searchParams.get('file'));if(!item){res.writeHead(404);res.end();return;}
  res.writeHead(200,{'Content-Type':'text/plain; charset=utf-8','Content-Disposition':`attachment; filename="${item.name}.preview.txt"`});res.end(Buffer.from(await sample(item).arrayBuffer()));return;
 }
 const file=files[url.pathname];
 if(!file||!['GET','HEAD'].includes(req.method)){res.writeHead(404);res.end();return;}
 try{let data=await readFile(path.join(import.meta.dirname,file));
  if(file==='basic.html'){
   const folder=url.searchParams.get('folder')||'',visible=basicFiles.filter(f=>!folder||f.folder===folder);
   const rows=visible.map(f=>`<tr><td class="check"><input type="checkbox" name="file" value="${f.id}" aria-label="Select ${escape(f.name)}"></td><td class="name"><label><strong>${escape(f.name)}</strong></label><div class="path">${escape(filePath(f))}</div><div class="meta">${f.type} · ${f.size}</div></td><td class="download"><a href="basic-download?file=${f.id}">Download</a></td></tr>`).join('');
   data=Buffer.from(data.toString().replace('<!-- file-rows -->',rows).replace('<!-- folder-label -->',escape(folder||'All book folders')));
  }
  if(file==='pair.html'&&url.searchParams.has('error'))data=Buffer.from(data.toString().replace('<!-- pairing-error -->','<p class="error">Incorrect code. Use demo code 482713.</p>'));
  res.writeHead(200,{'Content-Type':types[path.extname(file)],'Cache-Control':'no-store','X-Content-Type-Options':'nosniff'});res.end(req.method==='HEAD'?undefined:data);}catch{res.writeHead(500);res.end('Could not load the local prototype.');}
}).listen(port,'127.0.0.1',()=>console.log(`Local prototype: http://127.0.0.1:${port}`));
