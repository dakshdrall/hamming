# Corpus manifest format

A manifest lists corpus entries and says, for each, what it is, where its bytes are, how to check them and
where they came from. The schema is [`corpus/manifest.schema.json`](../corpus/manifest.schema.json) (JSON
Schema draft 2020-12). `hamming-verify <manifest.json>` validates a manifest against it, checks every
entry's sha256, and round-trips every value.

## Document

| Field | Required | Meaning |
|---|---|---|
| `schema_version` | yes | `0.2.0` |
| `network_passphrase` | yes | Passphrase transaction hashes were computed with. An entry's own `network_passphrase` overrides it. |
| `entries` | yes | The entries. |
| `schema`, `description`, `generated_by` | no | Schema URL, free text, and the command that regenerates the manifest. |

## Entry

| Field | Required | Meaning |
|---|---|---|
| `id` | yes | `level.family.pattern.serial` (e.g. `str.cap40-signed-payload-signer.p28.0001`), or `core.<test directory>.<golden file stem>` for core goldens. Stable forever, never reused. |
| `level` | yes | `structural` (a grammar atom), `value` (a boundary value inside a valid shape) or `relational` (a property across fields). |
| `xdr_type` | yes | Type to decode as, spelled as in the `.x` files (`LedgerCloseMeta`, `SCVal`, ...). |
| `encoding` | yes | `base64` (one value as base64 text), `raw` (one value as bytes) or `xdr-framed` (record-marked values: 4-byte big-endian length, high bit set). |
| `sha256` | yes | `base64`: hash of the decoded bytes. `raw` and `xdr-framed`: hash of the file as stored, record marks included. |
| `protocol_min` | yes | Lowest protocol the entry is valid for. |
| `source` | yes | Provenance, see below. |
| `synthetic` | yes | `true` unless the bytes were emitted by stellar-core. |
| `atoms` | yes | Shape atoms covered (atom IDs from `python/xdr_coverage.py`; value and relational atoms may use `val:` and `rel:` prefixes). |
| `file` | yes, except core goldens | Path relative to the manifest. Core goldens must not have one. |
| `caps` | yes, except core goldens | CAPs the entry exercises, `CAP-NNNN`. |
| `expected` | yes, except core goldens | Path -> expected decoded value. |
| `title`, `bytes`, `frames`, `protocol_max`, `network_passphrase`, `sdk_rules`, `sightings` | no | As named; `frames` counts the values in an `xdr-framed` entry. |

## Source

| `kind` | Meaning | Required fields |
|---|---|---|
| `core-golden` | A stellar-core golden file, referenced by `repo` + `commit` + `path` and checked by the entry's `sha256`. Not copied into the repository: `hamming-verify` reads it from `$HAMMING_CACHE/core-<commit7>/<path>`, as fetched by `scripts/fetch-inputs.sh`. Must be `synthetic: false` and `xdr-framed`. | `repo`, `commit`, `path`, `core_test` |
| `local-core` | Captured from a local stellar-core network. | `image`, `core_version`, `protocol`, `network_passphrase`, `tx_hashes` |
| `synthetic` | Built by a tool. Mutations of core goldens record the golden in `derived_from` (`repo`, `commit`, `path`, `sha256`) and the command in `reproduce`. | none |
| `mainnet`, `testnet` | Cut from a public network. | none |

## Changes from 0.1.0

- New source kinds `core-golden` and `local-core`, with the fields above. `synthetic` sources gain
  `derived_from`.
- `file`, `caps` and `expected` are optional for core goldens (and forbidden: `file`), required for every
  other entry as before.
- New encoding `xdr-framed`, and `frames`, for record-marked files such as golden and captured
  LedgerCloseMeta streams.
- Entry-level `network_passphrase`, because one corpus mixes stellar-core's test passphrase (goldens) with
  local and public networks.
- Ids may also be `core.<test directory>.<file stem>`.
- Unknown top-level fields and unknown source fields are rejected.
- The schema moved from `corpus/samples/` to `corpus/`, shared by every manifest. The samples manifest
  validates unchanged except for its `schema_version`.
