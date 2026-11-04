// Per-script build/update status — the account owner's own ask: for every script
// running on the fleet, what version it's on and whether an update is available.
//
// Read this before changing the wording in here: this panel can only ever ANSWER "does
// a newer commit exist for this script's own code." It cannot apply one. Go compiles
// every script into one binary (see this project's own HOTRELOAD.md for exactly why
// there is no way to hot-swap one script's compiled logic into a running process yet —
// no `plugin` support on Windows, and the proper fix, a separate process holding script
// logic over IPC, is real but far from finished). Picking up a newer commit always
// means rebuilding the binary and restarting the whole controlpanel process — every
// account on it, not just one — which is the existing kill -> cooldown -> relaunch
// procedure, not a button on this panel. Nothing here should ever be worded to suggest
// otherwise; see internal/controlpanel/web/panels/account.js's "Restart script" button
// for the one thing that genuinely IS just a click: restarting one account's already-
// running code (recovers a stuck script, does not fetch anything new).

import { anyOf, button, el, fmtDuration, serverNow, table } from '../util.js';

function commitLink(owner, repo, sha, fallbackUrl) {
    if (!sha) return el('span', 'none', '-');
    const a = el('a', '', sha.slice(0, 8));
    a.href = fallbackUrl || (owner && repo ? `https://github.com/${owner}/${repo}/commit/${sha}` : '#');
    a.target = '_blank';
    a.rel = 'noopener noreferrer';
    a.title = sha;
    return a;
}

export default {
    type: 'versions',
    title: 'Script versions',
    description: 'Per-script build SHA and whether GitHub has a newer commit touching that script’s own code — informational only. See the panel’s own note on why nothing here can apply an update by itself.',
    size: { w: 7, h: 3 },
    accounts: 'none',
    settings: [],
    create(body, ctx) {
        const note = el('div', 'none', 'This panel can only report whether newer code exists — applying it always means rebuilding the binary and restarting the whole fleet process (every account, not just one). It never happens automatically or from a button here.');
        const row = el('div', 'control');
        const refreshBtn = button('Check now', 'ask the server to check GitHub again immediately, instead of waiting for the next automatic poll');
        const status = el('div', 'none');
        row.append(refreshBtn, status);
        const summary = el('div', 'none');
        const listWrap = el('div');
        body.append(note, row, summary, listWrap);

        let closed = false;
        let busy = false;
        let pollTimer = null;

        function render(data) {
            listWrap.textContent = '';
            if (!data || !data.available) {
                summary.textContent = '';
                listWrap.append(el('div', 'none', 'not configured on this build — needs a build with VCS info and a GitHub token (see cmd/controlpanel’s -github-token-env)'));
                return;
            }
            const scripts = data.scripts ?? [];
            if (scripts.length === 0) {
                summary.textContent = '';
                listWrap.append(el('div', 'none', 'no scripts configured to check yet'));
                return;
            }
            summary.textContent = '';
            if (data.staleCount > 0) {
                const strong = el('b', '', `${data.staleCount} script${data.staleCount === 1 ? '' : 's'} have newer code available`);
                summary.append(strong, ' — restart the fleet (kill, wait, relaunch) to apply. Nothing applies itself.');
            } else {
                summary.append('every checked script matches its latest commit on GitHub, as of the checks below');
            }

            const now = serverNow(ctx.store);
            const rows = scripts.slice().sort((a, b) => a.script.localeCompare(b.script)).map(s => {
                const row = [
                    s.script,
                    commitLink(data.owner, data.repo, data.buildSha, data.buildSha ? `https://github.com/${data.owner}/${data.repo}/commit/${data.buildSha}` : ''),
                ];
                if (s.update) {
                    const cell = el('span');
                    cell.append(commitLink(data.owner, data.repo, s.update.sha, s.update.url), ' ', el('span', 'none', s.update.message || ''));
                    row.push(cell);
                } else if (s.error) {
                    const cell = el('span', 'bad', `check failed: ${s.error}`);
                    row.push(cell);
                } else {
                    row.push(el('span', 'none', 'up to date'));
                }
                row.push(s.checkedAt ? fmtDuration(now - s.checkedAt) + ' ago' : 'never');
                return row;
            });
            listWrap.append(table(['Script', 'This build', 'Newest on GitHub', 'Last checked'], rows));
        }

        let lastData = null;

        async function load() {
            if (closed || ctx.isVisible?.() === false) return;
            try {
                const res = await fetch('api/scriptversions', { cache: 'no-store' });
                if (closed) return;
                if (!res.ok) {
                    status.textContent = `HTTP ${res.status}`;
                    status.classList.add('bad');
                    return;
                }
                lastData = await res.json();
                status.textContent = '';
                status.classList.remove('bad');
                render(lastData);
            } catch (err) {
                if (closed) return;
                status.textContent = `request failed: ${err}`;
                status.classList.add('bad');
            }
        }

        refreshBtn.addEventListener('click', async () => {
            if (busy) return;
            busy = true;
            refreshBtn.disabled = true;
            status.textContent = 'checking...';
            status.classList.remove('bad');
            await load();
            busy = false;
            refreshBtn.disabled = false;
        });

        load();
        ctx.onVisibility?.(visible => { if (visible) load(); });
        // The server itself polls GitHub on its own (minutes-scale) interval; this just
        // re-reads whatever the server already knows every 30s so an open tab stays
        // current without every open tab hammering GitHub directly.
        pollTimer = setInterval(load, 30000);

        return {
            update(store, why) {
                if (!anyOf(why, 'clock')) return;
                if (lastData) render(lastData);
            },
            destroy() {
                closed = true;
                if (pollTimer) clearInterval(pollTimer);
            }
        };
    }
};
