# Facts this repository relies on

Every version, ref, constant or measured number that code or docs depend on, with the date it was checked
and where it came from. If something here changes upstream, update the row and the code together.

## Pinned upstream inputs

Fetched by `scripts/fetch-inputs.sh`, which pins full SHAs and fails if any of them stops resolving.

| Checked | Input | Value | Source |
|---|---|---|---|
| 2026-11-15 | stellar-core f2b9194 (protocol 30 goldens) | `f2b91947fb20fc8174cca38bf320eda7baa3edbb`, committed 2026-11-12, "Fix flaky unauthenticated peer signature verification test (#5500)" | https://github.com/stellar/stellar-core/commit/f2b91947fb20fc8174cca38bf320eda7baa3edbb |
| 2026-11-15 | f2b9194 `test-lcm-current/` | 866 `.xdr` files in 10 directories; every `index.json` has `"!cfg protocol version": 30` | fetched tree |
| 2026-11-15 | stellar-core abe1c55 (protocol 28 goldens) | `abe1c5541827caaafe023954c224cadeb5dddc44`, committed 2026-09-22, "Regenerate and prune the captured LedgerCloseMeta golden data" | https://github.com/stellar/stellar-core/commit/abe1c5541827caaafe023954c224cadeb5dddc44 |
| 2026-11-15 | abe1c55 `test-lcm-current/` | 844 `.xdr` files in 8 directories; every `index.json` has `"!cfg protocol version": 28` | fetched tree |
| 2026-11-15 | stellar-xdr tag `v30.0` | lightweight tag on `4f524bbac80c781c06e4fb5fc93a2d0d331d6705`; `.x` files at the repository root | https://github.com/stellar/stellar-xdr/tree/v30.0 |
| 2026-11-15 | wallet-backend 2a0a84e (before the #729 fix) | `2a0a84ea12523b6c92e278c0c84b443041240095`, committed 2026-09-04, "ci: make deploying a dev image to shared dev opt-in (#725)" | https://github.com/stellar/wallet-backend/commit/2a0a84ea12523b6c92e278c0c84b443041240095 |
| 2026-11-15 | wallet-backend 2c87335 (the #729 fix) | `2c873353c1f0f3c9b145e25a7d009023ad104a1c`, committed 2026-09-08, "fix: handle CAP-40 signed-payload signers in ingestion (#729)" | https://github.com/stellar/wallet-backend/commit/2c873353c1f0f3c9b145e25a7d009023ad104a1c |
| 2026-11-16 | wallet-backend 79ea2ac (before #716's silent truncation was stopped) | `79ea2acd1dc65a07166133cc04b12e76db314db0`, committed 2026-08-27, "build(deps): upgrade to protocol 28 (go-stellar-sdk v0.7.3, core/rpc 28) (#715)"; parent of 6e5a591 | https://github.com/stellar/wallet-backend/commit/79ea2acd1dc65a07166133cc04b12e76db314db0 |
| 2026-11-15 | wallet-backend 3e22515 (HEAD) | `3e22515a44979fdc3a558fda3991ca2b48e38cef`, committed 2026-11-12; still the default-branch HEAD on 2026-11-15 | https://github.com/stellar/wallet-backend/commit/3e22515a44979fdc3a558fda3991ca2b48e38cef |

## wallet-backend internals the harness depends on

Read from the source at each pinned commit (`git show <sha>:<path>` in the fetched clone).

| Checked | Fact | Value | Source |
|---|---|---|---|
| 2026-11-15 | `NewIndexer` at 2a0a84e and 2c87335 | `NewIndexer(networkPassphrase string, pool pond.Pool, ingestionMetrics *metrics.IngestionMetrics) (*Indexer, error)` | `internal/indexer/indexer.go:119` |
| 2026-11-15 | `NewIndexer` at 3e22515 | adds `collectContractData bool` as a fourth parameter | `internal/indexer/indexer.go:113` |
| 2026-11-15 | What production passes for `collectContractData` | true only in live ingestion mode when a protocol processor requires contract data; the harness passes true to exercise that path | `internal/services/ingest.go:248` (`collectsContractData`) at 3e22515 |
| 2026-11-15 | `ProcessLedger` at all three commits | `ProcessLedger(ctx, networkPassphrase string, ledgerMeta xdr.LedgerCloseMeta, ledgerIndexer *Indexer, buffer *IndexerBuffer) (int, error)` | `internal/indexer/indexer.go` |
| 2026-11-15 | Every transaction reaches the buffer unless processing returns an error (so "indexed < in meta" means dropped) | `ProcessLedgerTransactions` folds every non-nil result into the buffer; a nil result only occurs with an error, which aborts the ledger | `internal/indexer/indexer.go:168` at 3e22515 |
| 2026-11-15 | The #729 fix changed the signer column's encoder | `StateChange.SignerAccountID` is `NullAddressBytea` at 2a0a84e and `NullSignerKeyBytea` at 2c87335 | `internal/indexer/types/types.go` |
| 2026-11-15 | go-stellar-sdk used by wallet-backend | v0.7.3 at 2a0a84e and 2c87335; v0.8.0 at 3e22515 (all `go 1.25.9`) | `go.mod` |

## Decoder error wording the classifier matches

An "unsupported-protocol" verdict rests on recognising these messages; each was observed, not recalled.

| Checked | Decoder | Message | How observed |
|---|---|---|---|
| 2026-11-15 | go-stellar-sdk v0.7.3, protocol 30 golden | `decoding StellarValueType: '3' is not a valid StellarValueType enum value` | adapter at wallet-backend 2a0a84e |
| 2026-11-15 | go-stellar-sdk v0.9.0, unknown enum | `'9' is not a valid SignerKeyType enum value` | `xdr.SafeUnmarshal` (decoded live in `verdict_test.go`) |
| 2026-11-15 | go-stellar-sdk v0.9.0, unknown int-switched union arm | `union LedgerCloseMeta has invalid V (int32) switch value '7'` | `xdr.SafeUnmarshal` (decoded live in `verdict_test.go`) |
| 2026-11-15 | js-stellar-sdk 17.4.0, unknown union arm | `LedgerCloseMeta: no case for union discriminator -1` | `js/lcm_rt.js` on a one-frame file |
| 2026-11-15 | Name of StellarValueType 3 | `STELLAR_VALUE_SIGNED_MS` | `Stellar-ledger.x` at stellar-xdr v30.0 |

## Mutation rules

| Checked | Fact | Value | Source |
|---|---|---|---|
| 2026-11-15 | Order of an account's signers | kept sorted by `SignerKey operator<`: a new signer is inserted at the first position where `!(x.key < key)`, and `signerSponsoringIDs` gets an element at the same index | `findSignerByKey` in `src/transactions/TransactionUtils.h:41` and `addOrChangeSigner` in `src/transactions/SetOptionsOpFrame.cpp:70` at abe1c55 |
| 2026-11-15 | Strkey payload of a signed-payload signer (n payload bytes) | 32 + 4 + n rounded up to a multiple of 4: n=1 -> 40, n=32 -> 68, n=64 -> 100 | `TestSignedPayloadStrkeyLengths` (go-stellar-sdk v0.9.0) |
| 2026-11-15 | Empty signed payload | XDR allows it (`opaque payload<64>`), but go-stellar-sdk's strkey decoder rejects it: "signed payload declares 0 payload bytes, must be between 1 and 64" | `TestSignedPayloadStrkeyLengths` |
| 2026-11-15 | Strkey payload of a muxed account | 40 bytes (32-byte key + 8-byte id) | rule 2 sidecar (`strkey.Decode`) |
| 2026-11-15 | Coverage atom names for integer boundaries | named after the typedef, not the field: a muxed id above 2^53 is `uint64#2^53+1` | `python/xdr_coverage.py` on the rule 2 output |
| 2026-11-15 | Atoms absent from the protocol 30 goldens that the mutations add | `MuxedAccount\|KEY_TYPE_MUXED_ED25519`, `SignerKey\|SIGNER_KEY_TYPE_ED25519_SIGNED_PAYLOAD` and its `payload<nonempty>` / `payload<max:64>` | `results/coverage_test-lcm-current.json` (missing list) |

## wallet-backend#716

| Checked | Fact | Value | Source |
|---|---|---|---|
| 2026-11-16 | What #716 reports | `AddressBytea.Value()` keeps the first 32 bytes of any strkey payload without error, so muxed (M, 40-byte) and signed-payload (P, 36+ byte) addresses are stored truncated and read back undecodable; three producers: Soroban op participants from a muxed op source, SEP-41 muxed ScAddress, signed-payload signers | https://github.com/stellar/wallet-backend/issues/716 (opened 2026-08-27) |
| 2026-11-16 | How #716 was closed | closed 2026-09-18 with no linked commit or PR in its timeline | `gh api repos/stellar/wallet-backend/issues/716/timeline` |
| 2026-11-16 | Commit that ended the silent truncation | 6e5a591 "fix(indexer): reduce muxed addresses to their base account (#693)", 2026-08-27: M is reduced to its G account (documented as intentional), any other non-32-byte payload is rejected with "has a %d-byte key payload" | `git log -S'stores only 32-byte'` and `git diff 79ea2ac 6e5a591 -- internal/indexer/types/types.go` |
| 2026-11-16 | Truncating encoder at 79ea2ac | `AddressBytea.Value`: `strkey.DecodeAny`, then `copy(result[1:], rawBytes)` into 33 bytes | `internal/indexer/types/types.go` at 79ea2ac |
| 2026-11-16 | `strkey.DecodeAny` checks the payload length per version byte | yes, in go-stellar-sdk v0.7.3 and v0.8.0 (`checkPayloadLength`) | `strkey/main.go` in the module cache |

## Protocol constants

| Checked | Fact | Value | Source |
|---|---|---|---|
| 2026-11-15 | Network passphrase used by stellar-core's tests (and so by the goldens' transaction hashes) | `(V) (;,,;) (V)` | `src/test/test.cpp` line 848 at f2b9194, line 842 at abe1c55 |

## Toolchain and libraries

| Checked | Fact | Value | Source |
|---|---|---|---|
| 2026-11-15 | Latest stable Go | go1.27.4 (released 2026-11-02) | https://go.dev/VERSION?m=text |
| 2026-11-15 | go-stellar-sdk used by `go/` | v0.9.0 | `go/go.mod` |
| 2026-11-16 | rs-stellar-xdr used by `rust/verify` | stellar-xdr 30.0.0, the newest release on crates.io (2026-10-08); decodes all protocol 30 goldens; `type_enum` feature for decoding by type name | https://crates.io/api/v1/crates/stellar-xdr, `Cargo.lock` |
| 2026-11-16 | JSON Schema validator | jsonschema 0.61.2 (crates.io, released 2026-11-12, actively maintained), `validator_for` + `iter_errors`, default features off so no HTTP/file `$ref` resolvers are compiled in | https://crates.io/api/v1/crates/jsonschema, `src/lib.rs:1514` of the crate |
| 2026-11-16 | rs-stellar-xdr type names vs .x names | Rust casing (`ScVal` for `SCVal`); all 478 names distinct ignoring ASCII case | `TypeVariant::VARIANTS_STR`, test `xdr_names_resolve_without_case_collisions` |
| 2026-11-16 | CI Go | 1.27.4 (latest stable) | https://go.dev/VERSION?m=text |
| 2026-11-16 | CI Rust | 1.100.0, stable channel, released 2026-11-09 | https://static.rust-lang.org/dist/channel-rust-stable.toml |
| 2026-11-16 | CI Node.js | 26.13.1 (newest non-prerelease, an LTS release, 2026-11-10) | https://nodejs.org/dist/index.json |
| 2026-11-16 | CI Python | 3.15.2 (latest stable, 2026-10-25) | https://endoflife.date/api/python.json |
| 2026-11-16 | GitHub Actions used | actions/checkout v7, setup-go v7, setup-node v7, setup-python v7, cache v6 (latest releases) | `gh api repos/<action>/releases/latest` |
| 2026-11-16 | CI runner | `ubuntu-24.04`, which `ubuntu-latest` points to | https://github.com/actions/runner-images (README) |
| 2026-11-15 | js-stellar-sdk used by `js/` | @stellar/stellar-sdk 17.4.0, npm `latest` tag, published 2026-10-25 | `npm view @stellar/stellar-sdk dist-tags time` |

## Measured baselines

Regenerated by `make`; listed here because later checks compare against them.

| Checked | Measurement | Value | Command |
|---|---|---|---|
| 2026-11-16 | Peak memory of hamming-verify over the f2b9194 goldens | about 50 MB RSS on macOS (streamed frame by frame; printed, not committed, because it varies by machine) | `make verify` |
| 2026-11-15 | f2b9194 goldens decoded and re-encoded byte-for-byte | 15,306 LedgerCloseMeta, 77.9 MB, 0 mismatches in each of rs-stellar-xdr, go-stellar-sdk and js-stellar-sdk | `make verify` |
| 2026-11-15 | Atom coverage of the f2b9194 goldens | 459 of 899 atoms, 0 decode errors | `make coverage` |
| 2026-11-15 | Greedy golden-file cover (ties broken by path) | 51 of 866 files; lower bound 23 | `make coverage` |
| 2026-11-16 | Corpus v0 | 56 entries (51 core goldens referenced by commit + path + sha256, 5 synthetic mutations), 463 atoms covered (the goldens' 459 plus 4 only the mutations reach), schema 0.2.0 valid; hamming-verify round-trips all 903 values with 0 sha256 mismatches | `make corpus` |
| 2026-11-15 | wallet-backend golden replay at 3e22515 | 866 files, 15,306 ledgers, 13,107 transactions, 13,183 operations: all pass (0 decode errors, 0 indexer errors, 0 panics, 0 persist errors, 0 crashes) | `make harness` |
| 2026-11-15 | wallet-backend#729 entry | 2a0a84e mishandled(persist) (68-byte SignerAccountID payload), 2c87335 pass, 3e22515 pass | `make harness` |
| 2026-11-16 | wallet-backend#716 (signed-payload producer) | rule 1, 3 and 4 entries at 79ea2ac: mishandled(persist), "stored lossily and reads back as P..., which does not decode: invalid signed payload length 32, expected at least 36"; with the pre-read-back adapter the same entries pass | `make harness` |
| 2026-11-16 | Rule 2 (muxed payment destination) against #716 | pass at 79ea2ac, 2a0a84e, 2c87335 and 3e22515 with no lossy or normalized value: does not reproduce | `make harness` |
