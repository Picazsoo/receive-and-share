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
  constructor(){ this.upload={}; this.headers={}; requests.push(this); }
  open(){} setRequestHeader(name,value){this.headers[name]=value;} send(){} abort(){ this.onabort(); }
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
source=source.replace(/\}\)\(\);\s*$/, `globalThis.review={upload,send,pairAgain,poll,deleteDialog,seedFiles(files){library=files;selected=new Set(files.map(f=>f.path));allowDelete=true;connection="online";},seed(f){queue=[f];csrf='old-csrf';instance='old-instance';connection='online';},get connection(){return connection;}};})();`);
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
// A failed conversion offers an explicit, new upload of the original EPUB.
const original={uploadID:'b'.repeat(32),id:'1',name:'original.epub',size:24,file:{},status:'failed',convert:true,conversionFailed:true,error:'Conversion failed'};
app.seed(original);
const previousID=original.uploadID,before=requests.length;
const button={dataset:{action:'original'},closest:()=>({dataset:{id:'1'}})};
node('#queue-list').onclick({target:{closest:()=>button}});
await new Promise(resolve=>setImmediate(resolve));
assert.equal(requests.length,before+1);
assert.equal(original.convert,false);
assert.notEqual(original.uploadID,previousID);
assert.equal(requests[before].headers['X-Convert-EPUB'],'0');
const xhr=requests[before];xhr.status=200;xhr.responseText=JSON.stringify({name:'original.epub'});xhr.onload();
await new Promise(resolve=>setImmediate(resolve));
assert.equal(original.status,'complete');
// Reviewing a bulk deletion must not delete anything until explicit confirmation.
const files=[{path:'Books/a.pdf',name:'a.pdf',type:'PDF',size:24},{path:'Books/b.epub',name:'b.epub',type:'EPUB',size:42}];
app.seedFiles(files);
let deletions=0;
context.fetch=async (url,options)=>({ok:true,status:200,json:async()=>{
 if(url==='/api/delete-review'){
  assert.equal(options.method,'POST');assert.deepEqual(options.body.getAll('file'),files.map(f=>f.path));
  return {confirmation:'review-token',files,total:66};
 }
 if(url==='/api/delete'){
  deletions++;assert.equal(options.body.get('confirmation'),'review-token');assert.equal(options.body.has('file'),false);
  return [{path:files[0].path,deleted:true,message:'Deleted'},{path:files[1].path,deleted:false,message:'Book changed'}];
 }
 if(url==='/api/files')return [files[1]];
 throw Error('Unexpected request '+url);
}});
await app.deleteDialog();assert.equal(deletions,0);
assert.match(node('#modal-content').innerHTML,/Delete permanently/);
assert.match(node('#modal-content').innerHTML,/Books\/a.pdf/);
await node('#delete-confirm').onsubmit({preventDefault(){},target:{querySelector:()=>node('delete-submit')}});
assert.equal(deletions,1);assert.match(node('#modal-content').innerHTML,/1 of 2 books deleted/);assert.match(node('#modal-content').innerHTML,/Book changed/);
const searchable=[{path:'Books/Harry Potter ohnivy\u0301 poha\u0301r.pdf',name:'Harry Potter ohnivý pohár.pdf',type:'PDF',size:24},{path:'Books/Zlatý Řez.pdf',name:'Zlatý Řez.pdf',type:'PDF',size:42}];
app.seedFiles(searchable);
for(const [query,want,other] of [['ohnivy','Harry Potter ohnivý pohár.pdf','Zlatý Řez.pdf'],['OHNIVÝ','Harry Potter ohnivý pohár.pdf','Zlatý Řez.pdf'],['zlaty rez','Zlatý Řez.pdf','Harry Potter ohnivý pohár.pdf'],['ZLATÝ ŘEZ','Zlatý Řez.pdf','Harry Potter ohnivý pohár.pdf']]){
 node('#file-search').oninput({target:{value:query}});
 assert.ok(node('#library-list').innerHTML.includes(want));assert.ok(!node('#library-list').innerHTML.includes(other));
}
console.log('PASS: reconnect isolation, conversion fallback, deletion confirmation and case/diacritic-insensitive search.');
