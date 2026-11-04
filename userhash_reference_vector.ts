import JString from '../rsb0t-main/src/client/datastruct/JString.js';

const cases = ['ironladle', 'Spareacct', 'a', 'ABCDEFGHIJKLMNOP', '  spaced  ', '123abc', ''];
const out = cases.map(name => {
    const hash = JString.toUserhash(name);
    return { name, hash: hash.toString(), loginServer: Number((hash >> 16n) & 0x1fn) };
});
console.log(JSON.stringify(out, null, 2));
