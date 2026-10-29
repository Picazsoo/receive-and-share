// Local-only development proxy. Receiver on :4181, browser on :4182.
// POST :4183/online, /offline, /cut-upload-once, or /drop-ack-once.
import http from 'node:http';
let offline=false,cut=false,dropAck=false;
http.createServer((r,w)=>{if(r.method!=='POST'){w.writeHead(405).end();return;}switch(r.url){case '/online':offline=false;break;case '/offline':offline=true;break;case '/cut-upload-once':cut=true;break;case '/drop-ack-once':dropAck=true;break;default:w.writeHead(404).end();return;}w.end('ok');}).listen(4183,'127.0.0.1');
http.createServer((r,w)=>{
 if(offline){r.socket.destroy();return;}
 const isUpload=r.method==='POST'&&r.url==='/api/upload',cutThis=isUpload&&cut,dropThis=isUpload&&dropAck;if(cutThis)cut=false;if(dropThis)dropAck=false;
 const upstream=http.request({host:'127.0.0.1',port:4181,path:r.url,method:r.method,headers:{...r.headers,host:'127.0.0.1:4181'}},response=>{
  if(dropThis){response.resume();response.on('end',()=>{console.log('Dropped upload confirmation after receiver completed');w.socket?.destroy();});return;}
  w.writeHead(response.statusCode,response.headers);response.pipe(w);
 });upstream.on('error',()=>w.destroy());r.on('error',()=>upstream.destroy());w.on('close',()=>{if(!w.writableFinished)upstream.destroy();});
 if(cutThis){let bytes=0;r.on('data',chunk=>{bytes+=chunk.length;if(bytes>65536){offline=true;console.log('Disconnected during upload');upstream.destroy();r.socket.destroy();}else upstream.write(chunk);});r.on('end',()=>upstream.end());}else r.pipe(upstream);
}).listen(4182,'127.0.0.1',()=>console.log('Fault proxy: browser :4182, control :4183'));
