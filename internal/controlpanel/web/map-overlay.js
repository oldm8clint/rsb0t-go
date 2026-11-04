import { el, button } from './util.js';

// Map panel chrome that floats ON the map instead of taking layout space above or below it,
// so a map panel is exactly its map and never needs a scroll bar. Shared by the account map
// (panels/map.js) and the travel heat map (panels/heatmap.js).
//
// mapOverlayBar(wrap, controls) puts the controls in a translucent cluster in the map's top
// right corner. The cluster itself lets pointer events through (so dragging the map works
// right up to the buttons); only the controls take them. When the map is narrower than
// COLLAPSE_BELOW the controls fold behind one "Map ▾" button that opens them as a small menu.
// The map itself gets a 'narrow' class at the same width so other chrome (legends, status
// lines) can shrink too. Returns { bar, fit() }: call fit() whenever the map is resized.

const COLLAPSE_BELOW = 520;

export function mapOverlayBar(wrap, controls, label = 'Map') {
    const bar = el('div', 'mapbar');
    const toggle = button(`${label} ▾`, `Show the ${label.toLowerCase()} controls`, 'mapbar-toggle');
    const items = el('div', 'mapbar-items');
    items.append(...controls);
    bar.append(toggle, items);
    toggle.addEventListener('click', () => bar.classList.toggle('open'));
    // A choice made from the folded menu closes it again, so it does not sit over the map.
    items.addEventListener('change', ev => { if (ev.target.tagName === 'SELECT' && bar.classList.contains('collapsed')) bar.classList.remove('open'); });
    items.addEventListener('click', ev => { if (ev.target.closest('button') && bar.classList.contains('collapsed')) bar.classList.remove('open'); });
    // Leaflet listens on its own container, which is a sibling of this bar, so pointer and
    // wheel events on the controls never pan or zoom the map; nothing else needs stopping.
    wrap.append(bar);
    function fit() {
        const narrow = wrap.clientWidth > 0 && wrap.clientWidth < COLLAPSE_BELOW;
        bar.classList.toggle('collapsed', narrow);
        wrap.classList.toggle('narrow', narrow);
        if (!narrow) bar.classList.remove('open');
    }
    fit();
    return { bar, fit };
}

/** Marks a panel body as holding a full-bleed map: no padding scroll, the map fills it. */
export function fillPanel(body) {
    body.classList.add('mapbody');
}
