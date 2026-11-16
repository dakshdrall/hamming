use std::fs;
use std::path::{Path, PathBuf};

use hamming_corpus::*;
use serde_json::Value;

fn repo() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../..")
}

fn json(p: &str) -> Value {
    serde_json::from_str(&fs::read_to_string(repo().join(p)).unwrap()).unwrap()
}

fn mutated() -> Vec<Mutated> {
    let dir = repo().join("corpus/mutated");
    let mut sidecars: Vec<PathBuf> = fs::read_dir(&dir)
        .unwrap()
        .map(|e| e.unwrap().path())
        .filter(|p| {
            p.extension().is_some_and(|x| x == "json")
                && !p.file_name().unwrap().to_string_lossy().starts_with('.')
        })
        .collect();
    sidecars.sort();
    sidecars
        .iter()
        .map(|sc| Mutated {
            file: format!(
                "../mutated/{}",
                sc.with_extension("xdr")
                    .file_name()
                    .unwrap()
                    .to_string_lossy()
            ),
            sidecar: serde_json::from_str(&fs::read_to_string(sc).unwrap()).unwrap(),
            bytes: fs::read(sc.with_extension("xdr")).unwrap(),
        })
        .collect()
}

fn cover() -> Vec<String> {
    serde_json::from_value(json("results/golden_cover.json")).unwrap()
}

#[test]
fn v0_manifest_is_valid_and_keeps_every_atom() {
    let gm = json("results/core_golden_manifest.json");
    let m = build(&gm, &cover(), &mutated()).unwrap();
    let errors = hamming_verify::schema_errors(&json("corpus/manifest.schema.json"), &m).unwrap();
    assert!(errors.is_empty(), "{errors:?}");

    let entries = m["entries"].as_array().unwrap();
    let golden: Vec<_> = entries
        .iter()
        .filter(|e| e["source"]["kind"] == "core-golden")
        .collect();
    assert_eq!(golden.len(), cover().len());
    assert!(
        golden
            .iter()
            .all(|e| e.get("file").is_none() && e["synthetic"] == false),
        "goldens must be referenced, not copied"
    );
    assert!(entries
        .iter()
        .filter(|e| e["synthetic"] == true)
        .all(|e| e["source"]["derived_from"]["commit"].is_string()));

    // the cover keeps every atom the goldens cover, and the mutations only add
    let atoms = covered_atoms(&m);
    assert!(atoms.len() >= gm["atoms_covered"].as_u64().unwrap() as usize);
    assert!(atoms.len() >= 459);
    assert!(atoms.contains("SignerKey|SIGNER_KEY_TYPE_ED25519_SIGNED_PAYLOAD"));
}

#[test]
fn large_integers_in_expected_values_are_strings() {
    let m = build(
        &json("results/core_golden_manifest.json"),
        &cover(),
        &mutated(),
    )
    .unwrap();
    let muxed = m["entries"]
        .as_array()
        .unwrap()
        .iter()
        .find(|e| e["id"] == "str.muxed-payment-destination.p28.0001")
        .unwrap();
    assert_eq!(muxed["expected"]["derived.muxedId"], "9007199254740993"); // 2^53 + 1
    assert_eq!(muxed["expected"]["derived.strkeyPayloadBytes"], 40);
}

#[test]
fn a_tampered_mutation_or_stale_cover_is_rejected() {
    let mut muts = mutated();
    muts[0].bytes[10] ^= 0xff;
    let err = mutated_entry(&muts[0]).unwrap_err();
    assert!(err.contains("does not match its sidecar"), "{err}");

    let mut c = cover();
    c.push("NoSuchTests/0000000000000000.xdr".into());
    assert!(build(&json("results/core_golden_manifest.json"), &c, &mutated()).is_err());
}
