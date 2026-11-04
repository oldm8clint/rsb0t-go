// December 2004 Skills tab. Table selection follows locally; the dropdown does
// not change global selection. XP estimates use bounded observed samples only.

import { anyOf, el, fmtCount, fmtDuration, serverNow } from '../util.js';
import { drawIngameSkillsGrid, INGAME_SKILL_ORDER, skillProgress } from '../skillrender.js';

export default {
    type: 'skills',
    title: 'Skills',
    description: 'The December 2004 Skills tab for the table-selected account or a dropdown choice — a lighter alternative to turning on skills inside every Account detail panel.',
    size: { w: 5, h: 4 },
    accounts: 'many',
    settings: [
        // The in-game tab has no progress bar; it makes you hover. Users asked for more
        // information on the face of the panel, and then that it still look like the tab, so
        // this is a choice rather than a decision made for them.
        { key: 'progress', label: 'Show level progress bars', kind: 'bool', default: true }
    ],
    create(body, ctx) {
        const head = el('div', 'skillspanel-head');
        const pick = el('select');
        const total = el('span', 'skillspanel-total');
        head.append(pick);
        pick.setAttribute('aria-label', 'Skills account');
        const grid = el('div', 'ingame-skills');
        if (ctx.settings.progress === false) grid.classList.add('no-progress');
        const details = el('div', 'ingame-skill-details');
        details.setAttribute('aria-live', 'polite');
        const none = el('div', 'none', 'no accounts reported yet');
        body.append(head, grid, total, details, none);
        none.hidden = true;

        let chosen = '';
        let selectedSkill = 'Fishing';
        let selectionDrawn = '';
        const samples = new Map();
        grid.addEventListener('click', event => {
            const cell = event.target.closest('[data-skill]');
            if (cell) { selectedSkill = cell.dataset.skill; render(); }
        });
        grid.addEventListener('pointerover', event => {
            const cell = event.target.closest('[data-skill]');
            if (cell && selectedSkill !== cell.dataset.skill) { selectedSkill = cell.dataset.skill; render(); }
        });
        grid.addEventListener('focusin', event => {
            const cell = event.target.closest('[data-skill]');
            if (cell) { selectedSkill = cell.dataset.skill; render(); }
        });
        let optionsDrawn = '';

        pick.addEventListener('change', () => {
            chosen = pick.value;
            ctx.setSub(chosen);
            render();
        });

        function render() {
            const store = ctx.store;
            const list = (ctx.accounts.length ? store.accounts.filter(a => ctx.accounts.includes(a.name)) : store.accounts).map(a => a.name);
            if (store.selected !== selectionDrawn) {
                selectionDrawn = store.selected;
                if (list.includes(store.selected)) chosen = store.selected;
            }
            const optKey = list.join(',');
            if (optKey !== optionsDrawn) {
                optionsDrawn = optKey;
                pick.textContent = '';
                for (const name of list) pick.append(el('option', '', name));
                if (!list.includes(chosen)) chosen = list[0] ?? '';
                pick.value = chosen;
            }
            pick.value = chosen;
            details.hidden = total.hidden = list.length === 0;
            none.hidden = list.length > 0;
            grid.hidden = list.length === 0;
            head.hidden = list.length === 0;
            if (list.length === 0) {
                ctx.setSub('');
                return;
            }
            const a = store.byName.get(chosen);
            ctx.setSub(chosen);
            if (!a) {
                total.textContent = ''; details.textContent = '';
                grid.textContent = '';
                delete grid.dataset.key;
                return;
            }
            const skills = a.skills ?? {};
            drawIngameSkillsGrid(grid, skills, a.primarySkill, selectedSkill);
            const order = INGAME_SKILL_ORDER;
            const current = skills[selectedSkill]; const progress = skillProgress(current);
            const now = serverNow(store);
            const sampleKey = `${chosen}|${selectedSkill}`;
            let history = samples.get(sampleKey) || [];
            if (current) {
                const previous = history[history.length - 1];
                if (previous && (current.xp < previous.xp || now < previous.at || now - previous.at > 120000)) history = [];
                if (!previous || !history.length || now - previous.at >= 10000) history.push({at: now, xp: current.xp});
                while (history.length > 1 && now - history[0].at > 300000) history.shift();
                samples.delete(sampleKey); samples.set(sampleKey, history);
                if (samples.size > 64) samples.delete(samples.keys().next().value);
            }
            let detailText = `${selectedSkill}: not yet observed`;
            if (progress) {
                const seconds = history.length > 1 ? (history[history.length - 1].at - history[0].at) / 1000 : 0;
                const rate = seconds >= 60 ? (current.xp - history[0].xp) * 3600 / seconds : null;
                const eta = a.state && a.state !== 'ingame' ? 'ETA: account is not in game' : progress.next === null ? 'Maximum base level reached' : rate > 0 ? `Estimated next level: ${fmtDuration(progress.remaining / rate * 3600000)}` : seconds < 60 ? 'ETA: collecting a one-minute XP sample' : 'ETA: no XP gained in the observed window';
                detailText = `${selectedSkill} XP: ${current.xp.toLocaleString()}\n${progress.next === null ? 'Base level: 99' : `Next level at: ${progress.next.toLocaleString()} XP\nXP remaining: ${progress.remaining.toLocaleString()}`}\n${eta}${rate !== null ? `\nObserved rate: ${Math.round(rate).toLocaleString()} XP/hour` : ''}`;
            }
            if (details.textContent !== detailText) details.textContent = detailText;
            let totalLevel = 0;
            let totalGained = 0;
            let seen = 0;
            for (const name of order) {
                const sk = skills[name];
                if (!sk) continue;
                seen++;
                totalLevel += skillProgress(sk)?.base ?? sk.level;
                totalGained += sk.xpGained ?? 0;
            }
            total.innerHTML = '';
            total.append(seen ? `total lvl ` : 'not yet observed', seen ? el('b', '', String(totalLevel)) : '');
            if (totalGained > 0) total.append(' · ', el('span', 'skillspanel-gained', `+${fmtCount(totalGained)} xp this session`));
        }

        return {
            update(store, why) {
                if (!anyOf(why, 'init', 'status', 'meta', 'reset', 'select', 'clock')) return;
                if (why.has('reset')) samples.clear();
                if (why.has('select') && store.byName.has(store.selected) && (!ctx.accounts.length || ctx.accounts.includes(store.selected))) chosen = store.selected;
                render();
            }
        };
    }
};
