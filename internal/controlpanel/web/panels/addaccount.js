import { anyOf, button, el } from '../util.js';

// A fleet-level action panel (accounts: 'none' — it does not follow or list any one
// account) for internal/controlpanel.ConnectionControl.AddAccount: log a brand-new
// account, not in the fleet's original fleet.config.json roster, into the running
// process, and optionally start a registered script on it right away.
//
// The account owner's own ask was for this to also offer "duplicate an existing
// account's settings" so a new account can be set up to run identically to one already
// proven — that's the "Copy from" picker below: choosing an existing account just copies
// its current script + settings (already known to the page via AccountStatus.Settings,
// see fleet.go/fleetsource.go) into this form's own fields, plain client-side
// convenience, no extra backend surface needed beyond AddAccount itself.
export default {
    type: 'addaccount',
    title: 'Add account',
    description: 'Log a brand-new account into the running fleet, optionally starting a script — with a "copy from" picker to duplicate an existing account\'s settings.',
    size: { w: 4, h: 3 },
    accounts: 'none',
    settings: [],
    create(body, ctx) {
        const notSupported = el('div', 'none', 'This fleet does not support adding accounts.');
        const rows = el('div', 'rows');

        const copyFrom = el('select');
        const username = el('input');
        username.type = 'text';
        username.placeholder = 'throwaway1';
        username.autocomplete = 'off';
        const password = el('input');
        password.type = 'password';
        password.autocomplete = 'new-password';
        const scriptPick = el('select');
        const settingsBox = el('textarea', 'settingsbox');
        settingsBox.spellcheck = false;
        settingsBox.placeholder = '{ "maxLaps": 5 }';

        const row = (label, field) => {
            const l = el('label', 'row');
            l.append(el('span', '', label), field);
            return l;
        };
        rows.append(
            row('Copy settings from', copyFrom),
            row('Username', username),
            row('Password', password),
            row('Script', scriptPick)
        );

        const addBtn = button('Add account', 'connect this account and, if a script is picked, start it');
        const note = el('div', 'none');
        const actions = el('div', 'control');
        actions.append(addBtn, note);

        body.append(notSupported, rows, el('h3', '', 'Settings'), settingsBox, actions);
        notSupported.hidden = true;

        let scriptsDrawn = '';
        let copyDrawn = '';
        let busy = false;

        // Copying only fires on a deliberate pick, never automatically, so it never
        // clobbers whatever the operator has already typed.
        copyFrom.addEventListener('change', () => {
            const name = copyFrom.value;
            if (!name) return;
            const a = ctx.store.byName.get(name);
            if (!a) return;
            if (a.bot) scriptPick.value = a.bot;
            settingsBox.value = a.settings ? JSON.stringify(a.settings, null, 2) : '';
            note.textContent = `copied ${name}'s script and settings below — review before adding`;
            note.classList.remove('bad');
        });

        addBtn.addEventListener('click', async () => {
            const u = username.value.trim();
            const p = password.value;
            if (!u) {
                note.textContent = 'username is required';
                note.classList.add('bad');
                return;
            }
            if (!p) {
                note.textContent = 'password is required';
                note.classList.add('bad');
                return;
            }
            let settings;
            try {
                const text = settingsBox.value.trim();
                settings = text ? JSON.parse(text) : {};
            } catch (err) {
                note.textContent = `settings is not valid JSON: ${err.message}`;
                note.classList.add('bad');
                return;
            }
            busy = true;
            addBtn.disabled = true;
            note.textContent = 'adding...';
            note.classList.remove('bad');
            try {
                const res = await fetch('api/fleet/add', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ username: u, password: p, script: scriptPick.value, settings })
                });
                const data = await res.json().catch(() => ({}));
                if (res.ok) {
                    note.textContent = `${u} added${scriptPick.value ? ` and starting ${scriptPick.value}` : ''}`;
                    password.value = '';
                } else {
                    note.textContent = data.error || `HTTP ${res.status}`;
                    note.classList.add('bad');
                }
            } catch (err) {
                note.textContent = `request failed: ${err}`;
                note.classList.add('bad');
            } finally {
                busy = false;
                addBtn.disabled = false;
            }
        });

        return {
            update(store, why) {
                if (!anyOf(why, 'init', 'status', 'meta')) return;
                const supported = store.meta?.connectionControl === true;
                notSupported.hidden = supported;
                rows.hidden = actions.hidden = !supported;
                settingsBox.hidden = !supported;
                body.querySelectorAll('h3').forEach(h => { h.hidden = !supported; });
                if (!supported) return;

                const scripts = store.meta?.scripts ?? [];
                const scriptKey = scripts.join(',');
                if (scriptKey !== scriptsDrawn) {
                    scriptsDrawn = scriptKey;
                    const current = scriptPick.value;
                    scriptPick.textContent = '';
                    scriptPick.append(el('option', '', '(no script — just connect)'));
                    for (const s of scripts) scriptPick.append(el('option', '', s));
                    if (scripts.includes(current)) scriptPick.value = current;
                }

                const names = store.accounts.map(a => a.name).sort();
                const copyKey = names.join(',');
                if (copyKey !== copyDrawn) {
                    copyDrawn = copyKey;
                    const current = copyFrom.value;
                    copyFrom.textContent = '';
                    copyFrom.append(el('option', '', '(start blank)'));
                    for (const n of names) copyFrom.append(el('option', '', n));
                    if (names.includes(current)) copyFrom.value = current;
                }

                addBtn.disabled = busy;
                ctx.setSub(scripts.length ? `${scripts.length} script(s) available` : 'no scripts registered');
            }
        };
    }
};
