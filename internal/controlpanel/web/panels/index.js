// Every panel type the page offers. A new type is one module beside this file and one entry in
// PANELS; the add menu, the customize dialog and the layout all read this list and nothing else.
//
// A panel module default-exports:
//   type         lower-case id stored in the layout ("fleet")
//   title        shown in its heading and the add menu
//   description  one line for the add menu
//   size         { w, h } in grid cells when added
//   accounts     'many' (a checklist, none ticked means all), 'one' (a pick, or follow the table
//                selection) or 'none'
//   settings     field declarations, see settings.js
//   needsData    true for a Control page panel the fleet cannot fill yet (see placeholder.js)
//   create(body, ctx) -> { update(store, why), destroy?() }
//                body is the panel's element; ctx is { id, store, accounts, settings, setSub(text) }.
//                update is called with a Set of reasons (see store.js) and should return early
//                when none of them concern it. A customize change destroys and recreates the panel.

import fleet from './fleet.js';
import accounts from './accounts.js';
import account from './account.js';
import addaccount from './addaccount.js';
import feed from './feed.js';
import chat from './chat.js';
import alerts from './alerts.js';
import scarab from './scarab.js';
import memory from './memory.js';
import logins from './logins.js';
import bank from './bank.js';
import bankall from './bankall.js';
import ledger from './ledger.js';
import tally from './tally.js';
import map from './map.js';
import skills from './skills.js';
import hiscores from './hiscores.js';
import versions from './versions.js';
import capture from './capture.js';
import stream from './stream.js';
import heatmap from './heatmap.js';
import loot from './loot.js';
import timesplit from './timesplit.js';
import mule from './mule.js';
import market from './market.js';
import operations from './operations.js';
import workloads from './workloads.js';
import connections from './connections.js';
import resources from './resources.js';

export const PANELS = [operations, alerts, accounts, account, workloads, connections, resources, fleet, addaccount, feed, chat, scarab, memory, logins, bank, bankall, ledger, tally, map, skills, hiscores, versions, capture, stream, heatmap, loot, timesplit, mule, market];

// Retired panel types that now open as another panel, so a layout saved with one (the owner's
// server-saved layout included) still loads. 'trails' (Treasure trails, a placeholder) was
// merged into the Loot panel on 2026-09-19.
// 'gpu-map' was never a second panel: gpu-map.js was nine lines of `{...map, title: 'GPU
// account map'}`, so the add menu offered the same panel twice under two names and a viewer
// had no way to tell which to pick. The map has drawn its markers and walls on the GPU since
// the renderer was shared, so the distinction the title drew stopped being real. Aliased like
// 'trails' so every saved layout holding one still opens.
const ALIASES = { trails: 'loot', 'gpu-map': 'map' };

export function panelType(type) {
    const want = ALIASES[type] ?? type;
    return PANELS.find(p => p.type === want) ?? null;
}

/** The page a first visit gets, and the reset button. */
export function defaultLayout() {
    const panel = (id, type, col, row, w, h, title = '', settings = {}) => ({ id, type, title, col, row, w, h, accounts: [], settings });
    return {
        v: 1,
        panels: [
            panel('operations', 'operations', 0, 0, 4, 3),
            panel('alerts', 'alerts', 4, 0, 4, 3),
            panel('resources', 'resources', 8, 0, 4, 3),
            panel('accounts', 'accounts', 0, 3, 8, 4),
            panel('account', 'account', 8, 3, 4, 4),
            panel('workloads', 'workloads', 0, 7, 6, 3),
            panel('connections', 'connections', 6, 7, 6, 3),
            panel('skills', 'skills', 0, 10, 4, 3),
            panel('bankall', 'bankall', 4, 10, 8, 3),
            panel('tally', 'tally', 0, 13, 6, 4),
            panel('loot', 'loot', 6, 13, 6, 4),
            panel('log', 'feed', 0, 17, 6, 3, 'Fleet log'),
            panel('chat', 'chat', 6, 17, 6, 3),
            panel('map', 'map', 0, 20, 12, 4),
            panel('memory', 'memory', 0, 24, 6, 3),
            panel('versions', 'versions', 6, 24, 6, 3)
        ]
    };
}
