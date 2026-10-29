// Local-only disposable fixture for screenshots of the production UI.
import {mkdir,writeFile} from 'node:fs/promises';
import {spawn} from 'node:child_process';
import path from 'node:path';
const root=path.resolve('dist/screenshot-storage');
for(const dir of ['Fiction','Reference','Received Books','.adds/tmp']) await mkdir(path.join(root,dir),{recursive:true});
for(const [name,size] of [['Fiction/The quiet library.epub',1250000],['Fiction/A walk through Prague.epub',745000],['Reference/Field notes.pdf',2400000],['Reference/Reading guide.pdf',185000]]){
 const data=Buffer.alloc(size,32);data.write(name.endsWith('.pdf')?'%PDF-1.7\n':'PK\x03\x04');await writeFile(path.join(root,name),data);
}
await writeFile(path.join(root,'.adds/settings.json'),JSON.stringify({name:'kobo-demo',shareAll:true}));
const child=spawn(path.resolve('dist/kobo-receive-host.exe'),['-root',root,'-dest',path.join(root,'Received Books'),'-temp',path.join(root,'.adds/tmp'),'-ip','127.0.0.1','-port','4188','-mdns=false','-ttl','60m'],{stdio:'inherit',windowsHide:true});
process.on('SIGINT',()=>child.kill());process.on('SIGTERM',()=>child.kill());child.on('exit',code=>process.exit(code??0));
