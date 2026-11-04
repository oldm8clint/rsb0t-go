// Rendering the fixed-order, real-icon Skills grid — factored out of panels/account.js so the
// dedicated Skills panel (panels/skills.js) draws pixel-identical cells instead of re-implementing
// the same three-tier icon fallback and gold "primary skill" accent a second time.

import { el, fmtCount } from './util.js';
import { skillMonogram } from './itemicons.js';

/** The real client's own Skills tab order (internal/gamestate.SkillNames), used only until
 * store.meta.skillOrder arrives from the server so the grid never flashes empty; the server's
 * copy is authoritative. */
export const FALLBACK_SKILL_ORDER = ['Attack', 'Defence', 'Strength', 'Hitpoints', 'Ranged', 'Prayer', 'Magic', 'Cooking', 'Woodcutting', 'Fletching', 'Fishing', 'Firemaking', 'Crafting', 'Smithing', 'Mining', 'Herblore', 'Agility', 'Thieving', 'Slayer', 'Farming', 'Runecraft'];

export function skillOrder(store) {
    return store.meta?.skillOrder?.length ? store.meta.skillOrder : FALLBACK_SKILL_ORDER;
}

/** One skill cell: icon (three tiers — a live spritepack decode, the bundled static PNG, or a
 * monogram tile, in that order of preference) plus level/xp, gold-accented when `name` is the
 * account's primary skill. */
export function skillCell(name, sk, isPrimary, spriteSkills) {
    const cell = el('div', `skillcell${isPrimary ? ' is-primary' : ''}`);
    const iconWrap = el('span', 'skill-icon');
    // Bundled PNG first, live media.jag decode only for a skill the bundle lacks. See
    // skillIconImage below for why that order is the right way round: the live decode's frame
    // indices are guessed, and preferring them put wrong icons beside real levels.
    const img = document.createElement('img');
    img.alt = name;
    img.width = 26;
    img.height = 26;
    img.src = `skillicons/${encodeURIComponent(name)}.png`;
    img.onerror = () => {
        if (spriteSkills.has(name) && !img.dataset.tried) {
            img.dataset.tried = '1';
            img.src = `api/sprites/skill?name=${encodeURIComponent(name)}`;
            return;
        }
        iconWrap.textContent = '';
        iconWrap.innerHTML = `<svg viewBox="0 0 52 52" width="26" height="26" shape-rendering="crispEdges">${skillMonogram(name)}</svg>`;
    };
    iconWrap.append(img);
    const text = el('span', 'skill-text');
    text.append(el('span', 'skill-name', name));
    if (sk) {
        text.append(el('span', 'skill-stats', `lvl ${sk.level} · ${fmtCount(sk.xp)} xp`));
        // The account owner flagged this directly: XP gained this session needs to be legible
        // at a glance, not squeezed onto the same crowded line as level/total XP where it was
        // easy to miss or get clipped by the ellipsis. It's now its own line, in the same green
        // the rest of this app already uses for "good"/positive numbers, and never truncated.
        if (sk.xpGained > 0) text.append(el('span', 'skill-gained', `+${fmtCount(sk.xpGained)} xp gained`));
    } else {
        text.append(el('span', 'skill-stats none', 'not yet observed'));
    }
    cell.append(iconWrap, text);
    cell.title = isPrimary ? `${name} — this account's primary skill` : name;
    return cell;
}

/** Fills `grid` (a .skillsgrid element) with every skill in order, skipping the redraw when
 * nothing that would change the output has changed — the same key-based dedupe account.js always
 * used, now shared. */
export function drawSkillsGrid(grid, store, allSkills, primarySkill) {
    const order = skillOrder(store);
    const spriteSkills = new Set(store.meta?.spriteSkills ?? []);
    const key = `${order.join(',')}|${JSON.stringify(allSkills)}|${primarySkill}|${[...spriteSkills].join(',')}`;
    if (grid.dataset.key === key) return;
    grid.dataset.key = key;
    grid.textContent = '';
    for (const name of order) {
        grid.append(skillCell(name, allSkills[name], name === primarySkill, spriteSkills));
    }
}

// Lost City Content scripts/player/interfaces/stats.if, row-major order.
// This December 2004 interface has Runecraft alone in its final row.
export const INGAME_SKILL_ORDER = ['Attack', 'Hitpoints', 'Mining', 'Strength', 'Agility', 'Smithing', 'Defence', 'Herblore', 'Fishing', 'Ranged', 'Thieving', 'Cooking', 'Prayer', 'Crafting', 'Firemaking', 'Magic', 'Fletching', 'Woodcutting', 'Runecraft'];
const levelXP = [0, 0];
let accumulated = 0;
for (let level = 1; level < 99; level++) {
    accumulated += Math.floor(level + 300 * Math.pow(2, level / 7));
    levelXP[level + 1] = Math.floor(accumulated / 4);
}
export function skillProgress(skill) {
    if (!skill || !Number.isFinite(skill.xp) || !Number.isFinite(skill.level)) return null;
    let base = 1;
    while (base < 99 && skill.xp >= levelXP[base + 1]) base++;
    const next = base < 99 ? levelXP[base + 1] : null;
    // into/span are how far through the CURRENT level this skill is, which is what a progress
    // bar needs; remaining alone cannot give it, because each level's span differs. Added
    // here rather than recomputed by callers so levelXP stays this module's own business.
    const start = levelXP[base] ?? 0;
    const span = next === null ? 0 : Math.max(1, next - start);
    const into = next === null ? 0 : Math.max(0, Math.min(span, skill.xp - start));
    return {base, next, remaining: next === null ? 0 : Math.max(0, next - skill.xp), into, span};
}
// Use glyph masks from the existing p11 sheet at native proportional widths.
// No resized PNGs or fonts are invented, and no per-update canvas is allocated.
function setNativeDigits(element, value) {
    element.textContent = '';
    element.append(el('span', 'sr-only', value));
    const widths = {'0':5,'1':3,'2':5,'3':4,'4':4,'5':4,'6':5,'7':4,'8':5,'9':5,'?':5};
    for (const character of value) {
        const digit = el('span', 'ingame-digit');
        digit.setAttribute('aria-hidden', 'true');
        const code = character.charCodeAt(0);
        const offset = character === '7' ? 1 : 0;
        const position = `${-((code % 16) * 20 + offset) * 1.5}px ${-(Math.floor(code / 16) * 20 + 1) * 1.5}px`;
        digit.style.width = `${(widths[character] || 5) * 1.5}px`;
        digit.style.maskPosition = digit.style.webkitMaskPosition = position;
        element.append(digit);
    }
}
/**
 * The icon for one skill.
 *
 * ORDER MATTERS, and it is the opposite of what it looks like it should be. The bundled
 * PNGs in web/skillicons/ are the GOOD source: 20 of 21 skills, extracted properly from a
 * rasterised sprite sheet. The live media.jag decode behind /api/sprites/skill covers only
 * 14 skills and its frame indices are, in internal/spritepack/skillicons.go's own words,
 * "guessed from position" -- which is why that file deliberately leaves seven skills out
 * rather than risk "shipping a wrong icon next to a real level".
 *
 * Preferring the sprite endpoint here put guessed icons on 14 skills and the owner reported
 * immediately that the icons were wrong (2026-09-20). So: bundled PNG first, the live decode
 * only as a fallback for a skill the bundle does not carry, then a drawn monogram.
 */
function skillIconImage(name) {
    const image = el('img');
    image.alt = '';
    image.width = image.height = 25;
    image.src = `skillicons/${encodeURIComponent(name)}.png`;
    image.onerror = () => {
        image.onerror = () => {
            const holder = image.parentElement;
            image.remove();
            if (holder) holder.prepend(el('span', 'ingame-icon-fallback', name.slice(0, 2)));
        };
        image.src = `api/sprites/skill?name=${encodeURIComponent(name)}`;
    };
    return image;
}

export function drawIngameSkillsGrid(grid, skills, primarySkill, selectedSkill) {
    // Rebuild only when a level or its base changes; the progress bar and tooltips below are
    // updated every pass, since XP moves constantly and rebuilding 19 cells a second would
    // throw away the browser's own paint caching for no reason.
    const key = JSON.stringify(INGAME_SKILL_ORDER.map(name => {
        const sk = skills[name]; return [name, sk?.level, skillProgress(sk)?.base];
    }));
    if (grid.dataset.key !== key) {
        grid.dataset.key = key;
        grid.textContent = '';
        for (const name of INGAME_SKILL_ORDER) {
            const sk = skills[name];
            const progress = skillProgress(sk);
            const cell = el('button', 'ingame-skill');
            cell.type = 'button'; cell.dataset.skill = name;
            const image = skillIconImage(name);
            const current = el('span', 'ingame-current');
            const base = el('span', 'ingame-base');
            setNativeDigits(current, sk ? String(sk.level) : '?');
            setNativeDigits(base, progress ? String(progress.base) : '?');
            // How far through the current level this skill is, drawn under the numbers. The
            // in-game tab has no such bar, but users asked for more information and this
            // is the one thing the tab makes you hover to learn.
            const bar = el('span', 'ingame-progress');
            bar.append(el('span', 'ingame-progress-fill'));
            cell.append(image, current, el('span', 'ingame-slash', '/'), base, bar);
            grid.append(cell);
        }
    }
    for (const cell of grid.children) {
        const name = cell.dataset.skill;
        const sk = skills[name]; const progress = skillProgress(sk);
        const gained = sk?.xpGained > 0 ? `; +${sk.xpGained.toLocaleString()} XP gained` : '';
        const label = progress ? `${name}: ${sk.level}/${progress.base}; ${sk.xp.toLocaleString()} XP; ${progress.next === null ? 'level 99 reached' : `${progress.remaining.toLocaleString()} XP to next level`}${gained}` : `${name}: not yet observed`;
        cell.setAttribute('aria-label', label);
        cell.title = label;
        cell.setAttribute('aria-pressed', String(name === selectedSkill));
        cell.classList.toggle('is-primary', name === primarySkill);
        // A skill that has earned XP this run is worth spotting at a glance across 19 cells.
        cell.classList.toggle('is-gaining', !!(sk && sk.xpGained > 0));
        const fill = cell.querySelector('.ingame-progress-fill');
        if (fill) {
            const pct = progress && progress.next !== null && progress.span > 0
                ? Math.max(0, Math.min(100, Math.round(progress.into / progress.span * 100)))
                : (progress && progress.next === null ? 100 : 0);
            fill.style.width = `${pct}%`;
        }
    }
}
