// Read-only native cache loader for offline browser fixtures.
const fs=require('fs'),path=require('path'),assert=require('assert/strict');
// RSB0T_CACHE_DIR: the packed game cache (engine/data/pack); the default is the sibling checkout's.
const cacheDir=process.env.RSB0T_CACHE_DIR||path.resolve(__dirname,'../../../engine/data/pack');
function cacheEntry(archive,file){
    assert(Number.isInteger(archive)&&archive>=0&&archive<=4&&Number.isInteger(file)&&file>=0&&file<=65535);
    const index=fs.readFileSync(path.join(cacheDir,`main_file_cache.idx${archive}`));
    const size=index.readUIntBE(file*6,3);let sector=index.readUIntBE(file*6+3,3);
    assert(size>0&&size<=2000000&&sector>0);
    const data=Buffer.alloc(size),descriptor=fs.openSync(path.join(cacheDir,'main_file_cache.dat'),'r');
    try{for(let offset=0,part=0;offset<size;part++){
        const count=Math.min(512,size-offset),block=Buffer.alloc(8+count);
        assert(sector>0&&fs.readSync(descriptor,block,0,block.length,sector*520)===block.length);
        assert(block.readUInt16BE(0)===file&&block.readUInt16BE(2)===part&&block[7]===archive+1);
        block.copy(data,offset,8);offset+=count;sector=block.readUIntBE(4,3);
        if(offset===size)assert.equal(sector,0);
    }}finally{fs.closeSync(descriptor);}
    return data;
}
module.exports={cacheEntry};
