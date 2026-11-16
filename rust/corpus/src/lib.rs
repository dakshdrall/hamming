//! Builds the corpus v0 manifest: the greedy golden cover (core goldens referenced by commit, path and sha256,
//! never copied) plus every synthetic mutation in corpus/mutated, described by its sidecar.

use std::collections::BTreeSet;

use serde_json::{json, Map, Value};
use sha2::{Digest, Sha256};

/// stellar-core's test network passphrase, which the goldens and the mutations derived from them use.
pub const CORE_TEST_PASSPHRASE: &str = "(V) (;,,;) (V)";

/// A mutated entry as found on disk: its path relative to the manifest, its sidecar and its bytes.
pub struct Mutated {
    pub file: String,
    pub sidecar: Value,
    pub bytes: Vec<u8>,
}

fn str_field<'a>(v: &'a Value, key: &str, what: &str) -> Result<&'a str, String> {
    v[key]
        .as_str()
        .ok_or_else(|| format!("{what}: missing {key}"))
}

/// One core-golden entry per cover file, from results/core_golden_manifest.json's rows.
pub fn golden_entries(golden_manifest: &Value, cover: &[String]) -> Result<Vec<Value>, String> {
    let rows = golden_manifest["entries_index"]
        .as_array()
        .ok_or("golden manifest has no entries_index")?;
    let mut out = Vec::new();
    for c in cover {
        let row = rows
            .iter()
            .find(|r| {
                r["source"]["path"]
                    .as_str()
                    .is_some_and(|p| p.ends_with(&format!("/{c}")))
            })
            .ok_or_else(|| format!("cover file {c} has no row in the golden manifest"))?;
        let atoms = row["atom_list"].as_array().ok_or_else(|| {
            format!("{c}: no atom_list (run golden_manifest.py with the cover file)")
        })?;
        let core_test = row["core_test"].clone();
        let title = match core_test.as_str() {
            Some(t) => format!("stellar-core golden: {t}"),
            None => format!("stellar-core golden {c}"),
        };
        out.push(json!({
            "id": str_field(row, "id", c)?,
            "title": title,
            "level": "structural",
            "xdr_type": "LedgerCloseMeta",
            "encoding": "xdr-framed",
            "sha256": str_field(row, "sha256", c)?,
            "frames": row["ledger_close_metas"],
            "protocol_min": row["protocol"],
            "source": {
                "kind": "core-golden",
                "repo": str_field(&row["source"], "repo", c)?,
                "commit": str_field(&row["source"], "commit", c)?,
                "path": str_field(&row["source"], "path", c)?,
                "core_test": core_test,
            },
            "synthetic": false,
            "atoms": atoms,
        }));
    }
    out.sort_by(|a, b| a["id"].as_str().cmp(&b["id"].as_str()));
    Ok(out)
}

fn camel(snake: &str) -> String {
    let mut out = String::new();
    let mut up = false;
    for ch in snake.chars() {
        if ch == '_' {
            up = true;
        } else if up {
            out.extend(ch.to_uppercase());
            up = false;
        } else {
            out.push(ch);
        }
    }
    out
}

/// Expected values follow the manifest convention: integers beyond 2^53 are decimal strings.
fn json_safe(v: &Value) -> Value {
    const MAX_SAFE: u64 = 1 << 53;
    match v {
        Value::Number(n) if n.as_u64().is_some_and(|u| u > MAX_SAFE) => {
            Value::String(n.to_string())
        }
        Value::Number(n) if n.as_i64().is_some_and(|i| i.unsigned_abs() > MAX_SAFE) => {
            Value::String(n.to_string())
        }
        other => other.clone(),
    }
}

/// The manifest entry for one mutated file. Fails if the file's sha256 differs from the sidecar's.
pub fn mutated_entry(m: &Mutated) -> Result<Value, String> {
    let s = &m.sidecar;
    let id = str_field(s, "id", &m.file)?;
    let sum = hamming_verify::hex(&Sha256::digest(&m.bytes));
    if s["sha256"].as_str() != Some(sum.as_str()) {
        return Err(format!(
            "{}: file sha256 {sum} does not match its sidecar ({})",
            m.file, s["sha256"]
        ));
    }
    if s["synthetic"] != true {
        return Err(format!("{id}: sidecar is not marked synthetic"));
    }
    let level = str_field(s, "level", id)?;
    let prefix = match level {
        "structural" => "str",
        "value" => "val",
        "relational" => "rel",
        other => return Err(format!("{id}: unknown level {other}")),
    };
    let rule = str_field(s, "rule", id)?;
    let protocol = s["protocol"]
        .as_u64()
        .ok_or_else(|| format!("{id}: missing protocol"))?;
    let mut expected = Map::new();
    expected.insert("derived.txHash".into(), s["new_tx_hash"].clone());
    if let Some(details) = s["details"].as_object() {
        for (k, v) in details {
            expected.insert(format!("derived.{}", camel(k)), json_safe(v));
        }
    }
    let src = &s["source"];
    Ok(json!({
        "id": format!("{prefix}.{rule}.p{protocol}.0001"),
        "title": s["summary"],
        "level": level,
        "xdr_type": "LedgerCloseMeta",
        "file": m.file,
        "encoding": "xdr-framed",
        "sha256": sum,
        "bytes": m.bytes.len(),
        "frames": 1,
        "caps": s["caps"],
        "protocol_min": protocol,
        "network_passphrase": s["network_passphrase"],
        "source": {
            "kind": "synthetic",
            "generator": format!("hamming-mutate rule {} ({rule})", s["rule_number"]),
            "reproduce": "make mutated",
            "derived_from": {"repo": src["repo"], "commit": src["commit"], "path": src["path"], "sha256": src["sha256"]},
        },
        "synthetic": true,
        "atoms": s["atoms"],
        "expected": expected,
    }))
}

/// The whole manifest, entries in a stable order (core goldens by id, then mutations by id).
pub fn build(
    golden_manifest: &Value,
    cover: &[String],
    mutated: &[Mutated],
) -> Result<Value, String> {
    let mut entries = golden_entries(golden_manifest, cover)?;
    let mut muts = mutated
        .iter()
        .map(mutated_entry)
        .collect::<Result<Vec<_>, _>>()?;
    muts.sort_by(|a, b| a["id"].as_str().cmp(&b["id"].as_str()));
    entries.extend(muts);
    Ok(json!({
        "schema_version": "0.2.0",
        "description": "Hamming corpus v0: the greedy cover of stellar-core's protocol 30 goldens, referenced by commit, path and sha256, plus synthetic mutations of protocol 28 goldens.",
        "generated_by": "make corpus",
        "network_passphrase": CORE_TEST_PASSPHRASE,
        "entries": entries,
    }))
}

/// Every atom some entry covers.
pub fn covered_atoms(manifest: &Value) -> BTreeSet<String> {
    manifest["entries"]
        .as_array()
        .into_iter()
        .flatten()
        .flat_map(|e| e["atoms"].as_array().into_iter().flatten())
        .filter_map(|a| a.as_str().map(String::from))
        .collect()
}
