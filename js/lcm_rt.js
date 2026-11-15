// js-stellar-sdk 17.4.0 decoder: byte-exact round-trip of every golden LedgerCloseMeta.
const fs = require("fs"), path = require("path");
const S = require("sdk1740");
const files = [];
(function walk(d) { for (const e of fs.readdirSync(d)) { const p = path.join(d, e);
  if (fs.statSync(p).isDirectory()) walk(p); else if (p.endsWith(".xdr")) files.push(p); } })(process.argv[2]);
files.sort();
let n = 0, bad = 0, bytes = 0, errs = {};
const t0 = process.hrtime.bigint();
for (const f of files) {
  const b = fs.readFileSync(f);
  for (let i = 0; i < b.length;) {
    const l = b.readUInt32BE(i) & 0x7fffffff; i += 4;
    const fr = b.subarray(i, i + l); i += l;
    try {
      const m = S.xdr.LedgerCloseMeta.fromXDR(fr);
      const o = m.toXDR();
      if (!Buffer.from(o).equals(fr)) { bad++; errs["mismatch"] = (errs["mismatch"] || 0) + 1; }
    } catch (e) { bad++; const k = e.message.slice(0, 80); errs[k] = (errs[k] || 0) + 1; }
    n++; bytes += l;
  }
}
const s = Number(process.hrtime.bigint() - t0) / 1e9;
console.log(`js-stellar-sdk 17.4.0: ${n} LCMs, ${(bytes/1e6).toFixed(1)} MB, mismatches ${bad}, ${s.toFixed(2)}s, ${(n/s).toFixed(0)} LCM/s, ${(bytes/1e6/s).toFixed(1)} MB/s, heap ${(process.memoryUsage().rss/1e6).toFixed(0)} MB rss`);
if (bad) console.log(JSON.stringify(errs));
