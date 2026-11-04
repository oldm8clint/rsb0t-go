// Small DOM and formatting helpers every panel shares.

export function el(tag, cls, text) {
    const node = document.createElement(tag);
    if (cls) node.className = cls;
    if (text !== undefined && text !== null) node.textContent = String(text);
    return node;
}

export function button(text, title, cls) {
    const b = el('button', cls, text);
    b.type = 'button';
    if (title) {
        b.title = title;
        b.setAttribute('aria-label', title);
    }
    return b;
}

/** One term and its value in a <dl class="kv">. Returns the value cell. */
export function kv(dl, term, value) {
    const dd = el('dd');
    if (value instanceof Node) dd.append(value);
    else dd.textContent = value === undefined || value === null || value === '' ? '-' : String(value);
    dl.append(el('dt', '', term), dd);
    return dd;
}

/** A table whose rows select their account when clicked. Each row starts with the account name. */
export function table(headers, rows) {
    const t = el('table');
    const head = el('tr');
    for (const h of headers) head.append(el('th', '', h));
    const thead = el('thead');
    thead.append(head);
    const tbody = el('tbody');
    for (const row of rows) {
        const tr = el('tr');
        tr.dataset.name = String(row[0]);
        for (const cell of row) {
            const td = el('td');
            if (cell instanceof Node) td.append(cell);
            else {
                td.textContent = String(cell);
                td.title = String(cell);
            }
            tr.append(td);
        }
        tbody.append(tr);
    }
    t.append(thead, tbody);
    return t;
}

export function fmtBytes(n) {
    if (!Number.isFinite(n)) return '-';
    const units = ['B', 'KB', 'MB', 'GB'];
    let i = 0;
    while (n >= 1024 && i < units.length - 1) {
        n /= 1024;
        i++;
    }
    return `${i === 0 || n >= 10 ? n.toFixed(0) : n.toFixed(1)} ${units[i]}`;
}

export function fmtDuration(ms) {
    if (!(ms >= 0)) return '-';
    const s = Math.floor(ms / 1000);
    if (s < 60) return `${s}s`;
    const m = Math.floor(s / 60);
    if (m < 60) return `${m}m ${s % 60}s`;
    const h = Math.floor(m / 60);
    if (h < 48) return `${h}h ${m % 60}m`;
    return `${Math.floor(h / 24)}d ${h % 24}h`;
}

export function fmtClock(t) {
    return new Date(t).toTimeString().slice(0, 8);
}

// Reuse locale resolution and formatter state across fleet-sized tables.
const countFormatter = new Intl.NumberFormat();
export function fmtCount(n) {
    return countFormatter.format(Number(n));
}

/** The server's clock, which is what every timestamp in the store is on. */
export function serverNow(store) {
    return Date.now() - store.skew;
}

/** The accounts a panel shows: all of them when it names none. */
export function pick(accounts, names) {
    if (!names || names.length === 0) return accounts;
    const want = new Set(names);
    return accounts.filter(a => want.has(a.name));
}

export function statePill(state) {
    return el('span', `st st-${state}`, state);
}

/** botState ("idle"/"running"/"stopped"/"error", see internal/controlpanel.AccountStatus) gets
 * the same dot-plus-word treatment as the connection state pill, reusing the same .st-* colour
 * classes where the meaning lines up (running -> the "in game" green, error -> the same red). */
export function botStatePill(botState) {
    const cls = botState === 'running' ? 'st-ingame' : botState === 'error' ? 'st-error' : botState === 'stopped' ? 'st-reconnecting' : '';
    return el('span', `st ${cls}`, botState || '-');
}

export function logLine(ev, withAccount) {
    const line = el('div', `line lv-${ev.level}`);
    line.append(el('span', 't', fmtClock(ev.t)));
    if (withAccount && ev.account) line.append(el('span', 'who', ev.account));
    const msg = el('span', 'msg', ev.text);
    msg.title = `${ev.level}, ${ev.kind}: ${ev.text}`;
    line.append(msg);
    return line;
}

export function anyOf(why, ...reasons) {
    return reasons.some(r => why.has(r));
}
