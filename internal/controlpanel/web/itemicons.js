import { historicalItemIcons } from './itemiconmanifest.js';
// Current icons are rendered directly from historical cache models, not cropped
// screenshots. The older capture investigation below is retained as history.
// Category icons for inventory slots, standing in for real item sprites.
//
// Investigated before writing this: neither internal/cachecfg (the config archive reader) nor
// internal/archive (the JagFile/interface decoder it sits on) decodes actual sprite images —
// cachecfg.Obj carries name/stackable/cost/ops, nothing pixel-level. docs/assets/pixel-art-pack.svg
// is one hand-drawn showcase graphic (a handful of illustrative skill/item/action/UI icons plus
// banners), not a per-item sprite sheet keyed by item id, and there is no PNG pack committed
// anywhere in the tree despite the pack's own manifest naming one (rsb0t-go_pixel_art_pack.zip
// does not exist in this repo). Decoding the real 2004-era sprite format (an indexed-palette image
// keyed by name inside a `.jag`-style archive, ported from the client's own Pix24/Sprite loader)
// would be genuine new format-reverse-engineering work, not a small addition — worth doing later
// as its own scoped task, not silently folded into a "visual inventory" ask.
//
// So this is a deliberately honest fallback, and — since users asked specifically for
// the panel to look "pleasing" rather than merely technically correct — real effort went into the
// fallback's own art rather than treating it as an afterthought: every icon shares one tile frame
// (TILE, below) copied from docs/assets/pixel-art-pack.svg's own convention (a brown tile, grey
// border, and a dark bottom/right bevel edge matching this app's own button/window chrome — see
// style.css's `button`/`.window` rules), so new icons read as part of the same family as both the
// existing pixel-art pack and the surrounding UI, not a bolt-on. Each glyph is a small multi-shape
// illustration (not a single flat silhouette) chosen by matching the item's resolved display name
// against category keywords — a real improvement over raw "#405"/"#995 x160" ids, without
// overclaiming it as an extracted real game sprite.
//
// Re-checked directly, not just re-read, per the account owner's explicit follow-up ask for
// "real ingame sprites" in the bank viewer specifically (internal/controlpanel/web/panels/
// bank.js): (1) engine/src/cache/config/ObjType.ts's own getSprite() (the ground-truth
// source for how this exact client renders an inventory icon) builds a Pix32 bitmap through
// what a 3D model-render pipeline produces (owi/ohi dimensions from a rendered Pix32,
// plotSprite for certified-item overlays, an outline pass over rendered pixel data) — items in
// this era are rendered from their 3D model at load time and cached client-side, not read from
// a pre-baked 2D sprite sheet; (2) confirmed live against this project's own running local
// engine tonight — fetched a real media.jag (GET /media<crc>, crc = index 4 of /crc0, the same
// primitive internal/spritepack already uses for real skill icons) and probed it with
// internal/archive.JagFile.Read against ~25 plausible family names ("obj", "items",
// "itemicons", "objsprites", "invicons", etc.) — none matched any entry's hash. Skill icons
// worked via this same primitive because "staticons"/"staticons2" genuinely are named 2D
// sprite sheets in media.jag; items are not, for either of two real routes: (a) port
// ObjType.getSprite's 3D rasterization to Go/JS (a real model/texture-render pipeline, not a
// format decode), or (b) render items in the real webclient (server/webclient) against a
// throwaway local-engine account and screenshot-crop each icon, the same technique that
// produced this project's real skill icons (see skillicons/*.png and docs/assets/
// skills_panel_mockup.html). Both are genuinely bigger, separately-scoped tasks — not
// something to fold into a panel-wiring pass — see COLLAB.md for this finding and the
// follow-up task this was spawned as.
//
// UPDATE (the follow-up task above, done): route (b) — screenshot-crop from the real
// webclient — is now real, live infrastructure, not just a documented next step. See
// docs/item_icon_capture.md for the full pipeline (cmd/iconsetup stocks a throwaway local
// account's inventory/bank via ::give/::givebank, cmd/iconslotdump opens the bank and
// resolves every occupied slot's real id+name from the config archive, then the webclient's
// own canvas is captured via canvas.toDataURL() and cropped per-slot) and exactly which items
// are covered. web/itemicons_png/<id>.png now holds ~60 real captured icons (raw/cooked fish,
// tools, currency, common resources, runes — the items this fleet's own scripts actually
// handle); itemIconWrap() below tries the real PNG first and falls back to this file's own
// hand-drawn category glyph — unchanged, still real infrastructure — for anything not
// captured. Not exhaustive (item ids run into the thousands; this covers what's in-scope for
// the fleet, not the whole game), and the capture pipeline is reusable for more items later.

const TILE = '<rect width="52" height="52" fill="#5a3e1b"/><rect x="1" y="1" width="50" height="50" fill="none" stroke="#8a7554" stroke-width="2"/><path d="M2 49h47M49 2v47" stroke="#241a0c" stroke-width="2"/>';

function tile(glyph) {
    return TILE + glyph;
}

const CATEGORIES = [
    {
        cls: 'food',
        svg: tile(
            '<path d="M14 34c-4-5-4-13 2-18 6-5 15-7 21-2 5 4 4 11-1 16l-9 9c-4 4-10 1-13-5z" fill="#a04434" stroke="#5a1f16" stroke-width="1.5"/>' +
            '<path d="M18 26c2-3 6-5 10-4" stroke="#c96a52" stroke-width="2" fill="none" stroke-linecap="round"/>' +
            '<path d="M13 35l-6 8" stroke="#e8dcc0" stroke-width="4" stroke-linecap="round"/>'
        ),
        test: /\b(shrimp|sardine|herring|anchov|trout|salmon|tuna|lobster|swordfish|shark|pike|bass|meat|beef|chicken|karambwan|stew|pie|bread|cake|pizza|potato|cooked|roll|banana|apple|cabbage|onion|tomato|cheese|egg)\b/i
    },
    {
        cls: 'currency',
        svg: tile(
            '<circle cx="20" cy="31" r="11" fill="#a9832f" stroke="#5f4315" stroke-width="2"/>' +
            '<circle cx="31" cy="23" r="12" fill="#e6c565" stroke="#7a5f22" stroke-width="2"/>' +
            '<text x="31" y="28" font-family="Georgia, serif" font-weight="bold" font-size="12" fill="#4a3510" text-anchor="middle">gp</text>'
        ),
        test: /\bcoins?\b/i
    },
    {
        cls: 'rune',
        svg: tile(
            '<rect x="14" y="11" width="24" height="29" fill="#241733" stroke="#9d7bc4" stroke-width="2"/>' +
            '<path d="M20 34l6-19 6 19M22.5 27h7" stroke="#d3b8ef" stroke-width="2.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>' +
            '<circle cx="17" cy="15" r="1.6" fill="#e6d6f7"/><circle cx="36" cy="17" r="1.1" fill="#e6d6f7"/><circle cx="34" cy="35" r="1.1" fill="#e6d6f7"/>'
        ),
        test: /\brune\b/i
    },
    {
        cls: 'potion',
        svg: tile(
            '<rect x="21" y="7" width="10" height="6" rx="1" fill="#8a6a3a" stroke="#4a2f10" stroke-width="1"/>' +
            '<path d="M23 13h6v8l8 13c2 4-1 8-5 8H20c-4 0-7-4-5-8l8-13z" fill="#241a12" stroke="#8a7554" stroke-width="2"/>' +
            '<path d="M18.5 29h15l3.5 6c2 3-1 6-4 6H19c-3 0-5.5-3-3.5-6z" fill="#6f8f4f"/>' +
            '<circle cx="24" cy="20" r="1.3" fill="#cdeeae"/><circle cx="28" cy="17" r="1" fill="#cdeeae"/>'
        ),
        test: /\bpotion\b/i
    },
    {
        cls: 'resource',
        svg: tile(
            '<rect x="7" y="20" width="37" height="12" rx="5" fill="#6b4a26" stroke="#3a2a14" stroke-width="2"/>' +
            '<ellipse cx="10.5" cy="26" rx="5.5" ry="6.5" fill="#d8c89c" stroke="#3a2a14" stroke-width="1.5"/>' +
            '<circle cx="10.5" cy="26" r="2" fill="#a9875a"/>' +
            '<path d="M17 22c2 2 2 6 0 8M25 21c2 2 2 7 0 9M33 22c2 2 2 6 0 8" stroke="#4a331a" stroke-width="1.5" fill="none"/>'
        ),
        test: /\b(logs?|ore|bars?|essence|flax|wool|bones?|herb|seed|feathers?|leather|hide|thread|bowstring|clay|coal|gems?|shell|husk)\b/i
    },
    {
        cls: 'tool',
        svg: tile(
            '<path d="M11 41L32 20" stroke="#b09468" stroke-width="5" stroke-linecap="round"/>' +
            '<path d="M20 10c7-3 16 0 20 7-4 5-11 9-18 8-2-5-3-10-2-15z" fill="#9a9a9a" stroke="#454545" stroke-width="1.5"/>' +
            '<path d="M23 14c3-1 8 0 11 3" stroke="#c9c9c9" stroke-width="1.5" fill="none" stroke-linecap="round"/>'
        ),
        test: /\b(tinderbox|knife|hammer|chisel|needle|axe|hatchet|pickaxe|rod|net|saw|spade|rake|seed dibber|secateurs|shears|bucket|pestle)\b/i
    },
    {
        cls: 'equip',
        svg: tile(
            '<path d="M26 7l16 6v13c0 10-7 17-16 19-9-2-16-9-16-19V13z" fill="#3a5568" stroke="#16232b" stroke-width="2"/>' +
            '<path d="M26 7l16 6v4l-16-5.5-16 5.5v-4z" fill="#7ea3ba"/>' +
            '<circle cx="26" cy="26" r="5.5" fill="#d3ac57" stroke="#7a5f22" stroke-width="1.5"/>'
        ),
        test: /\b(sword|scimitar|dagger|mace|spear|axe|shield|helm|helmet|hat|coif|armour|armor|plate|plateb|platel|legs|skirt|boots|gloves|vambraces|cape|cloak|ring|amulet|necklace|crossbow|bow|arrow|bolt|staff|wand|orb)\b/i
    },
    {
        cls: 'clue',
        // A rolled clue scroll with a wax seal — worth calling out distinctly since it's never junk.
        svg: tile(
            '<rect x="12" y="8" width="27" height="35" rx="2" fill="#e8dcc0" stroke="#3a2a14" stroke-width="2"/>' +
            '<path d="M17 17h17M17 23.5h17M17 30h10" stroke="#9c8a63" stroke-width="2"/>' +
            '<circle cx="32" cy="37" r="5.5" fill="#a03830" stroke="#5a1a14" stroke-width="1.5"/>'
        ),
        test: /\bclue\b/i
    }
];

const DEFAULT_ICON = {
    cls: 'misc',
    // A drawstring sack, a friendlier generic-item glyph than an empty box.
    svg: tile(
        '<path d="M19 15h13l3 6c4.5 2 7 7.5 5.5 13.5l-2 6c-1.3 4.4-4.7 6.5-9 6.5h-8c-4.3 0-7.7-2.1-9-6.5l-2-6C9 28.5 11.5 23 16 21z" fill="#8a6a3f" stroke="#3a2a14" stroke-width="2"/>' +
        '<path d="M22 15c0-4.4 1.8-7.5 4-7.5s4 3.1 4 7.5" stroke="#3a2a14" stroke-width="2" fill="none"/>'
    )
};

/** The category icon (cls + svg body, both strings) for an item's resolved display name. */
export function iconFor(name) {
    if (name) for (const c of CATEGORIES) if (c.test.test(name)) return c;
    return DEFAULT_ICON;
}

/**
 * A real captured in-game icon for one item, falling back to the category glyph above when
 * this item wasn't part of the capture. Real icons live in web/itemicons_png/<id>.png — real
 * PNG screenshot-crops taken from the actual webclient's own inventory/bank rendering (see
 * docs/item_icon_capture.md for the capture pipeline and exactly which items are covered:
 * currently the ~60 items this fleet's scripts actually handle — raw/cooked fish, tools,
 * currency, common resources, runes — not the full item table). Mirrors skillrender.js's own
 * two-tier fallback (a real image first, an `onerror` swap to the hand-drawn SVG glyph second)
 * rather than pre-checking a maintained id list — an uncaptured item just costs one 404 before
 * falling back, the same tradeoff skillrender.js's own static-PNG tier already accepts.
 */
export function itemIconWrap(id, name) {
    const icon = iconFor(name);
    const wrap = document.createElement('span');
    wrap.className = 'item-icon';
    wrap.title = name || `item #${id}`;
    const identity = historicalItemIcons[id];
    const normalized = value => String(value || '').trim().toLowerCase().replace(/\s+/g, ' ');
    if (!identity || normalized(identity) !== normalized(name)) {
        wrap.innerHTML = `<svg viewBox="0 0 52 52" width="26" height="26" shape-rendering="crispEdges">${icon.svg}</svg>`;
        wrap.setAttribute('aria-label', name || `item #${id}`);
        return wrap;
    }
    const img = document.createElement('img');
    img.src = `itemicons_cache/${encodeURIComponent(id)}.png`;
    img.alt = name || `item #${id}`;
    img.width = 26;
    img.height = 26;
    img.onerror = () => {
        wrap.textContent = '';
        wrap.innerHTML = `<svg viewBox="0 0 52 52" width="26" height="26" shape-rendering="crispEdges">${icon.svg}</svg>`;
    };
    wrap.append(img);
    return wrap;
}

/**
 * A monogram tile for a skill with no confirmed real icon (see internal/spritepack's own doc
 * comment for exactly which skills that is and why) — the same TILE frame as every item-category
 * icon above, with the skill's first two letters instead of a picture, so an unmapped skill still
 * looks like a deliberate, finished part of the grid rather than a missing-image gap.
 */
export function skillMonogram(name) {
    const letters = (name || '?').slice(0, 2).toUpperCase();
    return tile(`<text x="26" y="33" font-family="Georgia, serif" font-weight="bold" font-size="19" fill="#d8c89c" text-anchor="middle">${letters}</text>`);
}
