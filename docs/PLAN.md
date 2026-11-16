# Hamming pre-submission prototype: what to build by Nov 18

**Build a working harness, not a demo.** It should run Hamming's corpus against real software and reproduce
the RFP's own acceptance tests before you submit. The riskiest piece is already proven:
**wallet-backend#729 reproduces on its pre-fix commit and passes on the fix and on HEAD**, using an in-process
adapter with no database. What's left is turning one-off test files into a tool, adding two more
reproductions, and replacing synthetic entries with captures from a local core.

All numbers below were measured on 2026-11-15 on a 2 vCPU / 7 GB machine (`results/measurements.json`).

---

## 1. What the prototype has to prove to a reviewer

| Claim | Evidence the reviewer can run | Status |
|---|---|---|
| The harness finds real bugs in a real indexer | wallet-backend#729: pre-fix 2a0a84e **mishandled**, fix 2c87335 **pass**, HEAD 3e22515 **pass** | Done (test files) |
| Decoders agree on core-emitted data | 4 decoders × 15,306 core LedgerCloseMeta, 0 byte mismatches | Done |
| Coverage is measured, not claimed | 459 of 899 atoms in core's goldens; 51 files carry them all | Done |
| It runs in CI on every PR | Full pipeline < 30 s on 2 vCPU | Measured; CI not yet wired |
| More acceptance tests | wallet-backend#716, #731 on pre-fix commits | To do |
| Entries come from core, not hand edits | Local stellar-core captures replace the mutated entries | To do (needs Docker on a team machine) |

---

## 2. The maths and physics (all measured)

**Throughput and memory, over 866 files / 15,306 LCMs / 77.9 MB:**

| Decoder | Time | LCM/s | MB/s | Byte mismatches |
|---|---|---|---|---|
| rs-stellar-xdr 30.0.0 (release) | 0.52 s | 29,277 | 149.0 | 0 (peak RSS 36 MB) |
| go-stellar-sdk v0.9.0 | 1.31 s | 11,682 | 59.5 | 0 |
| js-stellar-sdk 17.4.0 | 5.40 s | 2,835 | 14.4 | 0 (RSS 384 MB) |
| Grammar-driven Python decoder (with atom tracking) | 15.9 s | ~960 | — | 0 decode errors |
| wallet-backend indexer, in-process (HEAD) | 3.4 s | ~4,500 | — | 0 errors, 0 panics |

- **CI budget:** all four decoders plus the wallet-backend replay take about 27 s, so the RFP's "fast enough
  for every pull request" requirement is met with a large margin.
- **Corpus growth:** the average LCM is 77.9 MB ÷ 15,306 ≈ 5.1 KB. A corpus 10× this size still decodes in
  about 5 s in Rust.
- **Memory:** entries are streamed one frame at a time, so peak memory is bounded by the largest frame (Rust
  peaks at 36 MB), not by corpus size. That is the RFP's "no unbounded memory growth" requirement.

**Set cover.** 51 of the 866 golden files carry all 459 covered atoms, a 17× reduction. The optimum lies
between 23 (atoms found in only one file force that file in) and 51 (greedy result; greedy is within
H(220) = 5.97× of optimal).

**Storage widths, derived from the XDR, for the guide.** Strkey payload bytes; add 1 version byte when
stored, as wallet-backend does.

| Address | XDR layout | Strkey payload | Stored with version byte |
|---|---|---|---|
| G account | uint256 | 32 | 33 |
| C contract | ContractID (32) | 32 | 33 |
| M muxed account | **(id u64, ed25519)** | **ed25519 ‖ id = 40** | 41 |
| W muxed contract (CAP-84) | **(id u64, contractId)** | **contractId ‖ id = 40** | 41 |
| P signed payload (CAP-40) | ed25519 ‖ opaque<64> | 32 + 4 + pad₄(n), 1 ≤ n ≤ 64 → **40…100** | **41…101** |

- **P range.** pad₄(n) rounds n up to a multiple of 4, so n = 1 gives 40 bytes, not 36. XDR allows an empty
  payload, but go-stellar-sdk's strkey decoder rejects it ("must be between 1 and 64"), so 36 bytes is never
  a valid strkey (`TestSignedPayloadStrkeyLengths` in go/mutate).
- **P check.** A 32-byte payload gives 68 bytes, the exact figure in the pre-fix error. A 64-byte payload gives
  101 stored bytes, the exact bound in wallet-backend's fix commit ("up to 101 bytes for P... keys").
- **Field-order trap.** In XDR the id comes **first**; in the strkey it comes **last** (CAP-84, SEP-23). Any
  code that copies XDR bytes straight into a strkey payload is wrong. This becomes a relational atom and a
  linter rule.

---

## 3. What we build (and what's ours)

### Components

| Component | Language | Owner | State |
|---|---|---|---|
| `hamming-verify`: N-decoder round-trip and agreement, manifest and SHA-256 checks | Rust | Priya | Round-trip core done; add manifest/schema checks and a JSON report |
| `hamming-mutate`: rule-based golden mutation with re-hash | Go | Daksh | Rule 1 done (ed25519 → signed-payload signer); add rules 2–4 |
| `harness`: adapter interface, protocol routing, pass/mishandled/dropped report | Go | Daksh | wallet-backend adapter works as test files; extract it into a CLI |
| `corpus v0`: 51 core-golden entries (by commit + SHA-256) + mutated entries + samples | Data + Rust builder | Priya | Selection done (`results/golden_cover.json`); build manifest rows |
| Coverage meter + set cover | Python (Rust in M1) | Priya | Done |
| Conformance runner (JS/Go/Rust/Python) | Mixed | Daksh | Done (evidence bundle); wire into CI |
| CI regression oracle | GitHub Actions | Daksh | To do |

### Our own innovations, each now demonstrated

1. **Golden mutation with re-hash.** Take a core-emitted ledger, rewrite one structure to the rare shape in
   both the envelope and the meta, and recompute the transaction hash
   (`SHA-256(networkID ‖ ENVELOPE_TYPE_TX ‖ tx)`) so the meta still matches its envelope. This turns a missing
   atom into a test case in minutes instead of a scenario script. *What's preserved:* XDR validity (it
   re-decodes) and envelope↔result hash agreement. *What isn't:* signatures and the header's `txSetHash`.
   Every mutated entry is flagged synthetic and later replaced by a local-core capture.
2. **Persistence-boundary emulation.** #729 didn't fail in indexing; it failed when the row was encoded for
   the database. The adapter calls every `driver.Valuer` on every buffered state change, the same encoders the
   COPY uses, so storage-boundary bugs show up in CI without Postgres.
3. **Protocol-aware routing.** Today's P30 goldens (StellarValueType 3, CAP-88) don't even decode on the
   September wallet-backend SDK. The harness tags entries by protocol and reports "unsupported protocol"
   separately from failures, which also gives every target a forward-compatibility signal.
4. **Four-decoder agreement over core's own data.** Expected values only count when independent
   implementations agree. That's 15,306/15,306 today.
5. **Length-bound arithmetic → column-width table.** The guide derives storage bounds from the XDR (table
   above). These are exactly the numbers that broke wallet-backend.
6. **Relational atoms confirmed by real fix history.** "G and P signers over the same key" is in our
   taxonomy, and it's the exact case wallet-backend's #729 fix had to handle in a follow-up commit.

### Mutation rules to add (in priority order)

| # | Rule | Targets |
|---|---|---|
| 2 | G → M on a payment destination / tx source (40-byte payload) | wallet-backend#716 class, js#1785 class |
| 3 | P payload length 1 and 64 (40- and 100-byte strkey payloads) | Max-length atoms, the 101-byte bound |
| 4 | Add a G signer and a P signer over the same key in one account | The relational case from the #729 follow-up |
| 5 | SAC transfer creating a trustline (look for an existing CAP-73 golden first) | wallet-backend#731 |

---

## 4. Plan by person (Nov 15 evening → Nov 18)

About 45 hours in total across two people, within three days.

| When | Daksh (Go) | Hours | Priya (Rust) | Hours |
|---|---|---|---|---|
| Nov 16 | Extract the adapter into `harness/` + CLI; protocol routing; JSON report | 7 | `hamming-verify` CLI: manifest + schema + SHA-256 + round-trip report | 6 |
| Nov 16–17 | Mutation rules 2–4; attempt #716 reproduction on its pre-fix commit | 8 | Corpus v0 builder: rows from `golden_cover.json` with provenance and atoms | 5 |
| Nov 17 | #731: find a CAP-73 golden or mutation; reproduce | 5 | Local stellar-core (Docker quickstart): capture a signed-payload signer and a muxed payment to replace the mutated entries | 7 |
| Nov 17 | CI: verify + coverage + conformance + harness, with pinned wallet-backend commits (pre-fix must FAIL, fix must PASS) | 4 | README results table, reproduce commands | 2 |
| Nov 18 AM | Both: run everything from a clean clone, update the proposal's evidence section, submit | 2 | | 2 |

**Cut line if time runs short:** drop #731 and the Rust manifest builder first. Never drop the CI regression
oracle or the local-core capture. Those two are what make this executable rather than a show.

---

## 5. What reviewers run

```bash
make verify     # 4 decoders × 15,306 core ledgers → 0 mismatches
make coverage   # 459 / 899 atoms; 51-file cover
make harness    # wallet-backend: 2a0a84e mishandled (#729), 2c87335 pass, HEAD pass
make conform    # SDK divergence matrix
```

---

## 6. Honest limits (state these in the proposal)

- Mutated entries are synthetic until the local-core capture replaces them, and the manifest says so.
- Persistence is emulated with wallet-backend's own encoders, not a live Postgres. M2 adds a Postgres service
  container in CI.
- The adapter runs inside wallet-backend's `internal/indexer` package (Go's internal visibility), so CI drops
  the test file into a pinned checkout. That isn't a fork.
- No Docker daemon was available here, so local-core capture has to run on a team machine.
