// Input for the stream view, which acts only after the server has acknowledged input control
// (the provider's setInputEnabled): world and interface clicks turned into the game's own option
// menus (the reference client builds the list; scene-input.js orders and labels it), the menu
// drawn in the game's bitmap font, Use mode and typing (chat, or the count dialog's digits) which the
// game client keeps locally, and the validated action sent to the bridge. Without control the same
// menus open read-only (Examine is local). Everything it needs from the view comes through `c`:
//   state, host, controller, counters, and getters chrome, canvas, renderer, heightAt, adapter,
//   renderDistance, uiTab, nativeFonts, link, stopped, placed (the entities last drawn), plus
//   adapterCall(fn) (serialised decoder access) and markUi() (redraw the game's interfaces).

import { pickEntities, pickTile, buildMenu, interfaceItems, chatKey } from './scene-input.js';
import { toGame, tabAt } from './scene-chrome.js';

export function createActions(c) {
    const state = c.state;
    let inputOn = false, menuEl = null, inputLog = [];
    // Input state the game client keeps locally: the item a "Use" was started on and the line
    // being typed (chat, or the count dialog's digits). Both are drawn by the game's own code.
    let useItem = null, typed = '', toastEl = null, toastTimer = 0;

    async function onClick(e, right) {
        menuEl?.remove(); menuEl = null;
        // With a main interface open (a bank, a shop) the game's viewport is that interface.
        if (c.chrome.gameFrame && state.scene?.ui && state.scene.ui.main !== -1) return onInterfaceClick(e, right);
        if ((!inputOn && !right) || !state.scene || !c.heightAt || c.stopped) return;
        const r = c.canvas.getBoundingClientRect();
        const menu = await worldMenu(e.clientX - r.left, e.clientY - r.top);
        if (!menu) return;
        if (!menu.length) { if (!right && useItem) { useItem = null; c.markUi(); } return; }
        if (!right) { if (inputOn) await runItem(menu[0]); return; }
        showMenu(e, menu);
    }
    // worldMenu: the game-order option list for a point on the GPU viewport (CSS pixels).
    async function worldMenu(cx, cy) {
        if (!state.scene || !c.heightAt || c.stopped) return null;
        const project = (x, y, z) => c.renderer.project(x, y, z);
        const entities = pickEntities(project, c.placed, cx, cy);
        const [sx, sz] = state.selfAt(performance.now());
        const tile = pickTile(project, (x, z) => c.heightAt(x, z), Math.round(sx), Math.round(sz), cx, cy, Math.min(c.renderDistance, 30));
        const objs = tile ? (state.scene.objs || []).filter(([x, z]) => x === tile.x && z === tile.z) : [];
        let info = { npcs: {}, objs: {}, locs: [] };
        if (typeof c.adapter.menuInfo === 'function') {
            const task = c.adapterCall(() => c.adapter.menuInfo({ npcs: [...new Set(entities.filter(p=>p.e.k[0]==='n').map(p=>p.e.t))], objs: objs.map(o => o[2]),
                tile: tile ? { x: tile.x, z: tile.z, level: state.scene.self.level } : null }));
            info = await task;
        }
        if (c.stopped) return null;
        info.playerOps = state.scene.ui?.playerOps || [];
        info.playerOpPriority = state.scene.ui?.playerOpPriority || [];
        return buildMenu({ entities, tile, objs }, info, useItem);
    }
    // The interface snapshot the adapter replays (the game's frame, tabs, chatbox, modals).
    //
    // Entity tiles come from each motion's own eased x/z — the SAME position the GPU scene
    // draws the model at, so the game's minimap dot and the model cannot disagree. They used
    // to read a toX/toZ that no motion record has ever had (scene-state.js's newMotion has
    // always written x/z and obsX/obsZ), so every entity handed to the adapter arrived at
    // NaN and the reference client placed it on scene tile 0,0. That put every npc and player
    // ~200 minimap units away, past Client.minimapDrawDot's own distance test, which is why
    // the minimap drew terrain, walls and icons but never a single npc or player dot.
    function uiSnapshot() {
        const s = state.scene, looks = s.looks || {};
        const snapshot = { x: s.self.x, z: s.self.z, level: s.self.level, centreX: s.self.centreX, centreZ: s.self.centreZ, selfSlot: s.self.slot,
            appearance: looks.self, ui: s.ui, players: [], npcs: [] };
        for (const [k, m] of state.entities) {
            if (k[0] === 'p') snapshot.players.push({ index: +k.slice(1), name: m.e.n, x: m.x, z: m.z, appearance: looks[k] });
            else snapshot.npcs.push({ Index: +k.slice(1), Type: m.e.t, X: m.x, Z: m.z });
        }
        return snapshot;
    }
    // A click on the game's interfaces (side panel, chatbox, or an open main interface): the
    // reference client's own option list for that point (adapter interfaceMenu). A left click
    // on a scrollbar scrolls it instead, as the game's does. Tabs are the chrome's (local).
    async function onInterfaceClick(e, right) {
        menuEl?.remove(); menuEl = null;
        if (!state.scene?.ui || c.stopped || typeof c.adapter?.interfaceMenu !== 'function') return;
        const [gx, gy] = toGame(c.chrome.root, e);
        if (tabAt(gx, gy) !== undefined) return;
        const snap = uiSnapshot();
        if (!right && typeof c.adapter.interfaceScroll === 'function' && await c.adapterCall(() => c.adapter.interfaceScroll(snap, { x: gx, y: gy, tab: c.uiTab, click: true }))) { c.markUi(); return; }
        const res = await c.adapterCall(() => c.adapter.interfaceMenu(snap, { x: gx, y: gy, tab: c.uiTab, use: useItem }));
        if (c.stopped) return;
        const items = interfaceItems(res?.entries, useItem);
        if (!items.length) { if (!right && useItem) { useItem = null; c.markUi(); } return; }
        if (!right) { if (inputOn) await runItem(items[0]); return; }
        showMenu(e, items);
    }
    async function interfaceWheel(e) {
        if (!c.chrome.gameFrame || !state.scene?.ui || typeof c.adapter?.interfaceScroll !== 'function') return false;
        const [gx, gy] = toGame(c.chrome.root, e);
        const inViewport = gx > 4 && gy > 4 && gx < 516 && gy < 338;
        if (inViewport && state.scene.ui.main === -1) return false;
        const snap = uiSnapshot();
        if (await c.adapterCall(() => c.adapter.interfaceScroll(snap, { x: gx, y: gy, tab: c.uiTab, delta: Math.sign(e.deltaY) * 24 }))) c.markUi();
        return true;
    }
    function toast(text) {
        toastEl?.remove(); clearTimeout(toastTimer);
        toastEl = document.createElement('div'); toastEl.setAttribute('role', 'status'); toastEl.textContent = text;
        toastEl.style.cssText = 'position:absolute;left:4px;bottom:4px;background:rgba(0,0,0,.75);color:#0ff;font:12px Arial,sans-serif;padding:2px 6px;z-index:6;pointer-events:none;max-width:90%';
        c.canvas.parentElement.append(toastEl);
        toastTimer = setTimeout(() => { toastEl?.remove(); toastEl = null; }, 4000);
    }
    // runItem does what the game client does with a chosen option: start a Use or examine an
    // item locally, or ask the server to send it. Any choice other than starting a Use ends
    // use mode, as Client.ts useMenuOption does.
    async function runItem(item) {
        if (item.local?.description) { toast(item.local.description); return; }
        if (!inputOn || item.unavailable) return;
        if (item.local?.use) { useItem = item.local.use; c.markUi(); return; }
        useItem = null; c.markUi();
        if (item.local?.examine !== undefined) {
            const info = await c.adapterCall(() => c.adapter.menuInfo({ objs: [item.local.examine] }));
            const o = info?.objs?.[item.local.examine];
            toast(o?.desc || (o ? `It's a ${o.name}.` : item.label));
            return;
        }
        if (item.action) await sendAction(item);
    }
    // Keys, while the view has keyboard focus and input control is held: the game's chat (or
    // the count dialog's amount). Arrow keys and the rest stay the camera's.
    function onKey(e) {
        if (e.key === "Escape" && menuEl) { menuEl.remove(); menuEl=null; return true; }
        if (!inputOn || !state.scene?.ui) return false;
        const ui = state.scene.ui;
        if (e.key === 'Escape' && useItem) { useItem = null; c.markUi(); return true; }
        if (ui.chatModal !== -1 && !ui.countDialog) return e.key.length === 1; // a dialogue is open: the game types nothing
        const k = chatKey(typed, e.key, !!ui.countDialog);
        if (!k) return false;
        typed = k.text; c.markUi();
        const said = (k.submit || '').trim();
        if (said && ui.countDialog) void sendAction({ label: `Enter amount: ${said}`, action: { kind: 'count', value: parseInt(said, 10) || 0 } });
        else if (said) void sendAction({ label: `Say: ${said}`, action: { kind: 'chat', text: said } });
        return true;
    }
    // showMenu draws the game-style "Choose Option" menu at the pointer. Without input control
    // actions remain disabled until control is held; Examine is local and read-only.
    function showMenu(e, menu) {
        menuEl?.remove();
        const live = inputOn, font = c.nativeFonts.b12;
        const scale = c.chrome.gameFrame ? c.canvas.clientWidth / 512 : 1;
        const entries = [...menu, { label:'Cancel', cancel:true }];
        const width = Math.max(0, ...['Choose Option', ...entries.map(i => i.label)].map(t => (font ? font.width(t) : t.length*7)+8));
        const height = entries.length*15+22;
        menuEl = document.createElement('div'); menuEl.setAttribute('role','menu');
        menuEl.setAttribute('aria-label',live ? 'Choose Option' : 'Choose Option (view only)');
        const box = c.chrome.gameFrame ? c.chrome.root : c.canvas.parentElement;
        const at = box.getBoundingClientRect();
        menuEl.style.cssText = 'position:absolute;z-index:5;background:#5d5447;overflow:hidden';
        menuEl.style.left = Math.max(0,e.clientX-at.left-width*scale/2)+'px';
        menuEl.style.top = Math.max(0,e.clientY-at.top)+'px';
        menuEl.style.width = width*scale+'px'; menuEl.style.height = height*scale+'px';
        const art = document.createElement('canvas'); art.width=width; art.height=height;
        art.style.cssText='position:absolute;inset:0;width:100%;height:100%;image-rendering:pixelated;pointer-events:none';
        menuEl.append(art);
        let hover=-1;
        const draw = () => {
            const g=art.getContext('2d'); g.fillStyle='#5d5447'; g.fillRect(0,0,width,height);
            g.fillStyle='#000'; g.fillRect(1,1,width-2,16); g.strokeStyle='#000'; g.strokeRect(1.5,18.5,width-3,height-20);
            const text=(label,x,y,colour,shadow) => { if (font) font.draw(g,label,x,y,colour,shadow); else { g.font='bold 12px Arial'; g.fillStyle=colour; g.fillText(label,x,y); } };
            text('Choose Option',3,14,'#5d5447',false);
            entries.forEach((item,i) => text(item.display || item.label,3,i*15+31,i===hover?'#ffff00':'#ffffff',true));
        };
        menuEl.addEventListener('contextmenu',ev=>{ev.preventDefault();ev.stopPropagation();});
        menuEl.addEventListener('pointerdown',ev=>ev.stopPropagation());
        entries.forEach((item,i) => {
            const b=document.createElement('div'); b.setAttribute('role','menuitem'); b.setAttribute('aria-label',item.label);
            const enabled=item.cancel || !!item.local?.description || (live && !item.unavailable);
            if (!enabled) b.setAttribute('aria-disabled','true');
            b.title = !live && !item.cancel ? 'Take control to act' : item.unavailable ? 'Not available in stream view' : item.label;
            b.style.cssText='position:absolute;left:0;width:100%;cursor:'+ (enabled?'pointer':'default');
            b.style.top=(i*15+18)*scale+'px'; b.style.height=15*scale+'px';
            b.addEventListener('pointerenter',()=>{hover=i;draw();}); b.addEventListener('pointerleave',()=>{hover=-1;draw();});
            b.addEventListener('click',ev=>{
                ev.stopPropagation(); menuEl?.remove(); menuEl=null;
                if (item.cancel) { if (useItem) { useItem=null;uiDirty=true; } return; }
                if (enabled) void runItem(item);
            }); menuEl.append(b);
        });
        draw();
        box.append(menuEl);
        const hb=box.getBoundingClientRect(),mb=menuEl.getBoundingClientRect();
        if (mb.right>hb.right) menuEl.style.left=Math.max(0,hb.width-mb.width)+'px';
        if (mb.bottom>hb.bottom) menuEl.style.top=Math.max(0,hb.height-mb.height)+'px';
    }
    async function sendAction(item) {
        const started = performance.now();
        let result;
        try {
            // Never against a lease that is being reopened: the server would refuse it anyway.
            if (!c.link?.session) throw Error('the view is reconnecting');
            const res = await fetch('/api/stream/action', { method: 'POST', headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ session: c.link.session, epoch: c.link.epoch, seq: state.seq, action: item.action }), signal: c.controller.signal });
            const body = await res.json().catch(() => ({}));
            result = res.ok ? `sent: ${item.label}` : `refused: ${body?.error || res.status}`;
        } catch (e) { result = `failed: ${e.message || e}`; }
        inputLog.push({ at: Date.now(), label: item.label, result, ms: Math.round(performance.now() - started) });
        if (inputLog.length > 50) inputLog.shift();
        c.counters.inputActions++;
    }

    const interfaceMenuAt = async (gx, gy) => (state.scene?.ui && typeof c.adapter?.interfaceMenu === 'function')
        ? interfaceItems((await c.adapterCall(() => c.adapter.interfaceMenu(uiSnapshot(), { x: gx, y: gy, tab: c.uiTab, use: useItem })))?.entries, useItem).map(i => ({ label: i.label, unavailable: !!i.unavailable, local: !!i.local }))
        : [];

    return {
        get inputOn() { return inputOn; },
        get useItem() { return useItem; },
        get typed() { return typed; },
        get inputLog() { return inputLog; },
        onClick, onInterfaceClick, interfaceWheel, onKey, uiSnapshot, worldMenu, interfaceMenuAt,
        // setInput records whether the server acknowledged control; losing it drops everything typed or started.
        setInput(on) { inputOn = !!on; if (!inputOn) reset(); },
        // reset drops the open menu, a started Use and the typed line (input lost, or the view reopened).
        reset,
        destroy() { inputOn = false; menuEl?.remove(); menuEl = null; toastEl?.remove(); toastEl = null; clearTimeout(toastTimer); },
    };
    function reset() { menuEl?.remove(); menuEl = null; useItem = null; typed = ''; c.markUi(); }
}
