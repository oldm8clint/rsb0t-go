// What a panel's stored settings mean, given the fields its type declares.
//
// Only declared keys are read, and only when the stored value has the declared kind, so a layout
// written by an older or newer build still opens: anything unrecognisable falls back to its default.
//
// A field is { key, label, kind, default } plus, by kind:
//   bool
//   number  min, max, step
//   select  options: [{ value, label }]   (value is a string)
//   multi   options: [{ value, label }]   (default and value are arrays of option values)

export function normaliseSettings(fields, raw) {
    const src = typeof raw === 'object' && raw !== null && !Array.isArray(raw) ? raw : {};
    const out = {};
    for (const f of fields ?? []) {
        const v = src[f.key];
        switch (f.kind) {
            case 'bool':
                out[f.key] = typeof v === 'boolean' ? v : f.default;
                break;
            case 'number':
                out[f.key] = typeof v === 'number' && Number.isFinite(v) ? Math.min(f.max, Math.max(f.min, v)) : f.default;
                break;
            case 'select':
                out[f.key] = f.options.some(o => o.value === v) ? v : f.default;
                break;
            case 'multi':
                out[f.key] = Array.isArray(v) ? f.options.map(o => o.value).filter(x => v.includes(x)) : [...f.default];
                break;
            default:
                break;
        }
    }
    return out;
}
