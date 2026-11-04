// Native bitmap overlays and packet-driven projectile motion. No game input.
export function bitmapFont(data) {
    const glyphs = data.glyphs.map(g => {
        const canvas = document.createElement('canvas'); canvas.width = g.width; canvas.height = g.height;
        if (g.width && g.height) {
            const ctx = canvas.getContext('2d'), image = ctx.createImageData(g.width, g.height);
            for (let i = 0; i < g.mask.length; i++) if (g.mask[i]) image.data.set([255, 255, 255, 255], i * 4);
            ctx.putImageData(image, 0, 0);
        }
        return { ...g, canvas, colours: new Map() };
    });
    const chars = text => Array.from(text.replace(/@[a-z0-9]{3}@/gi, ''), ch => glyphs[ch.charCodeAt(0)] || glyphs[63]);
    return {
        width(text) { return chars(text).reduce((n, g) => n + g.advance, 0); },
        draw(ctx, text, x, y, colour = '#fff', shadow = false) {
            let ink = colour;
            const colours = {red:'#ff0000',gre:'#00ff00',blu:'#0000ff',yel:'#ffff00',cya:'#00ffff',mag:'#ff00ff',whi:'#ffffff',bla:'#000000',lre:'#ff9040',dre:'#800000',dbl:'#000080',or1:'#ffb000',or2:'#ff7000',or3:'#ff3000',gr1:'#c0ff00',gr2:'#80ff00',gr3:'#40ff00'};
            for (const token of text.match(/@[a-z0-9]{3}@|[^@]|@/gi) || []) {
                if (token.length === 5 && token[0] === '@') { ink = colours[token.slice(1,4)] || ink; continue; }
                const g = glyphs[token.charCodeAt(0)] || glyphs[63];
                if (g.width && g.height) {
                    const tint = c => {
                        if (!g.colours.has(c)) {
                            const canvas = document.createElement('canvas'); canvas.width = g.width; canvas.height = g.height;
                            const out = canvas.getContext('2d'); out.drawImage(g.canvas, 0, 0); out.globalCompositeOperation = 'source-in'; out.fillStyle = c; out.fillRect(0, 0, g.width, g.height);
                            g.colours.set(c, canvas);
                        }
                        return g.colours.get(c);
                    };
                    const gy = y - data.height + g.y;
                    if (shadow) ctx.drawImage(tint('#000'), x + g.x + 1, gy + 1);
                    ctx.drawImage(tint(ink), x + g.x, gy);
                }
                x += g.advance;
            }
        }
    };
}

export function bitmapSprite(sprite) {
    if (!sprite) return null;
    const c = document.createElement('canvas'); c.width = sprite.width; c.height = sprite.height;
    const ctx = c.getContext('2d'), image = ctx.createImageData(c.width, c.height);
    sprite.pixels.forEach((rgb, i) => { if (rgb) image.data.set([rgb >>> 16 & 255, rgb >>> 8 & 255, rgb & 255, 255], i * 4); });
    ctx.putImageData(image, 0, 0); return c;
}

export function effectFrame(info, cycles, loop = false) {
    const total = info.delays.reduce((a, b) => a + b, 0);
    if (cycles < 0 || (!loop && info.animated && cycles >= total)) return -1;
    if (loop) cycles %= total;
    for (let i = 0; i < info.delays.length; i++) { if (cycles < info.delays[i]) return i; cycles -= info.delays[i]; }
    return info.animated ? -1 : 0;
}

// ClientProj.setTarget's initial offset, launch slope, acceleration and orientation.
export function projectileAt(e, cycles, ground, target) {
    if (cycles < e.start || cycles > e.end) return null;
    const x0 = e.x + .5, z0 = e.z + .5, dx = e.toX + .5 - x0, dz = e.toZ + .5 - z0;
    const distance = Math.hypot(dx, dz), offset = distance ? e.arc / (128 * distance) : 0;
    const x = x0 + dx * offset, z = z0 + dz * offset, y = ground(x0, z0) + e.height / 128;
    const endX = target?.[0] ?? e.toX + .5, endZ = target?.[1] ?? e.toZ + .5;
    const duration = Math.max(1, e.end - e.start + 1), t = cycles - e.start;
    const vx = (endX - x) / duration, vz = (endZ - z) / duration;
    const vy = Math.hypot(vx, vz) * Math.tan(e.slope * Math.PI / 128);
    const endY = ground(endX, endZ) + e.endHeight / 128;
    const ay = 2 * (endY - y - vy * duration) / (duration * duration);
    return { x: x + vx * t, z: z + vz * t, y: y + vy * t + .5 * ay * t * t,
        yaw: Math.atan2(vx, vz) + Math.PI, pitch: -Math.atan2(vy + ay * t, Math.hypot(vx, vz)) };
}
