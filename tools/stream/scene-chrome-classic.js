// The RuneScape Classic chrome: Classic's own decoded interface sprites around the existing
// 2004 world viewport.
//
// WHAT THIS IS, EXACTLY, because the distinction decides whether anything here is honest:
//
//   * Every pixel of chrome drawn here is RuneScape Classic's own art, decoded from the
//     operator's mudclient204 cache by internal/eraassets and served, as PNGs this process
//     produced, from /stream/era/classic/. Nothing is redrawn, recoloured or imitated, and
//     no Classic asset is in this repository.
//   * The WORLD is still 2004. Classic's map data is a different world model (different tile
//     grid, different projection, 2D sprite entities) and the account's real position is a
//     2004 coordinate, so there is no Classic scene to draw for it. The view says so on
//     screen rather than letting the chrome imply otherwise.
//   * The LAYOUT is this file's design, not a port. mudclient's own drawUi() is not in this
//     checkout, so where each sprite sits is a reconstruction from the sprites' own
//     geometry — the tab strip's six 32x32 buttons and the panel headers' widths and
//     offsets are measured from the decoded frames, not invented, and each is named below.
//     Where a sprite's role in mudclient could not be established from the cache alone, the
//     comment at its use says so.
//   * Anything Classic has no equivalent for falls back to 2004, which is the owner's own
//     rule for this skin and what the manifest's `fallback` field declares: an open bank,
//     shop, trade or dialogue is composited from the reference client's own drawing of it.
//
// The contract is the same as scene-chrome.js's createGameFrame, so the provider feeds this
// exactly what it feeds the 2004 chrome.

import { itemIconWrap } from '/itemicons.js';
import { historicalItemIcons } from '/itemiconmanifest.js';
import { formatCount } from './scene-chrome.js';

// The Classic game window. mudclient204 draws a 512x346 window with the world filling it
// and the interface panels overlaid on top — unlike 2004, which reserves a sidebar.
export const CLASSIC_W = 512, CLASSIC_H = 346;

// The tab strip is the `inv1` sprite: 197x32 holding six 32x32 buttons at x 0, 33, 66, 99,
// 132 and 165 (measured from the decoded frame's own opaque columns, which have a 1px gap
// between each pair). Left to right the icons are options, friends, spells, stats, map and
// inventory, which is Classic's own order.
const TAB_BUTTON = 32, TAB_STEP = 33, TAB_COUNT = 6;
export const CLASSIC_TABS = [
    { key: 'options', header: 5, label: 'OPTIONS' },
    { key: 'friends', header: 4, label: 'FRIENDS' },
    { key: 'spells', header: 3, label: 'SPELLS' },
    { key: 'stats', header: 2, label: 'STATS' },
    { key: 'map', header: 1, label: 'MAP' },
    { key: 'inventory', header: 0, label: 'INVENTORY' },
];
// The 2004 side-tab index each Classic tab falls back to, for the panels this file does not
// draw itself. 3 is the backpack, 1 the skills tab, 6 magic, 8 friends, 11 settings.
const FALLBACK_TAB = { options: 11, friends: 8, spells: 6, stats: 1, map: 3, inventory: 3 };

// The stat order the session reports, copied from internal/gamestate.SkillNames so the
// STATS panel names a level rather than numbering it. Two slots are empty in content 289.
const SKILL_NAMES = ['Attack', 'Defence', 'Strength', 'Hitpoints', 'Ranged', 'Prayer', 'Magic',
    'Cooking', 'Woodcutting', 'Fletching', 'Fishing', 'Firemaking', 'Crafting',
    'Smithing', 'Mining', 'Herblore', 'Agility', 'Thieving', '', '', 'Runecraft'];

// Panel geometry, all taken from the decoded header frames rather than chosen: the `inv2`
// sheet's plane is 245x33, so a panel is 245 wide, and each header frame carries its own
// offset within that plane (INVENTORY at +0, MAP at +89, the rest at +48).
const PANEL_W = 245, HEADER_H = 33;
const STRIP_W = TAB_STEP * TAB_COUNT - 1; // 197, the inv1 sheet's own width
const STRIP_X = CLASSIC_W - STRIP_W - 3, STRIP_Y = 3;
const PANEL_X = CLASSIC_W - PANEL_W - 3, PANEL_Y = STRIP_Y + TAB_BUTTON + 2;
const PANEL_H = 204; // 6 rows of the 34px inventory slot below the header
// Classic's panels are a flat near-black field with a thin border; these are the colours a
// Classic screenshot shows, and they are the only two values here that are not from a file.
const PANEL_FILL = 'rgba(0,0,0,0.78)', PANEL_EDGE = '#3c5aa8';

// The 2004 reference window's own regions, for the fallback composites: the viewport where
// a main modal is drawn, the side panel, and the chatbox where a dialogue is drawn. These
// match scene-chrome.js/verify-stream-interface.cjs's AREAS.
const REF_MAIN = { x: 4, y: 4, w: 512, h: 334 };
const REF_SIDE = { x: 548, y: 168, w: 218, h: 335 };
const REF_CHAT = { x: 7, y: 345, w: 506, h: 130 };
// The reference window itself, for the whole-window fallback.
const REF_W = 765, REF_H = 503;

function div(style, role) { const d = document.createElement('div'); d.style.cssText = style; if (role) d.setAttribute('role', role); return d; }

// loadEraSprites fetches the manifest this process decoded and every frame in it. A skin
// whose art will not load must fail loudly: the caller shows the error rather than drawing
// a Classic-labelled 2004 view.
export async function loadEraSprites(era = 'classic', signal) {
    const res = await fetch(`/stream/era/${era}/manifest.json`, { signal });
    if (!res.ok) throw Error(`${era} chrome unavailable: ${(await res.text()).trim().slice(0, 200)}`);
    const manifest = await res.json();
    const byName = new Map();
    await Promise.all((manifest.sprites || []).map(async sp => {
        const img = new Image();
        img.src = sp.url;
        await img.decode();
        if (!byName.has(sp.name)) byName.set(sp.name, []);
        byName.get(sp.name)[sp.frame] = Object.assign({ img }, sp);
    }));
    if (!byName.has('inv1') || !byName.has('inv2')) throw Error(`${era} chrome is missing its panel sprites`);
    return { manifest, byName, get: (name, frame = 0) => byName.get(name)?.[frame] || null };
}

// createClassicFrame lays out the Classic window: the world underneath, Classic's chrome on
// a pixel-exact 512x346 canvas over it, and a DOM inventory grid (2004 item icons — the
// declared fallback, because the Classic cache's item sheets carry no names this build can
// map a 2004 item onto; see docs/stream_client_panel.md).
export function createClassicFrame(host, sprites, onTab) {
    const root = div('position:absolute;inset:0;background:#000;overflow:hidden');
    const scene = div('position:absolute;inset:0;overflow:hidden;background:#000');
    const fill = 'position:absolute;left:0;top:0;width:100%;height:100%;display:block';
    const glCanvas = document.createElement('canvas');
    glCanvas.style.cssText = fill + ';touch-action:none';
    glCanvas.setAttribute('aria-label', 'GPU scene: decoded 2004 cache terrain, scenery and models');
    const labels = document.createElement('canvas'); labels.style.cssText = fill + ';pointer-events:none';
    const frame = document.createElement('canvas');
    frame.width = CLASSIC_W; frame.height = CLASSIC_H;
    frame.style.cssText = 'position:absolute;inset:0;width:100%;height:100%;image-rendering:pixelated;pointer-events:none';
    frame.setAttribute('aria-label', 'RuneScape Classic interface sprites drawn from the operator’s mudclient204 cache');
    const overlay = div('position:absolute;left:0;right:0;bottom:0;padding:3px 5px;background:rgba(0,0,0,.55);color:#ff0;font:10px monospace;white-space:pre-wrap;pointer-events:none;opacity:0;transition:opacity .2s', 'status');
    // The inventory grid is DOM so it can reuse the dashboard's own 2004 item icons.
    const inv = div('position:absolute;display:grid;grid-template-columns:repeat(5,1fr);align-content:start');
    inv.setAttribute('aria-label', 'Backpack');
    scene.append(glCanvas, labels);
    root.append(scene, frame, inv, overlay);
    host.replaceChildren(root);

    const g = frame.getContext('2d');
    g.imageSmoothingEnabled = false;
    let tab = 'inventory', refCanvas = null, invArray = [], slots = [];
    let status = null, chat = [], accountName = '', selfX = 0, selfZ = 0, dots = [];
    let uiSkills = null;
    let mapBase = null, mapOrigin = null;
    const MAP_SCALE = 3;

    const draw = sp => { if (sp) g.drawImage(sp.img, 0, 0); };
    function drawAt(sp, x, y) { if (sp) g.drawImage(sp.img, x, y); }

    function layoutInventory() {
        // 5 columns of the 49x34 slot Classic uses, right under the panel header.
        const x = PANEL_X, y = PANEL_Y + HEADER_H, w = PANEL_W, h = PANEL_H;
        Object.assign(inv.style, {
            left: (x / CLASSIC_W * 100) + '%', top: (y / CLASSIC_H * 100) + '%',
            width: (w / CLASSIC_W * 100) + '%', height: (h / CLASSIC_H * 100) + '%',
            display: tab === 'inventory' && !mainOpen ? 'grid' : 'none',
        });
    }

    function slotAt(i) {
        while (slots.length <= i) {
            const s = div('position:relative;display:flex;align-items:center;justify-content:center;min-width:0;overflow:hidden;box-shadow:inset 0 0 0 1px rgba(60,90,168,.35)');
            s.dataset.slot = String(slots.length);
            inv.append(s); slots.push({ el: s, id: -1, count: -1 });
        }
        return slots[i];
    }

    // paintChrome redraws the whole 512x346 chrome layer. It is cheap (a handful of blits
    // and some text) and runs only when the provider says something changed.
    function paintChrome() {
        g.clearRect(0, 0, CLASSIC_W, CLASSIC_H);
        // 1. A MAIN modal — a bank, shop or trade — has no Classic equivalent at all, and
        //    it owns the side panel too (a shop's stock is the main interface, its
        //    inventory the side one). So the whole reference window is shown scaled to the
        //    Classic window's width, and Classic's own tab strip and panel stand down for
        //    it rather than being drawn over the top of it. That is what this skin's
        //    declared fallback means, said plainly on screen.
        if (refCanvas && mainOpen) {
            const k = CLASSIC_W / REF_W;
            g.drawImage(refCanvas, 0, 0, REF_W, REF_H, 0, 0, CLASSIC_W, Math.round(REF_H * k));
            text('2004 interface (no Classic equivalent)', 5, CLASSIC_H - 20, '#c8c8c8', '9px Helvetica,Arial,sans-serif');
            const footer = sprites.get('hbar2');
            if (footer) g.drawImage(footer.img, 0, CLASSIC_H - footer.height);
            text(status ? statusLine() : accountName, 4, CLASSIC_H - 4, '#ffffff', 'bold 11px Helvetica,Arial,sans-serif');
            return;
        }
        if (refCanvas && chatFallback) {
            // Scaled to the bottom fifth of the Classic window: the reference chatbox is
            // 506x130 of a 765x503 window, which would otherwise cover a third of this one.
            const scale = 0.62;
            drawRegion(REF_CHAT, 3, CLASSIC_H - Math.round(REF_CHAT.h * scale) - 18, scale);
        }
        // 2. Classic's tab strip and the open panel.
        const strip = sprites.get('inv1');
        drawAt(strip, STRIP_X, STRIP_Y);
        const active = CLASSIC_TABS.findIndex(t => t.key === tab);
        if (active >= 0) {
            g.strokeStyle = '#ffff00'; g.lineWidth = 1;
            g.strokeRect(STRIP_X + active * TAB_STEP + 0.5, STRIP_Y + 0.5, TAB_BUTTON - 1, TAB_BUTTON - 1);
            g.fillStyle = PANEL_FILL;
            g.fillRect(PANEL_X, PANEL_Y + HEADER_H, PANEL_W, PANEL_H);
            g.strokeStyle = PANEL_EDGE;
            g.strokeRect(PANEL_X + 0.5, PANEL_Y + HEADER_H + 0.5, PANEL_W - 1, PANEL_H - 1);
            const header = sprites.get('inv2', CLASSIC_TABS[active].header);
            if (header) g.drawImage(header.img, PANEL_X + header.offsetX, PANEL_Y + header.offsetY);
            panelBody(tab);
        }
        // 3. The compass, at the top-left of the world the way Classic puts it.
        drawAt(sprites.get('compass'), 6, 6);
        // 4. The bottom strip. hbar2 is a 512x15 sheet whose role in mudclient could not be
        //    established from the cache alone; it is used here as the window's footer,
        //    which is a choice of this file, not a reconstruction.
        const bar = sprites.get('hbar2');
        if (bar) g.drawImage(bar.img, 0, CLASSIC_H - bar.height);
        text(status ? statusLine() : accountName, 4, CLASSIC_H - 4, '#ffffff', 'bold 11px Helvetica,Arial,sans-serif');
        // 5. Chat, over the world at the bottom left, as Classic draws it.
        if (!refCanvas || !hasChatFallback()) {
            let y = CLASSIC_H - 32;
            for (const line of chat.slice(-5).reverse()) {
                text(line.user ? `${line.user}: ${line.text}` : line.text, 5, y, line.user ? '#ffff00' : '#ffffff');
                y -= 13;
            }
        }
        // Said on screen, not only in the manifest: the chrome is Classic's, the world is
        // not, and a viewer must never have to work that out. Placed above the footer so it
        // does not collide with the provider's own status HUD at the top left.
        text('world: 2004 cache · chrome: RuneScape Classic', 5, CLASSIC_H - 20, '#c8c8c8', '9px Helvetica,Arial,sans-serif');
    }

    function statusLine() {
        const parts = [accountName || status?.name];
        if (status?.hpKnown) parts.push(`HP ${status.hp}/${status.maxHp}`);
        if (typeof status?.run === 'number') parts.push(`run ${status.run}%`);
        return parts.filter(Boolean).join('  ·  ');
    }

    // chatFallback / mainOpen come from the session's OWN interface state, not from
    // guessing at the reference canvas: a chat modal or count dialog is what puts something
    // in the reference chatbox worth showing, and a main modal is what puts a bank, shop or
    // trade in its viewport.
    function hasChatFallback() { return chatFallback; }
    let chatFallback = false, mainOpen = false;

    function drawRegion(r, dx, dy, scale = 1) {
        if (!refCanvas) return;
        g.drawImage(refCanvas, r.x, r.y, r.w, r.h, dx, dy, Math.round(r.w * scale), Math.round(r.h * scale));
    }

    function text(s, x, y, colour = '#ffffff', font = '11px Helvetica,Arial,sans-serif') {
        if (!s) return;
        g.font = font; g.textBaseline = 'alphabetic';
        g.fillStyle = '#000'; g.fillText(s, x + 1, y + 1);
        g.fillStyle = colour; g.fillText(s, x, y);
    }

    function panelBody(which) {
        const x = PANEL_X + 6, top = PANEL_Y + HEADER_H + 14;
        if (which === 'stats') {
            const rows = (uiSkills || []).map((s, i) => [SKILL_NAMES[i] || '', s])
                .filter(([name, s]) => name && s && s.Generation);
            if (!rows.length) { text('no skills observed yet', x, top, '#c8c8c8'); return; }
            rows.forEach(([name, s], i) => {
                const col = i % 2, row = (i / 2) | 0;
                if (row > 11) return;
                text(`${name} ${s.Level}`, x + col * 116, top + row * 16, '#ffff00');
            });
            return;
        }
        if (which === 'map') {
            const size = Math.min(PANEL_W - 12, PANEL_H - 24);
            const mx = x, my = top - 10;
            g.save();
            g.beginPath(); g.rect(mx, my, size, size); g.clip();
            g.fillStyle = '#000'; g.fillRect(mx, my, size, size);
            const cx = selfX + 0.5, cz = selfZ + 0.5;
            if (mapBase) g.drawImage(mapBase, mx + size / 2 - ((cx - mapOrigin.minX) * MAP_SCALE + 1), my + size / 2 - ((mapOrigin.maxZ - cz) * MAP_SCALE + 1));
            for (const d of dots) {
                const px = mx + size / 2 + (d.x + 0.5 - cx) * MAP_SCALE, py = my + size / 2 - (d.z + 0.5 - cz) * MAP_SCALE;
                if (px < mx || py < my || px > mx + size || py > my + size) continue;
                g.fillStyle = d.npc ? '#ffff00' : '#ffffff';
                g.fillRect(px - 1, py - 1, 2, 2);
            }
            g.fillStyle = '#ffffff'; g.fillRect(mx + size / 2 - 1.5, my + size / 2 - 1.5, 3, 3);
            g.restore();
            return;
        }
        if (which === 'inventory') return; // the DOM grid draws it
        // options / friends / spells: the 2004 side panel, composited as the fallback and
        // scaled to fit inside Classic's panel — the reference sidebar is 218x335, taller
        // than a Classic panel, and letting it spill would draw 2004 chrome over the world.
        if (!refCanvas) { text('waiting for the 2004 panel', x, top, '#c8c8c8'); return; }
        const body = { x: PANEL_X + 1, y: PANEL_Y + HEADER_H + 1, w: PANEL_W - 2, h: PANEL_H - 16 };
        const k = Math.min(body.w / REF_SIDE.w, body.h / REF_SIDE.h);
        const dw = Math.round(REF_SIDE.w * k), dh = Math.round(REF_SIDE.h * k);
        g.save();
        g.beginPath(); g.rect(body.x, body.y, body.w, body.h); g.clip();
        g.drawImage(refCanvas, REF_SIDE.x, REF_SIDE.y, REF_SIDE.w, REF_SIDE.h,
            body.x + ((body.w - dw) / 2 | 0), body.y, dw, dh);
        g.restore();
        text('2004 panel (no Classic equivalent)', x, PANEL_Y + HEADER_H + PANEL_H - 5, '#c8c8c8', '9px Helvetica,Arial,sans-serif');
    }

    function hit(e) {
        const r = root.getBoundingClientRect();
        const x = (e.clientX - r.left) / r.width * CLASSIC_W, y = (e.clientY - r.top) / r.height * CLASSIC_H;
        if (y < STRIP_Y || y > STRIP_Y + TAB_BUTTON) return null;
        const i = Math.floor((x - STRIP_X) / TAB_STEP);
        if (i < 0 || i >= TAB_COUNT || x > STRIP_X + i * TAB_STEP + TAB_BUTTON) return null;
        return CLASSIC_TABS[i];
    }
    function click(e) {
        const t = hit(e);
        if (!t) return;
        tab = t.key; layoutInventory(); paintChrome();
        onTab?.(FALLBACK_TAB[t.key], t.key);
    }
    root.addEventListener('click', click);
    const show = () => { overlay.style.opacity = '.85'; }, hide = () => { overlay.style.opacity = '0'; };
    root.addEventListener('pointerenter', show); root.addEventListener('pointerleave', hide);
    layoutInventory(); paintChrome();

    return {
        glCanvas, labels, overlay, root, gameFrame: true, classic: true,
        aspect: { w: CLASSIC_W, h: CLASSIC_H },
        activeTab: () => tab,
        setInterface(canvas) { refCanvas = canvas; paintChrome(); },
        setInventory(array, change) {
            invArray = array;
            const which = change.all ? array.map((_, i) => i) : change.slots;
            if (change.all) while (slots.length > array.length) slots.pop().el.remove();
            for (const i of which) {
                if (i >= array.length) continue;
                const [id, count] = array[i] || [0, 0];
                const s = slotAt(i);
                if (s.id === id && s.count === count) continue;
                s.id = id; s.count = count; s.el.replaceChildren();
                if (count > 0) {
                    const name = historicalItemIcons[id] || '';
                    const icon = itemIconWrap(id, name);
                    icon.title = count > 1 ? `${name || 'item #' + id} x${count}` : (name || `item #${id}`);
                    s.el.append(icon);
                    if (count > 1) {
                        const c = div('position:absolute;left:1px;top:0;font:bold 10px Helvetica,Arial,sans-serif;color:#ffff00;text-shadow:1px 1px #000;pointer-events:none');
                        c.textContent = formatCount(count); s.el.append(c);
                    }
                }
            }
        },
        // setUi takes the session's own interface state, for the parts Classic draws
        // itself rather than compositing: the stat levels behind the STATS panel.
        setUi(ui) {
            uiSkills = ui?.skills || null;
            mainOpen = (ui?.main ?? -1) !== -1;
            chatFallback = (ui?.chatModal ?? -1) !== -1 || !!ui?.countDialog;
            layoutInventory(); paintChrome();
        },
        setStatusAndChat(st, lines, name) { status = st; chat = lines || []; accountName = name || ''; paintChrome(); },
        setMinimapTerrain(vertices, F = 7, averages = null) {
            const tint = (i, c) => {
                if (F < 10 || !averages || vertices[i + 9] < 0) return vertices[i + 3 + c];
                const rgb = averages[vertices[i + 9] | 0] | 0;
                return vertices[i + 3 + c] * (((rgb >> (16 - 8 * c)) & 255) / 255);
            };
            let minX = Infinity, minZ = Infinity, maxX = -Infinity, maxZ = -Infinity;
            for (let i = 0; i < vertices.length; i += F) {
                minX = Math.min(minX, vertices[i]); maxX = Math.max(maxX, vertices[i]);
                minZ = Math.min(minZ, vertices[i + 2]); maxZ = Math.max(maxZ, vertices[i + 2]);
            }
            if (!Number.isFinite(minX)) { mapBase = null; return; }
            const c = document.createElement('canvas');
            c.width = Math.ceil((maxX - minX) * MAP_SCALE) + 2; c.height = Math.ceil((maxZ - minZ) * MAP_SCALE) + 2;
            const mg = c.getContext('2d');
            for (let i = 0; i + 3 * F - 1 < vertices.length; i += 3 * F) {
                const avg = ch => (tint(i, ch) + tint(i + F, ch) + tint(i + 2 * F, ch)) / 3;
                mg.fillStyle = mg.strokeStyle = `rgb(${avg(0) * 255 | 0},${avg(1) * 255 | 0},${avg(2) * 255 | 0})`;
                mg.lineWidth = 0.6; mg.beginPath();
                for (let v = 0; v < 3; v++) {
                    const x = (vertices[i + v * F] - minX) * MAP_SCALE + 1, y = (maxZ - vertices[i + v * F + 2]) * MAP_SCALE + 1;
                    if (v) mg.lineTo(x, y); else mg.moveTo(x, y);
                }
                mg.closePath(); mg.fill(); mg.stroke();
            }
            mapBase = c; mapOrigin = { minX, maxZ };
        },
        drawMinimap(x, z, d) { selfX = x; selfZ = z; dots = d || []; if (tab === 'map') paintChrome(); },
        repaint: paintChrome,
        destroy() {
            root.removeEventListener('click', click);
            root.removeEventListener('pointerenter', show); root.removeEventListener('pointerleave', hide);
            host.replaceChildren(); slots.length = 0; mapBase = null; refCanvas = null;
        },
    };
}
