import {readFile} from 'node:fs/promises';
import vm from 'node:vm';
import assert from 'node:assert/strict';

// Execute the production JS with a minimal DOM and controlled network events.
// Old callbacks must not mutate failed uploads or issue an automatic retry.

const nodes = new Map();
function node(selector) {
  if (!nodes.has(selector)) nodes.set(selector, {
    classList: {add(){}, remove(){}, toggle(){}},
    addEventListener(){}, showModal(){}, close(){},
    querySelector: sub => node(selector+' '+sub),
  });
  return nodes.get(selector);
}
const requests = [];
class XHR {
  constructor(){ this.upload={}; requests.push(this); }
  open(){} setRequestHeader(){} send(){} abort(){ this.onabort(); }
}
let timerID=0;
const context=vm.createContext({
  document:{querySelector:node,querySelectorAll:()=>[],addEventListener(){}},
  window:{addEventListener(){}},
  setTimeout:()=>++timerID,clearTimeout(){},
  XMLHttpRequest:XHR, FormData:class {set(){}},
  AbortController, URLSearchParams, Uint8Array, crypto:globalThis.crypto,
  fetch:async url=>({ok:true,status:200,json:async()=>
    url==='/api/files'?[]:
    url.startsWith('/api/upload-status')?{status:'unknown'}:
    url==='/api/pair-challenge'?{nonce:'challenge'}:
    {csrf:'new-csrf',instance:'new-instance',shareRevision:0}}),
});
let source=await readFile(new URL('./web/app.js',import.meta.url),'utf8');
source=source.replace(/\}\)\(\);\s*$/, `globalThis.review={upload,send,pairAgain,poll,seed(f){queue=[f];csrf='old-csrf';instance='old-instance';connection='online';},get connection(){return connection;}};})();`);
vm.runInContext(source,context);
await new Promise(resolve=>setImmediate(resolve));
const app=context.review;
for (const mode of ['pair','restart']) {
 const file={uploadID:'a'.repeat(32),id:'1',name:'saved-before-restart.pdf',size:24,file:{},status:'sending',progress:100};
 app.seed(file);
 const before=requests.length;
 const oldUpload=app.upload(file);
 const xhr=requests[before];
 if(mode==='pair'){
  await app.pairAgain();
  await node('#re-pair-form').onsubmit({preventDefault(){},target:{querySelector:()=>node('button'),elements:{code:{value:'123456'}}}});
 }else await app.poll();
 assert.equal(file.status,'failed',mode);
 // Even delayed notifications already queued before abort must be harmless.
 xhr.onerror();xhr.onabort();xhr.status=200;xhr.responseText=JSON.stringify({name:'wrong.pdf'});xhr.onload();xhr.upload.onprogress({lengthComputable:true,loaded:1,total:2});
 await oldUpload;
 assert.equal(file.status,'failed',mode);
 assert.equal(file.saved,undefined,mode);
 assert.equal(file.progress,100,mode);
 await app.poll();await app.send();
 assert.equal(requests.length,before+1,'No automatic upload after '+mode);
}
console.log('PASS: re-pairing and receiver restart cancel uploads and ignore stale XHR callbacks; retry remains explicit.');
