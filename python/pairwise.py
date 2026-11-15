"""Greedy strength-2 covering array (every pair of factor values appears in some row).
Illustrates interaction atoms: how many entries cover all 2-way combinations vs the full product.
Factors are unconstrained here; in the real generator, stellar-core decides validity of each row."""
import itertools, math, random

FACTORS = {
    "address_kind": ["ACCOUNT", "CONTRACT", "MUXED_ACCOUNT", "CLAIMABLE_BALANCE", "LIQUIDITY_POOL", "MUXED_CONTRACT"],
    "position": ["event_topic", "event_data", "auth_credentials", "invoke_arg", "contract_data_key"],
    "envelope": ["TX_V0", "TX", "TX_FEE_BUMP"],
    "memo": ["NONE", "TEXT", "ID", "HASH", "RETURN"],
}

def greedy_pairwise(factors, seed=0, candidates=200):
    rng = random.Random(seed)
    names = list(factors)
    uncovered = {((a, va), (b, vb)) for a, b in itertools.combinations(names, 2)
                 for va in factors[a] for vb in factors[b]}
    rows = []
    while uncovered:
        best, best_gain = None, -1
        for _ in range(candidates):
            # seed the row with one uncovered pair, fill the rest greedily
            (a, va), (b, vb) = rng.choice(sorted(uncovered))
            row = {a: va, b: vb}
            for f in names:
                if f in row: continue
                row[f] = max(factors[f], key=lambda v: sum(((g, row[g]), (f, v)) in uncovered or ((f, v), (g, row[g])) in uncovered
                                                           for g in row))
            gain = sum(((x, row[x]), (y, row[y])) in uncovered for x, y in itertools.combinations(names, 2))
            if gain > best_gain: best, best_gain = row, gain
        rows.append(best)
        uncovered -= {((x, best[x]), (y, best[y])) for x, y in itertools.combinations(names, 2)}
    return rows

if __name__ == "__main__":
    full = math.prod(len(v) for v in FACTORS.values())
    sizes = sorted((len(v) for v in FACTORS.values()), reverse=True)
    lower = sizes[0] * sizes[1]
    best = min((greedy_pairwise(FACTORS, seed=s) for s in range(20)), key=len)
    print(f"full product: {full} rows; strength-2 lower bound: {lower}; greedy covering array: {len(best)} rows")
