//! hamming-verify <dir|manifest.json>
//!
//! dir:      round-trip every frame of every *.xdr file under dir (default type LedgerCloseMeta). With --out,
//!           write an agreement table that also includes other decoders' results given with --merge.
//! manifest: validate the manifest against its JSON Schema, check every entry's sha256, and round-trip every
//!           value as the entry's xdr_type.
//!
//! Exits 1 if anything disagrees, 2 on usage or I/O errors. Peak memory is printed, not written: it differs
//! from machine to machine and results/ must regenerate byte for byte.

use std::path::{Path, PathBuf};
use std::process::exit;
use std::time::Instant;

use hamming_verify::*;

struct Args {
    target: PathBuf,
    type_name: String,
    schema: Option<PathBuf>,
    cache: Option<PathBuf>,
    merge: Vec<PathBuf>,
    out: Option<PathBuf>,
    label: Option<String>,
}

fn usage() -> ! {
    eprintln!(
        "usage: hamming-verify [--type T] [--out FILE] [--merge DECODER.json]... [--label NAME] <dir>\n       \
         hamming-verify [--schema FILE] [--cache DIR] <manifest.json>"
    );
    exit(2)
}

fn parse() -> Args {
    let mut a = Args {
        target: PathBuf::new(),
        type_name: "LedgerCloseMeta".into(),
        schema: None,
        cache: std::env::var_os("HAMMING_CACHE").map(PathBuf::from),
        merge: vec![],
        out: None,
        label: None,
    };
    let mut it = std::env::args().skip(1);
    let mut target = None;
    while let Some(arg) = it.next() {
        let mut val = || it.next().unwrap_or_else(|| usage());
        match arg.as_str() {
            "--type" => a.type_name = val(),
            "--schema" => a.schema = Some(val().into()),
            "--cache" => a.cache = Some(val().into()),
            "--merge" => a.merge.push(val().into()),
            "--out" => a.out = Some(val().into()),
            "--label" => a.label = Some(val()),
            s if s.starts_with("--") => usage(),
            _ => target = Some(PathBuf::from(arg)),
        }
    }
    a.target = target.unwrap_or_else(|| usage());
    a
}

fn fail(msg: impl std::fmt::Display) -> ! {
    eprintln!("hamming-verify: {msg}");
    exit(2)
}

fn main() {
    let a = parse();
    let ok = if a.target.is_dir() {
        run_dir(&a)
    } else {
        run_manifest(&a)
    };
    eprintln!("peak RSS {:.1} MB", peak_rss_bytes() as f64 / 1e6);
    exit(if ok { 0 } else { 1 })
}

fn run_dir(a: &Args) -> bool {
    let variant = variant_for(&a.type_name)
        .unwrap_or_else(|| fail(format!("unknown XDR type {}", a.type_name)));
    let start = Instant::now();
    let t = verify_dir(&a.target, variant).unwrap_or_else(|e| fail(e));
    let s = start.elapsed().as_secs_f64();
    println!(
        "rs-stellar-xdr {}: {} files, {} frames, {:.1} MB, mismatches {}, decode errors {}, {:.2}s, {:.0} frames/s",
        stellar_xdr::VERSION.pkg, t.files, t.frames, t.bytes as f64 / 1e6, t.mismatches, t.decode_errors, s, t.frames as f64 / s
    );
    for e in &t.examples {
        println!("  {e}");
    }
    let mut rows = vec![decoder_row("rs-stellar-xdr", stellar_xdr::VERSION.pkg, &t)];
    for m in &a.merge {
        let s =
            std::fs::read_to_string(m).unwrap_or_else(|e| fail(format!("{}: {e}", m.display())));
        rows.push(
            serde_json::from_str(&s).unwrap_or_else(|e| fail(format!("{}: {e}", m.display()))),
        );
    }
    let label = a
        .label
        .clone()
        .unwrap_or_else(|| a.target.display().to_string());
    let table = agreement(&label, &rows);
    if let Some(out) = &a.out {
        let mut s = serde_json::to_string_pretty(&table).unwrap();
        s.push('\n');
        std::fs::write(out, s).unwrap_or_else(|e| fail(format!("{}: {e}", out.display())));
    }
    let agreed = table["agreement"]["all_byte_identical"] == true;
    if !a.merge.is_empty() {
        println!(
            "agreement across {} decoders over {} frames: {}",
            rows.len(),
            t.frames,
            if agreed { "all byte-identical" } else { "NO" }
        );
    }
    agreed
}

fn run_manifest(a: &Args) -> bool {
    let schema = a
        .schema
        .clone()
        .or_else(|| default_schema(&a.target))
        .unwrap_or_else(|| {
            fail(format!(
                "no manifest.schema.json next to {} or in its parent; pass --schema",
                a.target.display()
            ))
        });
    let rep = verify_manifest(&a.target, &schema, a.cache.as_deref().map(Path::new))
        .unwrap_or_else(|e| fail(e));
    println!(
        "{}: schema {} ({}), {} entries, {} values round-tripped, {} mismatches, {} decode errors, {} sha256 mismatches, {} missing",
        a.target.display(), rep.schema_version, if rep.schema_errors.is_empty() { "valid" } else { "INVALID" }, rep.entries,
        rep.tally.frames, rep.tally.mismatches, rep.tally.decode_errors, rep.sha256_mismatches.len(), rep.missing.len()
    );
    for m in rep
        .schema_errors
        .iter()
        .chain(&rep.sha256_mismatches)
        .chain(&rep.missing)
        .chain(&rep.tally.examples)
    {
        println!("  {m}");
    }
    rep.ok()
}
