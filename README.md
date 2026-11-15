# Hamming

A test kit for software that reads the Stellar ledger:

- **corpus**: rare but valid XDR, each entry with provenance (core golden, local-core capture, or synthetic
  mutation) and the shape atoms it covers;
- **harness**: replays the corpus through real indexers and reports, per entry, whether the target handled it;
- **verify**: decoder agreement across independent XDR implementations;
- **conformance runner**: SDK behaviour on the corpus;
- **linter and hardening guide** for indexer authors.

Hamming responds to the SCF RFP "XDR/CAP-Coverage Audit Tool and Indexer-Hardening Guide".

## Status

Pre-submission prototype. See [docs/PLAN.md](docs/PLAN.md) for the plan and [docs/FACTS.md](docs/FACTS.md)
for every pinned input and version this repository relies on.

## Results

Generated from `results/*.json` by `make`; not typed by hand.

## License

Apache-2.0. See [LICENSE](LICENSE).
