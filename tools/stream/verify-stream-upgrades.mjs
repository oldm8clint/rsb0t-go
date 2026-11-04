import assert from 'node:assert/strict';
import {buildMenu,buildUseMenu,pickEntity,pickEntities} from './scene-input.js';
import {applyUiDelta} from './scene-state.js';
import {meshBounds,boundsVisible,frustumPlanes,modelMatrix} from './scene-gl.js';
import {advancePaintDeadline} from './scene-view.js';

const entity={e:{k:'p42',n:'Zezima'}}, tile={x:3200,z:3200};
const info={playerOps:['Follow','Attack','','Trade with',''],playerOpPriority:[true,false,false,false,false]};
const menu=buildMenu({entity,tile},info);
assert.deepEqual(menu.map(m=>m.label),['Trade with Zezima','Walk here','Follow Zezima','Attack Zezima']);
assert.deepEqual(menu[0].action,{kind:'player',index:42,op:4});
assert.deepEqual(buildUseMenu({entity},info,{name:'Coins',obj:995,slot:0,com:3214})[0].action,{kind:'playeru',index:42,use:{obj:995,slot:0,com:3214}});
assert.equal(buildMenu({entity,tile},{playerOps:[]}).length,1,'Absent server options must not be invented');
const ui=applyUiDelta({},info);
assert.deepEqual(ui.playerOps,info.playerOps);
assert.deepEqual(applyUiDelta(ui,{playerOps:['','','','','']}).playerOps,['','','','','']);
const project=(x,y,z)=>[x*20,100-y*20,z+1];
assert.equal(pickEntity(project,[{...entity,cx:3,cz:1,y:0,top:2}],60,70).e.k,'p42');
const overlaps=pickEntities(project,[{...entity,cx:3,cz:1,y:0,top:2},{e:{k:'n7',t:1},cx:3,cz:1,y:0,top:2}],60,70);
assert.equal(overlaps.length,2);
assert(buildMenu({entities:overlaps,tile},{...info,npcs:{1:{name:'Goblin',ops:['Talk-to']}}}).some(m=>m.label==='Trade with Zezima'),'Overlapping NPC must not hide player menu');
const vertices=new Float32Array([-1,-1,-1,1,1,1,1,0,0,-1, 1,1,1,1,1,1,1,0,0,-1]);
const bounds=meshBounds(vertices);
assert.equal(bounds.r,Math.sqrt(3));
assert.equal(meshBounds(new Float32Array()),null);
const planes=frustumPlanes(modelMatrix(0,0,0,0));
assert(boundsVisible(bounds,modelMatrix(0,0,0,0),planes));
assert(!boundsVisible(bounds,modelMatrix(10,0,0,0),planes));
assert(boundsVisible(bounds,modelMatrix(2,0,0,0),planes),'Batches intersecting frame edge must remain visible');
for (const refresh of [60,90,120,144,240]) {
    let deadline=0, frames=0;
    for(let i=0;i<refresh*10;i++) { const now=i*1000/refresh; if(now+1>=deadline){frames++;deadline=advancePaintDeadline(deadline,now,60);} }
    assert(Math.abs(frames-600)<=1,`60 FPS budget must remain correct at ${refresh} Hz: ${frames}`);
}
assert(advancePaintDeadline(100,10000,60)>10000,'Long stalls skip missed paints');
console.log('Player menus, use, delta clearing, picking and conservative batch culling passed');
