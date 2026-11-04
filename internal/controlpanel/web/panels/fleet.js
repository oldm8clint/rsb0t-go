import { anyOf, el, fmtBytes, kv, pick } from '../util.js';

const ORDER = ['ingame', 'connecting', 'queued', 'reconnecting', 'offline', 'stopped', 'error'];

export default {
    type: 'fleet',
    title: 'Fleet overview',
    description: 'How many accounts are in each state, and what the process is using.',
    size: { w: 3, h: 2 },
    accounts: 'many',
    settings: [
        { key: 'hideEmpty', label: 'Hide states no account is in', kind: 'bool', default: false },
        { key: 'memory', label: 'Show process memory', kind: 'bool', default: true }
    ],
    create(body, ctx) {
        const tiles = el('div', 'tiles');
        const mem = el('dl', 'kv');
        mem.hidden = !ctx.settings.memory;
        body.append(tiles, mem);
        let countsKey = '';
        const memoryCells = new Map();
        const put = (term, value) => {
            let cell = memoryCells.get(term);
            if (!cell) { cell = kv(mem, term, value); memoryCells.set(term, cell); }
            else if (cell.textContent !== String(value)) cell.textContent = String(value);
        };
        return {
            update(store, why) {
                if (anyOf(why, 'init', 'status')) {
                    const list = pick(store.accounts, ctx.accounts);
                    const counts = Object.fromEntries((store.meta?.states ?? ORDER).map(s => [s, 0]));
                    for (const a of list) counts[a.state] = (counts[a.state] ?? 0) + 1;
                    const key = JSON.stringify(counts);
                    if (key !== countsKey) {
                        countsKey = key; tiles.textContent = '';
                        for (const [state, n] of Object.entries(counts)) {
                            if (ctx.settings.hideEmpty && n === 0) continue;
                            const tile = el('div', 'tile');
                            tile.append(el('b', '', n), el('span', `st st-${state}`, state));
                            tiles.append(tile);
                        }
                    }
                    const total = store.accounts.length + store.truncated;
                    ctx.setSub(ctx.accounts.length ? `${list.length} of ${total} accounts` : `${total} accounts`);
                }
                if (ctx.settings.memory && anyOf(why, 'init', 'memory')) {
                    const m = store.memory[store.memory.length - 1];
                    if (!m) {
                        if (!memoryCells.size) put('memory', 'no sample yet');
                        return;
                    }
                    if (memoryCells.has('memory')) { mem.replaceChildren(); memoryCells.clear(); }
                    put('heap in use', fmtBytes(m.heapInuse));
                    put('reserved from the OS', fmtBytes(m.sys));
                    put('goroutines', m.goroutines);
                    put('GC cycles', m.numGC);
                    put('last GC pause', `${(m.lastPauseNs / 1e6).toFixed(2)} ms`);
                }
            }
        };
    }
};
