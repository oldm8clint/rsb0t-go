// The customize dialog: a panel's title, the accounts it shows, and the settings its type declares.
// The form is generated from the type, so a new panel type gets its dialog for nothing.

import { el } from './util.js';
import { CONTROL } from './layout.js';
import { normaliseSettings } from './settings.js';

function labelled(text, control) {
    const label = el('label', 'row');
    label.append(el('span', '', text), control);
    return label;
}

function checkbox(text, checked) {
    const input = el('input');
    input.type = 'checkbox';
    input.checked = checked;
    const label = el('label');
    label.title = text;
    label.append(input, ` ${text}`);
    return { input, label };
}

function option(value, text) {
    const o = el('option', '', text);
    o.value = value;
    return o;
}

export function customize(dialog, type, panel, known, apply) {
    const settings = normaliseSettings(type.settings, panel.settings);
    const form = el('form');
    form.append(el('h3', '', `Customize ${panel.title || type.title}`));

    const title = el('input');
    title.type = 'text';
    title.maxLength = 48;
    title.value = panel.title;
    title.placeholder = type.title;
    form.append(labelled('Title', title));

    const names = [...new Set([...known, ...panel.accounts])].sort();
    let readAccounts = () => [];
    if (type.accounts === 'many') {
        const set = el('fieldset');
        set.append(el('legend', '', 'Accounts'));
        const all = checkbox('every account, including ones added later', panel.accounts.length === 0);
        const list = el('div', 'checks');
        const boxes = names.map(name => {
            const box = checkbox(name, panel.accounts.includes(name));
            list.append(box.label);
            return [name, box.input];
        });
        if (names.length === 0) list.append(el('span', 'none', 'no accounts reported yet'));
        const show = () => {
            list.hidden = all.input.checked;
        };
        all.input.addEventListener('change', show);
        show();
        set.append(all.label, list);
        form.append(set);
        readAccounts = () => (all.input.checked ? [] : boxes.filter(([, input]) => input.checked).map(([name]) => name));
    } else if (type.accounts === 'one') {
        const select = el('select');
        select.append(option('', 'the account last clicked in a table'));
        for (const name of names) select.append(option(name, name));
        select.value = panel.accounts[0] ?? '';
        form.append(labelled('Account', select));
        readAccounts = () => (select.value === '' ? [] : [select.value]);
    }

    const readers = [];
    for (const field of type.settings ?? []) {
        const value = settings[field.key];
        if (field.kind === 'bool') {
            const box = checkbox(field.label, value);
            const row = el('div');
            row.append(box.label);
            form.append(row);
            readers.push(() => [field.key, box.input.checked]);
        } else if (field.kind === 'number') {
            const input = el('input');
            input.type = 'number';
            input.min = String(field.min);
            input.max = String(field.max);
            input.step = String(field.step ?? 1);
            input.value = String(value);
            form.append(labelled(field.label, input));
            readers.push(() => [field.key, Number(input.value)]);
        } else if (field.kind === 'select') {
            const select = el('select');
            for (const o of field.options) select.append(option(o.value, o.label));
            select.value = value;
            form.append(labelled(field.label, select));
            readers.push(() => [field.key, select.value]);
        } else if (field.kind === 'multi') {
            const set = el('fieldset');
            set.append(el('legend', '', field.label));
            const list = el('div', 'checks');
            const boxes = field.options.map(o => {
                const box = checkbox(o.label, value.includes(o.value));
                list.append(box.label);
                return [o.value, box.input];
            });
            set.append(list);
            form.append(set);
            readers.push(() => [field.key, boxes.filter(([, input]) => input.checked).map(([v]) => v)]);
        }
    }

    const actions = el('div', 'actions');
    const cancel = el('button', '', 'Cancel');
    cancel.type = 'button';
    const ok = el('button', '', 'Apply');
    ok.type = 'submit';
    actions.append(cancel, ok);
    form.append(actions);
    cancel.addEventListener('click', () => dialog.close());
    form.addEventListener('submit', ev => {
        ev.preventDefault();
        const next = {
            ...panel,
            title: title.value.replace(CONTROL, '').trim().slice(0, 48),
            accounts: readAccounts(),
            settings: normaliseSettings(type.settings, Object.fromEntries(readers.map(read => read())))
        };
        dialog.close();
        apply(next);
    });

    dialog.textContent = '';
    dialog.append(form);
    dialog.showModal();
}
