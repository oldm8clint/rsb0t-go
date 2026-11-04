// View-side controls for the stream client: camera (zoom, rotate, modes), overlay toggles and
// keyboard shortcuts. Nothing here sends anything to the game; it only changes how this
// browser draws the account. Pure camera maths is exported for tests (verify-scene-state.mjs).

// The game's own camera pitch limits (Client.ts orbitCameraPitch 128..383 of 2048 per turn),
// as the elevation angle this renderer uses.
export const PITCH_MIN = 128 * Math.PI / 1024;
export const PITCH_MAX = 383 * Math.PI / 1024;
export const TOP_DOWN_PITCH = 1.5;
export const ZOOM_MIN = 6, ZOOM_MAX = 45, ZOOM_DEFAULT = 20;
export const MODES = ['follow', 'free', 'top-down'];

export const clampPitch = p => Math.max(PITCH_MIN, Math.min(PITCH_MAX, p));
export const clampZoom = d => Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, d));

// Keep the paint budget's phase across refresh rates and skip missed slots after a stall.
// Resetting the timestamp each frame turns a 60 FPS cap into 48 FPS on a 144 Hz display.
export function advancePaintDeadline(deadline, now, fps) {
    const period = 1000 / fps;
    return deadline + Math.max(1, Math.floor((now - deadline) / period) + 1) * period;
}

// easeToward moves value toward target by a frame-rate independent fraction (dt in seconds).
export function easeToward(value, target, dt, rate = 12) {
    const k = 1 - Math.exp(-rate * Math.max(0, dt));
    const next = value + (target - value) * k;
    return Math.abs(next - target) < 1e-3 ? target : next;
}

// Arrow keys: yaw and pitch speeds in radians per second (the client turns about half a turn
// a second with a held arrow key).
const YAW_SPEED = 2.2, PITCH_SPEED = 1.1, PAN_SPEED = 8;

export const HELP = [
    ['Mouse wheel / + -', 'zoom'],
    ['Middle drag, left drag, arrow keys', 'rotate and tilt'],
    ['C', 'camera: follow / free / top-down'],
    ['W A S D', 'move the free camera'],
    ['Home', 'reset the camera'],
    ['G', 'tile grid'],
    ['N', 'name tags'],
    ['I', 'ground item labels'],
    ['U', 'status HUD (bot, activity, XP/hour)'],
    ['F', 'distance fog'],
    ['H', 'ground shadows under actors'],
    ['P', 'screenshot (PNG)'],
    ['?', 'this help'],
    ['With input control', "letters type into the game's chat (or the amount); Enter sends, Esc clears or cancels Use; the letter shortcuts return on release"],
];

// createViewControls wires wheel, middle-drag, keys (only while `host` has focus) onto the
// canvas and host. onChange() is called when something needs repainting; onAction(name) for
// screenshot/help. step(dt, anchor) advances easing and held keys; returns true while moving.
// Optional hooks: onKey(e) sees a focused key first and returns true when it took it (the
// stream client's chat typing while input control is held); wheelTaken(e) returns true when
// the wheel belongs to something else (scrolling an open interface), so it does not zoom.
export function createViewControls(host, canvas, { onChange, onAction, onKey, wheelTaken }) {
    const cam = { yaw: 0, pitch: 0.95, distance: ZOOM_DEFAULT, targetDistance: ZOOM_DEFAULT, mode: 'follow', free: null };
    const toggles = { grid: false, names: false, items: false, hud: true, distances: false, fog: true, shadows: true };
    const held = new Set();
    let rotating = false, rx = 0, ry = 0;
    host.tabIndex = 0;
    host.style.outline = 'none';
    const rotate = (dx, dy) => {
        cam.yaw -= dx * 0.01;
        if (cam.mode !== 'top-down') cam.pitch = clampPitch(cam.pitch + dy * 0.006);
        onChange();
    };
    function wheel(e) {
        e.preventDefault();
        if (wheelTaken?.(e)) return;
        cam.targetDistance = clampZoom(cam.targetDistance * Math.pow(1.0015, e.deltaY));
        onChange();
    }
    function down(e) {
        host.focus({ preventScroll: true });
        if (e.button !== 1) return;
        e.preventDefault(); // no browser autoscroll on middle click
        rotating = true; rx = e.clientX; ry = e.clientY;
        canvas.setPointerCapture?.(e.pointerId);
    }
    function move(e) {
        if (!rotating) return;
        rotate(e.clientX - rx, e.clientY - ry);
        rx = e.clientX; ry = e.clientY;
    }
    function up(e) { if (e.button === 1) rotating = false; }
    function aux(e) { if (e.button === 1) e.preventDefault(); }
    const KEYS = new Set(['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'w', 'a', 's', 'd', 'W', 'A', 'S', 'D']);
    function keydown(e) {
        if (e.ctrlKey || e.metaKey || e.altKey) return;
        if (onKey?.(e)) { e.preventDefault(); onChange(); return; }
        if (KEYS.has(e.key)) { held.add(e.key.toLowerCase()); e.preventDefault(); onChange(); return; }
        const k = e.key.toLowerCase();
        if (k === '+' || k === '=') cam.targetDistance = clampZoom(cam.targetDistance * 0.85);
        else if (k === '-' || k === '_') cam.targetDistance = clampZoom(cam.targetDistance / 0.85);
        else if (k === 'c') { cam.mode = MODES[(MODES.indexOf(cam.mode) + 1) % MODES.length]; cam.free = null; }
        else if (k === 'home') { cam.yaw = 0; cam.pitch = 0.95; cam.targetDistance = ZOOM_DEFAULT; cam.mode = 'follow'; cam.free = null; }
        else if (k === 'g') toggles.grid = !toggles.grid;
        else if (k === 'n') toggles.names = !toggles.names;
        else if (k === 'i') toggles.items = !toggles.items;
        else if (k === 'u') toggles.hud = !toggles.hud;
        else if (k === 'f') toggles.fog = !toggles.fog;
        else if (k === 'h') toggles.shadows = !toggles.shadows;
        else if (k === 'p') onAction('screenshot');
        else if (k === '?' || k === '/') onAction('help');
        else return;
        e.preventDefault();
        onChange();
    }
    function keyup(e) { held.delete(e.key.toLowerCase()); }
    function blur() { held.clear(); rotating = false; }
    canvas.addEventListener('wheel', wheel, { passive: false });
    canvas.addEventListener('pointerdown', down);
    canvas.addEventListener('auxclick', aux);
    canvas.addEventListener('mousedown', aux);
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
    host.addEventListener('keydown', keydown);
    host.addEventListener('keyup', keyup);
    host.addEventListener('blur', blur);
    return {
        cam, toggles, rotate,
        // step advances easing and held keys by dt seconds. anchor is the account's position
        // {x, y, z}; returns the camera the renderer draws plus whether it is still moving.
        step(dt, anchor) {
            let moving = false;
            if (held.has('arrowleft')) { cam.yaw += YAW_SPEED * dt; moving = true; }
            if (held.has('arrowright')) { cam.yaw -= YAW_SPEED * dt; moving = true; }
            if (cam.mode !== 'top-down') {
                if (held.has('arrowup')) { cam.pitch = clampPitch(cam.pitch + PITCH_SPEED * dt); moving = true; }
                if (held.has('arrowdown')) { cam.pitch = clampPitch(cam.pitch - PITCH_SPEED * dt); moving = true; }
            }
            if (cam.mode === 'free') {
                cam.free ||= { ...anchor };
                const f = PAN_SPEED * dt * (cam.distance / ZOOM_DEFAULT);
                // Screen-relative: W moves away from the camera.
                const fx = -Math.sin(cam.yaw), fz = Math.cos(cam.yaw), sx = Math.cos(cam.yaw), sz = Math.sin(cam.yaw);
                if (held.has('w')) { cam.free.x += fx * f; cam.free.z += fz * f; moving = true; }
                if (held.has('s')) { cam.free.x -= fx * f; cam.free.z -= fz * f; moving = true; }
                if (held.has('d')) { cam.free.x += sx * f; cam.free.z += sz * f; moving = true; }
                if (held.has('a')) { cam.free.x -= sx * f; cam.free.z -= sz * f; moving = true; }
            }
            const d = easeToward(cam.distance, cam.targetDistance, dt);
            if (d !== cam.distance) { cam.distance = d; moving = true; }
            const target = cam.mode === 'free' && cam.free ? cam.free : anchor;
            return { camera: { x: target.x, y: target.y, z: target.z, yaw: cam.yaw, pitch: cam.mode === 'top-down' ? TOP_DOWN_PITCH : cam.pitch, distance: cam.distance }, moving };
        },
        destroy() {
            canvas.removeEventListener('wheel', wheel); canvas.removeEventListener('pointerdown', down);
            canvas.removeEventListener('auxclick', aux); canvas.removeEventListener('mousedown', aux);
            window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', up);
            host.removeEventListener('keydown', keydown); host.removeEventListener('keyup', keyup); host.removeEventListener('blur', blur);
        }
    };
}

// fitBox: the largest aspect-preserving box for a w x h game window inside a W x H area,
// centred. Scales are whole pixels so the 2D interface stays crisp.
export function fitBox(W, H, w = 765, h = 503) {
    const s = Math.max(0.1, Math.min(W / w, H / h));
    const width = Math.floor(w * s), height = Math.floor(h * s);
    return { left: Math.floor((W - width) / 2), top: Math.floor((H - height) / 2), width, height, scale: s };
}
