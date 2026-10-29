import {writeFile} from 'node:fs/promises';
const root='docs/screenshots';
const states=[['pair','Pairing'],['upload-queue','Upload queue'],['upload-progress','Upload in progress'],['upload-complete','Uploads received'],['files','File browser'],['selected','Bulk-download selection'],['file-details','File details'],['basic','Basic / Kobo client'],['basic-review','Basic bulk-download review'],['connection-lost','Connection lost'],['finished-sample','Finished session (sample render)']];
const device=[['connection','Connection screen'],['connection-received','Connection screen with test receipt'],['sharing','Books to share'],['device-name','Device name'],['session-files','Received this session']];
const figure=(file,label)=>`<figure><a href="${file}"><img loading="lazy" src="${file}" alt="${label}"></a><figcaption>${label} · <a href="${file}">Full size</a></figcaption></figure>`;
const html=`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Receive &amp; Share — screenshots</title><style>body{font:16px/1.5 system-ui,sans-serif;max-width:1300px;margin:auto;padding:24px;color:#222;background:#fafafa}h1{margin-top:0}nav{display:flex;gap:20px;flex-wrap:wrap}a{color:#285a42}.pair{display:grid;grid-template-columns:minmax(0,3fr) minmax(0,1fr);gap:20px;align-items:start}figure{margin:0;background:white;padding:10px;border:1px solid #ccc}img{display:block;width:100%;height:auto}figcaption{margin-top:8px}section{margin:36px 0}h2{border-top:1px solid #ccc;padding-top:18px}.device{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:20px}.device img{max-height:650px;object-fit:contain;object-position:top}small{color:#555}@media(max-width:650px){.pair{grid-template-columns:1fr}.pair figure:nth-child(2){max-width:390px}}</style><h1>Receive &amp; Share</h1><p>Screenshot review · 29 October 2026. Click any image for its original size.</p><nav><a href="#kobo">Real Kobo screens</a><a href="#client">Desktop and mobile client</a></nav><p>Client images show the production application with disposable sample books, at 1440 × 1000 desktop and 390 × 844 phone viewports (full-page captures). Phone captures use a desktop browser at a narrow viewport; they are not Android/iOS device captures. Progress and disconnection were captured using a loopback-only network throttle.</p><p>The finished-session sample uses the actual production template with dummy receipts, without running a native library scan. Real Kobo images are framebuffer captures from the Libra 2. Its small temporary test PDF was removed after upload; the receipt remains in that session's history.</p><section id="kobo"><h2>Real Kobo Libra 2</h2><div class="device">${device.map(([id,name])=>figure(`kobo-${id}.png`,name)).join('')}</div></section><section id="client"><h2>Client screens</h2>${states.map(([id,name])=>`<section><h3>${name}</h3><div class="pair">${figure(`desktop-${id}.jpg`,'Desktop — '+name)}${figure(`mobile-${id}.jpg`,'Phone viewport — '+name)}</div></section>`).join('')}</section></html>`;
await writeFile(root+'/index.html',html);
const preview=(file,label,width)=>`[<img src="${file}" alt="${label}" width="${width}">](${file})`;
const markdown=`# Screenshot gallery

[Back to the project README](../../README.md)

Click any image for its full size. All ${device.length+states.length*2} screenshots are shown below.
The Kobo images are real Libra 2 framebuffer captures. Client images use disposable
sample books. Mobile views use a 390 × 844 desktop browser viewport, not a physical
phone. The finished page uses the production template with dummy receipts.

## Kobo Libra 2

${device.map(([id,name])=>`### ${name}\n\n${preview(`kobo-${id}.png`,name,350)}`).join('\n\n')}

## Desktop and mobile client

${states.map(([id,name])=>`### ${name}\n\n| Desktop | Phone viewport |\n| --- | --- |\n| ${preview(`desktop-${id}.jpg`,'Desktop: '+name,640)} | ${preview(`mobile-${id}.jpg`,'Phone viewport: '+name,240)} |`).join('\n\n')}

## Local gallery and capture tools

For the HTML gallery, run \`node tools/screenshot-gallery.mjs\` from the repository
root and visit http://127.0.0.1:4190/.

- \`tools/screenshot-session.mjs\` starts the production host receiver on port 4188 with isolated storage in dist.
- \`tools/screenshot-proxy.mjs\` provides a throttled loopback proxy on port 4189.
- \`SCREENSHOT_FIXTURE_DIR\` enables \`go test -run TestExportScreenshotFixture\` to export the finished-page template.
- \`node tools/build-screenshot-gallery.mjs\` rebuilds this Markdown page and the HTML gallery after captures.

Screenshots are taken through browser controls. None of these fixture servers or
sample books are part of the Kobo installation.

[Back to the project README](../../README.md)
`;
await writeFile(root+'/README.md',markdown);
console.log(`Gallery: ${device.length+states.length*2} screenshots`);
