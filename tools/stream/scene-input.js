// Opt-in input for the stream client: turning a click on the GPU viewport into the game's own
// kind of option list (npc options, loc options, ground item options, "Walk here"). Pure
// picking and menu building; sending is the provider's job and the server validates every
// action again (internal/controlpanel/streaminput.go). Nothing here runs unless the viewer
// took input control, which the server only grants on a -stream-input build.

// pickEntity returns the nearest placed entity whose projected base-to-top segment passes
// within `slop` CSS pixels of (px, py). placed: [{e, cx, cz, y, top}].
export function pickEntity(project, placed, px, py, slop = 12) {
    return pickEntities(project,placed,px,py,slop)[0] || null;
}

// Preserve overlapping actors in the context menu, including players sharing an NPC tile.
export function pickEntities(project, placed, px, py, slop = 12) {
    const hits = [];
    for (const p of placed) {
        const a = project(p.cx, p.y, p.cz), b = project(p.cx, p.y + (p.top || 1.5), p.cz);
        if (!a || !b) continue;
        const dx = b[0] - a[0], dy = b[1] - a[1], len2 = dx * dx + dy * dy || 1;
        const t = Math.max(0, Math.min(1, ((px - a[0]) * dx + (py - a[1]) * dy) / len2));
        const qx = a[0] + t * dx - px, qy = a[1] + t * dy - py;
        const d = Math.hypot(qx, qy);
        if (d <= slop) hits.push({ ...p, depth: a[2], pickDistance:d });
    }
    return hits.sort((a,b)=>a.depth-b.depth || a.pickDistance-b.pickDistance);
}

// pickTile returns the tile whose projected ground quad contains (px, py), nearest the
// camera, searching `radius` tiles around (cx, cz). heightAt(x, z) is the ground height.
export function pickTile(project, heightAt, cx, cz, px, py, radius = 16) {
    let best = null;
    const inTri = (p, a, b, c) => {
        const s = (u, v, w) => (u[0] - w[0]) * (v[1] - w[1]) - (v[0] - w[0]) * (u[1] - w[1]);
        const d1 = s(p, a, b), d2 = s(p, b, c), d3 = s(p, c, a);
        return !((d1 < 0 || d2 < 0 || d3 < 0) && (d1 > 0 || d2 > 0 || d3 > 0));
    };
    const pt = [px, py];
    for (let x = cx - radius; x <= cx + radius; x++) for (let z = cz - radius; z <= cz + radius; z++) {
        const c = [[x, z], [x + 1, z], [x + 1, z + 1], [x, z + 1]].map(([u, v]) => project(u, heightAt(u, v) ?? 0, v));
        if (c.some(q => !q)) continue;
        if (inTri(pt, c[0], c[1], c[2]) || inTri(pt, c[0], c[2], c[3])) {
            const depth = (c[0][2] + c[2][2]) / 2;
            if (!best || depth < best.depth) best = { x, z, depth };
        }
    }
    return best;
}

// buildMenu turns what is under the cursor into the game's option order: the entity's
// options, then the tile's loc options, then ground items, then "Walk here". Each option
// carries the action the server is asked to perform. info comes from the adapter's
// menuInfo (cache names and options).
export function buildMenu({ entity, entities, tile, objs }, info, use = null) {
    if (use) return buildUseMenu({ entity, entities, objs }, info, use);
    const menu = [], examine = [], secondary = [];
    const targets = entities || (entity ? [entity] : []);
    for (const entity of targets) {
    if (entity && entity.e.k[0] === 'p') {
        const name = entity.e.n || 'player', index = +entity.e.k.slice(1);
        (info.playerOps || []).forEach((op, i) => {
            if (!op || op.toLowerCase() === 'hidden') return;
            const item = {display:`${op} @whi@${name}`,label:`${op} ${name}`,action:{kind:'player',index,op:i+1}};
            // Attack and server-prioritized options require an explicit menu choice.
            (info.playerOpPriority?.[i] || op.toLowerCase() === 'attack' ? secondary : menu).push(item);
        });
    }
    if (entity && entity.e.k[0] === 'n') {
        const t = info.npcs?.[entity.e.t];
        if (t) examine.push({label:'Examine '+t.name,display:'Examine @yel@'+t.name,local:{description:t.desc || `It's a ${t.name}.`}});
        (t?.ops || []).forEach((op, i) => { if (op && op !== 'hidden') menu.push({ display:`${op} @yel@${t.name}`, label: `${op} ${t.name}`, action: { kind: 'npc', index: +entity.e.k.slice(1), op: i + 1 } }); });
    }
    }
    for (const loc of info.locs || []) {
        examine.push({label:"Examine "+loc.name,display:"Examine @cya@"+loc.name,local:{description:loc.desc || `It's a ${loc.name}.`}});
        loc.ops.forEach((op, i) => { if (op && op !== 'hidden') menu.push({ display:`${op} @cya@${loc.name}`, label: `${op} ${loc.name}`, action: { kind: 'loc', x: loc.x, z: loc.z, type: loc.type, op: i + 1 } }); });
    }
    for (const [x, z, id] of objs || []) {
        const o = info.objs?.[id];
        if (!o) continue;
        examine.push({label:"Examine "+o.name,display:"Examine @lre@"+o.name,local:{description:o.desc || `It's a ${o.name}.`}});
        const ops = o.ops.slice();
        // The game offers Take (option 3) for any ground item whose own third option is unset.
        if (!ops[2]) ops[2] = 'Take';
        ops.forEach((op, i) => { if (op) menu.push({ display:`${op} @lre@${o.name}`, label: `${op} ${o.name}`, action: { kind: 'obj', x, z, id, op: i + 1 } }); });
    }
    if (tile) menu.push({ label: 'Walk here', action: { kind: 'walk', x: tile.x, z: tile.z } });
    return [...menu,...secondary,...examine];
}

// buildUseMenu is the world menu while an item is being used (the game's useMode): only
// "Use <item> with <target>" for the npc, locs and ground items under the cursor, and no
// "Walk here" (Client.ts addWorldOptions offers it only outside use mode). `use` is
// {obj, slot, com, name}; the server checks the used item again.
export function buildUseMenu({ entity, entities, objs }, info, use) {
    const menu = [], u = { obj: use.obj, slot: use.slot, com: use.com };
    const targets = entities || (entity ? [entity] : []);
    for (const entity of targets) {
    if (entity && entity.e.k[0] === 'p') menu.push({label:`Use ${use.name} with ${entity.e.n || 'player'}`,action:{kind:'playeru',index:+entity.e.k.slice(1),use:u}});
    if (entity && entity.e.k[0] === 'n') {
        const t = info.npcs?.[entity.e.t];
        if (t) menu.push({ label: `Use ${use.name} with ${t.name}`, action: { kind: 'npcu', index: +entity.e.k.slice(1), use: u } });
    }
    }
    for (const loc of info.locs || []) menu.push({ label: `Use ${use.name} with ${loc.name}`, action: { kind: 'locu', x: loc.x, z: loc.z, type: loc.type, use: u } });
    for (const [x, z, id] of objs || []) {
        const o = info.objs?.[id];
        if (o) menu.push({ label: `Use ${use.name} with ${o.name}`, action: { kind: 'obju', x, z, id, use: u } });
    }
    return menu;
}

// The game's MiniMenuAction codes an interface option may carry (MiniMenuAction.ts), and
// which the stream view can act on. Anything else is shown greyed.
export const MENU = { OP_HELD1: 694, OP_HELD2: 962, OP_HELD3: 795, OP_HELD4: 681, OP_HELD5: 100, OP_HELD6: 1328,
    INV_BUTTON1: 582, INV_BUTTON2: 113, INV_BUTTON3: 555, INV_BUTTON4: 331, INV_BUTTON5: 354,
    IF_BUTTON: 231, TOGGLE_BUTTON: 435, SELECT_BUTTON: 225, PAUSE_BUTTON: 997, CLOSE_BUTTON: 737,
    USEHELD_START: 102, USEHELD_ONHELD: 398 };
const SENT = new Set([MENU.OP_HELD1, MENU.OP_HELD2, MENU.OP_HELD3, MENU.OP_HELD4, MENU.OP_HELD5, MENU.INV_BUTTON1, MENU.INV_BUTTON2,
    MENU.INV_BUTTON3, MENU.INV_BUTTON4, MENU.INV_BUTTON5, MENU.IF_BUTTON, MENU.TOGGLE_BUTTON, MENU.SELECT_BUTTON, MENU.PAUSE_BUTTON,
    MENU.CLOSE_BUTTON, MENU.USEHELD_ONHELD]);

// interfaceItems turns the adapter's interfaceMenu entries (the reference client's own
// option list, top first) into menu items: `action` for what the server is asked to send,
// `local` for what the game client does by itself (start a Use, examine), neither for an
// option this view does not offer (spells, friends, a client-side button such as Logout).
export function interfaceItems(entries, use) {
    const u = use ? { obj: use.obj, slot: use.slot, com: use.com } : undefined;
    return (entries || []).map(e => {
        if (e.action === MENU.USEHELD_START) return { display:e.display, label: e.label, local: { use: { obj: e.a, slot: e.b, com: e.c, name: e.label.replace(/^Use /, '') } } };
        if (e.action === MENU.OP_HELD6) return { display:e.display, label: e.label, local: { examine: e.a } };
        if (!SENT.has(e.action) || (e.action === MENU.IF_BUTTON && e.clientCode)) return { display:e.display, label: e.label, unavailable: true };
        const action = { kind: 'if', menu: e.action, id: e.a, slot: e.b, com: e.c };
        if (e.action === MENU.USEHELD_ONHELD) action.use = u;
        return { display:e.display, label: e.label, action };
    });
}

// chatKey applies one key to the text being typed, the way the game client's chat input
// does (Client.ts): printable characters 32-122 up to 80 (digits only, up to 10, while the
// count dialog asks for an amount), Backspace deletes, Enter submits, Escape clears.
// Returns { text, submit } or null when the key is not the chat's.
export function chatKey(text, key, countDialog) {
    if (key === 'Enter') return { text: '', submit: text };
    if (key === 'Escape') return { text: '', submit: null };
    if (key === 'Backspace') return { text: text.slice(0, -1), submit: null };
    if (key.length !== 1) return null;
    const c = key.charCodeAt(0);
    if (countDialog) return c >= 48 && c <= 57 && text.length < 10 ? { text: text + key, submit: null } : { text, submit: null };
    if (c >= 32 && c <= 122 && text.length < 80) return { text: text + key, submit: null };
    return { text, submit: null };
}
