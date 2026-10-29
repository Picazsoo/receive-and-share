// Loopback-only network throttling for documentation captures; not shipped.
import http from 'node:http';
import {existsSync} from 'node:fs';
import path from 'node:path';
const offline=path.resolve('dist/screenshot-offline');
http.createServer(async(req,res)=>{
 if(existsSync(offline)){res.destroy();return;}
 const headers={...req.headers,host:'127.0.0.1:4188'};
 if(headers.origin)headers.origin='http://127.0.0.1:4188';
 const upstream=http.request({hostname:'127.0.0.1',port:4188,path:req.url,method:req.method,headers},r=>{res.writeHead(r.statusCode,r.headers);r.pipe(res);});
 upstream.on('error',()=>res.destroy());
 try{for await(const chunk of req){if(req.url==='/api/upload')await new Promise(r=>setTimeout(r,75));if(!upstream.write(chunk))await new Promise(r=>upstream.once('drain',r));}upstream.end();}catch{upstream.destroy();}
}).listen(4189,'127.0.0.1',()=>console.log('Throttled screenshot receiver: http://127.0.0.1:4189/'));
