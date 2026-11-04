// Loot — every loot source across the fleet in one showcase panel, from GET /api/loot (see
// internal/controlpanel/lootstats.go): fishing spots (loot.KindFishing, measured by
// internal/scripts/fisherrunner/catch.go — the fleet's main activity, and every spot npc shares
// the name "Fishing spot", so they arrive as one card carrying all of their models and all of
// their catch), npcs killed or pickpocketed (by name and npc type id) and openables (Casket,
// Oyster), each with the drops it gave, and random events (Strange box, Mysterious Old Man,
// Drunken Dwarf, Strange plant, Mime, Genie, Maze) with the rewards each paid: loot.KindRandomEvent
// sources, measured by internal/fleet/randomloot.go. ALL of them are ONE "Random events" card
// (a user request: "all random events should be given just 1 consolidated panel"): the events
// total, what they were worth, every item they paid, and a row per event inside it (the same
// breakdown the Thieving card has). The card's ring stands each event's own model (source.model
// 'npc', or 'obj' for the Strange box); the Maze has neither and shows as a row, and as the
// owner's map of it only while it is the card's one event (see STILLS).
//
// Each source is a card: its real cache model on a stone turntable (npcs play their idle or
// walk sequence; Casket and Oyster are their ground-item models, as they look dropped), the
// name in the game's font with the combat level coloured the way the client colours it, the
// kill / pickpocket / open count, and the drops as a bank grid with real item icons and
// client-style stack numbers. Above the cards: fleet totals with a gentle count-up, the top
// drops with sparklines from the history buckets, and a ticker of recent notable drops.
//
// Models: ONE WebGL context for the whole panel (web/loot-gl.js), a canvas over the card
// list drawing each on-screen card's stage through a scissored viewport; geometry from the
// capture adapter (web/loot-models.js), decoded once per page and only once this panel is on
// screen with something to show. Drawing runs at most ~30 fps, only while a model is on
// screen and the panel and tab are visible, and not at all under prefers-reduced-motion
// (still poses, redrawn only when something changes). Without hardware WebGL each card
// paints a still pose once with Canvas 2D; without the adapter it shows an icon.
//
// The drop grids stay DOM: the item icons are DOM images (web/itemicons.js), the cells need
// keyboard focus, hover tooltips and accessible names, and they change every ~10 s, not per
// frame. Drawing them through gpu-table.js would only move a cheap, static DOM subtree onto
// the GPU while losing those.
//
// Controls: period (lifetime, persisted across restarts, vs since this restart), source type,
// source sort (count, total value, gp/hour or most recent), item sort (count or value), an item
// search that narrows the cards to the sources that drop it, and a per-account table. Toolbar
// choices are this viewer's own (localStorage); the gear settings set the defaults.
//
// Clearing (a user request: "loot panel should have ability to reset/clear panels and certain
// stacks of items from npcs"): earlier builds filed loot on cards it does not belong to, and the
// lifetime totals are persisted, so each card has a small "clear" and each item stack a small cross,
// both behind a confirm(). They POST /api/loot/clear (internal/controlpanel/lootclear.go says what
// that removes and why nothing comes back) and draw the loot body it answers with. Both are
// delegated from the card list, so a card or a stack costs one button and no listener.
//
// Values are the real market prices the server now resolves (internal/controlpanel/marketprice.go,
// the median of ~1,300 recent trades, falling back to high alchemy then the cache cost), not the
// cache's unit cost — an uncut sapphire trades at 800 against a cache cost of 25. That is what
// makes "most valuable" and "best gp/hour" worth offering at all, and it is why each card carries
// its own gp/h: it is the one figure that makes two sources comparable.

import { anyOf, el, button, fmtCount, fmtDuration, table } from '../util.js';
import { itemIconWrap, skillMonogram } from '../itemicons.js';
import { createLootRenderer, drawModelSprite, loopLength, viewableArc } from '../loot-gl.js';
import { animationWanted, cachedAnimation, cachedMesh, loadAnimation, loadMeshes, meshWanted, modelsUsable, textureAverages } from '../loot-models.js';
import { drawRunecraft, isRunecraftSource, runecraftModel } from './lootrunecraft.js';
import { deathsModel, drawDeaths } from './lootdeaths.js';

const REFRESH_EVERY_MS = 8000; // the server refreshes its figures every 10 s
const FRAME_MS = 1000 / 30; // the spin's frame cap
const SPIN = 0.5; // radians per second: a full turn in ~12.5 s
// A model the cache only authored from some angles rocks through the arc it does support rather
// than turning all the way round (see viewableArc). One sweep and back in ~11 s, which is close
// enough to the spin's own pace to read as the same kind of motion rather than as a turntable
// that jammed. Eased at the ends by the sine itself, so it turns around rather than snapping.
const SWAY = (Math.PI * 2) / 11;
const REST_YAW = 0.5; // a three-quarter view, the pose when nothing turns
const COUNT_UP_MS = 900;
const COINS = 995;
const STRANGE_BOX = 3062; // the Random events card's picture when no model is on screen

// Rare drops: key halves, gems above sapphire, the cosmic talisman and pearls. A rise in
// any of these makes its cell shimmer and puts the drop in the ticker.
const RARE = new Set([985, 987, 1621, 1619, 1617, 1454, 411, 413]);

const PERIODS = [
    { value: 'lifetime', label: 'Lifetime (persisted)' },
    { value: 'sinceRestart', label: 'Since this restart' }
];
const KINDS = [
    { value: 'all', label: 'All' },
    { value: 'gathering', label: 'Gathering' },
    // Production: the skills that CONSUME an item to make another one — Fletching today. Its own
    // filter entry rather than a second meaning for Gathering; internal/loot/production.go argues
    // that line out in full.
    { value: 'production', label: 'Production' },
    // Runecrafting: the Runecraft card (a row per altar) and the Essence supply card, under a section that adds the runes by
    // type, the essence, each account and the mule's stock (panels/lootrunecraft.js; internal/loot/runecraft.go).
    { value: 'runecraft', label: 'Runecrafting' },
    // Deaths: every death with the items it dropped and whether the dead account or another account of the fleet picked them up again
    // (panels/lootdeaths.js; internal/deathloot). It replaces the source cards, which hold no deaths.
    { value: 'deaths', label: 'Deaths' },
    // Teleports: every teleport cast with the runes it cost, a tab of costs rather than loot
    // (internal/loot/teleport.go).
    { value: 'teleport', label: 'Teleports' },
    // Consumables: every food and drink eaten or drunk with its coin value, a tab of costs too
    // (internal/loot/consumable.go).
    { value: 'consumable', label: 'Consumables' },
    // Shop purchases: every item bought from any shop, with the coins paid and what it is worth, one
    // combined card (internal/loot/shop.go).
    { value: 'shop', label: 'Shop purchases' },
    // Thieving: every pickpocket and every stall or chest steal, on ONE card (owner, 2026-09-23:
    // "maybe combine pickpocketing to one panel"). internal/loot/thieving.go says why it is a kind
    // of its own. With pickpockets gone from it, the npc filter is kills alone, hence "Combat".
    { value: 'thieving', label: 'Thieving' },
    { value: 'npc', label: 'Combat' },
    { value: 'openable', label: 'Openables' },
    // Objs taken off the ground that no kill of ours dropped: the Kalphite Queen's Potato cacti,
    // a knife spawn, stray bones (internal/loot/pickup.go).
    { value: 'pickup', label: 'Ground items' },
    { value: 'randomevent', label: 'Random events' },
    { value: 'trail', label: 'Treasure trails' }
];
// Value sorting only became worth offering once loot was priced at what it actually trades for
// (internal/controlpanel/marketprice.go, the median of ~1,300 recent trades): at the cache's
// unit cost an uncut sapphire was 25 against the 800 it really fetches, so "most valuable" and
// "best gp/hour" would both have ranked sources by the wrong number entirely.
const SOURCE_SORTS = [
    { value: 'count', label: 'Most kills / opens' },
    { value: 'value', label: 'Most valuable' },
    { value: 'rate', label: 'Best gp/hour' },
    { value: 'recent', label: 'Most recent' }
];
// The "Best gp/hour" sort ranks by whatever that period's card actually shows: a real rate under
// "since this restart", a lifetime average under "lifetime". See perHour.
const ITEM_SORTS = [
    { value: 'count', label: 'Count' },
    { value: 'value', label: 'Value' }
];
const POSES = [
    { value: 'ready', label: 'Idle (readyanim)' },
    { value: 'walk', label: 'Walking (walkanim)' }
];

const VERBS = { kill: ['Killed', 'kill', 'kills'], pickpocket: ['Pickpocketed', 'pickpocket', 'pickpockets'], steal: ['Stole from', 'steal', 'steals'], open: ['Opened', 'open', 'opens'], complete: ['Completed', 'trail', 'trails'], event: ['Completed', 'event', 'events'], fish: ['Caught', 'catch', 'catches'], claim: ['Claimed', 'claim', 'claims'], mend: ['Mended', 'net', 'nets'], chop: ['Chopped', 'log', 'logs'], mine: ['Mined', 'rock', 'rocks'], fletch: ['Fletched', 'material', 'materials'], craft: ['Crafted', 'material', 'materials'], smith: ['Smelted', 'bar', 'bars'], superheat: ['Superheated', 'cast', 'casts'], 'superheat-runes': ['Superheated', 'cast', 'casts'], alch: ['Alched', 'cast', 'casts'], 'alch-runes': ['Alched', 'cast', 'casts'], lowalch: ['Alched', 'cast', 'casts'], 'lowalch-runes': ['Alched', 'cast', 'casts'], clean: ['Cleaned', 'herb', 'herbs'], bury: ['Buried', 'bone', 'bones'], take: ['Picked up', 'pickup', 'pickups'], teleport: ['Cast', 'teleport', 'teleports'], combat: ['Spells on', 'kill', 'kills'], eat: ['Ate', 'food', 'food'], drink: ['Drank', 'drink', 'drinks'], cook: ['Cooked', 'raw food', 'raw food'], buy: ['Bought', 'item', 'items'], pick: ['Picked', 'plant', 'plants'], forge: ['Worked', 'bar', 'bars'], runecraft: ['Crafted', 'craft', 'crafts'], handover: ['Handed over', 'essence', 'essence'] };

// A card that merges more than one ACTION needs a verb of its own: the Thieving card holds
// pickpockets and stall steals, and taking the first source's verb said "Pickpocketed 19,124"
// over a total a third of which were steals.
// The short names an npc card's experience line uses, per skill (internal/loot Drop.Skills).
const SKILL_ABBR = { Attack: 'Att', Strength: 'Str', Defence: 'Def', Ranged: 'Rng', Magic: 'Mag', Hitpoints: 'HP' };

const SKILL_VERBS = { Thieving: ['Thieved', 'theft', 'thefts'], Magic: ['Cast', 'cast', 'casts'] };

/** The verb a card or ticker line uses: the card's own when it merges several actions. */
function verbFor(s) {
    const actions = s.actions ?? [s.source?.action];
    if (actions.length > 1) return SKILL_VERBS[s.source?.skill] ?? ['Took', 'take', 'takes'];
    const a = s.source?.action;
    return VERBS[a] ?? [a, a, `${a}s`];
}

// Treasure trails used to be their own placeholder panel (type 'trails', which panels/index.js
// now aliases to this one). No clue solver runs on the accounts yet; once one reports each
// finished trail as a loot.KindTrail source, its rewards appear here like any other source.
const TRAILS_EMPTY = 'no treasure trail finished yet: no clue solver runs on the accounts. Once one does, each finished trail and what it paid shows here.';

// The client's combat-level colours (Client.combatColourCode, graphics/Colour.ts): the npc's
// level against the viewer's, green shades when it is lower, yellow when equal, orange then
// red when higher. This panel has no viewer, so it compares with a reference level: the
// setting below, default 3 — a fresh account's level, which is what most of this fleet's
// skilling accounts are.
export function combatColour(viewer, other) {
    const diff = viewer - other;
    if (diff < -9) return '#ff0000';
    if (diff < -6) return '#ff3000';
    if (diff < -3) return '#ff7000';
    if (diff < 0) return '#ffb000';
    if (diff > 9) return '#00ff00';
    if (diff > 6) return '#40ff00';
    if (diff > 3) return '#80ff00';
    if (diff > 0) return '#c0ff00';
    return '#ffff00';
}

// Stack numbers as users asked: yellow below 100,000, white "K" from 100,000, green "M"
// from 10,000,000 (later clients' colours). The 289 client itself draws every stack number
// yellow (Client.invNumber with Colour.YELLOW); the K/M text is the same.
export function stackLabel(n) {
    if (n < 100000) return { text: String(n), cls: 'stack-y' };
    if (n < 10000000) return { text: `${Math.floor(n / 1000)}K`, cls: 'stack-w' };
    return { text: `${Math.floor(n / 1000000)}M`, cls: 'stack-g' };
}

function fmtRate(perHour) {
    if (perHour === null || perHour === undefined) return '–';
    if (!(perHour > 0)) return '0';
    if (perHour >= 100) return fmtCount(Math.round(perHour));
    if (perHour >= 10) return perHour.toFixed(1);
    return perHour.toFixed(2);
}

// The shortest window an hourly figure may be extrapolated from.
//
// perHour used to floor its window at one minute, which multiplied a source's whole total by up
// to 60 the moment it first appeared. Below this, the source simply has no rate yet and says so.
const RATE_MIN_HOURS = 5 / 60;

// Experience was first measured on the skill cards and the npc cards by bot/lootxp.go, written at 03:01 on
// 7 Oct 2026 (UTC+8), so no experience figure can cover any time before it: a card's `first` is when its
// loot was first seen, weeks earlier, and dividing the experience measured since by that whole span read
// 0.03 xp per Guard pickpocket live. A lifetime xp/h counts from this moment at the earliest (the Prayer
// card's burials always carried their experience and are not given this floor). It is a floor, not the
// moment tracking really began, so the figure is still an under-estimate until the file is older than it.
export const XP_TRACKED_FROM_MS = Date.UTC(2026, 9, 6, 19, 1);

/**
 * count per hour over [max(since, floor), now], or null when that window is under RATE_MIN_HOURS (no rate
 * yet). Pure, so the window rule is checked without a browser.
 */
export function hourlyRate(count, now, since, floor = 0) {
    const hours = (now - Math.max(since, floor)) / 3600000;
    if (!(hours >= RATE_MIN_HOURS)) return null;
    return count / hours;
}

function ago(ms) {
    return ms < 1500 ? 'just now' : `${fmtDuration(ms)} ago`;
}

// A random event says which model it has (source.model); every other kind is its own model kind.
const isNpcModel = s => s.kind === 'npc' || s.model === 'npc';
// A gathering source's tree or rock is a scenery LOC, which is its own model kind.
const isLocModel = s => s.model === 'loc';
const hasModel = s => s.id >= 0 && (s.kind !== 'randomevent' || !!s.model);

/**
 * Sources the cache has no model for at all, shown as a framed map card rather than a monogram.
 *
 * The Maze is neither an npc nor an item — internal/loot/randomevent.go gives it Model "" and
 * ID -1 — so there is nothing to put on a turntable, and it fell back to its initials beside
 * twelve real models. The owner supplied the maze's own map for it.
 *
 * It is served from this dashboard's own origin (web/maze-map.png, embedded by server.go's
 * //go:embed web) rather than hotlinked: the page's CSP is img-src 'self' data: (blob: only
 * with the capture adapter installed, see internal/controlpanel/auth.go), so a third-party URL
 * would simply be blocked, and widening the policy to a host outside our control to show one
 * decorative image is not a trade worth making.
 *
 * Presentation: a flat picture among spinning turntables reads as a failure unless it is framed
 * as deliberately still. So it hangs in a stone frame on the same lit stage and sways on the
 * same ~12.5s period as the models turn (CSS, so it costs no frame budget and keeps working
 * with WebGL off), never far enough to go edge-on the way a real flat quad would. Dragging the
 * stage steers it, exactly as dragging a model stage turns the model.
 */
const STILLS = { maze: { src: 'maze-map.png', alt: 'Map of the random-event maze, seen from above' } };
// `rows` is how many events the card holds. Every random event is one card now, so the map is only
// the picture of a card whose one event is the maze; beside other events it is a row, like the rest.
const stillFor = (s, rows = 1) => (s.kind === 'randomevent' && !hasModel(s) && rows <= 1 && STILLS[(s.name ?? '').toLowerCase()]) || null;

/** What a source's drops are worth, gross, at the market prices the server now resolves. */
const sourceGross = s => (s.items ?? []).reduce((n, it) => n + (it.value ?? 0) * (it.count ?? 0), 0);

/**
 * What a source EARNED: its drops less whatever it consumed to get them (owner, 2026-09-21 —
 * "loot panel should consider the cost of a log ... when its putting a profit price", and the
 * crystal key every chest open costs).
 *
 * `cost` is the server's (internal/controlpanel/lootcost.go), already priced and already zero
 * when the input has no trustworthy price. It is subtracted rather than recomputed here because
 * the panel has no price table of its own — and because `costKnown` false means "nobody knows
 * what the input was worth", which is a different statement from "it was free" and is shown as
 * such on the card rather than folded into the number.
 */
const sourceValue = s => sourceGross(s) - (s.cost ?? 0);

// Kinds whose card shows its source large with its yields small around it, rather than the
// source alone (owner, 2026-09-20: "for fishing spot display 1 fishing spot and smaller models
// around it of what any of the fishing spots can provide").
//
// These are exactly the kinds whose source carries a SKILL and whose card is therefore titled by
// that skill rather than by a name — a spot with its fish around it, a tree with its logs, a pile
// of logs with the arrow shafts it was cut into. Named SKILL_CARDS rather than GATHERING since
// 2026-09-21, when Fletching became the first production kind and the arrangement turned out to
// suit it for the same reason it suits a fishing spot.
const SKILL_CARDS = new Set(['gathering', 'production']);

// Kinds whose card is a COST, not loot: nothing is looted, something is spent (the runes a teleport
// cast), so the card leads with what it spent instead of a negative gp figure.
const COST_KINDS = new Set(['teleport', 'consumable']);
// Kinds whose card MOVES something rather than making or spending it: the Essence supply card counts the essence handed to the
// mule, which was valued when it was mined, so the card leads with a count and a rate and shows no gp (internal/loot/runecraft.go).
const COUNT_KINDS = new Set(['supply']);
// Kinds whose card has a row per item or spell and so keeps only its busiest rows (compactParts): the
// cost cards and the Shop purchases card, which has a row per item bought.
const CAPPED_KINDS = new Set([...COST_KINDS, 'shop']);

// The skills web/skillicons has an icon for, which a skill-titled card falls back to without
// models. Only these, so a skill without an icon still gets its monogram rather than a broken image.
const SKILL_ICONS = new Set(['Fishing', 'Woodcutting', 'Mining', 'Fletching', 'Crafting', 'Smithing', 'Herblore', 'Magic', 'Prayer', 'Thieving', 'Cooking', 'Runecraft']);

/**
 * The retired "fishing" kind, folded onto the gathering card it belongs to.
 *
 * internal/loot dropped KindFishing when all three gathering skills became one kind, and
 * migrated nothing, so every catch persisted before that kept the dead kind and grouped under
 * it — which is why the owner saw a "Fishing" card and a separate "Fishing spot" card holding
 * the same activity. internal/controlpanel/lootstats.go now migrates those records as it reads
 * them back (loot.Migrate), so this page should never meet one.
 *
 * It is here anyway because the cost is one comparison and the failure it prevents is the panel
 * silently splitting the fleet's main activity across two cards again. Kept identical to
 * loot.Migrate on purpose: the same rewrite, at the other end of the wire.
 */
const LEGACY_FISHING = 'fishing';
// The same two rewrites internal/loot's Migrate and internal/controlpanel's restore make, for the
// same reason as the fishing one above, and so this page can be previewed against a server that
// has not been restarted onto them yet (cmd/panel-assets-proxy): a pickpocket that was kind "npc"
// and a stall steal that was kind "loc" belong on the Thieving card, and the bones ScarabSwarm filed
// under an openable named "Ground" (id -1) belong on the Cow that dropped them.
const LEGACY_GROUND_KILL = { kind: 'npc', action: 'kill', name: 'Cow', id: 81, level: 2 };
// Random events a past build recorded under a npc id that is not theirs, as loot.CanonicalEventSource
// rewrites them when the server reads its file back: the Genie as 411, which is macro_SWARM (so the
// panel drew it as a Swarm), and the Mysterious Old Man as 2344, which is no npc at all. Only these
// exact pairs, not the whole reward table: a page that copied the table would undo the server's the
// day an id there is corrected.
const RETIRED_EVENT_IDS = new Map([['genie:411', 409], ['mysterious old man:2344', 410]]);
export function normaliseSource(src) {
    if (!src) return src;
    if (src.kind === 'randomevent') {
        const id = RETIRED_EVENT_IDS.get(`${(src.name ?? '').trim().toLowerCase()}:${src.id}`);
        if (id !== undefined) return { ...src, id };
    }
    if (src.kind === LEGACY_FISHING) return { ...src, kind: 'gathering', skill: 'Fishing', action: 'fish', model: 'npc' };
    if (!src.skill && src.kind === 'npc' && src.action === 'pickpocket') return { ...src, kind: 'thieving', skill: 'Thieving', model: 'npc' };
    if (!src.skill && src.kind === 'loc' && src.action === 'steal') return { ...src, kind: 'thieving', skill: 'Thieving', model: 'loc' };
    // An oyster opened is the Oyster pearls row of the Fishing card; the closed oyster is never shown
    // (loot.Migrate does the same on the server).
    if ((src.kind === 'openable' || src.kind === 'gathering') && src.action === 'open' && (src.name ?? '').trim().toLowerCase() === 'oyster') return { ...src, kind: 'gathering', skill: 'Fishing', model: 'obj', name: 'Oyster pearls', id: 411 };
    // A Barbarian woman's kills are a Barbarian's: the same loot table (loot.sharedKill).
    if (src.kind === 'npc' && src.action === 'kill' && (src.name ?? '').trim().toLowerCase() === 'barbarian woman') return { ...src, name: 'Barbarian' };
    if (src.kind === 'openable' && src.action === 'open' && src.id === -1 && (src.name ?? '').trim().toLowerCase() === 'ground') return { ...LEGACY_GROUND_KILL };
    return src;
}

// How many yields a gathering card stands around its source, and how big each one is drawn.
//
// A satellite is scaled to a TARGET SIZE rather than by a fixed factor, because the item models
// differ wildly: a Casket is 0.34 tiles tall and a Raw shrimp 0.16, so one fixed factor either
// leaves the fish as specks or makes the casket a boulder. Sizing each to the same height makes
// the ring read as a row of yields instead of a scatter.
const SATELLITES = 6;
const SATELLITE_SIZE = 0.42;
const SATELLITE_MIN = 0.45;
const SATELLITE_MAX = 4;

/**
 * Where each model stands on the card's ONE pedestal.
 *
 * Every multi-model card used to split its stage into equal columns and draw a pedestal in each
 * (owner, 2026-09-20: "when displaying multiple models on one panel, have them only sit on 1
 * pedestel"). Now there is one plinth and the models are arranged on it, which is the same
 * mechanism for both shapes users asked for:
 *
 *   * a gathering card — its source in the middle at full size, its yields small on a ring
 *     around it, so the Fishing spot card reads as a spot and what comes out of it;
 *   * a merged same-name card — the three "Man" ids, genuinely three separate people, evenly
 *     spaced on the ring at full size with nothing in the middle.
 *
 * The ring is sized from the models' own footprints rather than a fixed radius, so neighbours do
 * not intersect whether they are four fishing spots or six raw fish. groupBounds in loot-gl.js
 * then frames the whole arrangement, so a card with satellites simply pulls the camera back.
 */
function ringRadius(feet, inner) {
    // Enough circumference for every footprint side by side, and never inside the middle model.
    const need = feet.reduce((n, f) => n + f * 2.3, 0) / (Math.PI * 2);
    return Math.max(need, inner + (Math.max(0, ...feet) || 0) + 0.08);
}

export function arrangePlaces(primary, satellites, boundsOf, animOf) {
    const places = [];
    const sats = satellites.slice(0, SATELLITES);
    const foot = k => boundsOf(k)?.foot ?? 0.3;
    if (primary) places.push({ model: primary, anim: animOf(primary) ?? '', scale: 1, ox: 0, oz: 0 });
    if (!sats.length) return places;
    // Satellites are sized to look alike; a ring of equals is the models at their own size.
    const scaleOf = k => {
        if (!primary) return 1;
        const b = boundsOf(k);
        if (!b) return 1;
        const size = Math.max(b.top, b.foot * 1.4, 0.02);
        return Math.min(SATELLITE_MAX, Math.max(SATELLITE_MIN, SATELLITE_SIZE / size));
    };
    const scales = sats.map(scaleOf);
    const feet = sats.map((k, i) => foot(k) * scales[i]);
    const r = ringRadius(feet, primary ? foot(primary) : 0);
    sats.forEach((k, i) => {
        // Started a third of a step round so a two-model ring is not edge-on to the camera.
        const a = ((i + 0.33) / sats.length) * Math.PI * 2;
        places.push({ model: k, anim: animOf(k) ?? '', scale: scales[i], ox: Math.cos(a) * r, oz: Math.sin(a) * r });
    });
    return places;
}

/**
 * Sources whose several models are the SAME creature at different times, rather than different
 * npcs that happen to share a name.
 *
 * A merged card normally splits its stage into one column per appearance, which is right for
 * the three "Man" ids — three people, side by side. It is wrong for the Kalphite Queen, whose
 * two level-333 ids are one creature in sequence: 1158 is the walking first form and 1160 the
 * flying second, 255 hitpoints each, which is why the second exists at all. Users asked for
 * that to be shown as it happens (2026-09-20): "spinning evolving walking and then flying from
 * her 2nd form rotating between the two models, fading cleanly". So these cards keep ONE stage
 * and show one form at a time, dissolving between them.
 *
 * `extra` are model ids to load that no loot source will ever report. Content 289 carries npc
 * 1159, a level-296 Kalphite Queen with no options at all — the form she is in while the first
 * one dies. Nothing kills it, so it is never a source, but it is the change itself and the card
 * passes through it between the two fighting forms.
 *
 * There is NO death animation to play, and it is worth being exact about why rather than
 * leaving it looking like an oversight: the capture adapter exports only an npc type's
 * readyanim and walkanim (tools/capture/client.ts, entityAnims), so no death sequence reaches
 * this page at all. Showing one means rebuilding that bundle. The dying form plus a clean
 * dissolve is what is actually available, and it reads as the change he asked for.
 */
const FORMS = {
    'kalphite queen': { order: ['npc:1158', 'npc:1159', 'npc:1160'], extra: [1159], holdMs: 3800, fadeMs: 650 }
};
const formsFor = s => (s.kind === 'npc' && FORMS[(s.name ?? '').toLowerCase()]) || null;

/**
 * Which form a forms card is showing, and how solid it is, at `clock` ms.
 *
 * Each form gets a slot of fade-in, hold, fade-out, so the two never overlap on the stage: the
 * outgoing one is gone before the incoming one appears. That is what keeps the dissolve clean
 * with depth testing on — two half-transparent models occupying the same space at once is
 * exactly what looks broken.
 */
// How many of an npc's sequences a card cycles through, and roughly how long each is held.
//
// Capped at three because every sequence loaded is another set of per-frame position buffers in
// memory and on the GPU, and the fourth and fifth a human model offers are two more sideways
// shuffles. Three gives a stand, a walk and one more — which is what the owner was after
// ("looks cooler than just spinning walking on the spot") without tripling the panel's memory.
const PLAYLIST_MAX = 3;
const PLAYLIST_HOLD = 5200;

/**
 * Which sequence a card is playing at `clock`, and how far into it.
 *
 * Each entry is held for a whole number of its own loops, so the switch lands where the loop
 * ends rather than mid-stride. Entries are {key, loopMs}; one with no length falls back to the
 * hold so a sequence whose delays failed to load cannot divide by zero or spin the list.
 */
export function playlistAt(entries, clock) {
    if (!entries.length) return null;
    const spans = entries.map(e => {
        const loop = e.loopMs > 0 ? e.loopMs : PLAYLIST_HOLD;
        return Math.max(loop, Math.ceil(PLAYLIST_HOLD / loop) * loop);
    });
    const total = spans.reduce((a, b) => a + b, 0);
    let t = ((clock % total) + total) % total;
    for (let i = 0; i < entries.length; i++) {
        if (t < spans[i]) return { key: entries[i].key, elapsed: t };
        t -= spans[i];
    }
    return { key: entries[entries.length - 1].key, elapsed: 0 };
}

export function formAt(keys, clock, holdMs, fadeMs) {
    if (!keys.length) return null;
    const slot = holdMs + fadeMs * 2, total = slot * keys.length;
    const t = ((clock % total) + total) % total;
    const i = Math.min(keys.length - 1, Math.floor(t / slot));
    const u = t - i * slot;
    const fade = u < fadeMs ? u / fadeMs
        : u < fadeMs + holdMs ? 1
            : 1 - (u - fadeMs - holdMs) / fadeMs;
    return { model: keys[i], fade: Math.max(0, Math.min(1, fade)) };
}

/**
 * Several npc type ids usually share one name: the fleet pickpockets "Man" ids 1, 2 and 3,
 * which is one person to anyone reading the panel but was three separate cards (owner,
 * 2026-09-20). Merge those into one card and show each distinct model side by side on its
 * stage, so nothing about the different appearances is lost.
 *
 * Grouped by ACTION as well as kind and name, so a Man that was killed and a Man that was
 * pickpocketed stay apart - they are genuinely different rows of loot.
 *
 * Every merged figure is derived rather than guessed: events and item counts add up, first
 * and last widen to the span, and `accounts` takes the MAXIMUM rather than the sum, because
 * the same account very likely pickpocketed more than one of the three ids and adding them
 * would report more accounts than the fleet has. A level is kept only when every id agrees
 * on it; ids that disagree drop it instead of showing one id's level for all of them.
 */
export function groupKeyOf(src) {
    // A source that NAMES A SKILL groups on that skill, so every tree lands on one Woodcutting
    // card, every rock on one Mining card, every material cut on one Fletching card and every
    // pickpocket and stall on one Thieving card. The KIND stays in the key so a gathering skill
    // and a production skill can never merge. Must match loot.Source.GroupKey in
    // internal/loot/loot.go.
    if (src.skill) return `${src.kind}|${src.skill.toLowerCase()}`;
    // EVERY RANDOM EVENT IS ONE CARD (a user request); partKeyOf below keeps each event its own
    // row inside it. Must match the same line in loot.Source.GroupKey.
    if (src.kind === 'randomevent') return src.kind;
    return partKeyOf(src);
}

/**
 * The row a source contributes to INSIDE its card: kind, action and name, so the three Man ids
 * are one row of the Thieving card and a Guard is another. Must match loot.Source.PartKey.
 */
export function partKeyOf(src) {
    return `${src.kind}|${src.action ?? ''}|${(src.name ?? '').trim().toLowerCase()}`;
}

/**
 * What a card is called: a skill-titled source by its skill, every random event by the one
 * "Random events" card they share, everything else by its name.
 */
const cardTitle = src => src.skill || (src.kind === 'randomevent' ? 'Random events' : src.name);

// Exported for the same reason combatColour, stackLabel and formAt are: it is pure, it decides a
// number the owner reads off the panel, and it can then be checked without a rendered page.
export function mergeSameName(sources) {
    const out = [], groups = new Map();
    for (const raw of sources) {
        const s = { ...raw, source: normaliseSource(raw.source ?? {}) };
        const src = s.source;
        const gk = groupKeyOf(src);
        const hit = groups.get(gk);
        if (!hit) {
            const copy = {
                ...s,
                accounts: s.groupAccounts ?? s.accounts,
                groupKey: gk,
                firstKey: s.key,
                ids: [src.id],
                models: hasModel(src) ? [modelKey(src)] : [],
                names: [sourceName(src)],
                parts: new Map(),
                items: (s.items ?? []).map(it => ({ ...it })),
                // The card's cost is the sum of its sources' — every material a Fletching card
                // cut, every key its opens spent. costKnown starts true and is cleared by any
                // source whose input the server could not price, so a card is only called net
                // when all of it is (see the header of internal/controlpanel/lootcost.go).
                cost: s.cost ?? 0,
                costUnits: s.costUnits ?? 0,
                costItem: s.costItem ?? '',
                costKnown: !s.costItem || !!s.costKnown,
                // The experience an npc's kills earned, per skill, summed over the card's sources.
                skills: (s.skills ?? []).map(k => ({ ...k }))
            };
            groups.set(gk, copy);
            out.push(copy);
            addPart(copy, s, src);
            continue;
        }
        addPart(hit, s, src);
        hit.ids.push(src.id);
        // On a SKILL-TITLED card the sibling models share the ring with the yields, and every
        // fishing-spot npc in content 289 is named "Fishing spot" — so three ids would stand
        // three near-identical spots around the one in the middle and push the fish off the
        // ring. A second model is kept only when its source has a name the card does not
        // already show, which keeps Woodcutting's Tree/Oak/Willow (distinct names, genuinely
        // different trees) and collapses the spots. Nothing is lost: the per-source rows
        // underneath still carry every id.
        //
        // Non-gathering merged cards are untouched: the three "Man" pickpocket ids are all
        // named "Man" and are meant to be drawn side by side, which is why this is not a
        // blanket rule.
        const named = hit.names.includes(sourceName(src));
        if (!named) hit.names.push(sourceName(src));
        const mk = hasModel(src) && !(SKILL_CARDS.has(src.kind) && named) ? modelKey(src) : '';
        if (mk && !hit.models.includes(mk)) hit.models.push(mk);
        hit.events = (hit.events ?? 0) + (s.events ?? 0);
        hit.xp = (hit.xp ?? 0) + (s.xp ?? 0);
        hit.cost = (hit.cost ?? 0) + (s.cost ?? 0);
        for (const k of s.skills ?? []) {
            const have = hit.skills.find(x => x.id === k.id);
            if (have) have.count = (have.count ?? 0) + (k.count ?? 0);
            else hit.skills.push({ ...k });
        }
        hit.costUnits = (hit.costUnits ?? 0) + (s.costUnits ?? 0);
        if (s.costItem && !hit.costItem) hit.costItem = s.costItem;
        if (s.costItem && !s.costKnown) hit.costKnown = false;
        // The server counts the distinct accounts across the whole group (groupAccounts); the
        // max below is only the fallback for a build that does not send it. The max is what made
        // the Fishing spot card read 154 accounts against 269 logged in — every account fishes
        // whichever spot it is standing at, so no single spot was used by all of them.
        hit.accounts = s.groupAccounts ?? Math.max(hit.accounts ?? 0, s.accounts ?? 0);
        hit.first = Math.min(hit.first ?? Infinity, s.first ?? Infinity);
        hit.last = Math.max(hit.last ?? 0, s.last ?? 0);
        if (hit.source.level !== src.level) hit.source = { ...hit.source, level: 0 };
        const byId = new Map(hit.items.map(it => [it.id, it]));
        for (const it of s.items ?? []) {
            const have = byId.get(it.id);
            if (have) have.count = (have.count ?? 0) + (it.count ?? 0);
            else { const c = { ...it }; hit.items.push(c); byId.set(it.id, c); }
        }
    }
    // A group of one keeps the source's own key, so nothing changes for the overwhelming
    // majority of sources; only a genuinely merged group gets a new, stable group key.
    for (const g of out) {
        g.key = g.ids.length === 1 ? g.firstKey : `group:${g.groupKey}`;
        g.parts = [...g.parts.values()].sort((a, b) => b.events - a.events || a.name.localeCompare(b.name));
        for (const p of g.parts) p.items.sort((a, b) => (b.count ?? 0) - (a.count ?? 0));
        g.actions = [...new Set(g.parts.map(p => p.action))];
        // A skill card whose sources are DIFFERENT things (the Thieving card's Man, Guard and
        // Baker's stall) stands one of each on its ring, most-robbed first, and only then any
        // further appearance of a name already shown (the other two Man ids) while there is
        // room. Pushed in server key order, the live card already had six models and the next
        // target added would have pushed the stall off the ring. SKILL_CARDS keep their own
        // arrangement (a source in the middle, its yields around it). The Random events card is
        // the same shape: a different event in every row, each standing on the ring as itself.
        if ((g.source.skill && !SKILL_CARDS.has(g.source.kind) || g.source.kind === 'randomevent') && g.parts.length > 1) {
            const first = g.parts.map(p => p.model).filter(Boolean);
            const rest = g.parts.flatMap(p => p.models.filter(m => m !== p.model).map(m => [m, p.modelEvents.get(m) ?? 0]))
                .sort((a, b) => b[1] - a[1]).map(e => e[0]);
            g.models = [...first, ...rest].filter((m, i, all) => all.indexOf(m) === i).slice(0, SATELLITES);
        }
    }
    return out;
}

/**
 * What one drop cell divides by, and whose drops it calls them. A row picked in the breakdown
 * divides by THAT row's events and is named after it. An unpicked merged card gives one rate
 * only when every row can give the item: otherwise the card's events blend targets that never
 * drop it, which is how the Thieving card came to say 7.82 coins "per theft" (the Guard alone
 * pays 30.01 per pickpocket, and 6,873 Baker's stall steals pay no coins at all). per is null
 * then, and fromRates carries each row's own rate instead. span is what per-hour divides by.
 */
export function dropRateOf(it, s, part) {
    const parts = s.parts ?? [];
    const verbOf = p => VERBS[p.action] ?? [p.action, p.action, `${p.action}s`];
    const rowRate = p => {
        const got = p.items.find(i => i.id === it.id);
        const count = got ? got.count : 0;
        return { name: p.name, count, per: count / Math.max(1, p.events), unit: verbOf(p)[1] };
    };
    if (part) return { per: rowRate(part).per, unit: verbOf(part)[1], owner: part.name || cardTitle(s.source), span: part, fromRates: [] };
    const from = parts.filter(p => p.items.some(i => i.id === it.id));
    const blended = parts.length > 1 && from.length < parts.length;
    return {
        per: blended ? null : it.count / Math.max(1, s.events),
        unit: verbFor(s)[1],
        owner: cardTitle(s.source),
        span: s,
        fromRates: parts.length > 1 ? from.map(rowRate) : []
    };
}

/**
 * A breakdown row's worth, on the same footing as its card's headline: net of the row's own
 * input when every input it used has a price, and marked gross when one does not. Fletching's
 * rows used to add up to about four times their net card (Willow logs 3,577,192 gp under a
 * 1,166,460 gp card), because they summed the output and never took the logs off.
 */
export function partWorthOf(p) {
    // A folded row (mergeParts) is worth what its rows are, each on its own basis: a group with one row
    // whose input has no price still nets the costs of the rest, as the card's own total does.
    if (p.folded) return p.folded;
    const gross = p.items.reduce((n, it) => n + (it.value ?? 0) * (it.count ?? 0), 0);
    if (!p.costItem) return { gp: gross, basis: '' };
    if (p.costKnown) return { gp: gross - (p.cost ?? 0), basis: 'net' };
    return { gp: gross, basis: 'gross' };
}

// ---- compact rows (a user request: "magic loot tab is very long because of the amount of items we
// have alched, condense it") ----
//
// The Magic card has a row per alched item (hundreds of Yew shortbows' worth of names) and the
// Teleports and Consumables cards a row per spell or food. compactParts folds that into the rows the
// owner reads: every alchemy row into one "High Alchemy" row (casts, coins gained, runes and items
// used, net, XP), the same for Low Alchemy and Superheat, the per-item breakdown behind an expander
// that shows the top few by value and sums the rest as "+K more". It only re-groups rows the card
// already has, so every figure on a group is the sum of the rows inside it and the card's totals are
// untouched. Pure, so it is tested without a browser.

/** The cast rows that are folded together on the Magic card, by their action. */
const CAST_GROUPS = {
    alch: 'High Alchemy', 'alch-runes': 'High Alchemy',
    lowalch: 'Low Alchemy', 'lowalch-runes': 'Low Alchemy',
    superheat: 'Superheat', 'superheat-runes': 'Superheat'
};
/** How many of a group's rows show when it is opened, and how many rows a long cost card keeps. */
export const COMPACT_TOP = 5;
export const COMPACT_ROWS = 8;

/** A row's net worth in gp (what partWorthOf says), the figure rows are ranked by. */
const worthGp = p => partWorthOf(p).gp;

/**
 * One row standing for several: events, experience, cost and items add, the accounts are the largest
 * of its rows' (the same account alches many items, so the sum would exceed the fleet), and first and
 * last widen. `key` is what picking it filters by, so the group's stacks are every stack of its rows.
 */
export function mergeParts(key, name, rows) {
    const out = {
        key, name, action: rows[0].action, kind: rows[0].kind, level: 0, events: 0, xp: 0, accounts: 0,
        first: Infinity, last: 0, items: [], models: [], modelEvents: new Map(), model: '', best: -1,
        cost: 0, costUnits: 0, costItem: '', costKnown: true, icon: rows[0].icon ?? -1, count: rows.length
    };
    const byId = new Map();
    const labels = [];
    for (const p of rows) {
        out.events += p.events ?? 0;
        out.xp += p.xp ?? 0;
        out.cost += p.cost ?? 0;
        out.costUnits += p.costUnits ?? 0;
        if (p.costItem) {
            if (!labels.includes(p.costItem)) labels.push(p.costItem);
            if (!p.costKnown) out.costKnown = false;
        }
        out.accounts = Math.max(out.accounts, p.accounts ?? 0);
        out.first = Math.min(out.first, p.first ?? Infinity);
        out.last = Math.max(out.last, p.last ?? 0);
        for (const it of p.items) {
            const have = byId.get(it.id);
            if (have) have.count = (have.count ?? 0) + (it.count ?? 0);
            else { const c = { ...it }; out.items.push(c); byId.set(it.id, c); }
        }
    }
    out.items.sort((a, b) => (b.count ?? 0) - (a.count ?? 0));
    // Its worth is the sum of its rows' own (partWorthOf), NOT recomputed from the merged cost: one row
    // whose input has no trustworthy price would otherwise make the whole group gross and drop the known
    // costs of the others, so the group stopped adding up to its rows and to the card.
    const worths = rows.map(partWorthOf);
    out.folded = {
        gp: worths.reduce((n, w) => n + w.gp, 0),
        basis: worths.some(w => w.basis === 'gross') ? 'gross' : worths.some(w => w.basis === 'net') ? 'net' : ''
    };
    // What was used: the items alched and the runes, named once each when there are few of them.
    out.costItem = rows.length > 1 ? `${rows.length} items, runes` : labels.join(' + ');
    return out;
}

/**
 * The rows a card draws, as [{ part, children, more }]. `children` is a group's rows, best first and
 * cut to the top COMPACT_TOP unless `showAll` names the group; `more` is the sum of the rest. Only the
 * Magic card folds alchemy, and only the Teleports and Consumables cards cap their list; every other
 * card's rows come back one by one, unchanged.
 */
export function compactParts(parts, source, showAll = new Set()) {
    const plain = parts.map(part => ({ part, children: null, more: null }));
    const magic = source?.skill === 'Magic';
    const costCard = CAPPED_KINDS.has(source?.kind);
    if (!magic && !costCard) return plain;
    const rows = [];
    if (magic) {
        const groups = new Map();
        for (const p of parts) {
            const label = p.kind === 'production' ? CAST_GROUPS[p.action] : '';
            if (!label) { rows.push({ part: p, children: null, more: null }); continue; }
            if (!groups.has(label)) { groups.set(label, []); rows.push({ group: label }); }
            groups.get(label).push(p);
        }
        for (let i = 0; i < rows.length; i++) {
            if (!rows[i].group) continue;
            const label = rows[i].group, members = groups.get(label);
            if (members.length < 2) { rows[i] = { part: members[0], children: null, more: null }; continue; }
            const key = `group:${label}`;
            const ranked = [...members].sort((a, b) => worthGp(b) - worthGp(a) || b.events - a.events);
            const shown = showAll.has(key) ? ranked : ranked.slice(0, COMPACT_TOP);
            const rest = ranked.slice(shown.length);
            rows[i] = {
                part: mergeParts(key, label, members), children: shown,
                more: rest.length ? mergeParts(`more:${key}`, `+${rest.length} more`, rest) : null
            };
        }
        rows.sort((a, b) => b.part.events - a.part.events || a.part.name.localeCompare(b.part.name));
        return rows;
    }
    // A cost card keeps its busiest rows and sums the rest.
    if (parts.length <= COMPACT_ROWS || showAll.has('rows')) return plain;
    const shown = plain.slice(0, COMPACT_ROWS);
    const rest = parts.slice(COMPACT_ROWS);
    shown.push({ part: mergeParts('more:rows', `+${rest.length} more`, rest), children: null, more: null, isMore: true });
    return shown;
}

/** The row a key names: a card's own row, or a group built from its rows ("group:High Alchemy"). */
export function findPart(parts, key, source) {
    if (!key) return null;
    const own = parts.find(p => p.key === key);
    if (own) return own;
    if (key.startsWith('group:')) {
        const label = key.slice(6);
        const rows = parts.filter(p => p.kind === 'production' && CAST_GROUPS[p.action] === label);
        if (rows.length > 1 && source?.skill === 'Magic') return mergeParts(key, label, rows);
    }
    return null;
}

/**
 * The body POST /api/loot/clear takes. A card is its group key. One item comes off the card, or, when a
 * row of its breakdown is picked and the grid is showing that row's stacks, off that row alone
 * (partKey is the row's key). A row narrows whatever else is asked and is never dropped, so it can
 * only make a request smaller. Exported to be checked without a rendered page, like the merge above.
 */
export function clearBody(groupKey, itemId, partKey) {
    const body = { group: groupKey };
    if (partKey) body.part = partKey;
    if (itemId !== undefined && itemId !== null) body.item = itemId;
    return body;
}

/**
 * What the confirm() says. It names the card or the item, says where the loot goes from and that
 * the card's figures start again, and that it cannot be undone: it is read just before something
 * permanent. `item` is the item's name when one item is being removed.
 */
export function clearPrompt({ title, verb, events = 0, rows = 1, stacks = 0, item = '', count = 0, part = '' }) {
    if (!item) {
        return `Clear the "${title}" card?\n\n` +
            `This removes everything recorded for it${rows > 1 ? ` (all ${rows} rows)` : ''}: ${fmtCount(events)} ${verb} and ${stacks} item stack${stacks === 1 ? '' : 's'}. ` +
            'It goes from Lifetime, Since this restart, the recent drops and the history, for every account.\n\n' +
            'Loot taken from now on is counted again from zero. This cannot be undone.';
    }
    return `Remove ${fmtCount(count)} x ${item} from the "${title}" card${part ? `, ${part} row only` : ''}?\n\n` +
        `This takes that item off ${part ? 'that row' : 'the card'} for every account, in Lifetime and Since this restart. ` +
        `The card keeps its ${verb} count and its other items.\n\n` +
        'Loot taken from now on is counted again. This cannot be undone.';
}

/** Folds one source into its card's row for its target (see partKeyOf). */
function addPart(g, s, src) {
    const pk = partKeyOf(src);
    let p = g.parts.get(pk);
    if (!p) {
        p = { key: pk, name: src.name ?? '', action: src.action ?? '', kind: src.kind, level: src.level ?? 0, events: 0, xp: 0, accounts: 0,
            first: Infinity, last: 0, items: [], models: [], modelEvents: new Map(), model: '', best: -1, cost: 0, costUnits: 0, costItem: '', costKnown: true,
            // A row with no drops of its own (a bone on the Prayer card) shows its own item instead.
            icon: src.model === 'obj' && src.id >= 0 ? src.id : -1 };
        g.parts.set(pk, p);
    }
    const events = s.events ?? 0;
    p.events += events;
    p.xp += s.xp ?? 0;
    p.cost += s.cost ?? 0;
    p.costUnits += s.costUnits ?? 0;
    if (s.costItem && !p.costItem) p.costItem = s.costItem;
    if (s.costItem && !s.costKnown) p.costKnown = false;
    // The server's union for this row, falling back to the max for a build that does not send it.
    p.accounts = s.partAccounts ?? Math.max(p.accounts, s.accounts ?? 0);
    p.first = Math.min(p.first, s.first ?? Infinity);
    p.last = Math.max(p.last, s.last ?? 0);
    if (p.level !== (src.level ?? 0)) p.level = 0;
    if (hasModel(src)) {
        const mk = modelKey(src);
        if (!p.models.includes(mk)) p.models.push(mk);
        p.modelEvents.set(mk, (p.modelEvents.get(mk) ?? 0) + events);
        // The row's own model is its most-robbed id.
        if (events > p.best) { p.best = events; p.model = mk; }
    }
    const byId = new Map(p.items.map(it => [it.id, it]));
    for (const it of s.items ?? []) {
        const have = byId.get(it.id);
        if (have) have.count = (have.count ?? 0) + (it.count ?? 0);
        else { const c = { ...it }; p.items.push(c); byId.set(it.id, c); }
    }
}
const modelKey = s => isLocModel(s) ? `loc:${s.id}` : isNpcModel(s) ? `npc:${s.id}` : `obj:${s.id}`;
const sourceName = s => (s?.name ?? '').trim().toLowerCase();

function readView(key, base) {
    try {
        const raw = JSON.parse(localStorage.getItem(key) ?? 'null');
        return raw && raw.base === base ? raw.view : null;
    } catch {
        return null;
    }
}

function writeView(key, base, view) {
    try { localStorage.setItem(key, JSON.stringify({ base, view })); } catch { /* this viewer only */ }
}

/** A small sparkline (inline SVG, no canvas context) over the history buckets. */
function sparkline(series, w = 72, h = 18) {
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('viewBox', `0 0 ${w} ${h}`);
    svg.setAttribute('width', String(w)); svg.setAttribute('height', String(h));
    svg.setAttribute('class', 'loot-spark');
    svg.setAttribute('aria-hidden', 'true');
    const max = Math.max(1, ...series);
    const n = series.length;
    const pts = series.map((v, i) => [n === 1 ? w / 2 : (i / (n - 1)) * (w - 2) + 1, h - 2 - (v / max) * (h - 4)]);
    const line = pts.map(p => `${p[0].toFixed(1)},${p[1].toFixed(1)}`).join(' ');
    const area = document.createElementNS(svg.namespaceURI, 'polygon');
    area.setAttribute('points', `${pts[0][0].toFixed(1)},${h - 1} ${line} ${pts[n - 1][0].toFixed(1)},${h - 1}`);
    area.setAttribute('class', 'loot-spark-area');
    const poly = document.createElementNS(svg.namespaceURI, 'polyline');
    poly.setAttribute('points', line);
    poly.setAttribute('class', 'loot-spark-line');
    svg.append(area, poly);
    return svg;
}

let tooltipSequence = 0;

export default {
    type: 'loot',
    title: 'Loot',
    description: 'Every loot source across the fleet — npcs killed or pickpocketed, caskets and oysters opened, random-event rewards — with real 3D cache models, drops in a bank grid, totals and rare-drop highlights.',
    size: { w: 12, h: 5 },
    accounts: 'none',
    settings: [
        { key: 'period', label: 'Period', kind: 'select', options: PERIODS, default: 'lifetime' },
        { key: 'kind', label: 'Show sources', kind: 'select', options: KINDS, default: 'all' },
        { key: 'sortSources', label: 'Sort sources by', kind: 'select', options: SOURCE_SORTS, default: 'count' },
        { key: 'sortItems', label: 'Sort drops by', kind: 'select', options: ITEM_SORTS, default: 'count' },
        { key: 'pose', label: 'NPC animation', kind: 'select', options: POSES, default: 'walk' },
        { key: 'spin', label: 'Turn the models', kind: 'bool', default: true },
        { key: 'ticker', label: 'Recent notable drops', kind: 'bool', default: true },
        { key: 'accountsTable', label: 'Per-account table', kind: 'bool', default: false },
        { key: 'refLevel', label: 'Colour combat levels against level', kind: 'number', min: 3, max: 126, step: 1, default: 3 }
    ],
    create(body, ctx) {
        const S = ctx.settings;
        const viewKey = `rsb0t-go.loot.view.${ctx.id}`;
        const viewBase = JSON.stringify([S.period, S.kind, S.sortSources, S.sortItems, S.accountsTable]);
        const view = { period: S.period, kind: S.kind, sortSources: S.sortSources, sortItems: S.sortItems, accounts: S.accountsTable, ...(readView(viewKey, viewBase) ?? {}) };
        const reduce = matchMedia('(prefers-reduced-motion: reduce)');
        // ?loot=2d or ?loot=icons forces a fallback, for checking them on a GPU machine.
        const force = new URLSearchParams(location.search).get('loot');

        body.classList.add('lootbody');
        const root = el('div', 'loot');
        const bar = el('div', 'loot-bar');
        const seg = (options, current, onPick, label) => {
            const group = el('div', 'loot-seg');
            group.setAttribute('role', 'group');
            group.setAttribute('aria-label', label);
            const buttons = options.map(o => {
                const b = button(o.label.replace(' (persisted)', ''), o.label);
                b.dataset.value = o.value;
                b.addEventListener('click', () => onPick(o.value));
                group.append(b);
                return b;
            });
            const sync = value => { for (const b of buttons) { const on = b.dataset.value === value; b.classList.toggle('is-active', on); b.setAttribute('aria-pressed', String(on)); } };
            sync(current);
            return { group, sync };
        };
        const selectOf = (options, current, label) => {
            const s = el('select');
            s.setAttribute('aria-label', label);
            for (const o of options) { const opt = el('option', '', o.label); opt.value = o.value; s.append(opt); }
            s.value = current;
            return s;
        };
        const periodSeg = seg(PERIODS, view.period, v => { view.period = v; changed(); }, 'Period');
        const kindSeg = seg(KINDS, view.kind, v => { view.kind = v; changed(); }, 'Source type');
        const sortSources = selectOf(SOURCE_SORTS, view.sortSources, 'Sort sources by');
        const sortItems = selectOf(ITEM_SORTS, view.sortItems, 'Sort drops by');
        sortSources.addEventListener('change', () => { view.sortSources = sortSources.value; changed(); });
        sortItems.addEventListener('change', () => { view.sortItems = sortItems.value; changed(); });
        const search = el('input', 'loot-search');
        search.type = 'search';
        search.placeholder = 'Which sources drop… (item name or id)';
        search.setAttribute('aria-label', 'Find the sources that drop an item');
        search.addEventListener('input', () => render());
        const accountsBtn = button('Accounts', 'Show the per-account breakdown');
        accountsBtn.addEventListener('click', () => { view.accounts = !view.accounts; changed(); });
        const sortWrap = el('label', 'loot-sort');
        sortWrap.append(el('span', '', 'sort'), sortSources, el('span', '', 'drops'), sortItems);
        bar.append(periodSeg.group, kindSeg.group, sortWrap, search, accountsBtn);

        const viewport = el('div', 'loot-view');
        const scroller = el('div', 'loot-scroll');
        const inner = el('div', 'loot-inner');
        const note = el('div', 'none loot-note');
        const totals = el('div', 'loot-totals');
        const tiles = el('div', 'loot-tiles');
        const top = el('div', 'loot-top');
        totals.append(tiles, top);
        const ticker = el('div', 'loot-ticker');
        ticker.setAttribute('aria-live', 'polite');
        const accountsWrap = el('div', 'loot-accounts');
        // The Runecrafting section, drawn only under the Runecrafting filter (panels/lootrunecraft.js).
        const runecraftWrap = el('div', 'loot-rc');
        runecraftWrap.hidden = true;
        // The Deaths section, drawn only under the Deaths filter (panels/lootdeaths.js).
        const deathsWrap = el('div', 'loot-rc loot-dx');
        deathsWrap.hidden = true;
        const cardsWrap = el('div', 'loot-cards');
        inner.append(note, totals, ticker, runecraftWrap, deathsWrap, accountsWrap, cardsWrap);
        scroller.append(inner);
        const glCanvas = el('canvas', 'loot-gl');
        glCanvas.setAttribute('aria-hidden', 'true');
        viewport.append(scroller, glCanvas);
        root.append(bar, viewport);
        body.append(root);

        const tooltip = el('div', 'bankall-tooltip loot-tooltip');
        tooltip.id = `loot-tooltip-${++tooltipSequence}`;
        tooltip.setAttribute('role', 'tooltip');
        tooltip.hidden = true;
        document.body.append(tooltip);

        let data = null;
        let fetchedAt = 0;
        let busy = false;
        let closed = false;
        let failure = '';
        let aborter = null;
        let loadEpoch = 0; // bumped by a clear, so a refresh that began before it cannot undo it
        let clearing = false; // a clear is in flight
        let onScreen = false;
        let drawnKey = '';
        const seen = new Map(); // "period|source|item" -> count, for the rare shimmer
        const seenPeriods = new Set();
        const tickerSeen = new Set();
        const cards = new Map(); // source key -> card
        const details = new WeakMap(); // cell -> tooltip text

        // ---- models: renderer choice ----

        let renderer = null; // the WebGL renderer, or null
        let sprites = false; // Canvas 2D stills per card
        let modelsOn = force !== 'icons';
        let modelState = 'waiting'; // waiting | loading | ready | icons
        let modelNote = '';
        let modelBusy = false;
        let lastEnsure = 0;
        let textures = null;
        if (modelsOn && force !== '2d') {
            try {
                renderer = createLootRenderer(glCanvas);
                // A lost context (a driver reset, the GPU process restarting, too many
                // contexts on the page) falls back to the Canvas 2D stills until the browser
                // restores it, so the cards keep their models rather than dropping to icons.
                renderer.onRestore = () => {
                    root.classList.remove('gl-lost');
                    sprites = false;
                    glCanvas.hidden = false;
                    for (const c of cards.values()) { c.uploaded = false; c.sprite?.remove(); c.sprite = null; c.spriteYaw = NaN; }
                    syncModels();
                    if (modelState === 'ready') setModelNote('ready', 'real cache models (WebGL)');
                    invalidate();
                };
                glCanvas.addEventListener('webglcontextlost', () => {
                    root.classList.add('gl-lost');
                    cancelAnimationFrame(raf); raf = 0;
                    glCanvas.hidden = true;
                    sprites = modelsOn;
                    syncModels();
                    setModelNote(modelState, 'WebGL context lost: showing Canvas 2D stills until the browser restores it');
                });
            } catch {
                renderer = null;
            }
        }
        if (!renderer) { glCanvas.hidden = true; sprites = modelsOn; }

        function changed() {
            periodSeg.sync(view.period); kindSeg.sync(view.kind);
            sortSources.value = view.sortSources; sortItems.value = view.sortItems;
            accountsBtn.classList.toggle('is-active', view.accounts);
            accountsBtn.setAttribute('aria-pressed', String(view.accounts));
            writeView(viewKey, viewBase, view);
            render();
        }

        // ---- data ----

        async function load() {
            if (busy || closed) return;
            busy = true;
            fetchedAt = Date.now();
            const startedAt = loadEpoch;
            aborter = new AbortController();
            try {
                const res = await fetch('api/loot', { cache: 'no-store', signal: aborter.signal });
                if (closed) return;
                if (res.status === 404) { failure = 'this build has no loot endpoint (GET /api/loot)'; data = null; }
                else if (!res.ok) failure = `loot refresh failed (HTTP ${res.status}); retrying`;
                else {
                    const body = await res.json();
                    // A clear that landed while this was in flight has already drawn newer data, and this
                    // answer may predate it: it would put the cleared card back until the next refresh.
                    if (startedAt === loadEpoch) { data = body; failure = ''; }
                }
            } catch {
                if (!closed) failure = data ? '' : 'loot refresh failed; retrying';
            } finally {
                busy = false;
                aborter = null;
            }
            if (closed) return;
            render();
            ensureModels();
        }

        // ---- totals, with a gentle count-up ----

        const TILES = [
            ['items', 'Items looted', p => p.items],
            ['coins', 'Coins', p => p.coins],
            // NET since 2026-09-21: what the fleet EARNED, after the logs it cut up and the
            // crystal keys it spent. The gross figure and the cost sit beside it rather than
            // behind it, so the headline can be checked instead of taken on trust, and so a drop
            // in it after this change is legible as netting rather than as lost loot.
            ['value', 'Loot value (net gp)', p => p.value],
            ['gross', 'Before costs (gp)', p => p.gross],
            ['cost', 'Inputs consumed (gp)', p => p.cost],
            // The part of "before costs" on rows whose input has no price to net it by: shown so the
            // headline's gross share is a figure, not a guess from the cards that say GROSS.
            ['unnetted', 'Gross not netted (no price)', p => p.unnetted],
            // Fish first among the activity counts: 267 of the 269 accounts fish, so on a real
            // fleet this is the biggest number on the panel by a wide margin.
            // The gathering counts. Fishing leads because 267 of the 269 accounts fish, so on a
            // real fleet it is the biggest number on the panel by a wide margin.
            ['fish', 'Fish caught', p => p.events?.fish],
            ['chop', 'Logs chopped', p => p.events?.chop],
            ['mine', 'Rocks mined', p => p.events?.mine],
            // Fletching sits beside the gathering counts because it is what the fleet does with
            // one of them: the woodcutters' start camp cuts every plain log it chops into arrow
            // shafts. The figure is MATERIALS CONSUMED — logs cut, not the fifteen shafts each
            // became (internal/loot/production.go's ActionFletch, and cost.go for why it has to
            // be that way round). The shafts are on the card, where a tree's logs are too.
            ['fletch', 'Materials fletched', p => p.events?.fletch],
            // Crafting (owner, 2026-10-04): gems cut and smashed, flax spun, wool spun. The
            // figure is MATERIALS CONSUMED as Fletching's is (internal/loot/craft.go); the cut
            // gems and bow strings are on the Crafting card. Smithing is bars smelted at a
            // furnace. The Magic card's casts (internal/loot/magic.go) split by whether a fire
            // staff paid the Fire runes ("-runes" when it did not); the tiles add both.
            ['craft', 'Materials crafted', p => p.events?.craft],
            ['smith', 'Bars smelted', p => p.events?.smith],
            ['clean', 'Herbs cleaned', p => p.events?.clean],
            ['superheat', 'Bars superheated', p => (p.events?.superheat ?? 0) + (p.events?.['superheat-runes'] ?? 0)],
            ['alch', 'Alchemy casts', p => (p.events?.alch ?? 0) + (p.events?.['alch-runes'] ?? 0) + (p.events?.lowalch ?? 0) + (p.events?.['lowalch-runes'] ?? 0)],
            ['kill', 'Kills', p => p.events?.kill],
            ['pickpocket', 'Pickpockets', p => p.events?.pickpocket],
            // Stall and chest steals share the Thieving card with the pickpockets, and until
            // 2026-09-23 no tile counted them at all (6,873 live steals, in no counter).
            ['steal', 'Stall steals', p => p.events?.steal],
            // The Prayer card's two figures: what was buried, and what it paid.
            ['bury', 'Bones buried', p => p.events?.bury],
            ['prayerxp', 'Prayer XP', p => p.xp?.Prayer],
            // Runecrafting: crafts (one altar click each), the experience they paid, and the essence the miners handed to the mule
            // (panels/lootrunecraft.js has the section that breaks them down).
            ['runecraft', 'Runecrafts', p => p.events?.runecraft],
            ['runecraftxp', 'Runecraft XP', p => p.xp?.Runecraft],
            ['handover', 'Essence handed over', p => p.events?.handover],
            ['take', 'Ground pickups', p => p.events?.take],
            ['casket', 'Caskets opened', p => p.events?.['open:Casket'] ?? p.events?.['open:casket']],
            ['oyster', 'Oysters opened', p => p.events?.['open:Oyster'] ?? p.events?.['open:oyster']],
            ['trail', 'Treasure trails', p => p.events?.complete],
            ['randomevent', 'Random events', p => p.events?.event]
        ];
        const counters = TILES.map(([key, label]) => {
            const tile = el('div', `loot-tile loot-tile-${key}`);
            const value = el('b', '', '0');
            tile.append(value, el('span', '', label));
            tiles.append(tile);
            return { key, value, shown: 0, from: 0, target: 0, start: 0 };
        });
        let countRaf = 0;
        let countPeriod = '';
        // The tile shows its figure in full. Loot is priced at real market value now (see
        // internal/controlpanel/marketprice.go), so the lifetime total passed ten digits and
        // was being cut off with an ellipsis — the owner's "loot value gp also displays … when
        // the amount gets too long". Rather than abbreviate the headline number of the panel,
        // the tile steps its type down as the figure grows; every tile does it, because the
        // counts grow too.
        // Measured against the live fleet's own figure (1,806,277,623 gp, 13 characters): it
        // fits the tile exactly at is-long, a hundred billion fits at is-huge, and is-vast
        // carries a number far past anything this fleet can reach. Nothing is ever cut.
        function showCount(c, n) {
            const text = fmtCount(n);
            c.value.textContent = text;
            c.value.classList.toggle('is-long', text.length >= 10 && text.length < 14);
            c.value.classList.toggle('is-huge', text.length >= 14 && text.length < 17);
            c.value.classList.toggle('is-vast', text.length >= 17);
        }
        function setCounter(c, target, animate) {
            c.target = target;
            if (!animate || target < c.shown) { c.shown = target; showCount(c, target); return; }
            if (target === c.shown) return;
            c.from = c.shown; c.start = performance.now();
            if (!countRaf) countRaf = requestAnimationFrame(countStep);
        }
        function countStep(now) {
            countRaf = 0;
            let more = false;
            for (const c of counters) {
                if (c.shown === c.target) continue;
                const t = Math.min(1, (now - c.start) / COUNT_UP_MS);
                const eased = 1 - (1 - t) ** 3;
                const next = t >= 1 ? c.target : Math.round(c.from + (c.target - c.from) * eased);
                if (next !== c.shown) { c.shown = next; showCount(c, next); }
                if (t < 1) more = true;
            }
            if (more && !closed) countRaf = requestAnimationFrame(countStep);
        }

        function drawTotals(P, periodKey) {
            const animate = !reduce.matches && onScreen && countPeriod === periodKey;
            countPeriod = periodKey;
            for (const c of counters) setCounter(c, Number(TILES.find(t => t[0] === c.key)[2](P)) || 0, animate);
        }

        function drawTopDrops(sources) {
            const agg = new Map();
            for (const s of sources) for (const it of s.items ?? []) {
                if (it.id === COINS) continue;
                let a = agg.get(it.id);
                if (!a) { a = { id: it.id, name: it.name, count: 0, value: it.value ?? 0 }; agg.set(it.id, a); }
                a.count += it.count;
            }
            const byValue = view.sortItems === 'value';
            const topItems = [...agg.values()].sort((p, q) => (byValue ? q.count * q.value - p.count * p.value : q.count - p.count) || p.name.localeCompare(q.name)).slice(0, 6);
            top.textContent = '';
            if (!topItems.length) return;
            top.append(el('span', 'loot-top-label', byValue ? 'Top drops by value' : 'Top drops'));
            const history = data.history ?? [];
            const bucketMs = data.bucketMs || 600000;
            // Every bucket from the oldest kept to now, zero where nothing dropped.
            const starts = [];
            if (history.length) for (let t = history[0].start; t <= data.now; t += bucketMs) starts.push(t);
            const byStart = new Map(history.map(b => [b.start, b.items ?? {}]));
            for (const it of topItems) {
                const chip = el('div', `loot-topchip${RARE.has(it.id) ? ' is-rare' : ''}`);
                chip.tabIndex = 0;
                chip.append(itemIconWrap(it.id, it.name));
                const text = el('span', 'loot-topchip-text');
                text.append(el('span', 'loot-topchip-name', it.name), el('b', '', fmtCount(it.count)));
                chip.append(text);
                const series = starts.map(t => Number(byStart.get(t)?.[String(it.id)]) || 0);
                // The newest bucket is still filling: scaled to a whole bucket, so the line
                // shows the current pace rather than always dipping at its right-hand end.
                const filled = series.length ? Math.max(0.1, Math.min(1, (data.now - starts[starts.length - 1]) / bucketMs)) : 1;
                if (series.length) series[series.length - 1] = Math.round(series[series.length - 1] / filled);
                if (series.length > 1) chip.append(sparkline(series));
                const lastHour = starts.map(t => Number(byStart.get(t)?.[String(it.id)]) || 0).slice(-Math.max(1, Math.round(3600000 / bucketMs))).reduce((a, b) => a + b, 0);
                details.set(chip, `${it.name}\n${fmtCount(it.count)} dropped${byValue && it.value ? ` · ${fmtCount(it.count * it.value)} gp` : ''}\n${fmtCount(lastHour)} in the last hour (all sources, fleet-wide)`);
                chip.setAttribute('aria-label', details.get(chip).replace(/\n/g, ', '));
                top.append(chip);
            }
        }

        function drawTicker() {
            ticker.textContent = '';
            if (!S.ticker) { ticker.hidden = true; return; }
            ticker.hidden = false;
            const notable = (data.recent ?? []).filter(r => (r.items ?? []).some(it => RARE.has(it.id))).slice(0, 8);
            ticker.append(el('span', 'loot-ticker-label', 'Notable drops'));
            if (!notable.length) {
                ticker.append(el('span', 'none', 'none yet — key halves, uncut emeralds, rubies and diamonds, cosmic talismans and pearls appear here'));
                return;
            }
            const now = data.now;
            const first = tickerSeen.size === 0;
            for (const r of notable) {
                // Only a drop not shown before slides in; a redraw of known ones stays still.
                const id = `${r.at}|${r.account}|${r.source.name}`;
                const item = el('span', `loot-tick${!first && !tickerSeen.has(id) ? ' is-new' : ''}`);
                tickerSeen.add(id);
                item.append(el('span', 'loot-tick-t', ago(now - r.at)), el('span', 'who', r.account));
                const verb = verbFor({ source: r.source })[0].toLowerCase();
                item.append(el('span', 'loot-tick-src', `${verb} ${r.source.name}${r.events > 1 ? ` x${r.events}` : ''}`));
                for (const it of r.items.filter(i => RARE.has(i.id))) {
                    const chip = el('span', 'loot-tick-item');
                    chip.append(itemIconWrap(it.id, it.name), el('span', '', `${it.count > 1 ? `${fmtCount(it.count)} x ` : ''}${it.name}`));
                    item.append(chip);
                }
                ticker.append(item);
            }
        }

        // Under the Runecrafting filter: the runes by type, the essence, each account and the supply side. The rates use the
        // same window rule as every card (perHour above): a real rate since this restart, a lifetime average otherwise.
        function drawRunecraftSection(P) {
            if (view.kind !== 'runecraft') { runecraftWrap.hidden = true; runecraftWrap.textContent = ''; return; }
            const since = view.period === 'sinceRestart';
            const model = runecraftModel(P.runecraft, {
                now: data.now, life: !since, recent: data.recent ?? [], stock: data.essenceStock ?? [],
                rate: (count, first) => hourlyRate(count, data.now, since ? data.processStart : first)
            });
            drawRunecraft(runecraftWrap, model, { fmtRate, ago });
        }

        // Under the Deaths filter: the deaths of the period, what they dropped and what came back. Lifetime is every death on file, since
        // this restart the ones after the process started.
        function drawDeathsSection(P) {
            if (view.kind !== 'deaths') { deathsWrap.hidden = true; deathsWrap.textContent = ''; return; }
            drawDeaths(deathsWrap, deathsModel(P.deaths, { now: data.now, life: view.period !== 'sinceRestart' }), { ago });
        }

        function drawAccounts(P) {
            accountsWrap.textContent = '';
            if (!view.accounts) { accountsWrap.hidden = true; return; }
            accountsWrap.hidden = false;
            const rows = [...(P.accounts ?? [])].sort((a, b) => b.value - a.value || b.events - a.events || a.account.localeCompare(b.account))
                .map(a => [a.account, fmtCount(a.events), fmtCount(a.items), fmtCount(a.coins), fmtCount(a.value), a.last ? ago(data.now - a.last) : '-']);
            if (!rows.length) { accountsWrap.append(el('div', 'none', 'no account has looted anything in this period yet')); return; }
            const t = table(['Account', 'Kills / opens', 'Items', 'Coins', 'Value (gp)', 'Last loot'], rows);
            t.classList.add('tally-table', 'loot-accounts-table');
            accountsWrap.append(t);
        }

        // ---- source cards ----

        /**
         * An hourly figure, or null when the window cannot support one.
         *
         * Since-restart divides by this process's own uptime, which is a real, continuous window:
         * that number is a rate. Lifetime divides by wall-clock since the source was first seen,
         * which includes every restart, login ramp and stretch with a different number of
         * accounts running — an average over a changing fleet, not a rate anyone can act on. The
         * owner read the lifetime figure as if it were the current one and found it too high
         * (2026-09-20), which it is: the fishing cards implied about 39% more fish per minute
         * than 269 accounts can physically catch. The two are shown differently now rather than
         * being made to look alike (see the worth line in fillCard).
         */
        function perHour(count, s, floor = 0) {
            return hourlyRate(count, data.now, view.period === 'sinceRestart' ? data.processStart : s.first, view.period === 'sinceRestart' ? 0 : floor);
        }
        const lifetimeView = () => view.period !== 'sinceRestart';

        function makeCard(s) {
            const forms = formsFor(s.source);
            const card = { key: s.key, el: el('article', 'loot-card'), sig: '', itemsSig: '', partsSig: '', partFilter: '', open: new Set(), showAll: new Set(), model: s.models?.[0] ?? modelKey(s.source), models: s.models?.length ? s.models : [modelKey(s.source)], anims: new Map(), plays: new Map(), kind: s.source.kind, type: s.source.id, forms,
                // A forms card starts its clock past the first fade-in, so it opens on a solid
                // model rather than on 650ms of nothing.
                yaw: REST_YAW + (reduce.matches ? 0 : ((cards.size * 1.7) % (Math.PI * 2))), clock: forms ? forms.fadeMs : 0, hover: false, drag: null, top: 0, left: 0, w: 0, h: 0, sprite: null, spriteYaw: NaN, uploaded: false };
            card.el.dataset.key = s.key;
            // What POST /api/loot/clear names the card by; fixed for a card, since a card's key is
            // either its one source's or its group's.
            card.groupKey = s.groupKey;
            // A forms card shows its models in the order FORMS names, including the ones no
            // loot source reports, so syncModels uploads them all and frame() can cycle them.
            if (forms) {
                const want = forms.order.filter(k => !card.models.includes(k));
                card.models = [...card.models, ...want].sort((a, c) => forms.order.indexOf(a) - forms.order.indexOf(c));
            }
            const stage = el('div', 'loot-stage');
            const fallback = el('div', 'loot-stage-fallback');
            // The icon fallback is an ITEM icon, so it only fits an obj source. A loc (a tree, a
            // rock) has no item of that id — item 1276 is not a tree — so it falls back to the
            // monogram, as an npc does. This is the path a build whose capture adapter predates
            // the loc branch takes for every Woodcutting and Mining card.
            //
            // A card titled by a SKILL falls back to that skill's own icon (web/skillicons, served
            // from this origin, so the page's img-src 'self' allows it) rather than to a monogram
            // or to whichever source happens to be first on it.
            if (s.source.skill && SKILL_ICONS.has(s.source.skill)) {
                const img = el('img', 'loot-skill-icon');
                img.src = `skillicons/${s.source.skill}.png`;
                img.alt = s.source.skill;
                fallback.append(img);
            } else if (s.source.kind === 'randomevent') {
                // One card for every event, so no one event's face stands for it: the Strange box
                // (the Mysterious box every event is remembered by) is the neutral picture.
                fallback.append(itemIconWrap(STRANGE_BOX, 'Random events'));
            } else if (!isNpcModel(s.source) && !isLocModel(s.source) && hasModel(s.source)) fallback.append(itemIconWrap(s.source.id, s.source.name));
            else fallback.innerHTML = `<svg viewBox="0 0 52 52" width="44" height="44" shape-rendering="crispEdges">${skillMonogram(cardTitle(s.source))}</svg>`;
            stage.append(fallback);
            const still = stillFor(s.source, s.parts?.length ?? 1);
            if (still) {
                const wrap = el('div', 'loot-map-wrap');
                const img = el('img', 'loot-map');
                img.src = still.src;
                img.alt = still.alt;
                img.decoding = 'async';
                wrap.append(img);
                stage.append(wrap);
                stage.classList.add('has-still');
                card.still = img;
            }
            card.stage = stage;
            const info = el('div', 'loot-info');
            const title = el('div', 'loot-title');
            card.nameEl = el('span', `loot-name ${isNpcModel(s.source) && s.source.kind !== 'randomevent' ? 'is-npc' : 'is-obj'}`, cardTitle(s.source));
            title.append(card.nameEl);
            if (s.source.kind === 'npc' && s.source.level) {
                card.levelEl = el('span', 'loot-level', ` (level-${s.source.level})`);
                title.append(card.levelEl);
            }
            card.verbEl = el('span', 'loot-verb');
            card.countEl = el('b', 'loot-count');
            const counts = el('div', 'loot-counts');
            counts.append(card.verbEl, card.countEl);
            card.metaEl = el('div', 'loot-meta');
            card.worthEl = el('div', 'loot-worth');
            info.append(title, counts, card.worthEl, card.metaEl);
            // The per-target breakdown of a card that merges different things: one row per npc
            // or stall on the Thieving card, per tree on Woodcutting, per bone on Prayer. A row
            // is a button that narrows the drop grid below to what that target gave.
            card.partsEl = el('div', 'loot-parts');
            card.partsEl.hidden = true;
            card.partsEl.addEventListener('click', ev => {
                const target = ev.target instanceof Element ? ev.target : null;
                const flip = (set, key) => { if (!set.delete(key)) set.add(key); card.partsSig = ''; drawnKey = ''; render(); };
                const arrow = target?.closest('[data-toggle]');
                if (arrow) return flip(card.open, arrow.dataset.toggle);
                const moreBtn = target?.closest('[data-more]');
                if (moreBtn) return flip(card.showAll, moreBtn.dataset.more);
                const row = target ? target.closest('.loot-part') : null;
                if (!row) return;
                card.partFilter = card.partFilter === row.dataset.part ? '' : row.dataset.part;
                card.itemsSig = ''; card.partsSig = '';
                drawnKey = ''; render();
            });
            card.grid = el('div', 'loot-drops');
            card.clearBtn = button('clear', `Clear everything recorded for ${cardTitle(s.source)}`, 'loot-clear');
            card.el.append(stage, info, card.clearBtn, card.partsEl, card.grid);
            stage.addEventListener('pointerenter', () => { card.hover = true; });
            stage.addEventListener('pointerleave', () => { card.hover = false; invalidate(); });
            stage.addEventListener('pointerdown', ev => {
                if (ev.button !== 0) return;
                card.drag = { x: ev.clientX, id: ev.pointerId };
                try { stage.setPointerCapture(ev.pointerId); } catch { /* moves still arrive over the stage */ }
                stage.classList.add('is-dragging');
                ev.preventDefault();
            });
            stage.addEventListener('pointermove', ev => {
                if (!card.drag || card.drag.id !== ev.pointerId) return;
                card.yaw += (ev.clientX - card.drag.x) * 0.012;
                card.drag.x = ev.clientX;
                // A map card has no model to turn; it tilts instead, held to a shallow angle so
                // it never reaches the edge-on sliver a real flat quad would show.
                if (card.still) steerStill(card);
                else if (sprites) paintSprite(card);
                else invalidate();
            });
            const endDrag = () => { card.drag = null; stage.classList.remove('is-dragging'); };
            stage.addEventListener('pointerup', endDrag);
            stage.addEventListener('pointercancel', endDrag);
            return card;
        }

        // Dragging a map card tilts it and takes it off its idle CSS sway, the way dragging a
        // model stage takes the model off its spin.
        function steerStill(card) {
            card.still.classList.add('is-held');
            const deg = Math.max(-26, Math.min(26, (card.yaw - REST_YAW) * 26));
            card.still.style.transform = `rotateY(${deg.toFixed(1)}deg)`;
        }

        // part is the row picked in the breakdown, if any: its drops are then that row's alone,
        // so every rate below divides by the row and names it (see dropRateOf).
        function slot(it, s, total, part) {
            const rare = RARE.has(it.id);
            const cell = el('div', `loot-slot${rare ? ' is-rare' : ''}`);
            cell.tabIndex = 0;
            cell.dataset.id = String(it.id);
            cell.append(itemIconWrap(it.id, it.name));
            const lab = stackLabel(it.count);
            cell.append(el('span', `loot-stack ${lab.cls}`, lab.text));
            // Out of the tab order (the stack itself is focusable, and Delete on it does the same): a
            // second stop per item would double a keyboard walk through every grid.
            const drop = button('\u00d7', `Remove ${it.name || `item #${it.id}`} from this ${part ? 'row' : 'card'}`, 'loot-slot-x');
            drop.tabIndex = -1;
            cell.append(drop);
            const rate = dropRateOf(it, s, part);
            const lines = [
                it.name || `item #${it.id}`,
                `${fmtCount(it.count)} total`,
                `${total ? ((it.count / total) * 100).toFixed(1) : '0'}% of ${rate.owner}'s drops${rate.per === null ? '' : ` · ${rate.per.toFixed(2)} per ${rate.unit}`}`
            ];
            // On a merged card, which targets gave it, each at its own rate.
            if (rate.fromRates.length) {
                lines.push(`from ${rate.fromRates.map(r => `${r.name} ${fmtCount(r.count)} (${r.per.toFixed(2)} per ${r.unit})`).join(', ')}`);
            }
            const each = perHour(it.count, rate.span);
            if (each !== null) lines.push(`${fmtRate(each)} per hour${lifetimeView() ? ' (lifetime average)' : ''}`);
            if (it.value) lines.push(`${fmtCount(it.value * it.count)} gp (${fmtCount(it.value)} each)`);
            if (rare) lines.push('Rare drop');
            details.set(cell, lines.join('\n'));
            cell.setAttribute('aria-label', lines.join(', '));
            return cell;
        }

        function sortedItems(items) {
            const byValue = view.sortItems === 'value';
            return [...items].sort((p, q) => (byValue ? (q.value ?? 0) * q.count - (p.value ?? 0) * p.count : q.count - p.count) || (p.name || '').localeCompare(q.name || ''));
        }

        /**
         * The per-target rows of a merged card (see partKeyOf): who was robbed, how often, for
         * what, by how many accounts. Shown whenever a card merges more than one target, which is
         * the Thieving card by design and also Woodcutting's trees, Fletching's materials and the
         * Prayer card's bones — the rows the grouping comment has always promised were "still
         * there underneath" and that the panel never actually drew.
         */
        function drawParts(card, s) {
            const parts = s.parts ?? [];
            const show = parts.length > 1;
            card.el.classList.toggle('has-parts', show);
            card.partsEl.hidden = !show;
            if (!show) { card.partsEl.textContent = ''; card.partsSig = ''; return; }
            // Alchemy folds into one row per kind, a long cost card into its busiest rows (compactParts);
            // a group opens to its best rows and sums the rest as "+K more".
            const entries = compactParts(parts, s.source, card.showAll);
            // The worth is in the signature too: prices move between polls (the market's median),
            // and a row keyed on counts alone kept showing the gp it was first drawn with.
            const rowSig = p => { const w = partWorthOf(p); return `${p.key}:${p.events}:${p.accounts}:${p.xp}:${w.gp}:${w.basis}:${p.items.map(it => it.id).join('.')}`; };
            const sig = `${card.partFilter}|${S.refLevel}|${[...card.open].join('+')}|${[...card.showAll].join('+')}|${view.period}|${entries.map(e => `${rowSig(e.part)}${e.children ? `[${e.children.map(rowSig).join(',')}${e.more ? `|${rowSig(e.more)}` : ''}]` : ''}`).join(',')}`;
            if (sig === card.partsSig) return;
            card.partsSig = sig;
            card.partsEl.textContent = '';
            const buildRow = (p, cls = '', toggle = null) => {
                const picked = card.partFilter === p.key;
                const row = el('button', `loot-part${picked ? ' is-picked' : ''}${cls}`);
                row.type = 'button';
                row.dataset.part = p.key;
                row.setAttribute('aria-pressed', String(picked));
                const verb = VERBS[p.action] ?? [p.action, p.action, `${p.action}s`];
                const name = el('span', 'loot-part-name', p.name || '?');
                if (toggle !== null) {
                    // The expander is its own target inside the row: it opens the group, the rest of the row picks it.
                    const arrow = el('span', 'loot-part-toggle', toggle ? '▾ ' : '▸ ');
                    arrow.dataset.toggle = p.key;
                    arrow.title = toggle ? 'Hide the items' : `Show the best ${COMPACT_TOP} items`;
                    name.prepend(arrow);
                    name.append(el('span', 'loot-part-level', ` (${p.count} items)`));
                }
                // A production row's level is not a combat level (the Runecraft rows carry the Runecrafting level read), so it is not
                // coloured against one.
                if (p.level > 0 && p.kind !== 'production' && !p.model.startsWith('loc:')) {
                    const lvl = el('span', 'loot-part-level', ` (${p.level})`);
                    lvl.style.color = combatColour(S.refLevel, p.level);
                    name.append(lvl);
                }
                // Net of the row's own input, as the card's headline is; "gross" when that input
                // has no trustworthy price, so the rows never add up to more than their card.
                const w = partWorthOf(p);
                const xpHour = p.xp > 0 && p.items.length ? perHour(p.xp, p, XP_TRACKED_FROM_MS) : null;
                const what = el('span', 'loot-part-count', `${verb[0]} ${fmtCount(p.events)}${p.xp > 0 && p.items.length ? ` · ${fmtCount(p.xp)} xp${xpHour === null ? '' : ` (${fmtRate(xpHour)}/h)`}` : ''}`);
                const worth = el('span', 'loot-part-worth', COST_KINDS.has(p.kind) || p.action === 'combat' ? `${fmtCount(p.cost ?? 0)} gp spent` : p.xp > 0 && !p.items.length ? `${fmtCount(p.xp)} xp` : `${fmtCount(w.gp)} gp${w.basis === 'gross' ? ' gross' : ''}`);
                if (w.basis) worth.title = w.basis === 'net' ? `net of ${fmtCount(p.costUnits)} x ${p.costItem} (${fmtCount(p.cost)} gp)` : `gross: no trustworthy price for the ${p.costItem} used`;
                const accts = el('span', 'loot-part-accts', `${fmtCount(p.accounts)} acct${p.accounts === 1 ? '' : 's'}`);
                const icons = el('span', 'loot-part-items');
                for (const it of p.items.slice(0, 3)) icons.append(itemIconWrap(it.id, it.name));
                if (!p.items.length && p.icon >= 0) icons.append(itemIconWrap(p.icon, p.name));
                row.append(name, what, worth, accts, icons);
                row.title = `${p.name}: ${verb[0].toLowerCase()} ${fmtCount(p.events)} time${p.events === 1 ? '' : 's'} by ${fmtCount(p.accounts)} account${p.accounts === 1 ? '' : 's'}${p.items.length ? ` · ${p.items.slice(0, 12).map(it => `${fmtCount(it.count)} ${it.name}`).join(', ')}${p.items.length > 12 ? `, +${p.items.length - 12} more` : ''}` : ''}\n${picked ? 'Click to show every target again' : 'Click to show only what this target gave'}`;
                return row;
            };
            // A "+K more" row only reveals the rest; it is not a target.
            const moreRow = (p, key) => {
                const row = el('button', 'loot-part loot-part-more');
                row.type = 'button';
                row.dataset.more = key;
                const w = partWorthOf(p);
                row.append(el('span', 'loot-part-name', p.name), el('span', 'loot-part-count', `${fmtCount(p.events)} in all`),
                    el('span', 'loot-part-worth', COST_KINDS.has(p.kind) ? `${fmtCount(p.cost ?? 0)} gp spent` : `${fmtCount(w.gp)} gp${w.basis === 'gross' ? ' gross' : ''}`), el('span', 'loot-part-accts', 'show all'));
                row.title = 'Show every row';
                return row;
            };
            for (const e of entries) {
                if (e.isMore) { card.partsEl.append(moreRow(e.part, 'rows')); continue; }
                const grouped = !!e.children;
                const open = grouped && card.open.has(e.part.key);
                card.partsEl.append(buildRow(e.part, grouped ? ' is-group' : '', grouped ? open : null));
                if (open) {
                    for (const c of e.children) card.partsEl.append(buildRow(c, ' is-child'));
                    if (e.more) card.partsEl.append(moreRow(e.more, e.part.key));
                }
            }
        }

        function fillCard(card, s, periodKey, query) {
            card.data = s; // what the clear controls read their confirm text from
            const verb = verbFor(s);
            // A card's models can change after it was made: a new target joins the Thieving card,
            // a new tree joins Woodcutting. makeCard fixed them once, so a joiner was never drawn
            // until the page reloaded.
            const want = s.models?.length ? s.models : [modelKey(s.source)];
            if (!card.forms && want.join(',') !== card.models.join(',')) { card.models = want; card.model = want[0]; }
            const xpCard = (s.xp ?? 0) > 0 && !(s.items ?? []).length;
            const costCard = COST_KINDS.has(s.source.kind);
            const countCard = COUNT_KINDS.has(s.source.kind);
            const lvl = card.levelEl ? combatColour(S.refLevel, s.source.level) : '';
            const hourly = perHour(s.events, s);
            const worth = sourceValue(s);
            const gpHour = perHour(worth, s);
            const skillsSig = (s.skills ?? []).map(k => `${k.id}:${k.count}`).join(',');
            const sig = `${s.events}|${s.last}|${s.accounts}|${lvl}|${Math.round(hourly * 100)}|${worth}|${s.cost ?? 0}|${s.costKnown ? 1 : 0}|${s.xp ?? 0}|${skillsSig}|${Math.floor((data.now - s.last) / 5000)}`;
            if (sig !== card.sig) {
                card.sig = sig;
                if (card.levelEl) card.levelEl.style.color = lvl;
                card.verbEl.textContent = verb[0];
                card.countEl.textContent = fmtCount(s.events);
                // What the source is worth. Which number LEADS changes with the period on
                // purpose: since-restart leads with gp/h, because that window is a real rate;
                // lifetime leads with the total and demotes its hourly figure to a muted
                // average, because dividing a lifetime by wall-clock is not a rate and must not
                // look like the same kind of number as the one beside it.
                const life = lifetimeView();
                card.worthEl.textContent = '';
                card.worthEl.classList.toggle('is-lifetime', life);
                if (countCard) {
                    // Essence handed over is a count (the line above) and a rate (hourly is per essence, one event each), never an
                    // amount of coins.
                    card.worthEl.append(el('b', '', hourly === null ? 'no rate yet' : `${fmtRate(hourly)} essence/h`),
                        el('span', '', life ? 'lifetime average handed to the mule' : 'handed to the mule since this restart'));
                } else if (costCard) {
                    const costHour = perHour(s.cost ?? 0, s);
                    card.worthEl.append(el('b', '', s.costKnown || !s.costItem ? `${fmtCount(s.cost ?? 0)} gp spent` : 'cost not priced'),
                        el('span', '', costHour === null ? 'in total' : `in total · ${fmtRate(costHour)} gp/h${life ? ' average' : ''}`));
                } else if (xpCard) {
                    // A card whose output is EXPERIENCE (the Prayer card: a burial yields no
                    // item) leads with it. Its gp figure is only what it used up, which is a cost,
                    // and leading with a negative number of coins would misread as a loss.
                    const xpHour = perHour(s.xp, s);
                    card.worthEl.append(el('b', '', `${fmtCount(s.xp)} ${s.source.skill || ''} XP`.replace('  ', ' ')),
                        el('span', '', xpHour === null ? 'earned in total' : `${fmtRate(xpHour)} xp/h${life ? ' average' : ''}`));
                } else if (life) {
                    card.worthEl.append(el('b', '', `${fmtCount(worth)} gp`),
                        el('span', '', gpHour === null ? 'looted in total' : `looted in total · ${fmtRate(gpHour)} gp/h average`));
                } else {
                    card.worthEl.append(el('b', '', `${fmtRate(gpHour)} gp/h`), el('span', '', `${fmtCount(worth)} gp total`));
                }
                const rate = hourly === null || countCard ? '' : `${fmtRate(hourly)} ${verb[2]}/h${life ? ' average' : ''} · `;
                // A card that CONSUMED something says so, because otherwise the figure above it
                // silently changed meaning. Three states, and the third is the point: netted,
                // nothing to net, or "we cannot price the input" — which leaves the number gross
                // and must not be passed off as profit (internal/controlpanel/lootcost.go).
                // Written "595 x Logs" rather than pluralised: the cache's own item names are
                // already plural as often as not ("Logs", "Oak logs"), and the first render of
                // this said "595 Logss spent".
                // A card that merges several inputs names each: "3 x Big bones, 2 x Bones", not the
                // first source's input against the whole card's count.
                const inputs = (s.parts ?? []).filter(p => p.costUnits > 0 && p.costItem);
                const spentWhat = inputs.length > 1 && new Set(inputs.map(p => p.costItem)).size > 1
                    ? inputs.map(p => `${fmtCount(p.costUnits)} x ${p.costItem}`).join(', ')
                    : `${fmtCount(s.costUnits)} x ${s.costItem}`;
                const spent = !s.costItem ? ''
                    : costCard ? ` · ${s.costKnown ? `${fmtCount(Math.round((s.cost ?? 0) / Math.max(1, s.events)))} gp each · ` : ''}${(s.parts ?? []).length > 1 ? `${s.parts.length} kinds` : s.costItem}`
                    // The Prayer card (owner, 2026-10-04: "count the bone prices"): the XP is
                    // set against what the bones would have sold for, as gp a point of XP.
                    : xpCard ? (s.costKnown ? ` · used ${spentWhat} (${fmtCount(s.cost)} gp${(s.xp ?? 0) > 0 ? `, ${(s.cost / s.xp).toFixed(1)} gp/XP` : ''})` : ` · used ${spentWhat}, no trustworthy price for it`)
                        : s.costKnown ? ` · net of ${spentWhat} (${fmtCount(s.cost)} gp)`
                            : ` · GROSS: ${spentWhat} spent, no trustworthy price for it`;
                // Experience per hour (a user request): a skill card says how much its own skill
                // earned and at what rate, an npc's card how much each combat skill earned from its
                // kills. The Prayer card leads with its own (xpCard above).
                const xpBits = [];
                if ((s.xp ?? 0) > 0 && !xpCard && s.source.skill) {
                    const h = perHour(s.xp, s, XP_TRACKED_FROM_MS);
                    xpBits.push(`${fmtCount(s.xp)} ${s.source.skill} xp${h === null ? '' : ` (${fmtRate(h)} xp/h${life ? ' average' : ''})`}`);
                }
                for (const k of s.skills ?? []) {
                    const h = perHour(k.count, s, XP_TRACKED_FROM_MS);
                    xpBits.push(`${SKILL_ABBR[k.name] ?? k.name} ${fmtCount(k.count)}${h === null ? '' : ` (${fmtRate(h)}/h)`}`);
                }
                const xpText = xpBits.length ? ` · xp: ${xpBits.join(', ')}` : '';
                card.metaEl.textContent = `${rate}${s.accounts} account${s.accounts === 1 ? '' : 's'} · last ${ago(data.now - s.last)}${spent}${xpText}`;
                card.el.setAttribute('aria-label', `${cardTitle(s.source)}${card.levelEl && s.source.level ? ` level ${s.source.level}` : ''}: ${verb[0].toLowerCase()} ${fmtCount(s.events)} times, ${xpCard ? `${fmtCount(s.xp)} experience` : `worth ${fmtCount(worth)} gp, ${fmtRate(gpHour)} gp per hour`}`);
            }
            drawParts(card, s);
            // A row picked in the breakdown narrows the grid to what that target gave.
            const part = card.partFilter ? findPart(s.parts ?? [], card.partFilter, s.source) : null;
            if (card.partFilter && !part) card.partFilter = '';
            const items = sortedItems(part ? part.items : (s.items ?? []));
            const total = items.reduce((n, it) => n + it.count, 0);
            // A gathering card stands its best yields around its source (see arrangePlaces).
            // Coins have no model worth showing on a plinth.
            if (SKILL_CARDS.has(s.source.kind)) {
                // From the whole card, not the row picked in the breakdown: the ring is the card.
                card.yields = sortedItems(s.items ?? []).filter(it => it.id !== COINS).slice(0, SATELLITES).map(it => `obj:${it.id}`);
            }
            const itemsSig = `${view.sortItems}|${view.period}|${card.partFilter}|${Math.floor(data.now / 60000)}|${items.map(it => `${it.id}:${it.count}`).join(',')}`;
            if (itemsSig !== card.itemsSig) {
                card.itemsSig = itemsSig;
                card.grid.textContent = '';
                for (const it of items) card.grid.append(slot(it, s, total, part));
                if (!items.length) card.grid.append(el('div', 'none', xpCard ? `burying pays ${s.source.skill || ''} experience, not items` : countCard ? 'the essence was valued when it was mined: handing it over is a count, not loot' : costCard ? 'a cost, not loot: what was spent is above' : part ? `nothing from ${part.name} yet` : 'nothing dropped yet'));
            }
            // Search: light up the matching cells, dim the rest.
            for (const cell of card.grid.children) {
                if (!cell.dataset.id) continue;
                const it = items.find(i => String(i.id) === cell.dataset.id);
                const hit = !!query && !!it && (String(it.id) === query || (it.name || '').toLowerCase().includes(query));
                cell.classList.toggle('is-match', hit);
                cell.classList.toggle('is-dim', !!query && !hit);
            }
            // Rare shimmer: a rare item whose count rose since the last refresh of this period.
            const first = !seenPeriods.has(periodKey);
            for (const it of items) {
                const k = `${periodKey}|${s.key}|${it.id}`;
                const before = seen.get(k);
                seen.set(k, it.count);
                if (!first && RARE.has(it.id) && it.count > (before ?? 0)) shimmer(card.grid.querySelector(`[data-id="${it.id}"]`));
            }
        }

        function shimmer(cell) {
            if (!cell || reduce.matches) return;
            // A fresh element starts its CSS animation from the beginning without a reflow.
            const shine = el('span', 'loot-shine');
            shine.addEventListener('animationend', () => shine.remove(), { once: true });
            cell.classList.add('is-new');
            cell.append(shine);
            setTimeout(() => cell.classList.remove('is-new'), 2600);
        }

        function sourceMatches(s, query) {
            if (!query) return true;
            // Every name on the card, not just the first source's: searching "baker" must find
            // the Thieving card the Baker's stall is on.
            if ([cardTitle(s.source), ...(s.names ?? [])].some(n => (n ?? '').toLowerCase().includes(query))) return true;
            return (s.items ?? []).some(it => String(it.id) === query || (it.name || '').toLowerCase().includes(query));
        }

        function render() {
            if (closed) return;
            const query = search.value.trim().toLowerCase();
            const key = `${view.period}|${view.kind}|${view.sortSources}|${view.sortItems}|${view.accounts}|${query}|${data?.now}|${failure}|${S.ticker}`;
            if (key === drawnKey) return;
            drawnKey = key;
            accountsBtn.classList.toggle('is-active', view.accounts);
            accountsBtn.setAttribute('aria-pressed', String(view.accounts));
            if (!data) {
                note.textContent = failure || 'loading…';
                totals.hidden = true; ticker.hidden = true; accountsWrap.hidden = true; runecraftWrap.hidden = true; deathsWrap.hidden = true;
                return;
            }
            if (data.available === false) {
                note.textContent = 'this fleet does not report loot';
                totals.hidden = true; ticker.hidden = true; accountsWrap.hidden = true; runecraftWrap.hidden = true; deathsWrap.hidden = true;
                for (const c of cards.values()) c.el.remove();
                cards.clear();
                layoutDirty = true; invalidate();
                return;
            }
            const P = data[view.period] ?? { sources: [], accounts: [], events: {} };
            const periodKey = view.period;
            // Normalised before the comparison, so a record still carrying the retired
            // "fishing" kind is shown under Gathering rather than vanishing from every filter.
            const kindOk = s => {
                if (view.kind === 'all') return true;
                const src = normaliseSource(s.source);
                // Runecrafting is a filter by what the source is, not by its kind: the Runecraft card is a production source.
                if (view.kind === 'deaths') return false; // the Deaths section is not a source card
                return view.kind === 'runecraft' ? isRunecraftSource(src) : src?.kind === view.kind;
            };
            const inKind = mergeSameName((P.sources ?? []).filter(kindOk));
            const since = view.period === 'sinceRestart' ? data.processStart : data.trackingSince;
            note.textContent = `${failure ? `${failure} · ` : ''}${view.period === 'sinceRestart' ? 'since this restart' : 'lifetime'}, tracking for ${fmtDuration(data.now - since)} · ${modelNote || 'models load when the panel is on screen'}`;
            const deathsOnly = view.kind === 'deaths';
            totals.hidden = deathsOnly;
            if (!deathsOnly) {
                drawTotals(P, periodKey);
                drawTopDrops(inKind);
                drawTicker();
                drawAccounts(P);
            } else {
                ticker.hidden = true; accountsWrap.hidden = true;
            }
            drawRunecraftSection(P);
            drawDeathsSection(P);

            const list = inKind.filter(s => sourceMatches(s, query));
            if (view.sortSources === 'recent') list.sort((a, b) => b.last - a.last || b.events - a.events);
            else if (view.sortSources === 'value') list.sort((a, b) => sourceValue(b) - sourceValue(a) || b.events - a.events);
            // A source too new to have a rate sorts last rather than sorting as NaN.
            else if (view.sortSources === 'rate') list.sort((a, b) => (perHour(sourceValue(b), b) ?? -1) - (perHour(sourceValue(a), a) ?? -1) || sourceValue(b) - sourceValue(a));
            else list.sort((a, b) => b.events - a.events || b.last - a.last);
            const keep = new Set(list.map(s => s.key));
            for (const [k, c] of cards) if (!keep.has(k)) { c.el.remove(); cards.delete(k); }
            list.forEach((s, i) => {
                let card = cards.get(s.key);
                if (!card) { card = makeCard(s); cards.set(s.key, card); }
                fillCard(card, s, periodKey, query);
                if (cardsWrap.children[i] !== card.el) cardsWrap.insertBefore(card.el, cardsWrap.children[i] ?? null);
            });
            seenPeriods.add(periodKey);
            let empty = cardsWrap.querySelector(':scope > .none');
            if (!list.length) {
                if (!empty) { empty = el('div', 'none'); cardsWrap.append(empty); }
                empty.textContent = deathsOnly ? '' : query ? `no source drops “${search.value.trim()}” in this period` : view.kind === 'trail' ? TRAILS_EMPTY : 'nothing looted in this period yet';
            } else empty?.remove();
            ctx.setSub(`${list.length} source${list.length === 1 ? '' : 's'}${modelState !== 'ready' ? '' : renderer && !renderer.lost ? ' · WebGL' : ' · Canvas'}`);
            syncModels();
            layoutDirty = true;
            invalidate();
        }

        // ---- clearing: a card, or one item off it (POST /api/loot/clear) ----

        // seen remembers each stack's last count for the rare-drop shimmer, by period, card and item. A
        // cleared card or stack must not leave a count behind for a later, smaller one to be compared with.
        function forgetSeen(cardKey, itemId) {
            for (const { value: period } of PERIODS) {
                const prefix = `${period}|${cardKey}|`;
                if (itemId !== undefined) seen.delete(prefix + itemId);
                else for (const k of seen.keys()) if (k.startsWith(prefix)) seen.delete(k);
            }
        }

        // The server answers with the new /api/loot body, so it is drawn without asking again. A refresh
        // that began before the clear is then ignored (loadEpoch), or it would put the card back.
        async function sendClear(body, what, cardKey) {
            clearing = true;
            root.classList.add('is-clearing');
            let done = false;
            try {
                const res = await fetch('api/loot/clear', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
                const answer = await res.json().catch(() => null);
                if (closed) return;
                if (res.ok && answer?.lifetime) {
                    data = answer; failure = ''; fetchedAt = Date.now(); loadEpoch++; done = true;
                    forgetSeen(cardKey, body.item);
                } else failure = `${what} failed: ${answer?.error || `HTTP ${res.status}`}`;
            } catch {
                if (!closed) failure = `${what} failed: the request did not go through`;
            } finally {
                clearing = false;
                root.classList.remove('is-clearing');
            }
            if (closed) return;
            // The stack the tooltip was over may be gone, and a removed node never sends its pointerout.
            hideTip();
            drawnKey = ''; render();
            if (done) ensureModels();
        }

        function askClearCard(card) {
            const s = card.data;
            if (!s || clearing) return;
            const title = cardTitle(s.source);
            const sure = confirm(clearPrompt({ title, verb: verbFor(s)[2], events: s.events ?? 0, rows: s.parts?.length ?? 1, stacks: (s.items ?? []).length }));
            if (sure) sendClear(clearBody(card.groupKey), `clearing ${title}`, card.key);
        }

        function askClearItem(card, id) {
            const s = card.data;
            if (!s || clearing || !Number.isInteger(id)) return;
            // With a row picked the grid shows that row's stacks, so it is that row the stack comes off.
            const part = card.partFilter ? findPart(s.parts ?? [], card.partFilter, s.source) : null;
            const it = (part ? part.items : s.items ?? []).find(i => i.id === id);
            if (!it) return;
            const name = it.name || `item #${id}`;
            const sure = confirm(clearPrompt({ title: cardTitle(s.source), verb: verbFor(s)[2], item: name, count: it.count, part: part ? (part.name || 'this') : '' }));
            if (sure) sendClear(clearBody(card.groupKey, id, part && !part.key.startsWith('group:') ? part.key : undefined), `removing ${name}`, card.key);
        }

        // One listener each for every card and stack on the page.
        const cardOf = node => {
            const c = node.closest('.loot-card');
            return c ? cards.get(c.dataset.key) : null;
        };
        cardsWrap.addEventListener('click', ev => {
            const hit = ev.target instanceof Element ? ev.target.closest('.loot-clear, .loot-slot-x') : null;
            const card = hit && cardOf(hit);
            if (!card) return;
            if (hit.classList.contains('loot-clear')) askClearCard(card);
            else askClearItem(card, Number(hit.closest('.loot-slot')?.dataset.id));
        });
        // The cross is out of the tab order, so Delete on a focused stack does the same.
        cardsWrap.addEventListener('keydown', ev => {
            if (ev.key !== 'Delete' || !(ev.target instanceof Element) || !ev.target.classList.contains('loot-slot')) return;
            const card = cardOf(ev.target);
            if (!card) return;
            ev.preventDefault();
            askClearItem(card, Number(ev.target.dataset.id));
        });

        // ---- tooltips (one per panel, like the combined bank's) ----

        let tipOwner = null;
        function hideTip() { if (tipOwner) tipOwner.removeAttribute('aria-describedby'); tipOwner = null; tooltip.hidden = true; }
        function showTip(cell) {
            const text = details.get(cell);
            if (!text) return;
            tipOwner = cell;
            tooltip.textContent = text;
            cell.setAttribute('aria-describedby', tooltip.id);
            tooltip.hidden = false;
            tooltip.style.left = '8px'; tooltip.style.top = '8px';
            const rect = cell.getBoundingClientRect();
            const left = Math.max(8, Math.min(rect.left, window.innerWidth - tooltip.offsetWidth - 8));
            const below = rect.bottom + 4;
            tooltip.style.left = `${left}px`;
            tooltip.style.top = `${below + tooltip.offsetHeight <= window.innerHeight - 8 ? below : Math.max(8, rect.top - tooltip.offsetHeight - 4)}px`;
        }
        const tipTarget = ev => ev.target instanceof Element ? ev.target.closest('.loot-slot, .loot-topchip') : null;
        inner.addEventListener('pointerover', ev => { const c = tipTarget(ev); if (c && c !== tipOwner) showTip(c); });
        inner.addEventListener('pointerout', ev => { if (tipOwner && !tipOwner.contains(ev.relatedTarget)) hideTip(); });
        inner.addEventListener('focusin', ev => { const c = tipTarget(ev); if (c) showTip(c); });
        inner.addEventListener('focusout', hideTip);
        // A row of the per-account table selects that account, which detail panels follow.
        accountsWrap.addEventListener('click', ev => {
            const row = ev.target instanceof Element ? ev.target.closest('tr[data-name]') : null;
            if (row) ctx.store.select(row.dataset.name);
        });
        const onKey = ev => { if (ev.key === 'Escape') hideTip(); };
        window.addEventListener('keydown', onKey);
        scroller.addEventListener('scroll', () => { hideTip(); invalidate(); }, { passive: true });

        // ---- models: loading ----

        function modelRequests() {
            const out = new Map();
            for (const period of ['lifetime', 'sinceRestart']) for (const raw of data?.[period]?.sources ?? []) {
                const s = { ...raw, source: normaliseSource(raw.source) };
                if (!hasModel(s.source)) continue;
                const k = modelKey(s.source);
                if (!out.has(k)) {
                    // shape 10 is the ordinary centrepiece shape every tree and rock uses.
                    out.set(k, isNpcModel(s.source) ? { key: k, kind: 'npc', type: s.source.id }
                        : isLocModel(s.source) ? { key: k, kind: 'loc', id: s.source.id, shape: 10 }
                        : { key: k, kind: 'obj', id: s.source.id });
                }
                // A forms card also needs the models no loot source reports (the Kalphite
                // Queen's dying form; see FORMS).
                for (const type of formsFor(s.source)?.extra ?? []) {
                    const ek = `npc:${type}`;
                    if (!out.has(ek)) out.set(ek, { key: ek, kind: 'npc', type });
                }
                // A gathering card also needs its yields' item models for the ring around the
                // source. Only the few it actually shows, and only once across the whole panel.
                if (SKILL_CARDS.has(s.source.kind)) {
                    for (const it of sortedItems(s.items ?? []).filter(i => i.id !== COINS).slice(0, SATELLITES)) {
                        const yk = `obj:${it.id}`;
                        if (!out.has(yk)) out.set(yk, { key: yk, kind: 'obj', id: it.id });
                    }
                }
            }
            return [...out.values()];
        }

        function poseSeq(mesh) {
            return poseList(mesh)[0] ?? -1;
        }

        /**
         * Every sequence this npc actually has, best first, capped at PLAYLIST_MAX.
         *
         * Read from the cache rather than assumed, and what it holds is worth stating exactly,
         * because users asked for attack and death poses and they are not available at any
         * price. In client revision 289 an npc type carries ONLY readyanim and the four
         * walkanims (src/client/config/NpcType.ts: readyanim, walkanim, walkanim_b, walkanim_r,
         * walkanim_l). Attack and death are not properties of the npc at all — the server tells
         * the client which sequence to play at the moment it happens, from content the cache
         * never sees. So rebuilding the capture adapter would export nothing further; there is
         * nothing further there.
         *
         * What there IS, measured across the thirteen npcs this panel draws: the three human
         * models have five distinct sequences (a stand, a walk and three directional walks),
         * most creatures have two, and the Strange plant, the Mime and the Queen's flying form
         * have exactly one and simply keep looping it.
         */
        function poseList(mesh) {
            const a = mesh?.anims;
            if (!a) return [];
            const order = S.pose === 'walk'
                ? [a.walk, a.ready, a.walk_b, a.walk_l, a.walk_r]
                : [a.ready, a.walk, a.walk_b, a.walk_l, a.walk_r];
            const out = [];
            for (const seq of order) {
                if (seq >= 0 && !out.includes(seq) && out.length < PLAYLIST_MAX) out.push(seq);
            }
            return out;
        }

        function setModelNote(state, text) {
            modelState = state;
            if (text === modelNote) return;
            modelNote = text;
            drawnKey = ''; render();
        }

        async function ensureModels() {
            if (!modelsOn || modelBusy || closed || !onScreen || !data) return;
            lastEnsure = performance.now();
            const meta = ctx.store.meta;
            if (!modelsUsable(meta)) {
                if (meta) {
                    modelsOn = false;
                    root.classList.add('no-models');
                    setModelNote('icons', 'icons: this panel runs without the capture adapter (-capture-renderer, with a stream client installed), so models cannot load');
                }
                return;
            }
            const reqs = modelRequests();
            if (!reqs.length) return;
            const missing = reqs.filter(r => meshWanted(r.key));
            const wantAnim = !reduce.matches && S.spin;
            const anims = wantAnim ? reqs.filter(r => r.kind === 'npc' && poseList(cachedMesh(r.key)).some(seq => animationWanted(`${r.key}|${seq}`))) : [];
            if (!missing.length && !anims.length && textures) { syncModels(); return; }
            modelBusy = true;
            if (modelState === 'waiting') setModelNote('loading', 'decoding the game cache for the models…');
            try {
                // The adapter itself: a failure here (not installed, stale, init deadline) is
                // final for this page, and the cards keep their icons.
                if (!textures) { textures = await textureAverages(meta); renderer?.setTextureAverages(textures); }
            } catch (error) {
                modelBusy = false;
                if (closed) return;
                if (error?.slow) { setModelNote('loading', 'still decoding the game cache for the models…'); return; }
                modelsOn = false;
                root.classList.add('no-models');
                setModelNote('icons', `icons: models unavailable (${error?.message || error})`);
                return;
            }
            let problem = '';
            try {
                if (!closed && missing.length) await loadMeshes(meta, missing);
                syncModels();
                if (wantAnim) {
                    // Every sequence the npc has, not just one: the card cycles them (see
                    // poseList). The first is loaded before the rest so a card starts moving
                    // rather than waiting for the whole playlist.
                    for (const r of reqs) {
                        if (closed) break;
                        const mesh = cachedMesh(r.key);
                        if (r.kind !== 'npc' || !mesh) continue;
                        for (const seq of poseList(mesh)) {
                            if (closed) break;
                            if (await loadAnimation(meta, r.key, r.type, seq)) syncModels();
                        }
                    }
                }
            } catch (error) {
                // One model or sequence failing leaves the others; it is retried later.
                problem = ` (some still loading: ${error?.message || error})`;
            } finally {
                modelBusy = false;
            }
            if (!closed) setModelNote('ready', `${renderer && !renderer.lost ? 'real cache models (WebGL)' : 'real cache models (Canvas 2D stills: no hardware WebGL)'}${problem}`);
            // A model still downloading is asked for again on a later beat (update, every ~5 s).
        }

        // Hand every loaded mesh/animation to the renderer (once), and mark the cards that have one.
        function syncModels() {
            for (const card of cards.values()) {
                const mesh = cachedMesh(card.model);
                if (renderer && !renderer.lost) {
                    // Every model on this card, not just the first: a merged card draws one
                    // stage per distinct appearance, and each needs its own mesh uploaded
                    // and its own animation resolved.
                    let any = false;
                    for (const key of [...card.models, ...(card.yields ?? [])]) {
                        const m = cachedMesh(key);
                        if (!m) { card.anims.delete(key); continue; }
                        any = true;
                        if (!renderer.hasModel(key)) renderer.setModel(key, m);
                        // The whole playlist, in order, keeping only the sequences that have
                        // actually arrived. A card with one plays it on a loop as before.
                        const list = [];
                        for (const sq of poseList(m)) {
                            const k = `${key}|${sq}`;
                            const a = cachedAnimation(k);
                            if (!a) continue;
                            if (!renderer.hasAnimation(k)) renderer.setAnimation(k, a);
                            list.push({ key: k, loopMs: loopLength(a.delays) });
                        }
                        const play = !reduce.matches && S.spin;
                        card.plays.set(key, play ? list : []);
                        card.anims.set(key, play && list.length ? list[0].key : '');
                    }
                    card.anim = card.anims.get(card.model) ?? '';
                    card.stage.classList.toggle('has-model', any);
                } else if (sprites && mesh) {
                    card.stage.classList.add('has-model');
                    if (!card.sprite) {
                        card.sprite = el('canvas', 'loot-sprite');
                        card.sprite.setAttribute('aria-hidden', 'true');
                        card.stage.append(card.sprite);
                    }
                    if (card.spriteYaw !== card.yaw) paintSprite(card);
                }
            }
            invalidate();
        }

        let spriteRaf = 0;
        const spriteQueue = new Set();
        function paintSprite(card) {
            spriteQueue.add(card);
            if (!spriteRaf) spriteRaf = requestAnimationFrame(() => {
                spriteRaf = 0;
                const dpr = Math.min(devicePixelRatio || 1, 2);
                for (const c of spriteQueue) {
                    const mesh = cachedMesh(c.model);
                    if (!mesh || !c.sprite || !c.el.isConnected) continue;
                    const w = c.stage.clientWidth, h = c.stage.clientHeight;
                    c.sprite.width = Math.max(1, Math.round(w * dpr)); c.sprite.height = Math.max(1, Math.round(h * dpr));
                    drawModelSprite(c.sprite, mesh, c.yaw, textures);
                    c.spriteYaw = c.yaw;
                }
                spriteQueue.clear();
            });
        }

        // ---- models: drawing ----

        let raf = 0;
        let lastFrame = 0;
        let layoutDirty = true;
        let viewW = 0, viewH = 0;

        // A model's own bounds, for sizing the ring and the satellites on it.
        const boundsOf = key => cachedMesh(key)?.bounds;
        // The arc a model can be shown through, or null when it survives a whole turn. Measured
        // once per mesh and cached on its bounds, so this is a lookup after the first frame.
        const arcOf = key => { const m = cachedMesh(key); return m ? viewableArc(m) : null; };

        const docVisible = () => document.visibilityState === 'visible';
        const drawing = () => !!renderer && !closed && onScreen && docVisible() && !renderer.lost;
        const moving = () => !reduce.matches && S.spin;

        function relayout() {
            layoutDirty = false;
            const base = scroller.getBoundingClientRect();
            const st = scroller.scrollTop, sl = scroller.scrollLeft;
            for (const c of cards.values()) {
                const r = c.stage.getBoundingClientRect();
                c.top = r.top - base.top + st; c.left = r.left - base.left + sl; c.w = r.width; c.h = r.height;
            }
        }

        function resizeCanvas() {
            const dpr = Math.min(devicePixelRatio || 1, 2);
            const w = Math.max(1, Math.round(viewW * dpr)), h = Math.max(1, Math.round(viewH * dpr));
            if (glCanvas.width !== w || glCanvas.height !== h) { glCanvas.width = w; glCanvas.height = h; }
            return dpr;
        }

        // Something moved (scroll, resize, data, a model arriving): the next vsync draws even
        // inside the frame cap, so a model keeps up with its card while the list scrolls.
        let urgent = false;
        function invalidate() {
            urgent = true;
            if (!raf && drawing()) raf = requestAnimationFrame(frame);
        }

        function frame(now) {
            raf = 0;
            if (!drawing()) return;
            // The spin is capped at ~30 fps: a vsync that comes too soon just waits for the next.
            if (!urgent && lastFrame && now - lastFrame < FRAME_MS - 1) { raf = requestAnimationFrame(frame); return; }
            urgent = false;
            const dt = lastFrame ? Math.min(0.1, (now - lastFrame) / 1000) : 0;
            lastFrame = now;
            if (layoutDirty) relayout();
            const dpr = resizeCanvas();
            const st = scroller.scrollTop, sl = scroller.scrollLeft;
            const stages = [];
            let turning = false;
            for (const c of cards.values()) {
                const ready = c.models.filter(m => renderer.hasModel(m));
                if (!ready.length) continue;
                const y = c.top - st, x = c.left - sl;
                if (y + c.h <= 0 || y >= viewH || x + c.w <= 0 || x >= viewW) continue;
                // A model the cache did not author all the way round rocks through the arc it
                // does support instead of turning through the half where it is not there.
                const arc = arcOf(ready[0]);
                if (moving()) {
                    if (!c.hover && !c.drag) {
                        if (arc) c.yaw = arc.centre + Math.sin(c.clock / 1000 * SWAY) * arc.half * 0.92;
                        else c.yaw += SPIN * dt;
                    }
                    c.clock += dt * 1000;
                    turning = true;
                } else if (arc && !c.drag) {
                    // Still poses settle inside the arc rather than at whatever REST_YAW is.
                    c.yaw = arc.centre;
                }
                // Dragging such a card steers it, but only as far as the model holds up.
                if (arc && c.drag) c.yaw = Math.max(arc.centre - arc.half, Math.min(arc.centre + arc.half, c.yaw));
                // A forms card (the Kalphite Queen; see FORMS) is one creature at different
                // times, so it keeps ONE stage and dissolves between its forms instead of
                // showing them side by side. Under prefers-reduced-motion the clock never
                // advances, so it settles on the first form at full strength rather than
                // freezing part-way through a fade.
                if (c.forms) {
                    const order = c.forms.order.filter(m => renderer.hasModel(m));
                    const pick = moving() ? formAt(order, c.clock, c.forms.holdMs, c.forms.fadeMs) : { model: order[0], fade: 1 };
                    if (pick && pick.model) {
                        // A form plays its own sequences while it is the one showing, so the two
                        // mechanisms compose rather than competing.
                        const list = c.plays.get(pick.model) ?? [];
                        const at = list.length > 1 ? playlistAt(list, c.clock) : null;
                        stages.push({ model: pick.model, anim: at ? at.key : (c.anims.get(pick.model) ?? ''), yaw: c.yaw,
                            elapsed: at ? at.elapsed : c.clock, fade: pick.fade, rect: { x, y, w: c.w, h: c.h } });
                    }
                    continue;
                }
                // One stage, one pedestal, everything arranged on it: a gathering card's source
                // in the middle with its yields small around it, a merged same-name card's
                // several ids evenly on the ring. Until now each model got a column and a
                // plinth of its own.
                const yields = (c.yields ?? []).filter(m => renderer.hasModel(m));
                const gathering = SKILL_CARDS.has(c.kind) && yields.length > 0;
                // Each model plays through the sequences it has rather than looping one (see
                // playlistAt). elapsed is per placement, so two models on one plinth are not
                // forced onto the same point of the same sequence.
                const playing = m => {
                    const list = c.plays.get(m) ?? [];
                    if (list.length < 2) return { anim: c.anims.get(m) ?? '', elapsed: c.clock };
                    const at = playlistAt(list, c.clock);
                    return { anim: at.key, elapsed: at.elapsed };
                };
                const places = gathering
                    ? arrangePlaces(ready[0], [...ready.slice(1), ...yields], boundsOf, m => playing(m).anim)
                    : arrangePlaces(ready.length === 1 ? ready[0] : null, ready.length === 1 ? [] : ready, boundsOf, m => playing(m).anim);
                for (const p of places) p.elapsed = playing(p.model).elapsed;
                if (places.length) stages.push({ place: places, yaw: c.yaw, elapsed: c.clock, rect: { x, y, w: c.w, h: c.h } });
            }
            if (stages.length) renderer.draw(stages, dpr);
            else renderer.clear();
            if (turning) raf = requestAnimationFrame(frame);
            else lastFrame = 0;
        }

        // ---- visibility: the panel on screen, the tab in front ----

        const io = new IntersectionObserver(entries => {
            const was = onScreen;
            onScreen = entries.some(e => e.isIntersecting);
            if (onScreen && !was) {
                if (Date.now() - fetchedAt >= REFRESH_EVERY_MS) load();
                ensureModels();
                layoutDirty = true;
                invalidate();
            }
            if (!onScreen) { cancelAnimationFrame(raf); raf = 0; lastFrame = 0; }
        });
        io.observe(viewport);
        const onVisibility = () => {
            if (docVisible()) { layoutDirty = true; invalidate(); }
            else { cancelAnimationFrame(raf); raf = 0; lastFrame = 0; }
        };
        document.addEventListener('visibilitychange', onVisibility);
        const onMotion = () => {
            for (const c of cards.values()) if (reduce.matches) { c.yaw = REST_YAW; c.clock = 0; }
            syncModels();
        };
        reduce.addEventListener('change', onMotion);
        const ro = new ResizeObserver(() => {
            viewW = scroller.clientWidth || viewport.clientWidth;
            viewH = viewport.clientHeight;
            layoutDirty = true;
            invalidate();
        });
        ro.observe(viewport);
        ro.observe(cardsWrap);
        ro.observe(inner);
        // Expose the renderer's counters for measurement (read-only; see docs/control_panel_gpu.md).
        glCanvas.lootStats = renderer?.stats ?? null;

        changed();

        return {
            update(store, why) {
                if (!anyOf(why, 'init', 'status', 'clock', 'reset', 'meta')) return;
                if (store.meta && store.meta.loot === false) {
                    data = null; failure = 'this fleet does not report loot';
                    drawnKey = ''; render();
                    return;
                }
                if (onScreen && !busy && Date.now() - fetchedAt >= REFRESH_EVERY_MS) load();
                if (anyOf(why, 'meta', 'init') || performance.now() - lastEnsure > 5000) ensureModels();
            },
            destroy() {
                closed = true;
                aborter?.abort();
                cancelAnimationFrame(raf); cancelAnimationFrame(countRaf); cancelAnimationFrame(spriteRaf);
                io.disconnect(); ro.disconnect();
                document.removeEventListener('visibilitychange', onVisibility);
                reduce.removeEventListener('change', onMotion);
                window.removeEventListener('keydown', onKey);
                hideTip(); tooltip.remove();
                renderer?.destroy(); renderer = null;
                cards.clear();
            }
        };
    }
};
