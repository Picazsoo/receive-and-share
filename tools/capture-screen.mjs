// Usage from repository root: node tools/capture-screen.mjs ../.local-ssh [output.png]
// Requires tools/screenshot.go built for ARM and uploaded to /tmp/kobo-screenshot.
import {readFileSync,writeFileSync,mkdirSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import path from 'node:path';
const state=path.resolve(process.argv[2]??'../.local-ssh');
const output=path.resolve(process.argv[3]??'dist/kobo-screen.png');
const address=readFileSync(path.join(state,'device-ip.txt'),'utf8').trim();
if(!/^\d+\.\d+\.\d+\.\d+$/.test(address))throw Error('Invalid device address');
const result=spawnSync('ssh.exe',['-T','-p','2222','-i',path.join(state,'kobo_ed25519'),'-o','BatchMode=yes','-o','IdentitiesOnly=yes','-o','PreferredAuthentications=publickey','-o','PasswordAuthentication=no','-o','KbdInteractiveAuthentication=no','-o','StrictHostKeyChecking=yes','-o',`UserKnownHostsFile=${path.join(state,'known_hosts')}`,'-o','HostKeyAlias=kobo-libra2','-o','ConnectTimeout=8',`root@${address}`,'/tmp/kobo-screenshot'],{maxBuffer:40*1024*1024,windowsHide:true});
if(result.stderr.length)process.stderr.write(result.stderr);
if(result.error)throw result.error;
if(result.status!==0)process.exit(result.status??1);
if(!result.stdout.subarray(0,8).equals(Buffer.from([137,80,78,71,13,10,26,10])))throw Error('Screenshot did not return PNG data');
mkdirSync(path.dirname(output),{recursive:true});writeFileSync(output,result.stdout);
console.log(output);
