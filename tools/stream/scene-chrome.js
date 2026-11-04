// DOM chrome around the stream client's GPU scene, laid out like the game window's regions
// (scene top-left, minimap and backpack on the right, chat underneath) — built from data the
// existing Go session already has. It sends nothing to the game.
//
// What each part really is:
//   * Backpack: the account's real inventory (interface 3214) from the observation, drawn with
//     the dashboard's own item icons (itemicons.js: icons rendered from the historical cache,
//     identity-checked by name, falling back to a category glyph).
//   * Minimap: a north-up top-down raster of the SAME decoded cache terrain the GPU scene
//     draws (its vertex colours), with yellow npc dots and white player dots. Textured tiles
//     (water, most paths) are omitted by the terrain export, so they show as dark gaps. The
//     game's own minimap also draws walls, map icons and rotates with the camera; this does not.
//   * Status/chat: the dashboard's status line for the account and its last few chat lines.
// None of this is the game's own interface rendering.

import { itemIconWrap } from '/itemicons.js';
import { historicalItemIcons } from '/itemiconmanifest.js';

const CSS = {
    root: 'position:absolute;inset:0;display:grid;grid-template-columns:67% 33%;grid-template-rows:66.4% 33.6%;background:#2b2419;font:12px Arial,sans-serif;color:#ffff00;overflow:hidden',
    scene: 'position:relative;grid-column:1;grid-row:1;overflow:hidden;background:#000;border:2px solid #4a3d2a',
    fill: 'position:absolute;left:0;top:0;width:100%;height:100%;display:block',
    overlay: 'position:absolute;left:0;right:0;top:0;padding:3px 5px;background:rgba(0,0,0,.55);color:#ffff00;font:11px monospace;white-space:pre-wrap;pointer-events:none',
    side: 'grid-column:2;grid-row:1 / span 2;display:flex;flex-direction:column;align-items:center;gap:6px;padding:6px 4px;border-left:2px solid #4a3d2a;background:#3e3529;min-width:0',
    minimap: 'width:min(150px,80%);aspect-ratio:1;border-radius:50%;background:#000;box-shadow:0 0 0 3px #5a4a33',
    inv: 'display:grid;grid-template-columns:repeat(4,1fr);gap:2px;width:92%;flex:1;min-height:0;align-content:start',
    slot: 'position:relative;aspect-ratio:1;display:flex;align-items:center;justify-content:center;min-width:0;max-height:40px',
    count: 'position:absolute;left:1px;top:0;font:bold 10px Arial,sans-serif;color:#ffff00;text-shadow:1px 1px #000;pointer-events:none',
    chat: 'grid-column:1;grid-row:2;display:flex;flex-direction:column;justify-content:flex-start;padding:4px 8px;background:#d8c9a0;color:#000;border:2px solid #4a3d2a;overflow:hidden;min-width:0',
    status: 'color:#6b2a00;font-weight:bold;white-space:nowrap;overflow:hidden;text-overflow:ellipsis',
    line: 'white-space:nowrap;overflow:hidden;text-overflow:ellipsis',
    note: 'color:#b8a67a;font:10px Arial,sans-serif;text-align:center'
};

function div(style, role) { const d = document.createElement('div'); d.style.cssText = style; if (role) d.setAttribute('role', role); return d; }

export function createChrome(host) {
    const root = div(CSS.root);
    const scene = div(CSS.scene);
    const glCanvas = document.createElement('canvas'); glCanvas.style.cssText = CSS.fill + ';touch-action:none';
    glCanvas.setAttribute('aria-label', 'GPU scene: decoded cache terrain with entity markers');
    const labels = document.createElement('canvas'); labels.style.cssText = CSS.fill + ';pointer-events:none';
    const overlay = div(CSS.overlay, 'status');
    scene.append(glCanvas, labels, overlay);
    const side = div(CSS.side);
    const minimap = document.createElement('canvas'); minimap.style.cssText = CSS.minimap; minimap.width = minimap.height = 152;
    minimap.setAttribute('aria-label', 'Minimap from decoded terrain colours; yellow npcs, white players');
    const mapNote = div(CSS.note); mapNote.textContent = 'terrain minimap · north up';
    const inv = div(CSS.inv); inv.setAttribute('aria-label', 'Backpack');
    const slots = [];
    const sideNote = div(CSS.note); sideNote.textContent = 'backpack (observed)';
    side.append(minimap, mapNote, inv, sideNote);
    const chat = div(CSS.chat); chat.setAttribute('aria-label', 'Status and recent chat');
    const status = div(CSS.status);
    const lines = div('display:flex;flex-direction:column');
    chat.append(status, lines);
    root.append(scene, side, chat);
    host.replaceChildren(root);

    let mapBase = null, mapOrigin = null;
    const MAP_SCALE = 4; // CSS px per tile in the 152px minimap, the game's own minimap scale

    function slotAt(i) {
        while (slots.length <= i) {
            const s = div(CSS.slot); s.dataset.slot = String(slots.length); inv.append(s); slots.push({ el: s, id: -1, count: -1 });
        }
        return slots[i];
    }

    return {
        glCanvas, labels, overlay, root,
        // setInventory redraws the given slots (all when `all`) from the scene's inv array.
        setInventory(invArray, change) {
            const which = change.all ? invArray.map((_, i) => i) : change.slots;
            if (change.all) while (slots.length > invArray.length) slots.pop().el.remove();
            for (const i of which) {
                if (i >= invArray.length) continue;
                const [id, count] = invArray[i] || [0, 0];
                const s = slotAt(i);
                if (s.id === id && s.count === count) continue;
                s.id = id; s.count = count;
                s.el.replaceChildren();
                if (count > 0) {
                    const name = historicalItemIcons[id] || '';
                    const icon = itemIconWrap(id, name);
                    icon.title = count > 1 ? `${name || 'item #' + id} x${count}` : (name || `item #${id}`);
                    s.el.append(icon);
                    if (count > 1) { const c = div(CSS.count); c.textContent = formatCount(count); s.el.append(c); }
                }
            }
        },
        setStatusAndChat(st, chatLines, name) {
            const parts = [name, st?.bot, st?.activity];
            if (st?.hpKnown) parts.push(`HP ${st.hp}/${st.maxHp}`);
            if (typeof st?.run === 'number') parts.push(`run ${st.run}%`);
            status.textContent = parts.filter(Boolean).join(' · ') || 'no status';
            lines.replaceChildren(...(chatLines || []).map(l => {
                const d = div(CSS.line);
                d.textContent = l.user ? `${l.user}: ${l.text}` : l.text;
                return d;
            }));
        },
        // setMinimapTerrain rasterizes the terrain mesh (absolute tile space) once per decode.
        // Textured vertices (10-float format, tex >= 0) carry only a brightness; like the game's
        // own minimap, they are coloured by their texture's average (averages: 50 packed RGBs).
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
            const g = c.getContext('2d');
            for (let i = 0; i + 3 * F - 1 < vertices.length; i += 3 * F) {
                const avg = c => (tint(i, c) + tint(i + F, c) + tint(i + 2 * F, c)) / 3;
                const r = avg(0), gr = avg(1), b = avg(2);
                g.fillStyle = `rgb(${r * 255 | 0},${gr * 255 | 0},${b * 255 | 0})`;
                g.strokeStyle = g.fillStyle; g.lineWidth = 0.6;
                g.beginPath();
                for (let v = 0; v < 3; v++) {
                    const x = (vertices[i + v * F] - minX) * MAP_SCALE + 1, y = (maxZ - vertices[i + v * F + 2]) * MAP_SCALE + 1;
                    if (v) g.lineTo(x, y); else g.moveTo(x, y);
                }
                g.closePath(); g.fill(); g.stroke();
            }
            mapBase = c; mapOrigin = { minX, maxZ };
        },
        // drawMinimap: base terrain centred on the account's eased position, then dots.
        drawMinimap(selfX, selfZ, dots) {
            const g = minimap.getContext('2d'), W = minimap.width, H = minimap.height;
            g.save(); g.clearRect(0, 0, W, H);
            g.beginPath(); g.arc(W / 2, H / 2, W / 2, 0, Math.PI * 2); g.clip();
            g.fillStyle = '#000'; g.fillRect(0, 0, W, H);
            const cx = selfX + 0.5, cz = selfZ + 0.5;
            if (mapBase) g.drawImage(mapBase, W / 2 - ((cx - mapOrigin.minX) * MAP_SCALE + 1), H / 2 - ((mapOrigin.maxZ - cz) * MAP_SCALE + 1));
            for (const d of dots) {
                const x = W / 2 + (d.x + 0.5 - cx) * MAP_SCALE, y = H / 2 - (d.z + 0.5 - cz) * MAP_SCALE;
                if (x < -3 || y < -3 || x > W + 3 || y > H + 3) continue;
                g.fillStyle = d.npc ? '#ffff00' : '#ffffff';
                g.fillRect(x - 1.5, y - 1.5, 3, 3);
            }
            g.fillStyle = '#ffffff'; g.fillRect(W / 2 - 2, H / 2 - 2, 4, 4);
            g.restore();
        },
        destroy() { host.replaceChildren(); mapBase = null; slots.length = 0; }
    };
}

// formatCount follows the game's own stack text: plain below 100,000, then K, then M.
export function formatCount(n) {
    if (n >= 10000000) return Math.floor(n / 1000000) + 'M';
    if (n >= 100000) return Math.floor(n / 1000) + 'K';
    return String(n);
}

// The game's own window (765 x 503) around the GPU scene, when the capture adapter can draw
// it (interfaceFrame): the reference client's frame, side tabs, chatbox, rotating minimap and
// open modal, with the GPU scene in the game's own 512 x 334 viewport at (4, 4). Clicking a
// side-tab icon changes which tab this VIEW shows — the game client switches tabs locally
// too, without telling the server, so this sends nothing to the game.
// The side-tab hit boxes exactly as Client.ts tests them (x0, x1, y0, y1 exclusive, tab): the
// top row is tabs 0-6, the bottom row 7-13, in the game's 765 x 503 window coordinates.
export const TAB_BOXES = [
    [539, 573, 169, 205, 0], [569, 599, 168, 205, 1], [597, 627, 168, 205, 2], [625, 669, 168, 203, 3],
    [666, 696, 168, 205, 4], [694, 724, 168, 205, 5], [722, 756, 169, 205, 6],
    [540, 574, 466, 502, 7], [572, 602, 466, 503, 8], [599, 629, 466, 503, 9], [627, 671, 467, 502, 10],
    [669, 699, 466, 503, 11], [696, 726, 466, 503, 12], [724, 758, 466, 502, 13]];
// tabAt: the tab under a game-window point, first match in the client's own order.
export const tabAt = (x, y) => (TAB_BOXES.find(([x0, x1, y0, y1]) => x >= x0 && x <= x1 && y >= y0 && y < y1) || [])[4];
// toGame maps a pointer event to the game's 765 x 503 coordinates through the element that
// shows the whole window, whatever its CSS size (the backing store's devicePixelRatio does
// not enter into it: events and getBoundingClientRect are both in CSS pixels).
export function toGame(root, e) {
    const r = root.getBoundingClientRect();
    return [Math.floor((e.clientX - r.left) / r.width * 765), Math.floor((e.clientY - r.top) / r.height * 503)];
}
export function createGameFrame(host, onTab) {
    const pct = (v, of) => (v / of * 100) + '%';
    const root = div('position:absolute;inset:0;background:#000;overflow:hidden');
    const scene = div(`position:absolute;left:${pct(4, 765)};top:${pct(4, 503)};width:${pct(512, 765)};height:${pct(334, 503)};overflow:hidden;background:#000`);
    const glCanvas = document.createElement('canvas'); glCanvas.style.cssText = CSS.fill + ';touch-action:none';
    glCanvas.setAttribute('aria-label', 'GPU scene: decoded cache terrain, scenery and models');
    const labels = document.createElement('canvas'); labels.style.cssText = CSS.fill + ';pointer-events:none';
    const frame = document.createElement('canvas'); frame.width = 765; frame.height = 503;
    frame.style.cssText = 'position:absolute;inset:0;width:100%;height:100%;image-rendering:pixelated;pointer-events:none';
    frame.setAttribute('aria-label', 'Game interfaces drawn by the reference client from the session interface state');
    const overlay = div(CSS.overlay + ';opacity:.8;font-size:10px', 'status');
    scene.append(glCanvas, labels);
    root.append(scene, frame, overlay);
    overlay.style.top = 'auto'; overlay.style.bottom = pct(503 - 338, 503); overlay.style.width = pct(512, 765); overlay.style.left = pct(4, 765); overlay.style.right = 'auto';
    host.replaceChildren(root);
    function click(e) {
        const [x, y] = toGame(root, e);
        const tab = tabAt(x, y);
        if (tab !== undefined) onTab(tab);
    }
    root.addEventListener('click', click);
    // The diagnostic stats line would cover the game's own interfaces: shown only on hover.
    overlay.style.opacity = '0'; overlay.style.transition = 'opacity .2s';
    const show = () => { overlay.style.opacity = '.85'; }, hide = () => { overlay.style.opacity = '0'; };
    root.addEventListener('pointerenter', show); root.addEventListener('pointerleave', hide);
    return {
        glCanvas, labels, overlay, root, gameFrame: true,
        setInterface(canvas) { const g = frame.getContext('2d'); g.clearRect(0, 0, 765, 503); g.drawImage(canvas, 0, 0); },
        setInventory() {}, setStatusAndChat() {}, setMinimapTerrain() {}, drawMinimap() {},
        destroy() { root.removeEventListener('click', click); root.removeEventListener('pointerenter', show); root.removeEventListener('pointerleave', hide); host.replaceChildren(); }
    };
}
