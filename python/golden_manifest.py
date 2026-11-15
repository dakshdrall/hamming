"""Index stellar-core's committed golden LCMs as corpus seed entries: one row per golden file,
with the core test section that produced it and the atoms it covers. Files are referenced by
path + core commit + sha256 (not copied)."""
import hashlib, json, os, sys
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import xdr_shapes as XS, xdr_coverage as XC
xdr_dir, root_dir, commit = sys.argv[1], sys.argv[2], sys.argv[3]
defs, atoms, _ = XS.summarize(xdr_dir)
dec = XC.Decoder(defs)
rows, first_seen = [], {}
for d in sorted(os.listdir(root_dir)):
    idx = json.load(open(os.path.join(root_dir, d, "index.json")))
    proto = idx.get("!cfg protocol version")
    for f in sorted(x for x in os.listdir(os.path.join(root_dir, d)) if x.endswith(".xdr")):
        p = os.path.join(root_dir, d, f)
        dec.hit = set(); n = 0
        for fr in XC.frames(p):
            dec.decode_frame(fr, "LedgerCloseMeta"); n += 1
        got = sorted(dec.hit & atoms)
        for a in got: first_seen.setdefault(a, idx.get(f[:-4], f))
        rows.append({"id": f"core.{d}.{f[:-4]}", "source": {"kind": "core-golden", "repo": "stellar/stellar-core",
                     "commit": commit, "path": f"{os.path.basename(root_dir)}/{d}/{f}"},
                     "sha256": hashlib.sha256(open(p, "rb").read()).hexdigest(), "protocol": proto,
                     "core_test": idx.get(f[:-4]), "ledger_close_metas": n, "atoms": len(got)})
out = {"core_commit": commit, "entries": len(rows), "atoms_covered": len(first_seen), "atoms_total": len(atoms),
       "atom_to_first_core_test": first_seen, "entries_index": rows}
json.dump(out, open("core_golden_manifest.json", "w"), indent=1)
best = sorted(rows, key=lambda r: -r["atoms"])[:3]
print(len(rows), "entries;", len(first_seen), "atoms traced to a core test")
for r in best: print(r["atoms"], r["core_test"])
for a in ["SCAddress|SC_ADDRESS_TYPE_MUXED_CONTRACT", "StellarValue.ext|STELLAR_VALUE_SIGNED_MS", "ChangeTrustAsset|ASSET_TYPE_POOL_SHARE"]:
    print(a, "<-", first_seen.get(a))
