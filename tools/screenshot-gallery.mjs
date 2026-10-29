import http from 'node:http';
import {readFile} from 'node:fs/promises';
import path from 'node:path';
const root=path.resolve('docs/screenshots');
http.createServer(async(req,res)=>{
 const name=new URL(req.url,'http://localhost').pathname.slice(1)||'index.html';
 if(!/^[a-z0-9-]+\.(html|jpg|png)$/.test(name)){res.writeHead(404).end();return;}
 try{
  const file=name==='finished.html'?path.resolve('dist/screenshot-pages/finished.html'):path.join(root,name);
  const data=await readFile(file);res.setHeader('Content-Type',name.endsWith('.html')?'text/html; charset=utf-8':name.endsWith('.png')?'image/png':'image/jpeg');res.end(data);
 }catch{res.writeHead(404).end();}
}).listen(4190,'127.0.0.1',()=>console.log('Screenshot gallery: http://127.0.0.1:4190/'));
