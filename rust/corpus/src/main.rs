//! hamming-corpus: build and check corpus/v0/manifest.json.
//!
//!   hamming-corpus --golden-manifest results/core_golden_manifest.json --cover results/golden_cover.json \
//!                  --mutated corpus/mutated --schema corpus/manifest.schema.json --out corpus/v0/manifest.json \
//!                  [--min-atoms 459]
//!
//! Exits 1 if the manifest does not validate against the schema or covers fewer atoms than --min-atoms.

use std::fs;
use std::path::{Path, PathBuf};
use std::process::exit;

use hamming_corpus::*;
use serde_json::Value;

fn fail(msg: impl std::fmt::Display) -> ! {
    eprintln!("hamming-corpus: {msg}");
    exit(2)
}

fn read_json(p: &Path) -> Value {
    let s = fs::read_to_string(p).unwrap_or_else(|e| fail(format!("{}: {e}", p.display())));
    serde_json::from_str(&s).unwrap_or_else(|e| fail(format!("{}: {e}", p.display())))
}

/// Relative path from directory `from` to file `to`, both under the same root.
fn relative(from: &Path, to: &Path) -> String {
    let from: Vec<_> = from.components().collect();
    let to: Vec<_> = to.components().collect();
    let common = from.iter().zip(&to).take_while(|(a, b)| a == b).count();
    let mut parts: Vec<String> = vec!["..".into(); from.len() - common];
    parts.extend(
        to[common..]
            .iter()
            .map(|c| c.as_os_str().to_string_lossy().into_owned()),
    );
    parts.join("/")
}

fn main() {
    let (mut gm, mut cover, mut mutated, mut schema, mut out, mut min_atoms) =
        (None, None, None, None, None, 0usize);
    let mut it = std::env::args().skip(1);
    while let Some(a) = it.next() {
        let mut v = || {
            PathBuf::from(
                it.next()
                    .unwrap_or_else(|| fail(format!("{a} needs a value"))),
            )
        };
        match a.as_str() {
            "--golden-manifest" => gm = Some(v()),
            "--cover" => cover = Some(v()),
            "--mutated" => mutated = Some(v()),
            "--schema" => schema = Some(v()),
            "--out" => out = Some(v()),
            "--min-atoms" => {
                min_atoms = v()
                    .to_string_lossy()
                    .parse()
                    .unwrap_or_else(|_| fail("--min-atoms takes a number"))
            }
            _ => fail(format!("unknown argument {a}")),
        }
    }
    let need =
        |p: Option<PathBuf>, name: &str| p.unwrap_or_else(|| fail(format!("{name} is required")));
    let (gm, cover_path, mutated_dir, schema, out) = (
        need(gm, "--golden-manifest"),
        need(cover, "--cover"),
        need(mutated, "--mutated"),
        need(schema, "--schema"),
        need(out, "--out"),
    );

    let cover: Vec<String> = serde_json::from_value(read_json(&cover_path))
        .unwrap_or_else(|e| fail(format!("cover: {e}")));
    let out_dir = out.parent().unwrap_or(Path::new("."));
    fs::create_dir_all(out_dir).unwrap_or_else(|e| fail(e));
    let abs =
        |p: &Path| fs::canonicalize(p).unwrap_or_else(|e| fail(format!("{}: {e}", p.display())));
    let mut sidecars: Vec<PathBuf> = fs::read_dir(&mutated_dir)
        .unwrap_or_else(|e| fail(e))
        .map(|e| e.unwrap().path())
        .filter(|p| {
            p.extension().is_some_and(|x| x == "json")
                && !p.file_name().unwrap().to_string_lossy().starts_with('.')
        })
        .collect();
    sidecars.sort();
    let mutated: Vec<Mutated> = sidecars
        .iter()
        .map(|sc| {
            let xdr = sc.with_extension("xdr");
            let bytes = fs::read(&xdr).unwrap_or_else(|e| fail(format!("{}: {e}", xdr.display())));
            Mutated {
                file: relative(&abs(out_dir), &abs(&xdr)),
                sidecar: read_json(sc),
                bytes,
            }
        })
        .collect();

    let manifest = build(&read_json(&gm), &cover, &mutated).unwrap_or_else(|e| fail(e));
    let errors =
        hamming_verify::schema_errors(&read_json(&schema), &manifest).unwrap_or_else(|e| fail(e));
    let atoms = covered_atoms(&manifest);
    let entries = manifest["entries"].as_array().map(|a| a.len()).unwrap_or(0);
    let mut s = serde_json::to_string_pretty(&manifest).unwrap();
    s.push('\n');
    fs::write(&out, s).unwrap_or_else(|e| fail(format!("{}: {e}", out.display())));

    println!(
        "{}: {} entries ({} core goldens, {} synthetic), {} atoms covered, schema {}",
        out.display(),
        entries,
        cover.len(),
        mutated.len(),
        atoms.len(),
        if errors.is_empty() {
            "valid"
        } else {
            "INVALID"
        }
    );
    for e in &errors {
        println!("  {e}");
    }
    if atoms.len() < min_atoms {
        println!("  covered atoms {} < required {min_atoms}", atoms.len());
    }
    if !errors.is_empty() || atoms.len() < min_atoms {
        exit(1)
    }
}
