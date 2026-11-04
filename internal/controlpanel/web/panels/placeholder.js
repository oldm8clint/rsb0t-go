import { el } from '../util.js';

/**
 * A Control page panel this fleet cannot fill yet.
 *
 * It is registered like any other, so it can be placed and kept in a layout, and it says what data
 * it is waiting for instead of drawing an empty grid that looks like an empty bank.
 */
export function needsData({ type, title, description, size, needs }) {
    return {
        type,
        title,
        description,
        size,
        accounts: 'many',
        settings: [],
        needsData: true,
        create(body, ctx) {
            body.append(el('div', 'none', 'This panel needs data the fleet does not report yet:'));
            const list = el('ul', 'needs');
            for (const need of needs) list.append(el('li', '', need));
            body.append(list);
            ctx.setSub('needs data');
            return { update() {} };
        }
    };
}
