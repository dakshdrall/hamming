"""Grammar-driven XDR decoder that records shape atoms while decoding.

It reuses the parser in xdr_shapes.py, so the atom IDs it emits are the same
ones the shape calculator counts. Pointed at stellar-core's committed golden
LedgerCloseMeta files (record-marked XDR streams), it measures how much of the
shape space stellar-core's own test suite already exercises.

Usage: python3 xdr_coverage.py <stellar-xdr dir> <golden root> [root type] [--out covered_missing.json]

Exits 1 if any frame fails to decode.
"""
import json, os, struct, sys
from collections import Counter, defaultdict
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import xdr_shapes as XS

sys.setrecursionlimit(20000)


class Decoder:
    def __init__(self, defs):
        self.defs = defs
        self.enum_vals = {}                       # constant name -> int, across all enums
        pending = [(n, v) for name, (kind, body) in defs.items() if kind == "enum" for n, v in body]
        for _ in range(10):                       # enum values may reference other enums' constants
            left = []
            for n, v in pending:
                if v is not None and (v.lstrip("-").isdigit() or v.startswith("0x")): self.enum_vals[n] = int(v, 0)
                elif v in self.enum_vals: self.enum_vals[n] = self.enum_vals[v]
                else: left.append((n, v))
            pending = left
            if not pending: break
        assert not pending, pending[:5]
        self.enum_maps = {name: {self.enum_vals[n]: n for n, _ in body}
                          for name, (kind, body) in defs.items() if kind == "enum"}
        self.hit = set()
        self.buf = b""
        self.pos = 0

    # ---- primitives
    def u32(self):
        v = struct.unpack_from(">I", self.buf, self.pos)[0]; self.pos += 4; return v
    def i32(self):
        v = struct.unpack_from(">i", self.buf, self.pos)[0]; self.pos += 4; return v
    def u64(self):
        v = struct.unpack_from(">Q", self.buf, self.pos)[0]; self.pos += 8; return v
    def i64(self):
        v = struct.unpack_from(">q", self.buf, self.pos)[0]; self.pos += 8; return v
    def raw(self, n):
        b = self.buf[self.pos:self.pos + n]
        if len(b) != n: raise ValueError("short read")
        self.pos += n + ((4 - n % 4) % 4)
        return b

    def const(self, v):
        if v in self.enum_vals: return self.enum_vals[v]
        r = XS.resolve_const(self.defs, v)
        if isinstance(r, int): return r
        raise KeyError(v)

    # ---- mirrors xdr_shapes.walk naming exactly
    def named(self, name):
        kind, body = self.defs[name]
        if kind == "typedef": return self.decl(body, name)
        if kind == "struct":
            return {d["name"]: self.decl(d, f"{name}.{d['name']}") for d in body}
        if kind == "union": return self.union(body, name)
        if kind == "enum": return self.u32_enum(name, None)
        raise TypeError(kind)

    def u32_enum(self, ename, path):
        v = self.i32()
        names = self.enum_maps[ename]
        if v not in names: raise ValueError(f"bad enum {ename}={v}")
        if path is not None: self.hit.add(f"{path}={names[v]}")
        return names[v]

    def union(self, u, path):
        disc_t = u["disc"]
        if disc_t[0] == "ref" and self.defs.get(disc_t[1], ("",))[0] == "enum":
            d = self.i32()
        elif disc_t[0] == "prim" and disc_t[1] in ("uint", "uint32"):
            d = self.u32()
        else:
            d = self.i32()
        default = None
        for cases, decl in u["arms"]:
            for c in cases:
                if c == "default": default = (cases, decl); continue
                if self.const(c) == d:
                    self.hit.add(f"{path}|{c}")
                    return {c: self.decl(decl, f"{path}|{cases[0]}")}
        if default:
            self.hit.add(f"{path}|default")
            return {"default": self.decl(default[1], f"{path}|{default[0][0]}")}
        raise ValueError(f"no arm for {path} discriminant {d}")

    def decl(self, d, path):
        k = d["kind"]
        if k == "void": return None
        if k == "opt":
            present = self.u32()
            self.hit.add(f"{path}?{'present' if present else 'absent'}")
            return self.typ(d["type"], path) if present else None
        if k == "var_arr":
            b = XS.resolve_const(self.defs, d.get("bound"))
            n = self.u32()
            if isinstance(b, int) and n > b: raise ValueError(f"{path} length {n} > {b}")
            self.hit.add(f"{path}[len={n}]" if n in (0, 1) else f"{path}[len>1]")
            if isinstance(b, int) and n == b: self.hit.add(f"{path}[len=max:{b}]")
            return [self.typ(d["type"], path + "[]") for _ in range(n)]
        if k in ("var_opaque", "var_string"):
            b = XS.resolve_const(self.defs, d.get("bound"))
            n = self.u32()
            if isinstance(b, int) and n > b: raise ValueError(f"{path} length {n} > {b}")
            data = self.raw(n)
            self.hit.add(f"{path}<{'empty' if n == 0 else 'nonempty'}>")
            if isinstance(b, int) and n == b: self.hit.add(f"{path}<max:{b}>")
            if k == "var_string":
                try: data.decode("utf-8")
                except UnicodeDecodeError: self.hit.add(f"{path}<non-utf8>")
            return data.hex()
        if k == "fixed_arr":
            n = XS.resolve_const(self.defs, d["bound"])
            return [self.typ(d["type"], path + "[]") for _ in range(n)]
        if k in ("fixed_opaque", "fixed_string"):
            return self.raw(XS.resolve_const(self.defs, d["bound"])).hex()
        return self.typ(d["type"], path)

    def typ(self, t, path):
        kind = t[0]
        if kind == "ref":
            name = t[1]
            if self.defs[name][0] == "enum": return self.u32_enum(name, path)
            return self.named(name)
        if kind == "prim":
            p = t[1]
            if p == "bool":
                v = self.u32(); self.hit.add(f"{path}={'true' if v else 'false'}"); return bool(v)
            if p == "int": return self.i32()
            if p in ("uint", "uint32"): return self.u32()
            if p == "uhyper":
                v = self.u64()
                if v == 0: self.hit.add(f"{path}#0")
                if v > 2**53: self.hit.add(f"{path}#2^53+1")
                if v == 2**64 - 1: self.hit.add(f"{path}#max")
                return v
            if p == "hyper":
                v = self.i64()
                if v == 0: self.hit.add(f"{path}#0")
                if v < 0: self.hit.add(f"{path}#neg")
                if abs(v) > 2**53: self.hit.add(f"{path}#2^53+1")
                if v == 2**63 - 1: self.hit.add(f"{path}#max")
                return v
            raise TypeError(p)
        if kind == "enum":
            v = self.i32()
            names = {(int(x, 0) if x.lstrip("-").isdigit() or x.startswith("0x") else self.enum_vals[x]): n for n, x in t[1]}
            self.hit.add(f"{path}={names[v]}"); return names[v]
        if kind == "struct":
            return {d["name"]: self.decl(d, f"{path}.{d['name']}") for d in t[1]}
        if kind == "union":
            return self.union(t[1], path)
        raise TypeError(kind)

    def decode_frame(self, data, root):
        self.buf, self.pos = data, 0
        v = self.named(root)
        if self.pos != len(data):
            raise ValueError(f"trailing bytes: consumed {self.pos} of {len(data)}")
        return v


def frames(path):
    b = open(path, "rb").read()
    i = 0
    while i < len(b):
        mark = struct.unpack_from(">I", b, i)[0]; i += 4
        n = mark & 0x7FFFFFFF
        yield b[i:i + n]; i += n


def main():
    args = sys.argv[1:]
    out_path = None
    if "--out" in args:
        i = args.index("--out")
        out_path = args[i + 1]
        del args[i:i + 2]
    xdr_dir, golden, root = args[0], args[1], (args[2] if len(args) > 2 else "LedgerCloseMeta")
    defs, atoms, summary = XS.summarize(xdr_dir, root)
    dec = Decoder(defs)
    files = lcms = errors = 0
    per_dir = defaultdict(set)
    for dirpath, _, names in os.walk(golden):
        for n in sorted(names):
            if not n.endswith(".xdr"): continue
            files += 1
            before = set(dec.hit)
            for fr in frames(os.path.join(dirpath, n)):
                try:
                    dec.decode_frame(fr, root); lcms += 1
                except Exception as e:
                    errors += 1
                    if errors <= 3: print("decode error", n, e, file=sys.stderr)
            per_dir[os.path.basename(dirpath)] |= (dec.hit - before) | (dec.hit & before)
    covered = dec.hit & atoms
    stray = dec.hit - atoms           # should be empty: every emitted atom is a counted atom
    def cat(a):
        last = a.split("|")[-1].split(".")[-1]
        if a.endswith("?absent") or a.endswith("?present"): return "optional"
        if "[len" in a.rsplit("]", 1)[0][-30:] and a.endswith("]"): return "var_array_len"
        if a.endswith(">"): return "var_bytes_len"
        if "#" in last: return "wide_int_boundary"
        if "=" in last: return "enum_or_bool_value"
        return "union_arm"
    tot, cov = Counter(cat(a) for a in atoms), Counter(cat(a) for a in covered)
    missing_arms = sorted(a for a in atoms - covered if cat(a) == "union_arm")
    out = {
        "golden_root": golden, "root": root, "files": files, "ledger_close_metas": lcms, "decode_errors": errors,
        "atoms_total": len(atoms), "atoms_covered": len(covered), "coverage": round(len(covered) / len(atoms), 4),
        "by_category": {c: f"{cov[c]}/{tot[c]}" for c in sorted(tot)},
        "atoms_emitted_but_uncounted": sorted(stray)[:10],
        "missing_union_arms_sample": missing_arms[:60],
        "missing_union_arms_count": len(missing_arms),
    }
    print(json.dumps(out, indent=1))
    if out_path is None:
        out_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), f"coverage_{os.path.basename(golden.rstrip('/'))}.json")
    json.dump({"covered": sorted(covered), "missing": sorted(atoms - covered)}, open(out_path, "w"), indent=1)
    if errors:
        sys.exit(1)


if __name__ == "__main__":
    main()
