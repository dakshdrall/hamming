"""Shape-space calculator for Stellar XDR.

Parses the .x files, walks every type reachable from a root (default LedgerCloseMeta),
and counts *shape atoms*: the structural choices a decoder must handle.

  union arm        -> 1 atom per discriminant value (+1 for default)
  optional (T*)    -> 2 atoms (absent, present)
  var array T<N>   -> 0, 1, many, and max (if bounded) = 3-4 atoms
  var opaque/string-> empty, non-empty, max (if bounded)
  enum field value -> 1 atom per value (enums used as plain fields, not as discriminants)
  bool field       -> 2 atoms
  wide ints        -> boundary atoms {0, max, 2^53+1 for u64/i64, sign for signed}

Usage: python3 xdr_shapes.py <dir-or-git-tag> [root]
"""
import os, re, subprocess, sys, json
from collections import defaultdict

def load_sources(src):
    if os.path.isdir(src):
        files = sorted(f for f in os.listdir(src) if f.endswith(".x"))
        return {f: open(os.path.join(src, f)).read() for f in files}
    # git tag in the stellar-xdr checkout
    repo = os.environ.get("XDR_REPO", "/home/claude/stellar/stellar-xdr")
    names = subprocess.check_output(["git", "-C", repo, "ls-tree", "--name-only", src], text=True).split()
    return {n: subprocess.check_output(["git", "-C", repo, "show", f"{src}:{n}"], text=True)
            for n in names if n.endswith(".x")}

def strip(text):
    text = re.sub(r"/\*.*?\*/", " ", text, flags=re.S)
    text = re.sub(r"//[^\n]*", " ", text)
    out, skip = [], 0
    for line in text.split("\n"):
        s = line.strip()
        if s.startswith("#ifdef") or s.startswith("#if "):
            skip += 1; continue            # feature-flagged blocks (TEST_FEATURE) are not on the wire
        if s.startswith("#endif"):
            skip = max(0, skip - 1); continue
        if s.startswith("#else"):
            continue
        if skip or s.startswith("%") or s.startswith("#"):
            continue
        out.append(line)
    return "\n".join(out)

TOK = re.compile(r"\s*(?:(0x[0-9a-fA-F]+|-?\d+)|([A-Za-z_][A-Za-z0-9_]*)|(.))")
def tokenize(text):
    toks = []
    for m in TOK.finditer(text):
        n, i, s = m.groups()
        if n: toks.append(("num", n))
        elif i: toks.append(("id", i))
        elif s and not s.isspace(): toks.append(("sym", s))
    return toks

class P:
    def __init__(s, toks): s.t, s.i = toks, 0
    def peek(s, k=0): return s.t[s.i + k] if s.i + k < len(s.t) else (None, None)
    def nxt(s): tok = s.t[s.i]; s.i += 1; return tok
    def eat(s, v):
        tok = s.nxt()
        assert tok[1] == v, f"expected {v} got {tok} at {s.i}"
    def val(s):
        k, v = s.nxt(); return v

    def spec(s):
        defs = {}
        while s.i < len(s.t):
            k, v = s.peek()
            if v == "namespace":
                s.nxt(); s.nxt(); s.eat("{")
                depth = 1
                while depth:
                    k, v = s.peek()
                    if v == "}": s.nxt(); depth -= 1; continue
                    s.definition(defs)
            else:
                s.definition(defs)
        return defs

    def definition(s, defs):
        k, v = s.nxt()
        if v == "typedef":
            d = s.declaration(); s.eat(";"); defs[d["name"]] = ("typedef", d)
        elif v == "enum":
            name = s.val(); defs[name] = ("enum", s.enum_body()); s.eat(";")
        elif v == "struct":
            name = s.val(); defs[name] = ("struct", s.struct_body()); s.eat(";")
        elif v == "union":
            name = s.val(); defs[name] = ("union", s.union_body()); s.eat(";")
        elif v == "const":
            name = s.val(); s.eat("="); defs[name] = ("const", s.val()); s.eat(";")
        elif v == ";":
            pass
        else:
            raise SyntaxError(f"unexpected {v} at {s.i}")

    def enum_body(s):
        s.eat("{"); vals = []
        while True:
            name = s.val()
            if s.peek()[1] == "=": s.nxt(); vals.append((name, s.val()))
            else: vals.append((name, None))
            if s.peek()[1] == ",": s.nxt(); continue
            s.eat("}"); return vals

    def struct_body(s):
        s.eat("{"); fields = []
        while s.peek()[1] != "}":
            fields.append(s.declaration()); s.eat(";")
        s.eat("}"); return fields

    def union_body(s):
        s.eat("switch"); s.eat("(")
        disc_t = s.type_spec(); disc_name = s.val(); s.eat(")"); s.eat("{")
        arms, default = [], None
        while s.peek()[1] != "}":
            cases = []
            while s.peek()[1] in ("case", "default"):
                if s.nxt()[1] == "case": cases.append(s.val())
                else: cases.append("default")
                s.eat(":")
            d = s.declaration(); s.eat(";")
            arms.append((cases, d))
        s.eat("}")
        return {"disc": disc_t, "arms": arms}

    def type_spec(s):
        k, v = s.nxt()
        if v == "unsigned":
            if s.peek()[1] in ("int", "hyper"): return ("prim", "u" + s.nxt()[1])
            return ("prim", "uint")
        if v in ("int", "hyper", "float", "double", "bool", "quadruple"): return ("prim", v)
        if v == "enum": return ("enum", s.enum_body())
        if v == "struct": return ("struct", s.struct_body())
        if v == "union": return ("union", s.union_body())
        return ("ref", v)

    def declaration(s):
        if s.peek()[1] == "void": s.nxt(); return {"kind": "void", "name": None}
        if s.peek()[1] in ("opaque", "string"):
            base = s.nxt()[1]; name = s.val()
            if s.peek()[1] == "[": s.nxt(); n = s.val(); s.eat("]"); return {"kind": "fixed_" + base, "name": name, "bound": n}
            s.eat("<"); n = None
            if s.peek()[1] != ">": n = s.val()
            s.eat(">"); return {"kind": "var_" + base, "name": name, "bound": n}
        t = s.type_spec()
        if s.peek()[1] == "*": s.nxt(); return {"kind": "opt", "type": t, "name": s.val()}
        name = s.val()
        if s.peek()[1] == "[": s.nxt(); n = s.val(); s.eat("]"); return {"kind": "fixed_arr", "type": t, "name": name, "bound": n}
        if s.peek()[1] == "<":
            s.nxt(); n = None
            if s.peek()[1] != ">": n = s.val()
            s.eat(">"); return {"kind": "var_arr", "type": t, "name": name, "bound": n}
        return {"kind": "plain", "type": t, "name": name}

def parse(sources):
    defs = {}
    for f, text in sources.items():
        defs.update(P(tokenize(strip(text))).spec())
    return defs

def resolve_const(defs, v):
    if v is None: return None
    if re.fullmatch(r"-?\d+|0x[0-9a-fA-F]+", v): return int(v, 0)
    if v in defs and defs[v][0] == "const": return resolve_const(defs, defs[v][1])
    return v

WIDE = {"uhyper": ("u64",), "hyper": ("i64",)}

def walk(defs, root):
    atoms, edges, visited_named = set(), defaultdict(set), set()
    bounds = []

    def type_atoms(t, path, owner):
        kind = t[0]
        if kind == "ref":
            name = t[1]
            if name not in defs: atoms.add(f"?unresolved:{name}"); return
            edges[owner].add(name)
            named(name); return
        if kind == "prim":
            if t[1] == "bool": atoms.update({f"{path}=false", f"{path}=true"})
            elif t[1] in ("uhyper",): atoms.update({f"{path}#0", f"{path}#2^53+1", f"{path}#max"})
            elif t[1] in ("hyper",): atoms.update({f"{path}#0", f"{path}#neg", f"{path}#2^53+1", f"{path}#max"})
            return
        if kind == "enum":
            for n, _ in t[1]: atoms.add(f"{path}={n}")
            return
        if kind == "struct":
            for d in t[1]: decl_atoms(d, f"{path}.{d['name']}", owner)
            return
        if kind == "union":
            union_atoms(t[1], path, owner); return

    def union_atoms(u, path, owner):
        for cases, d in u["arms"]:
            for c in cases: atoms.add(f"{path}|{c}")
            decl_atoms(d, f"{path}|{cases[0]}", owner)

    def decl_atoms(d, path, owner):
        k = d["kind"]
        if k == "void": return
        if k == "opt":
            atoms.update({f"{path}?absent", f"{path}?present"}); type_atoms(d["type"], path, owner); return
        if k == "var_arr":
            b = resolve_const(defs, d.get("bound"))
            atoms.update({f"{path}[len=0]", f"{path}[len=1]", f"{path}[len>1]"})
            if isinstance(b, int): atoms.add(f"{path}[len=max:{b}]"); bounds.append((path, b))
            type_atoms(d["type"], path + "[]", owner); return
        if k in ("var_opaque", "var_string"):
            b = resolve_const(defs, d.get("bound"))
            atoms.update({f"{path}<empty>", f"{path}<nonempty>"})
            if isinstance(b, int): atoms.add(f"{path}<max:{b}>"); bounds.append((path, b))
            if k == "var_string": atoms.update({f"{path}<non-utf8>"})
            return
        if k == "fixed_arr":
            type_atoms(d["type"], path + "[]", owner); return
        if k in ("fixed_opaque", "fixed_string"): return
        type_atoms(d["type"], path, owner)

    def named(name):
        if name in visited_named: return
        visited_named.add(name)
        kind, body = defs[name]
        if kind == "typedef": decl_atoms(body, name, name)
        elif kind == "struct":
            for d in body: decl_atoms(d, f"{name}.{d['name']}", name)
        elif kind == "union": union_atoms(body, name, name)
        elif kind == "enum":
            pass   # enum values become atoms only where used as a field (handled at use site via ref)

    # enum used as a plain field -> values are atoms; patch: treat ref-to-enum at use site
    orig_type_atoms = type_atoms
    def type_atoms_enum_aware(t, path, owner):
        if t[0] == "ref" and t[1] in defs and defs[t[1]][0] == "enum":
            for n, _ in defs[t[1]][1]: atoms.add(f"{path}={n}")
            visited_named.add(t[1]); return
        orig_type_atoms(t, path, owner)
    type_atoms = type_atoms_enum_aware  # noqa: F841  (closure rebinding below)

    # rebind closures to use enum-aware version
    def type_atoms_final(t, path, owner): return type_atoms_enum_aware(t, path, owner)
    nonlocal_fix = {"f": type_atoms_final}

    named(root)
    return atoms, edges, visited_named, bounds

def sccs(edges, nodes):
    index, low, onstack, stack, out, counter = {}, {}, set(), [], [], [0]
    sys.setrecursionlimit(10000)
    def strong(v):
        index[v] = low[v] = counter[0]; counter[0] += 1; stack.append(v); onstack.add(v)
        for w in edges.get(v, ()):
            if w not in index: strong(w); low[v] = min(low[v], low[w])
            elif w in onstack: low[v] = min(low[v], index[w])
        if low[v] == index[v]:
            comp = []
            while True:
                w = stack.pop(); onstack.discard(w); comp.append(w)
                if w == v: break
            if len(comp) > 1 or v in edges.get(v, ()): out.append(sorted(comp))
    for n in nodes:
        if n not in index: strong(n)
    return out

def summarize(src, root="LedgerCloseMeta"):
    defs = parse(load_sources(src))
    atoms, edges, named, bounds = walk(defs, root)
    cat = defaultdict(int)
    for a in atoms:
        last = re.split(r"[|.]", a)[-1]
        if a.endswith("?absent") or a.endswith("?present"): cat["optional"] += 1
        elif re.search(r"\[len[^\]]*\]$", a): cat["var_array_len"] += 1
        elif a.endswith(">"): cat["var_bytes_len"] += 1
        elif "#" in last: cat["wide_int_boundary"] += 1
        elif "=" in last: cat["enum_or_bool_value"] += 1
        elif "|" in a: cat["union_arm"] += 1
        else: cat["other"] += 1
    cyc = sccs(edges, named)
    return defs, atoms, {"root": root, "types_reachable": len(named), "shape_atoms": len(atoms),
                          "by_category": dict(sorted(cat.items())), "recursive_groups": cyc}

if __name__ == "__main__":
    src = sys.argv[1]; root = sys.argv[2] if len(sys.argv) > 2 else "LedgerCloseMeta"
    defs, atoms, s = summarize(src, root)
    print(json.dumps(s, indent=1))
