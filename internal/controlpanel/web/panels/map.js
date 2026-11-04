import { anyOf, el, button, pick, statePill } from '../util.js';
import { webglRenderer } from '../map-webgl.js';
import { mapOverlayBar, fillPanel } from '../map-overlay.js';

// Real accounts plotted on real map art. Was originally "a coordinate scatter plot, NOT a
// rendered game map"; users asked directly for the latter, and pointed at a genuine
// source for it: example.invalid's own public /deaths map (https://example.invalid/deaths) renders
// world-overview images at documented coordinates for exactly this server. AREAS below (area
// bounds, pxPerTile, image paths) were read directly out of that page's own embedded JSON on
// 2026-09-14, not guessed — see the "verified against the live site" note in COLLAB.md's
// panelrestyle findings entry for the exact fetch. The images are loaded live from example.invalid at
// runtime (internal/controlpanel/auth.go's CSP was extended to allow that one host) rather than
// vendored, since they're the server's own live-updated world render — if example.invalid ever moves
// those paths this degrades gracefully back to the flat coordinate plot (loadArea()'s onerror),
// it does not break the panel.
//
// Two areas, not one: the site's own map keeps "surface" and "underground" as separate named
// regions with separate images rather than one continuous world texture. Picking between them
// used to key off each account's own `pos.level` (0 = surface, >=1 = underground, majority
// wins for the whole panel) — that was wrong, found and fixed 2026-09-16 while chasing the
// account owner's "still doesn't show them when they're on another level" report:
// internal/controlpanel/source.go's Position keeps X/Z and Level as separate fields, and this
// engine's `level` is which FLOOR of a building/staircase stack a player is on (an upstairs
// bank is level 1 while standing on perfectly normal *surface* X/Z), not whether they're in a
// cave. Real dungeons/caves in this server's own coordinate space instead live at literal X/Z
// offsets far from the surface map — confirmed against example.invalid/deaths itself, whose "no
// named area nearby — mostly underground" coordinate list (fetched 2026-09-16) is full of real
// Z values like 9762, 9969, 10362 that land squarely inside AREAS.underground's own z0..z0+h
// band below, while an account merely upstairs in a Varrock bank keeps an ordinary surface Z
// under 4032 despite level>=1. So the old level-based bucketing routed upstairs-on-the-surface
// accounts into the underground bucket, where their real coordinates could never be found in
// that image's box (drawAreaImage's own bounds-intersection check always failed for them) —
// exactly the reported bug. areaFor() below buckets by which AREA's real x0/z0/w/h box an
// account's actual position falls inside instead, which is the same signal AREAS' own bounds
// were sourced from and needs no knowledge of `level` at all. A third 'outside' bucket covers
// positions in neither box (example.invalid's own /deaths page notes ~0.2% of deaths are "outlying
// cells" it can't place either) — honestly flagged and plotted on the flat grid, never guessed
// onto either image.
//
// Explv's public OSRS map (https://explv.github.io/, tiles from
// https://raw.githubusercontent.com/Explv/osrs_map_tiles) was investigated 2026-09-16 as a
// possible source of real per-plane tile art per the account owner's ask — see
// docs/map_panel_plan_2026-09-16.md for the full writeup of why it was ruled out (wrong game
// era: current live OSRS terrain, not this server's 2004-era revision 289 world) rather than
// wired in here.
const AREAS = {
    surface: { x0: 2048, z0: 2624, w: 1600, h: 1408, pxPerTile: 0.5, image: 'https://example.invalid/img/map/world-overview-surface.webp' },
    underground: { x0: 2048, z0: 9024, w: 1600, h: 1408, pxPerTile: 0.5, image: 'https://example.invalid/img/map/world-overview-underground.webp' }
};

const AREA_LABEL = { surface: 'Surface', underground: 'Underground', outside: 'Other' };

// Every cave and dungeon is 6400 tiles north of the surface above it, so a fleet on both
// spans ~7000 tiles of z with nothing between: the box around all of it is centred on that
// empty band, and even the furthest zoom out cannot show both ends. Fitting to that box left
// the map (and the heat map) showing only black. Fits take the busier side of this line;
// the Surface/Underground buttons go to the other. heatmap.go's heatUndergroundZ matches.
const UNDERGROUND_Z = 6400;

// ---------------------------------------------------------------------------
// Real tile map (2026-09-16 follow-up): when the server reports a generated
// tile pyramid (store.meta.mapTiles — see Config.MapTilesDir's own doc
// comment in internal/controlpanel/config.go and docs/map_panel_plan_2026-09-16.md
// Part 5), this panel switches to a genuinely interactive Leaflet map instead
// of the flat cropped-image canvas above: real per-tile terrain (decoded from
// this project's own Engine-TS checkout's worldmap.jag, not a guessed or
// third-party source — see the Engine-TS tools/pack/map/ExportTiles.ts this
// pyramid was built from), covering the WHOLE indexed world in one
// continuous, genuinely pannable/zoomable view — not just the two hand-
// picked surface/underground crops AREAS above still falls back to when no
// tile pyramid is configured. This is deliberately additive: everything
// above (AREAS, the canvas renderer, areaFor bucketing) is untouched and
// stays the exact fallback for a build with no Config.MapTilesDir set —
// which is every production fleet today.
const LEAFLET_JS = 'vendor/leaflet/leaflet.js';
const LEAFLET_CSS = 'vendor/leaflet/leaflet.css';

let leafletLoadPromise = null;
/** Injects the vendored Leaflet script+stylesheet once (shared across every map panel
 * instance) and resolves to the global `L` it defines. Vendored rather than pulled from a
 * CDN at runtime, matching this control panel's existing fully-embedded asset model
 * (internal/controlpanel/server.go's `//go:embed web` — see internal/controlpanel/web/
 * vendor/leaflet's own presence there). */
export function loadLeaflet() {
    if (leafletLoadPromise) return leafletLoadPromise;
    leafletLoadPromise = new Promise((resolve, reject) => {
        if (window.L) {
            resolve(window.L);
            return;
        }
        const link = document.createElement('link');
        link.rel = 'stylesheet';
        link.href = LEAFLET_CSS;
        document.head.append(link);
        const script = document.createElement('script');
        script.src = LEAFLET_JS;
        script.onload = () => resolve(window.L);
        script.onerror = () => reject(new Error('failed to load ' + LEAFLET_JS));
        document.head.append(script);
    });
    return leafletLoadPromise;
}

let tileMetaPromise = null;
/** Fetches /maptiles/meta.json once (shared across every map panel instance) — the
 * pixel<->game-tile origin TilePyramid.ts wrote alongside the tile pyramid itself. Null on
 * any failure (tiles misconfigured, meta.json missing) so callers fall back cleanly. */
export function loadTileMeta() {
    if (tileMetaPromise) return tileMetaPromise;
    tileMetaPromise = fetch('maptiles/meta.json', { cache: 'no-store' })
        .then(r => (r.ok ? r.json() : null))
        .catch(() => null);
    return tileMetaPromise;
}

/** The tile pyramid's whole raster as Leaflet bounds (the same lat/lng frame as each panel's
 * worldToLatLng), for a tile layer's `bounds`: without it Leaflet asks for tiles beside the
 * raster, which do not exist, and every one is a 404 in the console. */
export function rasterBounds(L, meta) {
    const unit = Math.pow(2, meta.nativeZoom);
    return L.latLngBounds([-meta.heightAtNative / unit, 0], [0, meta.widthAtNative / unit]);
}

let poiPromise = null;
/** Fetches /api/poi once (shared across every map panel instance) — static data (see
 * internal/controlpanel/mapdata.go's own doc comment), so one fetch for the page's lifetime
 * is correct, not a polling gap. Empty array on any failure (Config.Locs/LocTypes unset, or
 * the endpoint 404s on an older build) so callers degrade to "no POI markers" cleanly. */
function loadPOI() {
    if (poiPromise) return poiPromise;
    poiPromise = fetch('api/poi', { cache: 'no-store' })
        .then(r => (r.ok ? r.json() : { poi: [] }))
        .then(d => d.poi || [])
        .catch(() => []);
    return poiPromise;
}

let wallsPromise = null;
/** Fetches /api/walls once, same caching rationale as loadPOI above. */
function loadWalls() {
    if (wallsPromise) return wallsPromise;
    wallsPromise = fetch('api/walls', { cache: 'no-store' })
        .then(r => (r.ok ? r.json() : { walls: [] }))
        .then(d => d.walls || [])
        .catch(() => []);
    return wallsPromise;
}

// POI_STYLE: category -> {label, color} for /api/poi markers (internal/controlpanel/
// mapdata.go's own poiCategory doc comment explains exactly how each category is matched
// against this project's real config archive). Colors are just distinguishable dots, not
// custom art — the account owner's own ask ("icons for altars banks etc") didn't require
// bespoke art, and simple colored dots keep every category legible at a glance without a
// sprite sheet to maintain.
const POI_STYLE = {
    bank: { label: 'Bank', color: '#e0b030' },
    altar: { label: 'Altar', color: '#c060e0' },
    rc_altar: { label: 'Runecrafting altar', color: '#60a0e0' },
    anvil: { label: 'Anvil', color: '#909090' },
    furnace: { label: 'Furnace', color: '#e06030' },
    range: { label: 'Cooking range', color: '#e0a060' }
};

// LOCATIONS: curated region/city name labels for the real tile map. No per-region boundary
// dataset exists anywhere in this project's own data (checked: neither the Engine-TS
// checkout ../engine nor its own map-pack tooling ships named-area polygons/labels for
// anything beyond the individual mapsquare "labels.dat" entries tools/pack/map/Worldmap.ts
// writes into worldmap.jag for the client's OWN world-map screen -- a real, more complete
// source for a future pass, not decoded this session; see this file's own follow-up notes)
// -- so this is a small, hand-curated list, exactly the pragmatic fallback the account owner
// explicitly signed off on. Coordinates marked "(repo)" are copied verbatim from this
// codebase's own already-verified tile constants (grepped from internal/scripts/freshstart,
// mulereceiverplan, barbarianrunner, freshstartrunner) rather than re-guessed; the rest are
// general RS2/2004-era world knowledge, approximate town-centre coordinates, not independently
// re-verified against this specific content revision's terrain this session -- worth a spot
// check next time any of them look visibly wrong on the real rendered map.
const LOCATIONS = [
    { name: 'Lumbridge', x: 3222, z: 3218 }, // repo: freshstart.LumbridgeCentre
    { name: 'Draynor Village', x: 3093, z: 3243 }, // repo: freshstart.Draynor
    { name: 'Port Sarim', x: 3025, z: 3218 },
    { name: 'Falador', x: 2965, z: 3378 }, // repo: mulereceiverplan Falador bank landing
    { name: 'Varrock', x: 3213, z: 3424 }, // repo: mulereceiverplan Varrock bank landing
    { name: 'Edgeville', x: 3087, z: 3496 },
    { name: 'Barbarian Village', x: 3082, z: 3422 },
    { name: 'Al Kharid', x: 3293, z: 3182 },
    { name: 'Rimmington', x: 3044, z: 3306 }, // repo: freshstartrunner doc comment (real Stile fixture)
    { name: 'Catherby', x: 2807, z: 3437 },
    { name: "Seers' Village", x: 2725, z: 3491 }, // repo: mulereceiverplan.SeersStand
    { name: 'Ardougne', x: 2662, z: 3305 },
    { name: 'Brimhaven', x: 2772, z: 3234 }, // repo: freshstart.BrimhavenPier
    { name: 'Musa Point', x: 2850, z: 3150 }, // repo: barbarianrunner test (Karamja)
    { name: 'Yanille', x: 2606, z: 3095 }
];

/** Which AREAS bucket a real world position falls inside — 'outside' if neither (see the
 * doc comment above `AREAS`). This, not `pos.level`, is the real signal for which overview
 * image (if either) actually depicts a position: it's the same box every account's dot is
 * cropped/placed against in drawAreaImage below, so a position can never land on the wrong
 * art or silently vanish between the two — it's either inside the box being shown, or it
 * isn't and that's reported rather than guessed at. */
function areaFor(x, z) {
    for (const key of ['surface', 'underground']) {
        const a = AREAS[key];
        if (x >= a.x0 && x < a.x0 + a.w && z >= a.z0 && z < a.z0 + a.h) return key;
    }
    return 'outside';
}

/** One Image per area, loaded once and reused; `ready`/`failed` let draw() fall back cleanly
 * while a fetch is in flight or if it never succeeds (offline, host unreachable, path moved). */
function loadArea(area) {
    const img = new Image();
    const state = { img, ready: false, failed: false };
    img.onload = () => {
        state.ready = true;
    };
    img.onerror = () => {
        state.failed = true;
    };
    img.src = area.image;
    return state;
}
const AREA_IMAGES = { surface: loadArea(AREAS.surface), underground: loadArea(AREAS.underground) };

// Markers ease toward each new reported position (and the viewport eases toward a new
// bounding box) over EASE_MS rather than snapping the instant a poll lands, driven by its own
// requestAnimationFrame loop independent of how often the fleet actually reports — so the map
// reads as continuously alive between polls instead of a slideshow of static snapshots, even
// though the underlying position data only updates as often as the fleet reports it.
const EASE_MS = 600;

// MIN_SPAN_TILES floors how tight the zoom-to-fit box (bounds, below) is ever allowed to get in
// world tiles, independent of the small "don't divide by zero" floor tick() already applies.
// Found live, not guessed: the account owner reported the real map art rendering "very blurry"
// tonight, and reading AREAS' own pxPerTile (0.5 -- confirmed against the real fetched images,
// both exactly 800x704 for a 1600x1408-tile area) makes the reason arithmetic, not a mystery.
// Two accounts standing close together (or one alone) used to collapse bounds toward that old
// ~20-tile floor, which is *10 source image pixels* at this resolution -- drawImage then had to
// stretch a literal handful of pixels across the entire canvas (often 40-60x magnification), and
// no amount of image smoothing makes that look like anything but mud. This is a real overview
// texture (2 world tiles per source pixel), not a per-tile game map, so it was never going to
// hold up under that much zoom. Chosen so the smallest possible crop is still a defensible ~150
// source pixels wide (300 tiles * 0.5 px/tile) -- close-up account clusters still center and
// fill the frame, they just stop being magnified well past what the source art can show. This is
// a floor on the crop, not a real interactive zoom -- see this panel's own still-open "fully
// interactable, zoom in and out" ask, unaddressed by this fix.
const MIN_SPAN_TILES = 300;

export default {
    type: 'map',
    title: 'Account map',
    description: 'Every account’s last known tile (x/z) plotted on a real world map, fetched live from example.invalid’s own public map — falls back to a plain coordinate plot if that’s unreachable. Accounts underground are shown on a separate real map from those on the surface; use the Surface/Underground buttons to pick which to view. Hover a point for its activity; click to select it.',
    size: { w: 5, h: 3 },
    accounts: 'many',
    settings: [
        { key: 'renderer', label: 'Draw markers and walls with', kind: 'select', options: [{ value: 'gpu', label: 'GPU / WebGL' }, { value: 'canvas', label: 'Canvas fallback' }], default: 'gpu' },
        { key: 'pois', label: 'Show banks, altars and facilities', kind: 'bool', default: true },
        { key: 'walls', label: 'Show walls and doors when zoomed in', kind: 'bool', default: true },
        { key: 'locations', label: 'Show town names', kind: 'bool', default: true },
        { key: 'labels', label: 'Show account names', kind: 'bool', default: true },
        { key: 'players', label: 'Show other players our accounts can see', kind: 'bool', default: true }
    ],
    create(body, ctx) {
        const wrap = el('div', 'mapwrap');
        const canvas = el('canvas');
        const tip = el('div', 'tip');
        const foot = el('div', 'foot');
        tip.hidden = true;

        // leafletEl: the real interactive map's own DOM container, kept alongside (never
        // instead of) `canvas` — see the LEAFLET_JS block's doc comment above. Hidden until
        // store.meta.mapTiles is true AND Leaflet + meta.json have both loaded (leaflet below),
        // so a build with no tile pyramid configured never even shows an empty div.
        const leafletEl = el('div', 'mapleaflet');
        leafletEl.hidden = true;
        // leaflet holds the lazily-created Leaflet map instance and its own small state; null
        // until initLeaflet() (below) has actually run, which only happens once this panel
        // first observes store.meta.mapTiles true.
        let leaflet = null; // { map, markers: Map<name, L.CircleMarker>, worldToLatLng(x,z) }
        let leafletInitStarted = false;
        let destroyed = false;
        let floorMode = 'all';
        const toolbar = [];
        const floors = el('select');
        floors.title = 'Filter accounts by building floor; cave coordinates are a separate map area.';
        for (const value of ['all','0','1','2','3']) { const option=el('option','',value==='all'?'All floors':`Floor ${value}`);option.value=value;floors.append(option); }
        floors.addEventListener('change',()=>{floorMode=floors.value;if(leaflet&&latest)drawLeaflet(pick(latest.accounts,ctx.accounts).filter(a=>a.pos),latest);if(leaflet)drawPlayers();});
        toolbar.push(floors);
        const fitFleet=button('Fit fleet'),fitSelected=button('Follow selected');
        fitFleet.addEventListener('click',()=>{if(leaflet&&latest){leaflet.fitted=false;drawLeaflet(pick(latest.accounts,ctx.accounts).filter(a=>a.pos),latest);}});
        fitSelected.addEventListener('click',()=>{if(leaflet&&latest){const a=latest.accounts.find(a=>a.name===latest.selected&&a.pos);if(a){floorMode='all';floors.value='all';leaflet.map.setView(leaflet.worldToLatLng(a.pos.x,a.pos.z),leaflet.detailZoom);}}});
        toolbar.push(fitFleet,fitSelected);
        for(const area of ['surface','underground']) {const jump=button(AREA_LABEL[area]);jump.addEventListener('click',()=>{if(!leaflet||!latest)return;const group=pick(latest.accounts,ctx.accounts).filter(a=>a.pos&&areaFor(a.pos.x,a.pos.z)===area);if(group.length){leaflet.map.fitBounds(group.map(a=>leaflet.worldToLatLng(a.pos.x,a.pos.z)),{padding:[40,40],maxZoom:leaflet.detailZoom});}else{const bounds=AREAS[area];leaflet.map.setView(leaflet.worldToLatLng(bounds.x0+bounds.w/2,bounds.z0+bounds.h/2),leaflet.detailZoom-4);}});toolbar.push(jump);}

        // areaCtl: a small Surface/Underground/Other toggle overlaid on the map itself, so a
        // viewer can pin exactly which real area's accounts to look at instead of only ever
        // trusting the auto-picked majority — the "switch/view a specific level" half of the
        // account owner's ask that real tile art doesn't (yet) exist to serve properly (see
        // docs/map_panel_plan_2026-09-16.md). The 'Other' button only appears once there's an
        // account to show it for (areaBtns.outside, wired up in draw()) — most sessions will
        // never see it, since almost every real position falls inside one of the two known
        // boxes. Clicking the already-pinned button un-pins back to auto rather than needing a
        // separate reset control.
        const areaCtl = el('div', 'arealevel');
        const areaBtns = {
            surface: button('Surface', 'Show only accounts on the surface'),
            underground: button('Underground', 'Show only accounts underground (caves/dungeons)'),
            outside: button('Other', 'Show only accounts outside both known map areas')
        };
        areaBtns.outside.hidden = true;
        for (const key of Object.keys(areaBtns)) {
            areaBtns[key].addEventListener('click', () => {
                // Un-pin back to 'auto' on a second click of the already-pinned button rather
                // than needing a separate reset control. tick()'s own requestAnimationFrame
                // loop (already running) picks this up within one frame — no need to force a
                // draw here.
                areaMode = areaMode === key ? 'auto' : key;
            });
        }
        areaCtl.append(areaBtns.surface, areaBtns.underground, areaBtns.outside);

        // The toolbar and the status line float over the map (map-overlay.js), so the map
        // fills the whole panel and the panel never scrolls.
        wrap.append(canvas, leafletEl, areaCtl, tip, foot);
        const overlay = mapOverlayBar(wrap, toolbar);
        foot.classList.add('mapfoot');
        fillPanel(body);
        body.append(wrap);
        canvas.setAttribute('role', 'img');

        /** Lazily builds the real Leaflet map the first time store.meta.mapTiles is seen
         * true, using L.CRS.Simple with a linear game-tile<->pixel transform derived from
         * meta.json (see TilePyramid.ts's own doc comment for exactly how the tile pyramid's
         * zoom numbering was chosen so this transform lines up 1:1 with Leaflet's own
         * zoom-to-scale doubling, with no custom CRS subclass needed). No-ops past the first
         * successful call. */
        function initLeaflet() {
            if (leafletInitStarted) return;
            leafletInitStarted = true;
            Promise.all([loadLeaflet(), loadTileMeta()]).then(([L, meta]) => {
                if (destroyed) return;
                if (!L || !meta) {
                    leafletInitStarted = false; // let a later tick retry (e.g. meta.json 404 during a mid-restart)
                    return;
                }
                const unit = Math.pow(2, meta.nativeZoom);
                // worldToLatLng: the one coordinate transform every marker and fitBounds call
                // below goes through. lng tracks world X directly; lat tracks the raster ROW
                // (north-up, matching ExportTiles.ts's own row flip), NOT world Z directly, so
                // this needs no separate sign flip anywhere else in this file -- EXCEPT here:
                // L.CRS.Simple's own transformation is `new L.Transformation(1, 0, -1, 0)`
                // (Leaflet's source, not guessed), i.e. it negates lat when converting to a
                // projected pixel (pixelY = -lat * 2^zoom) so that "lat" reads the normal
                // geographic way (north/up = positive) even though CRS.Simple has no real
                // geography. Found live: an earlier version of this function returned +row/unit
                // and Leaflet requested tiles like /maptiles/2/1/-9.png (confirmed via
                // read_network_requests in a headless browser) -- negative Y tile indices,
                // because that extra negation was still being applied on top of an
                // already-correct positive row. Negating row here (not the tile scheme, not
                // Leaflet's CRS) is the one place this cancels out correctly.
                const worldToLatLng = (x, z) => {
                    const row = meta.heightAtNative - 0.5 - (z - meta.originTileZ);
                    const col = x - meta.originTileX + 0.5;
                    return L.latLng(-row / unit, col / unit);
                };
                // worldCornerToLatLng: the SW-corner analogue of worldToLatLng above, used only
                // for wall segment endpoints (drawWalls below). A world tile (x,z)'s pixel in
                // the raster spans a whole unit square, not a point; worldToLatLng places a
                // marker at the centre of that square. Walls instead use literal corner
                // coordinates so their outlines align with the terrain boundaries.
                // Wall edge coordinates (from internal/controlpanel/mapdata.go's own
                // CollisionEngine.ts-derived geometry) are literal tile corners and need the
                // corner mapping, not the marker one, or every wall line would sit half a tile
                // off from the terrain edge it's supposed to trace.
                const worldCornerToLatLng = (x, z) => {
                    const row = meta.heightAtNative - (z - meta.originTileZ);
                    const col = x - meta.originTileX;
                    return L.latLng(-row / unit, col / unit);
                };
                const renderer = ctx.settings.renderer !== 'canvas' ? webglRenderer(L) || L.canvas() : L.canvas();
                const map = L.map(leafletEl, {
                    crs: L.CRS.Simple,
                    minZoom: meta.minZoom,
                    maxZoom: meta.maxZoom + 4,
                    attributionControl: false,
                    zoomControl: true,
                    // walls (drawWalls below) can be several thousand segments world-wide;
                    // canvas renders those as one draw surface instead of one SVG DOM node per
                    // segment, the difference between smooth and unusable at that count.
                    renderer
                });
                L.tileLayer('maptiles/{z}/{x}/{y}.png', {
                    tileSize: meta.tileSize,
                    minZoom: meta.minZoom,
                    maxNativeZoom: meta.maxZoom,
                    maxZoom: meta.maxZoom + 4,
                    noWrap: true,
                    bounds: rasterBounds(L, meta),
                    // a tile with no mapsquare data is simply not written (see TilePyramid.ts's
                    // own "skip fully-transparent tiles" note) — errorTileUrl none needed, a
                    // missing tile just shows the pane background (style.css's own --plot dark
                    // fill) rather than a broken-image glyph.
                    className: 'maptile'
                }).addTo(map);
                // Start on Lumbridge until the accounts arrive and the first fit runs: the
                // raster's own centre (z ~5800) is the empty band between surface and
                // underground.
                map.setView(worldToLatLng(3222, 3218), meta.nativeZoom - 1);

                // Other (non-fleet) players, added BEFORE markerLayer so our own accounts
                // always draw on top of them (the GPU renderer draws in add order).
                const playerLayer = L.layerGroup().addTo(map);
                const markerLayer = L.layerGroup().addTo(map);
                // labelLayer/poiLayer/wallLayer are added/removed from the map by
                // updateZoomLayers (not just hidden) based on the current zoom, per the
                // account owner's own ask that labels/icons not clutter the zoomed-out view or
                // require zooming in past usefulness — see each MIN_ZOOM constant below.
                const labelLayer = L.layerGroup();
                const poiLayer = L.layerGroup();
                const wallLayer = L.layerGroup();
                const LABEL_MIN_ZOOM = Math.max(meta.minZoom, meta.nativeZoom - 1);
                const POI_MIN_ZOOM = meta.nativeZoom;
                const WALL_MIN_ZOOM = meta.nativeZoom;
                function updateZoomLayers() {
                    const z = map.getZoom();
                    setLayerShown(labelLayer, map, ctx.settings.locations !== false && z >= LABEL_MIN_ZOOM);
                    setLayerShown(poiLayer, map, ctx.settings.pois !== false && z >= POI_MIN_ZOOM);
                    setLayerShown(wallLayer, map, ctx.settings.walls !== false && z >= WALL_MIN_ZOOM);
                }
                map.on('zoomend', updateZoomLayers);
                updateZoomLayers();

                for (const loc of LOCATIONS) {
                    L.marker(worldToLatLng(loc.x, loc.z), {
                        icon: L.divIcon({ className: 'maplabel', html: loc.name, iconSize: null }),
                        interactive: false
                    }).addTo(labelLayer);
                }

                loadPOI().then(pois => {
                    for (const p of pois) {
                        const style = POI_STYLE[p.category];
                        if (!style) continue;
                        L.circleMarker(worldToLatLng(p.x, p.z), {
                            radius: 5,
                            color: '#111',
                            weight: 1,
                            fillColor: style.color,
                            fillOpacity: 0.95
                        })
                            .bindTooltip(`${style.label}${p.name && p.name !== style.label ? ` (${p.name})` : ''}`, { className: 'maptip' })
                            .addTo(poiLayer);
                    }
                });

                loadWalls().then(walls => {
                    // Two multi-segment polylines total (one white, one red), not one Leaflet
                    // layer per wall — thousands of individual layer objects visibly stalled
                    // panning/zooming in an earlier version of this function; two batched
                    // polylines on the canvas renderer above render the same pixels for a
                    // fraction of the JS/DOM overhead.
                    const plain = [], doors = [];
                    for (const w of walls) {
                        const seg = [worldCornerToLatLng(w.x0, w.z0), worldCornerToLatLng(w.x1, w.z1)];
                        (w.door ? doors : plain).push(seg);
                    }
                    if (plain.length) L.polyline(plain, { color: '#f0f0f0', weight: 2, opacity: 0.9 }).addTo(wallLayer);
                    if (doors.length) L.polyline(doors, { color: '#e03030', weight: 2, opacity: 0.95 }).addTo(wallLayer);
                });

                map.on('click', e => {
                    // clicking empty map (not a marker — markers stopPropagation their own
                    // click below) clears nothing today; kept simple for this first real slice.
                    void e;
                });
                const GPUCircle = L.CircleMarker;
                leaflet = { map, renderer, GPUCircle, detailZoom:meta.nativeZoom+3, markerLayer, playerLayer, playerMarkers: new Map(), poiLayer, wallLayer, labelLayer, markers: new Map(), worldToLatLng, fitted: false };
                if(latest)drawLeaflet(pick(latest.accounts,ctx.accounts).filter(a=>a.pos),latest);
                pollPlayers();
                cancelAnimationFrame(raf);raf=0;
            });
        }

        // ---- other players the fleet can see (GET /api/players) ----
        // The union of every connected session's own PLAYER_INFO view, de-duplicated and
        // with our own accounts removed server-side. Coverage is ONLY view distance around
        // our accounts; the footer says so rather than implying a world-wide list.
        let seen = null, playersTimer = 0, playersUnsupported = false;
        const PLAYERS_POLL_MS = 3000;
        async function pollPlayers() {
            clearTimeout(playersTimer); playersTimer = 0;
            if (destroyed || !leaflet || playersUnsupported || ctx.isVisible?.() === false) return;
            if (ctx.settings.players !== false && !document.hidden) {
                try {
                    const res = await fetch('api/players', { cache: 'no-store' });
                    if (res.status === 501) { playersUnsupported = true; seen = null; drawPlayers(); return; }
                    if (res.ok) { seen = await res.json(); drawPlayers(); }
                } catch (_) { /* next poll retries */ }
            } else if (ctx.settings.players === false && seen) { seen = null; drawPlayers(); }
            if (!destroyed) playersTimer = setTimeout(pollPlayers, PLAYERS_POLL_MS);
        }
        function drawPlayers() {
            if (!leaflet) return;
            const { playerLayer, playerMarkers, worldToLatLng } = leaflet;
            const wanted = ctx.settings.players !== false && seen ? seen.players.filter(p => floorMode === 'all' || String(p.level) === floorMode) : [];
            const present = new Set(wanted.map(p => p.name));
            for (const [name, marker] of playerMarkers) if (!present.has(name)) { playerLayer.removeLayer(marker); playerMarkers.delete(name); }
            for (const p of wanted) {
                const ll = worldToLatLng(p.x, p.z);
                const style = { radius: 3, color: '#101010', weight: 1, fillColor: p.inCombat ? '#ff9a3c' : '#f2f2f2', fillOpacity: 0.95, bubblingMouseEvents: false };
                let marker = playerMarkers.get(p.name);
                if (!marker) {
                    marker = L.circleMarker(ll, style).addTo(playerLayer);
                    marker.bindTooltip(el('span', '', p.name), { direction: 'right', offset: [6, 0], className: 'maptip maptip-other' });
                    playerMarkers.set(p.name, marker);
                } else { marker.setLatLng(ll); marker.setStyle(style); }
            }
            if (latest) drawLeaflet(pick(latest.accounts, ctx.accounts).filter(a => a.pos), latest);
        }
        function playersNote() {
            if (ctx.settings.players === false || playersUnsupported || !seen) return '';
            const shown = floorMode === 'all' ? seen.players.length : seen.players.filter(p => String(p.level) === floorMode).length;
            return ` · ${shown} other player${shown === 1 ? '' : 's'} seen by ${seen.observers} of our accounts (view distance only, not the whole world${seen.truncated ? '; list truncated' : ''})`;
        }

        /** Adds/removes layer from map to match wanted — used instead of Leaflet's own
         * per-layer minZoom (which only tile layers support) so labels/POI/walls can each gate
         * on their own zoom threshold. */
        function setLayerShown(layer, map, wanted) {
            const has = map.hasLayer(layer);
            if (wanted && !has) layer.addTo(map);
            else if (!wanted && has) map.removeLayer(layer);
        }

        /** Real-map draw path, used instead of the canvas renderer once leaflet is ready.
         * Plots every positioned account across the WHOLE indexed world in one continuous
         * view — no surface/underground split needed here, since the tile pyramid already
         * covers both (and everywhere else a mapsquare exists) in one continuous raster; see
         * this file's top-of-file doc comment on the LEAFLET_JS block. */
        function drawLeaflet(list, store) {
            if (floorMode !== 'all') list=list.filter(a=>String(a.pos.level)===floorMode);
            leafletEl.hidden = false;
            canvas.hidden = true;
            areaCtl.hidden = true;
            const { map, markerLayer, markers, worldToLatLng } = leaflet;
            // The map was created while leafletEl was still hidden (display:none, size 0) —
            // Leaflet has no way to know its container has since gained real dimensions (or
            // been resized by the panel grid) without being told; called at the store's own
            // poll cadence (not per animation frame), so cheap enough to just always call.
            map.invalidateSize(false);
            const present = new Set(list.map(a => a.name));
            for (const [name, marker] of markers) {
                if (!present.has(name)) {
                    markerLayer.removeLayer(marker);
                    markers.delete(name);
                }
            }
            // Keep every dot at its actual tile. A popup lists the entire stack rather
            // than displacing accounts into invented screen positions.
            const tileKey = a => `${a.pos.x},${a.pos.z},${a.pos.level}`;
            const tileGroups = new Map();
            for (const a of list) {
                const key = tileKey(a);
                if (!tileGroups.has(key)) tileGroups.set(key, []);
                tileGroups.get(key).push(a);
            }
            function markerIcon(size, color, ringColor) {
                const half = size / 2;
                // A real DOM element, not an HTML string with a `style="..."` attribute: this
                // control panel's own CSP is `style-src 'self'` (no 'unsafe-inline'), which
                // blocks any inline style ATTRIBUTE — including one Leaflet would otherwise
                // insert verbatim via innerHTML — but does NOT block direct CSSOM property
                // assignment (`el.style.foo = ...` below), the same reason Leaflet's own
                // internal tile/marker positioning (which works the same way) was never
                // affected. Found live: an earlier version built `html:
                // '<div style="...">'` and every account dot silently failed to render at all
                // (confirmed via read_console_messages: hundreds of "Applying inline style
                // violates ... style-src" CSP errors, one per marker redraw).
                const div = document.createElement('div');
                div.className = 'acct-dot';
                div.style.width = size + 'px';
                div.style.height = size + 'px';
                div.style.background = color;
                div.style.borderColor = ringColor;
                return L.divIcon({
                    className: 'acct-marker',
                    html: div,
                    iconSize: [size, size],
                    iconAnchor: [half, half]
                });
            }

            const c = palette();
            // Bounds per part of the world: the fit below takes the busier one, never the box
            // around both (see UNDERGROUND_Z).
            const fitBoxes = [{ n: 0 }, { n: 0 }];
            for (const a of list) {
                const ll = worldToLatLng(a.pos.x, a.pos.z);
                const box = fitBoxes[a.pos.z >= UNDERGROUND_Z ? 1 : 0];
                if (!box.n++) { box.minLat = box.maxLat = ll.lat; box.minLng = box.maxLng = ll.lng; }
                box.minLat = Math.min(box.minLat, ll.lat); box.maxLat = Math.max(box.maxLat, ll.lat);
                box.minLng = Math.min(box.minLng, ll.lng); box.maxLng = Math.max(box.maxLng, ll.lng);
                const color = dotColor(c, a.state);
                const selected = store.selected === a.name;
                const icon = leaflet.renderer._gpu ? null : markerIcon(selected ? 16 : 12, color, selected ? c.gold : c.plot);
                let marker = markers.get(a.name);
                if (!marker) {
                    marker = leaflet.renderer._gpu
                        ? new leaflet.GPUCircle(ll,{radius:selected?8:6,color:selected?c.gold:c.plot,fillColor:color,fillOpacity:.95,weight:selected?2:1})
                        : L.marker(ll, { icon });
                    marker.on('click', ev => {
                        L.DomEvent.stopPropagation(ev);
                        ctx.store.select(marker._account.name);
                    });
                    marker.addTo(markerLayer);
                    markers.set(a.name, marker);
                } else {
                    if(leaflet.renderer._gpu){marker.setStyle({radius:selected?8:6,color:selected?c.gold:c.plot,fillColor:color,weight:selected?2:1});}
                    else marker.setIcon(icon);
                    marker.setLatLng(ll);
                }
                const permanent = !!ctx.settings.labels && (list.length<=80||selected);
                if(marker._labelPermanent!==permanent){marker.unbindTooltip();marker.bindTooltip(el('span','',a.name),{permanent,direction:'right',offset:[10,0],className:'maptip'});marker._labelPermanent=permanent;}
                marker._account=a;
                marker._stack=tileGroups.get(tileKey(a));
                if(!marker.getPopup())marker.bindPopup(()=>{
                    const current=marker._account, popup=el('div'), stack=marker._stack;
                    popup.append(el('b','',`${stack.length} account${stack.length===1?'':'s'} on this tile`),
                        el('div','',`${current.pos.x}, ${current.pos.z}, floor ${current.pos.level}`));
                    const choices=el('div');choices.style.maxHeight='200px';choices.style.overflowY='auto';
                    for(const account of stack){
                        const choice=el('button','',account.name);choice.type='button';choice.dataset.mapAccount=account.name;
                        choice.style.display='block';choice.style.width='100%';
                        choice.title=account.activity||'(no activity reported)';
                        choice.addEventListener('click',()=>ctx.store.select(account.name));choices.append(choice);
                    }
                    popup.append(choices);return popup;
                });
            }
            foot.textContent = `${leaflet.renderer._gpu?'WebGL':'Canvas'} · ${list.length} account${list.length === 1 ? '' : 's'} plotted on the real map`
                + (store.accounts.some(a=>!a.pos) ? `, ${store.accounts.filter(a=>!a.pos).length} without a known position` : '')
                + (floorMode!=='all' ? ` (floor ${floorMode})` : '')
                + playersNote();

            // First time accounts actually appear, fit the view to them once rather than every
            // frame (which would fight a viewer's own pan/zoom) — mirrors the canvas path's
            // own "sticky bounds" idea with leafletFitted below.
            if (list.length > 0 && !leaflet.fitted) {
                leaflet.fitted = true;
                const box = fitBoxes[0].n >= fitBoxes[1].n ? fitBoxes[0] : fitBoxes[1];
                map.fitBounds([[box.minLat, box.minLng], [box.maxLat, box.maxLng]], { padding: [40, 40],maxZoom:leaflet.detailZoom });
            }
        }

        let latest = null;
        let hover = null; // {x, y} in canvas pixels
        // plotted is rebuilt by draw() every frame: the last-drawn screen position of every
        // point currently on the map, so hitTest (hover and click both) always matches what's
        // actually on screen instead of recomputing the layout separately.
        let plotted = [];

        // eased holds each account's currently-displayed world tile, name -> {x, z}, chasing
        // whatever the fleet last reported (below) — this is what actually gets plotted, not
        // the raw report, so a position update reads as the dot gliding there rather than
        // teleporting. bounds is the same idea applied to the viewport's fitted box, so the
        // whole plot pans/zooms smoothly too when accounts log in, out, or spread out.
        const eased = new Map();
        let bounds = null; // {minX, maxX, minZ, maxZ}
        // currentArea only ever changes while accounts are actually positioned (see tick()) —
        // once picked it stays put through a momentary empty poll rather than flapping back to
        // 'surface', the same "sticky" treatment bounds already gets.
        let currentArea = 'surface';
        // areaMode pins currentArea to a specific bucket when the viewer has clicked one of
        // areaCtl's buttons above; 'auto' restores the old majority-vote-with-a-preference-
        // for-whichever-bucket-the-selected-account-is-in behaviour (see tick()).
        let areaMode = 'auto';
        // lastGroups is tick()'s most recent areaFor() bucketing of every positioned account,
        // kept around so the resize observer and the areaCtl buttons' own count labels can read
        // it without recomputing groups themselves between animation frames.
        let lastGroups = { surface: [], underground: [], outside: [] };
        let lastFrame = 0;
        let raf = 0;

        function palette() {
            const cs = getComputedStyle(body);
            const read = name => cs.getPropertyValue(name).trim();
            return { grid: read('--grid'), dim: read('--dim'), plot: read('--plot'), good: read('--good'), warn: read('--warn'), gold: read('--gold'), text: read('--text') };
        }

        function dotColor(c, state) {
            if (state === 'ingame') return c.good;
            if (state === 'reconnecting' || state === 'connecting' || state === 'queued') return c.warn;
            return c.dim;
        }

        /** The plotted point nearest (x, y), within a small pixel radius, or null. */
        function hitTest(x, y) {
            let best = null;
            let bestD = 12 * 12; // pixel radius squared
            for (const p of plotted) {
                const d = (p.x - x) ** 2 + (p.y - y) ** 2;
                if (d <= bestD) {
                    bestD = d;
                    best = p;
                }
            }
            return best;
        }

        /** Eases current toward target by factor k (0..1), in place. */
        function approach(current, target, k) {
            return current + (target - current) * k;
        }

        /** Grows b (a world-tile box) on whichever axis is too short so its aspect ratio
         * matches the destination rect's (iw x ih CSS pixels), centred on b's own centre --
         * "contain" fit, never stretch. The second real cause behind tonight's "blurry AND not
         * scaled correctly" report: bounds' own aspect ratio (from the account spread, or the
         * MIN_SPAN_TILES floor -- either way essentially arbitrary) was being handed straight
         * to drawImage's destination rect (iw x ih, the panel's own shape) with no correction,
         * so real terrain got stretched non-uniformly on X vs Z any time those two aspect
         * ratios didn't already match, which is most of the time. Used for both the map image
         * crop and the marker X()/Y() transforms so dots stay aligned with the terrain under
         * them either way. */
        function fitBounds(b, iw, ih) {
            const spanX = b.maxX - b.minX;
            const spanZ = b.maxZ - b.minZ;
            if (spanX <= 0 || spanZ <= 0 || iw <= 0 || ih <= 0) return b;
            const cx = (b.minX + b.maxX) / 2;
            const cz = (b.minZ + b.maxZ) / 2;
            const wantRatio = iw / ih;
            const haveRatio = spanX / spanZ;
            let halfX = spanX / 2, halfZ = spanZ / 2;
            if (haveRatio > wantRatio) {
                halfZ = halfX / wantRatio;
            } else {
                halfX = halfZ * wantRatio;
            }
            return { minX: cx - halfX, maxX: cx + halfX, minZ: cz - halfZ, maxZ: cz + halfZ };
        }

        function tick(now) {
            raf = 0;
            if (ctx.isVisible?.() === false || document.hidden) { lastFrame = 0; return; }
            raf = requestAnimationFrame(tick);
            if (lastFrame && now - lastFrame < 1000 / 30) return;
            const dt = lastFrame ? now - lastFrame : 16;
            lastFrame = now;
            // A frame-rate-independent ease: the fraction of the remaining distance closed
            // this frame, so a slow browser (bigger dt) still lands on time rather than
            // crawling forever.
            const k = 1 - Math.exp(-dt / EASE_MS);

            const store = latest;
            if (!store) return;
            // Real tile map takes over entirely once available — see the LEAFLET_JS block's
            // doc comment. Leaflet manages its own render loop (pan/zoom, tile loading) once
            // created, so this rAF loop's only job for that mode is to kick off initLeaflet();
            // marker positions are refreshed from update(store) below, at the store's own poll
            // cadence, not every animation frame (a Leaflet DOM/marker update is much heavier
            // than the canvas redraw this loop was designed around).
            if (store.meta?.mapTiles) {
                initLeaflet();
                canvas.hidden = !!leaflet;
                leafletEl.hidden = !leaflet;
                if (leaflet) return;
            }
            const list = pick(store.accounts, ctx.accounts).filter(a => a.pos);
            const present = new Set(list.map(a => a.name));
            for (const name of eased.keys()) if (!present.has(name)) eased.delete(name);

            // Bucket every positioned account by which real AREAS box its own x/z actually
            // falls inside (areaFor, above) -- NOT by pos.level, see this file's top-of-file
            // doc comment for why that used to be the wrong signal. groups.outside covers the
            // rare position in neither box.
            const groups = { surface: [], underground: [], outside: [] };
            for (const a of list) groups[areaFor(a.pos.x, a.pos.z)].push(a);
            lastGroups = groups;

            let targetBounds = null;
            if (list.length > 0) {
                // Auto mode prefers the selected account's own real area (so picking an
                // account elsewhere brings its level into view), then falls back to whichever
                // area has more accounts in it -- a tie, or every account being 'outside' both
                // known boxes, favours 'surface' same as the old logic did. A pinned areaMode
                // always wins outright, even onto an empty bucket (an honest "nobody's here"
                // beats silently overriding what the viewer asked to see).
                if (areaMode !== 'auto') {
                    currentArea = areaMode;
                } else {
                    const selected = store.selected && list.find(a => a.name === store.selected);
                    const selArea = selected ? areaFor(selected.pos.x, selected.pos.z) : null;
                    if (selArea && selArea !== 'outside' && groups[selArea].length > 0) {
                        currentArea = selArea;
                    } else if (groups.surface.length === 0 && groups.underground.length === 0) {
                        currentArea = 'outside';
                    } else {
                        currentArea = groups.underground.length > groups.surface.length ? 'underground' : 'surface';
                    }
                }

                const shown = groups[currentArea];
                if (shown.length > 0) {
                    let minX = Infinity, maxX = -Infinity, minZ = Infinity, maxZ = -Infinity;
                    for (const a of shown) {
                        minX = Math.min(minX, a.pos.x);
                        maxX = Math.max(maxX, a.pos.x);
                        minZ = Math.min(minZ, a.pos.z);
                        maxZ = Math.max(maxZ, a.pos.z);
                    }
                    // A little breathing room, and a floor (MIN_SPAN_TILES -- see its own doc
                    // comment) so one lone point or a tight cluster neither divides by zero nor
                    // forces the real map art to be magnified well past what it can actually show.
                    const spanX = Math.max(maxX - minX, MIN_SPAN_TILES);
                    const spanZ = Math.max(maxZ - minZ, MIN_SPAN_TILES);
                    targetBounds = { minX: minX - spanX * 0.15, maxX: maxX + spanX * 0.15, minZ: minZ - spanZ * 0.15, maxZ: maxZ + spanZ * 0.15 };
                }
            }
            if (targetBounds) {
                if (!bounds) bounds = { ...targetBounds };
                else {
                    bounds.minX = approach(bounds.minX, targetBounds.minX, k);
                    bounds.maxX = approach(bounds.maxX, targetBounds.maxX, k);
                    bounds.minZ = approach(bounds.minZ, targetBounds.minZ, k);
                    bounds.maxZ = approach(bounds.maxZ, targetBounds.maxZ, k);
                }
            }
            for (const a of list) {
                const cur = eased.get(a.name);
                if (!cur) eased.set(a.name, { x: a.pos.x, z: a.pos.z });
                else {
                    cur.x = approach(cur.x, a.pos.x, k);
                    cur.z = approach(cur.z, a.pos.z, k);
                }
            }
            draw(groups[currentArea] || []);
        }

        /** Draws the real overview image for currentArea, cropped to view (bounds already
         * corrected to the destination rect's own aspect ratio by fitBounds, above — see its
         * doc comment) and scaled into the (pad, pad, iw, ih) plot rect — so the zoom-to-fit-
         * accounts behaviour keeps working exactly as before, now with real, undistorted
         * terrain under it instead of a flat fill. Returns whether it actually drew anything;
         * the caller falls back to the plain grid when it didn't (image still loading, failed
         * to load, or every account is outside the area's own indexed extent). */
        function drawAreaImage(g, pad, iw, ih, view) {
            const area = AREAS[currentArea];
            const state = AREA_IMAGES[currentArea];
            if (!area || !state || !state.ready || state.failed || !view) return false;

            // Clamp the fitted bounds to the area's own real extent — an account can in
            // principle report a tile outside the indexed region, and the crop math below must
            // not read outside the source image.
            const bx0 = Math.max(view.minX, area.x0);
            const bx1 = Math.min(view.maxX, area.x0 + area.w);
            const bz0 = Math.max(view.minZ, area.z0);
            const bz1 = Math.min(view.maxZ, area.z0 + area.h);
            if (bx1 <= bx0 || bz1 <= bz0) return false;

            const sx = (bx0 - area.x0) * area.pxPerTile;
            const sw = (bx1 - bx0) * area.pxPerTile;
            // Image row 0 is the area's northmost (highest z) tile — mirrors northEdge() in
            // example.invalid's own deaths-map.js (fetched and read directly, not guessed).
            const sy = (area.z0 + area.h - bz1) * area.pxPerTile;
            const sh = (bz1 - bz0) * area.pxPerTile;
            if (sw <= 0 || sh <= 0) return false;

            const dx = pad + ((bx0 - view.minX) / (view.maxX - view.minX)) * iw;
            const dw = ((bx1 - bx0) / (view.maxX - view.minX)) * iw;
            const dy = pad + (1 - (bz1 - view.minZ) / (view.maxZ - view.minZ)) * ih;
            const dh = ((bz1 - bz0) / (view.maxZ - view.minZ)) * ih;

            // The overview art is only area.pxPerTile (0.5) image pixels per game tile, so a
            // fitted crop is routinely magnified 10x or more onto the device-pixel canvas.
            // The 2D context's default bilinear smoothing turned that into the blurry map an
            // operator sees whenever the tile-pyramid (Leaflet) view is not in use. Magnified,
            // draw nearest-neighbour — the same choice the Leaflet tiles make with
            // `image-rendering: pixelated` in style.css — so edges stay sharp; only a
            // minified draw keeps smoothing, where nearest-neighbour would alias instead.
            const dpr = window.devicePixelRatio || 1;
            const magnified = (dw * dpr) / sw > 1 || (dh * dpr) / sh > 1;
            const smoothing = g.imageSmoothingEnabled;
            g.imageSmoothingEnabled = !magnified;
            g.drawImage(state.img, sx, sy, sw, sh, dx, dy, dw, dh);
            g.imageSmoothingEnabled = smoothing;
            return true;
        }

        /** Refreshes areaCtl's three buttons: label + live count, which one (if any) is
         * currently shown (.is-active — pinned or auto-picked, both read the same to the
         * viewer), and hides 'Other' entirely while nothing has ever landed outside the two
         * known boxes so the common case never shows a button for a bucket that's always
         * empty. */
        function updateAreaCtl() {
            const total = lastGroups.surface.length + lastGroups.underground.length + lastGroups.outside.length;
            areaCtl.hidden = total === 0;
            if (lastGroups.outside.length > 0) areaBtns.outside.hidden = false;
            for (const key of Object.keys(areaBtns)) {
                const btn = areaBtns[key];
                if (key === 'outside' && btn.hidden) continue;
                btn.textContent = `${AREA_LABEL[key]} (${lastGroups[key].length})`;
                btn.classList.toggle('is-active', total > 0 && currentArea === key);
                btn.title = areaMode === key
                    ? `Pinned to ${AREA_LABEL[key]} — click again to follow automatically`
                    : `Show only accounts in ${AREA_LABEL[key].toLowerCase()}`;
            }
        }

        function draw(list) {
            const store = latest;
            updateAreaCtl();
            const w = wrap.clientWidth;
            const h = wrap.clientHeight;
            if (w < 20 || h < 20) return;
            const dpr = window.devicePixelRatio || 1;
            if (canvas.width !== Math.round(w * dpr) || canvas.height !== Math.round(h * dpr)) {
                canvas.width = Math.round(w * dpr);
                canvas.height = Math.round(h * dpr);
            }
            const g = canvas.getContext('2d');
            g.setTransform(dpr, 0, 0, dpr, 0, 0);
            const c = palette();
            g.fillStyle = c.plot;
            g.fillRect(0, 0, w, h);

            plotted = [];
            const pad = 24;
            const totalPositioned = lastGroups.surface.length + lastGroups.underground.length + lastGroups.outside.length;
            if (!list || list.length === 0 || !bounds) {
                g.fillStyle = c.dim;
                g.font = '11px Helvetica, Arial, sans-serif';
                g.textAlign = 'center';
                g.textBaseline = 'middle';
                // Two different honest-empty states: genuinely nobody positioned yet, versus
                // accounts positioned but all on a different bucket than the one pinned/shown
                // right now (areaMode pinned onto an empty bucket) — the latter still needs a
                // way back, hence pointing at the toggle rather than just saying "no accounts".
                g.fillText(
                    totalPositioned === 0 ? 'no positioned accounts yet' : `no accounts on ${AREA_LABEL[currentArea].toLowerCase()} right now`,
                    w / 2, h / 2
                );
                foot.textContent = totalPositioned === 0
                    ? '0 accounts positioned'
                    : `0 on ${AREA_LABEL[currentArea]} — ${totalPositioned} elsewhere, use the buttons above to switch`;
                tip.hidden = true;
                return;
            }

            const iw = w - pad * 2;
            const ih = h - pad * 2;
            // view is bounds grown on whichever axis is too short to match the destination
            // rect's own aspect ratio (fitBounds, above) — drawn/plotted against instead of the
            // raw eased bounds so real terrain is never stretched non-uniformly, and so a
            // marker's dot still lands exactly on the terrain under it either way.
            const view = fitBounds(bounds, iw, ih);
            // World z increases "north"; plotted with north up, same convention as the game's
            // own minimap, even though the fitted box (view) is a zoom-to-fit window, not the
            // real map's full extent.
            const X = x => pad + ((x - view.minX) / (view.maxX - view.minX)) * iw;
            const Y = z => pad + (1 - (z - view.minZ) / (view.maxZ - view.minZ)) * ih;

            const drewRealMap = drawAreaImage(g, pad, iw, ih, view);
            if (!drewRealMap) {
                // Real map art not ready (still loading, or example.invalid unreachable) — the
                // original flat coordinate plot, faint grid and all, so the panel never goes
                // blank or breaks while that's true.
                g.strokeStyle = c.grid;
                g.lineWidth = 1;
                for (let i = 1; i < 4; i++) {
                    const gx = Math.round(pad + (iw * i) / 4) + 0.5;
                    const gy = Math.round(pad + (ih * i) / 4) + 0.5;
                    g.beginPath();
                    g.moveTo(gx, pad);
                    g.lineTo(gx, h - pad);
                    g.moveTo(pad, gy);
                    g.lineTo(w - pad, gy);
                    g.stroke();
                }
            }

            // Dots get a dark halo when they're sitting on real map art (photo-real terrain
            // under a flat-coloured dot needs more separation than the plain dark plot did) so
            // they stay legible over bright terrain like desert or snow.
            g.save();
            if (drewRealMap) {
                g.shadowColor = 'rgba(0, 0, 0, 0.85)';
                g.shadowBlur = 4;
            }

            for (const a of list) {
                const p = eased.get(a.name);
                if (!p) continue;
                const x = X(p.x);
                const y = Y(p.z);
                plotted.push({ x, y, a });
                const selected = a.name === store.selected;
                // A soft halo under every dot reads as "alive" even at a glance, and a bigger,
                // brighter one under the selected account draws the eye straight to it.
                g.beginPath();
                g.arc(x, y, selected ? 10 : 7, 0, Math.PI * 2);
                g.fillStyle = dotColor(c, a.state);
                g.globalAlpha = 0.22;
                g.fill();
                g.globalAlpha = 1;
                g.beginPath();
                g.arc(x, y, selected ? 6 : 4.5, 0, Math.PI * 2);
                g.fillStyle = dotColor(c, a.state);
                g.fill();
                g.lineWidth = selected ? 2 : 1;
                g.strokeStyle = selected ? c.gold : c.plot;
                g.stroke();
                if (ctx.settings.labels) {
                    g.font = '10px Helvetica, Arial, sans-serif';
                    g.textAlign = 'left';
                    g.textBaseline = 'middle';
                    g.fillStyle = selected ? c.gold : c.text;
                    g.fillText(a.name, x + 8, y);
                }
            }
            g.restore();

            // otherCount: accounts that ARE positioned but in a different bucket than the one
            // being shown — distinct from "without a known position" (no pos report at all) so
            // the two very different reasons an account isn't on screen don't get conflated.
            const otherCount = totalPositioned - list.length;
            const unpositioned = store.accounts.length - totalPositioned;
            foot.textContent = `${list.length} account${list.length === 1 ? '' : 's'} on ${AREA_LABEL[currentArea]}`
                + (otherCount > 0 ? `, ${otherCount} on another level` : '')
                + (unpositioned > 0 ? `, ${unpositioned} without a known position` : '');

            const hit = hover ? hitTest(hover.x, hover.y) : null;
            if (!hit) {
                tip.hidden = true;
                return;
            }
            tip.textContent = '';
            tip.append(el('div', 't', hit.a.name));
            tip.append(el('div', '', hit.a.activity || '(no activity reported)'));
            tip.append(el('div', '', `${hit.a.pos.x}, ${hit.a.pos.z}, level ${hit.a.pos.level}`));
            tip.append(statePill(hit.a.state));
            tip.hidden = false;
            const tw = tip.offsetWidth || 140;
            tip.style.left = `${hit.x + 12 + tw > w ? Math.max(0, hit.x - 12 - tw) : hit.x + 12}px`;
            tip.style.top = `${Math.max(0, hit.y - 10)}px`;
        }

        // Every panel resize (edit layout, the window, the grid) re-fits the map: Leaflet re-reads
        // its container size, which also resizes the WebGL renderer's canvas and fetches the
        // newly exposed tiles; the canvas fallback simply redraws at the new size.
        const resize = new ResizeObserver(() => {overlay.fit();if(leaflet)leaflet.map.invalidateSize({pan:false});else draw(lastGroups[currentArea] || []);});
        resize.observe(wrap);
        canvas.addEventListener('pointermove', ev => {
            const r = canvas.getBoundingClientRect();
            hover = { x: ev.clientX - r.left, y: ev.clientY - r.top };
        });
        canvas.addEventListener('pointerleave', () => {
            hover = null;
        });
        // Click-to-select today; hitTest already resolves screen coords back to an account (and
        // that account carries its own world tile in .pos), so a future "walk here" action needs
        // only to read hit.a.pos / a chosen empty-space world coordinate from here, not a rewrite
        // of the hit-testing itself.
        canvas.addEventListener('click', ev => {
            const r = canvas.getBoundingClientRect();
            const hit = hitTest(ev.clientX - r.left, ev.clientY - r.top);
            if (hit) ctx.store.select(hit.a.name);
        });

        ctx.onVisibility?.(visible => {
            if (visible && !destroyed) {
                if (!raf) raf = requestAnimationFrame(tick);
                if (leaflet && !playersTimer) pollPlayers();
            }
            else if (!visible) { cancelAnimationFrame(raf); raf = 0; lastFrame = 0; }
            if (!visible) { clearTimeout(playersTimer); playersTimer = 0; }
        });
        raf = requestAnimationFrame(tick);

        return {
            update(store, why) {
                latest = store;
                if (!anyOf(why, 'init', 'status', 'select', 'meta')) return;
                if (store.meta?.mapTiles && leaflet) {
                    drawLeaflet(pick(store.accounts, ctx.accounts).filter(a => a.pos), store);
                }
            },
            destroy() {
                destroyed = true;
                clearTimeout(playersTimer);
                resize.disconnect();
                cancelAnimationFrame(raf);
                if (leaflet) leaflet.map.remove();
            }
        };
    }
};
