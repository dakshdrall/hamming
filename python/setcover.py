"""Greedy set cover over stellar-core golden files: fewest files that keep all covered atoms.
Greedy is within H(d) <= ln(d)+1 of optimal (d = most atoms in one file); also reports a lower bound."""
import json, math, os, sys
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import xdr_shapes as XS, xdr_coverage as XC
xdr_dir, root = sys.argv[1], sys.argv[2]
defs, atoms, _ = XS.summarize(xdr_dir)
dec = XC.Decoder(defs)
sets = {}
for d, dirs, names in os.walk(root):
    dirs.sort()                          # walk order decides greedy ties: make it the same on every OS
    for n in sorted(names):
        if not n.endswith(".xdr"): continue
        dec.hit = set()
        for fr in XC.frames(os.path.join(d, n)): dec.decode_frame(fr, "LedgerCloseMeta")
        sets[os.path.relpath(os.path.join(d, n), root)] = frozenset(dec.hit & atoms)
universe = frozenset().union(*sets.values())
left, chosen = set(universe), []
while left:
    f = max(sets, key=lambda k: len(sets[k] & left))
    chosen.append((f, len(sets[f] & left))); left -= sets[f]
d = max(len(s) for s in sets.values())
# lower bound: atoms that appear in exactly one file force that file in
unique = {}
for f, s in sets.items():
    for a in s: unique.setdefault(a, set()).add(f)
forced = {next(iter(fs)) for a, fs in unique.items() if len(fs) == 1}
print(json.dumps({"files": len(sets), "atoms": len(universe), "greedy_cover_files": len(chosen),
                  "max_atoms_in_one_file": d, "greedy_bound_factor_H(d)": round(sum(1/i for i in range(1, d+1)), 2),
                  "files_forced_by_unique_atoms (lower bound)": len(forced),
                  "first_5": chosen[:5]}, indent=1))
json.dump([c[0] for c in chosen], open("golden_cover.json", "w"), indent=1)
