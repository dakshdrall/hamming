// js-stellar-sdk decoder: byte-exact round-trip of every golden LedgerCloseMeta.
//
//   node lcm_rt.js [--json result.json] <dir>
//
// --json writes the decoder's row for hamming-verify's agreement table (results/decoders.json).
const fs = require("fs"), path = require("path");
const S = require("sdk1740");
// The package's "exports" hide package.json from require, so find it on disk from the resolved entry point.
const version = (() => {
  for (let d = path.dirname(require.resolve("sdk1740")); d !== path.dirname(d); d = path.dirname(d)) {
    const p = path.join(d, "package.json");
    if (fs.existsSync(p)) { const j = JSON.parse(fs.readFileSync(p, "utf8")); if (j.name === "@stellar/stellar-sdk") return j.version; }
  }
  throw new Error("cannot find @stellar/stellar-sdk's package.json");
})();

const args = process.argv.slice(2);
let out = null;
if (args[0] === "--json") { out = args[1]; args.splice(0, 2); }
if (args.length !== 1) { console.error("usage: node lcm_rt.js [--json result.json] <dir>"); process.exit(2); }

const files = [];
(function walk(d) {
  for (const e of fs.readdirSync(d)) {
    if (e.startsWith(".")) continue; // macOS AppleDouble "._x.xdr" twins
    const p = path.join(d, e);
    if (fs.statSync(p).isDirectory()) walk(p); else if (p.endsWith(".xdr")) files.push(p);
  }
})(args[0]);
files.sort();

const r = { decoder: "js-stellar-sdk", version, files: 0, frames: 0, bytes: 0, mismatches: 0, decode_errors: 0 };
const errs = {};
const t0 = process.hrtime.bigint();
for (const f of files) {
  const b = fs.readFileSync(f);
  r.files++;
  for (let i = 0; i < b.length;) {
    if (b.length - i < 4) { r.decode_errors++; break; }
    const l = b.readUInt32BE(i) & 0x7fffffff; i += 4;
    if (l > b.length - i) { r.decode_errors++; break; }
    const fr = b.subarray(i, i + l); i += l;
    r.frames++; r.bytes += l;
    let m;
    try { m = S.xdr.LedgerCloseMeta.fromXDR(fr); } catch (e) {
      r.decode_errors++; const k = e.message.slice(0, 80); errs[k] = (errs[k] || 0) + 1; continue;
    }
    if (!Buffer.from(m.toXDR()).equals(fr)) r.mismatches++;
  }
}
const s = Number(process.hrtime.bigint() - t0) / 1e9;
console.log(`js-stellar-sdk ${version}: ${r.files} files, ${r.frames} frames, ${(r.bytes / 1e6).toFixed(1)} MB, ` +
  `mismatches ${r.mismatches}, decode errors ${r.decode_errors}, ${s.toFixed(2)}s, ${(r.frames / s).toFixed(0)} frames/s, ` +
  `rss ${(process.memoryUsage().rss / 1e6).toFixed(0)} MB`);
if (Object.keys(errs).length) console.log(JSON.stringify(errs));
if (out) fs.writeFileSync(out, JSON.stringify(r, null, 2) + "\n");
if (r.mismatches || r.decode_errors) process.exit(1);
