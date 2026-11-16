use std::collections::HashSet;
use std::fs;
use std::io::Cursor;
use std::path::{Path, PathBuf};

use hamming_verify::*;
use serde_json::Value;
use stellar_xdr::TypeVariant;

fn repo() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../..")
}

fn scratch(name: &str) -> PathBuf {
    let d = std::env::temp_dir().join(format!("hamming-verify-test-{}-{name}", std::process::id()));
    let _ = fs::remove_dir_all(&d);
    fs::create_dir_all(&d).unwrap();
    d
}

#[test]
fn frames_stream_and_reject_truncation() {
    let mut two = vec![0x80, 0, 0, 2, 0xaa, 0xbb, 0x80, 0, 0, 1, 0xcc];
    let mut f = Frames::new(Cursor::new(two.clone()));
    assert_eq!(f.next_frame().unwrap(), Some(&[0xaa, 0xbb][..]));
    assert_eq!(f.next_frame().unwrap(), Some(&[0xcc][..]));
    assert_eq!(f.next_frame().unwrap(), None);

    two.truncate(10); // second record mark intact, its 1-byte payload missing
    let mut f = Frames::new(Cursor::new(two));
    f.next_frame().unwrap();
    assert!(f
        .next_frame()
        .unwrap_err()
        .to_string()
        .contains("truncated frame"));

    let mut f = Frames::new(Cursor::new(vec![0x80, 0]));
    assert!(f
        .next_frame()
        .unwrap_err()
        .to_string()
        .contains("truncated record mark"));

    let mut f = Frames::new(Cursor::new(vec![0, 0, 0, 1, 0]));
    assert!(f
        .next_frame()
        .unwrap_err()
        .to_string()
        .contains("not a last fragment"));
}

#[test]
fn xdr_names_resolve_without_case_collisions() {
    assert_eq!(
        variant_for("LedgerCloseMeta"),
        Some(TypeVariant::LedgerCloseMeta)
    );
    assert_eq!(variant_for("SCVal"), Some(TypeVariant::ScVal)); // .x spelling
    assert_eq!(variant_for("NoSuchType"), None);
    let mut seen = HashSet::new();
    for n in TypeVariant::VARIANTS_STR {
        assert!(
            seen.insert(n.to_ascii_lowercase()),
            "{n} collides with another type ignoring case"
        );
    }
}

#[test]
fn round_trip_reports_decode_errors() {
    // LedgerCloseMeta with an unknown union arm
    assert!(matches!(
        round_trip(TypeVariant::LedgerCloseMeta, &[0, 0, 0, 7]),
        RoundTrip::DecodeError(_)
    ));
    // a valid value round-trips: MemoType MEMO_NONE
    assert_eq!(
        round_trip(TypeVariant::MemoType, &[0, 0, 0, 0]),
        RoundTrip::Identical
    );
}

#[test]
fn samples_manifest_checks_out() {
    let m = repo().join("corpus/samples/manifest.json");
    let rep = verify_manifest(&m, &default_schema(&m).unwrap(), None).unwrap();
    assert!(rep.ok(), "{rep:?}");
    assert_eq!((rep.entries, rep.tally.frames), (3, 3));
}

#[test]
fn manifest_problems_are_reported() {
    let src = repo().join("corpus/samples");
    let d = scratch("manifest");
    for f in fs::read_dir(&src).unwrap() {
        let p = f.unwrap().path();
        if !p.file_name().unwrap().to_str().unwrap().starts_with('.') {
            fs::copy(&p, d.join(p.file_name().unwrap())).unwrap();
        }
    }
    let path = d.join("manifest.json");
    let mut m: Value = serde_json::from_str(&fs::read_to_string(&path).unwrap()).unwrap();
    m["entries"][0]["sha256"] = Value::String("0".repeat(64)); // wrong hash, valid shape
    m["entries"][1].as_object_mut().unwrap().remove("atoms"); // schema violation
    m["entries"][2]["file"] = Value::String("gone.xdr".into()); // missing file
    fs::write(&path, serde_json::to_string(&m).unwrap()).unwrap();

    let rep = verify_manifest(&path, &repo().join("corpus/manifest.schema.json"), None).unwrap();
    assert!(!rep.ok());
    assert_eq!(rep.sha256_mismatches.len(), 1, "{rep:?}");
    assert!(
        rep.schema_errors.iter().any(|e| e.contains("atoms")),
        "{:?}",
        rep.schema_errors
    );
    assert_eq!(rep.missing.len(), 1);
    let _ = fs::remove_dir_all(d);
}

#[test]
fn agreement_needs_equal_counts_and_clean_rows() {
    let t = Tally {
        files: 1,
        frames: 10,
        bytes: 100,
        ..Default::default()
    };
    let rust = decoder_row("rs-stellar-xdr", "30.0.0", &t);
    let mut other = rust.clone();
    assert_eq!(
        agreement("c", &[rust.clone(), other.clone()])["agreement"]["all_byte_identical"],
        true
    );
    other["frames"] = 9.into();
    assert_eq!(
        agreement("c", &[rust.clone(), other.clone()])["agreement"]["all_byte_identical"],
        false
    );
    other["frames"] = 10.into();
    other["mismatches"] = 1.into();
    assert_eq!(
        agreement("c", &[rust, other])["agreement"]["all_byte_identical"],
        false
    );
}

/// The measured baseline: stellar-core f2b9194's protocol 30 goldens hold 15,306 LedgerCloseMeta and every one
/// round-trips byte for byte. Skipped without the fetched inputs unless HAMMING_REQUIRE_INPUTS is set.
#[test]
fn protocol_30_goldens_baseline() {
    let cache = std::env::var("HAMMING_CACHE")
        .map(PathBuf::from)
        .unwrap_or_else(|_| repo().join(".cache"));
    let dir = cache.join("core-f2b9194/test-lcm-current");
    if !dir.exists() {
        assert!(
            std::env::var_os("HAMMING_REQUIRE_INPUTS").is_none(),
            "missing {} (run make inputs)",
            dir.display()
        );
        eprintln!("skipping: {} not fetched", dir.display());
        return;
    }
    let t = verify_dir(&dir, TypeVariant::LedgerCloseMeta).unwrap();
    assert_eq!(
        (t.files, t.frames, t.mismatches, t.decode_errors),
        (866, 15306, 0, 0),
        "{:?}",
        t.examples
    );
}

/// Schema 0.2.0: core goldens are referenced (no file, caps or expected), local captures carry their network.
#[test]
fn schema_0_2_0_rules() {
    let schema: Value = serde_json::from_str(
        &fs::read_to_string(repo().join("corpus/manifest.schema.json")).unwrap(),
    )
    .unwrap();
    let golden = serde_json::json!({
        "id": "core.BumpSequenceTests.27d2533d64835282", "level": "structural", "xdr_type": "LedgerCloseMeta",
        "encoding": "xdr-framed", "sha256": "0".repeat(64), "protocol_min": 30, "synthetic": false,
        "atoms": ["LedgerCloseMeta|2"],
        "source": {"kind": "core-golden", "repo": "stellar/stellar-core", "commit": "f".repeat(40),
                   "path": "test-lcm-current/BumpSequenceTests/27d2533d64835282.xdr", "core_test": "bump sequence"}
    });
    let doc = |e: Value| serde_json::json!({"schema_version": "0.2.0", "network_passphrase": "p", "entries": [e]});
    let errs = |e: Value| schema_errors(&schema, &doc(e)).unwrap();
    assert!(
        errs(golden.clone()).is_empty(),
        "{:?}",
        errs(golden.clone())
    );

    let mut with_file = golden.clone();
    with_file["file"] = "copy.xdr".into();
    assert!(
        !errs(with_file).is_empty(),
        "a core golden must be referenced, not copied"
    );

    let mut no_core_test = golden.clone();
    no_core_test["source"]
        .as_object_mut()
        .unwrap()
        .remove("core_test");
    assert!(!errs(no_core_test).is_empty());

    let mut local = golden.clone();
    local["id"] = "str.cap40-signed-payload-signer.p28.0001".into();
    local["file"] = "x.xdr".into();
    local["caps"] = serde_json::json!(["CAP-0040"]);
    local["expected"] = serde_json::json!({"derived.txHash": "00"});
    local["source"] = serde_json::json!({"kind": "local-core", "core_version": "v28", "protocol": 28,
                                         "network_passphrase": "Standalone Network ; February 2017", "tx_hashes": ["0".repeat(64)]});
    assert!(
        !errs(local.clone()).is_empty(),
        "local-core without image accepted"
    );
    local["source"]["image"] = "stellar/quickstart:tag".into();
    assert!(errs(local.clone()).is_empty(), "{:?}", errs(local.clone()));

    let mut synthetic = local.clone();
    synthetic["source"] = serde_json::json!({"kind": "synthetic"});
    synthetic.as_object_mut().unwrap().remove("caps");
    assert!(
        !errs(synthetic).is_empty(),
        "non-golden entry without caps accepted"
    );
}
