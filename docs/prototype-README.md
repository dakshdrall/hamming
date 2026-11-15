# Hamming prototype (pre-submission)

Read `PLAN.md` first. Results are in `results/measurements.json`.

## Reproduce (Linux/macOS, Go ≥ 1.25, Rust stable, Node ≥ 20, Python ≥ 3.11)

```bash
# Inputs
git clone --filter=blob:none --sparse https://github.com/stellar/stellar-core core
(cd core && git sparse-checkout set test-lcm-current)                        # protocol 30 goldens
git clone https://github.com/stellar/stellar-xdr xdr
git clone https://github.com/stellar/wallet-backend wb

# 1. Decoder agreement (rs-stellar-xdr, go-stellar-sdk, js-stellar-sdk, grammar decoder)
(cd rust/verify && cargo run --release -- ../../core/test-lcm-current)
(cd go && go run ./cmd/rt ../core/test-lcm-current)
npm i sdk1740@npm:@stellar/stellar-sdk@17.4.0 && node js/lcm_rt.js core/test-lcm-current
python3 python/xdr_coverage.py xdr core/test-lcm-current                       # 459/899 atoms
python3 python/setcover.py xdr core/test-lcm-current                           # 51-file cover

# 2. wallet-backend replay of every core golden (HEAD)
cp adapters/wallet-backend/hamming_golden_test.go wb/internal/indexer/
(cd wb && HAMMING_GOLDEN=$PWD/../core/test-lcm-current go test ./internal/indexer -run TestHammingGolden -v)

# 3. wallet-backend#729 reproduction (protocol 28 entry)
for c in 2a0a84e 2c87335; do
  git -C wb worktree add ../wb-$c $c
  sed 's|NEWINDEXER|NewIndexer(pass, pond.NewPool(runtime.NumCPU()), nil)|' \
    adapters/wallet-backend/hamming_persist_test.go.tmpl > wb-$c/internal/indexer/hamming_persist_test.go
  (cd wb-$c && HAMMING_LEDGER=$PWD/../corpus/mutated/cap40-signed-payload-signer-p28.xdr \
     go test ./internal/indexer -run TestHammingPersist -v)
done
# expected: 2a0a84e → "mishandled (ledger cannot be persisted)", 2c87335 → "pass"
# HEAD uses NewIndexer(pass, pool, nil, true)

# Regenerate the mutated entry (needs core goldens from core commit abe1c55, protocol 28)
(cd go && go run ./cmd/mutate <core@abe1c55>/test-lcm-current out.xdr out.json)
```

`corpus/mutated/*` entries are **synthetic** (derived from a core golden; signatures and the header
`txSetHash` no longer match). Replace them with local stellar-core captures before treating them as
core-emitted.
