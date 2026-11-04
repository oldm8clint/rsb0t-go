import assert from 'node:assert/strict';
import { effectFrame, projectileAt } from './scene-effects.js';
import { SceneState, sceneFromSnapshot } from './scene-state.js';

const info = { delays:[2,3], animated:true };
assert.equal(effectFrame(info,-1),-1);
assert.equal(effectFrame(info,0),0);
assert.equal(effectFrame(info,2),1);
assert.equal(effectFrame(info,5),-1);
assert.equal(effectFrame(info,5,true),0);

const effect = {x:10,z:20,toX:14,toZ:20,height:64,endHeight:32,start:5,end:35,slope:16,arc:16};
assert.equal(projectileAt(effect,4,()=>0),null);
assert.equal(projectileAt(effect,36,()=>0),null);
const launch = projectileAt(effect,5,()=>0);
assert.equal(launch.x,10.625);
assert.equal(launch.y,.5);
const arc = projectileAt(effect,20,()=>0);
assert(arc.x>launch.x && arc.x<14.5);
assert(arc.y>launch.y,'Launch slope produces a rising arc');
const targeted = projectileAt(effect,20,()=>0,[18.5,20.5]);
assert(targeted.x>arc.x,'Tracked target changes destination');

const combat = {seen:true,tick:1,health:7,total:10,hits:[{seen:true,tick:1,damage:3,type:1}]};
const s = sceneFromSnapshot({x:1,z:2,selfSlot:7,combat,effects:[effect],ui:{tick:1},npcs:[{Index:1,X:2,Z:2,Combat:combat}],players:[]});
assert.deepEqual(s.self.combat,combat);
assert.deepEqual(s.entities[0].combat,combat);
const state = new SceneState();
state.applyFrame({seq:1,scene:s},0);
state.applyFrame({seq:2,delta:{base:1,effects:[]}},20);
assert.deepEqual(state.scene.effects,[],'Expired effects clear through deltas');
console.log(JSON.stringify({sceneEffects:'pass'}));
