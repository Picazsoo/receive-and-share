/* Small STORE-only ZIP writer for local prototype downloads. Production ZIPs
   will be streamed by the Kobo using Go's archive/zip package. */
((root) => {
  const table = Uint32Array.from({length:256},(_,i)=>{for(let b=0;b<8;b++)i=(i&1)?0xedb88320^(i>>>1):i>>>1;return i>>>0;});
  const crc32 = bytes => {let c=0xffffffff;for(const b of bytes)c=table[(c^b)&255]^(c>>>8);return(c^0xffffffff)>>>0;};
  async function zipStore(entries,onProgress=()=>{}) {
    if(entries.length>65535)throw Error('Too many files for this prototype.');
    const parts=[],central=[];let offset=0,index=0;const encoder=new TextEncoder();
    for(const entry of entries){
      if(entry.name.startsWith('/')||entry.name.split('/').some(p=>p==='..'))throw Error('Unsafe ZIP entry name.');
      const name=encoder.encode(entry.name),data=new Uint8Array(await entry.blob.arrayBuffer()),crc=crc32(data);
      if(name.length>65535||offset+data.length>0xffffffff)throw Error('ZIP too large for this prototype.');
      const local=new Uint8Array(30+name.length),lv=new DataView(local.buffer);
      lv.setUint32(0,0x04034b50,true);lv.setUint16(4,20,true);lv.setUint16(6,0x800,true);
      lv.setUint16(12,0x5d5c,true);lv.setUint32(14,crc,true);lv.setUint32(18,data.length,true);lv.setUint32(22,data.length,true);lv.setUint16(26,name.length,true);local.set(name,30);
      const record=new Uint8Array(46+name.length),cv=new DataView(record.buffer);
      cv.setUint32(0,0x02014b50,true);cv.setUint16(4,20,true);cv.setUint16(6,20,true);cv.setUint16(8,0x800,true);cv.setUint16(14,0x5d5c,true);
      cv.setUint32(16,crc,true);cv.setUint32(20,data.length,true);cv.setUint32(24,data.length,true);cv.setUint16(28,name.length,true);cv.setUint32(42,offset,true);record.set(name,46);
      parts.push(local,data);central.push(record);offset+=local.length+data.length;onProgress(++index,entries.length);
      await new Promise(resolve=>setTimeout(resolve,0));
    }
    const centralSize=central.reduce((sum,b)=>sum+b.length,0),end=new Uint8Array(22),ev=new DataView(end.buffer);
    ev.setUint32(0,0x06054b50,true);ev.setUint16(8,entries.length,true);ev.setUint16(10,entries.length,true);ev.setUint32(12,centralSize,true);ev.setUint32(16,offset,true);
    return new Blob([...parts,...central,end],{type:'application/zip'});
  }
  root.KoboZip={zipStore,crc32};
  if(typeof module!=='undefined')module.exports={zipStore,crc32};
})(typeof window!=='undefined'?window:globalThis);
