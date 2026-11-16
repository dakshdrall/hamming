//! hamming-verify: decoder round-trip and corpus manifest checks with rs-stellar-xdr.
//!
//! Frames are streamed one at a time from record-marked XDR (4-byte big-endian length with the high bit set),
//! so memory is bounded by the largest frame, not by corpus size. Each frame is decoded as the named XDR type,
//! re-encoded, and compared byte for byte with the input.

use std::fs::{self, File};
use std::io::{self, BufReader, Read};
use std::path::{Path, PathBuf};
use std::str::FromStr;

use base64::Engine as _;
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use stellar_xdr::{Limits, Type, TypeVariant, WriteXdr};

/// Decode limits: deep enough for any ledger, and a 64 MiB cap on a single length prefix.
pub fn limits() -> Limits {
    Limits {
        depth: 1000,
        len: 64 << 20,
    }
}

/// Resolves an XDR type name. Manifests use the names in the .x files ("SCVal"); rs-stellar-xdr uses Rust
/// casing ("ScVal"). Its 478 names are distinct ignoring ASCII case (checked in tests), so that match is
/// unambiguous.
pub fn variant_for(name: &str) -> Option<TypeVariant> {
    if let Ok(v) = TypeVariant::from_str(name) {
        return Some(v);
    }
    TypeVariant::VARIANTS_STR
        .iter()
        .position(|n| n.eq_ignore_ascii_case(name))
        .map(|i| TypeVariant::VARIANTS[i])
}

/// Streams record-marked frames from a reader, reusing one buffer.
pub struct Frames<R> {
    r: R,
    buf: Vec<u8>,
    offset: u64,
}

impl<R: Read> Frames<R> {
    pub fn new(r: R) -> Self {
        Frames {
            r,
            buf: Vec::new(),
            offset: 0,
        }
    }

    /// The next frame, or `Ok(None)` at a clean end of input.
    pub fn next_frame(&mut self) -> io::Result<Option<&[u8]>> {
        let mut mark = [0u8; 4];
        let mut got = 0;
        while got < 4 {
            match self.r.read(&mut mark[got..])? {
                0 if got == 0 => return Ok(None),
                0 => {
                    return Err(bad(format!(
                        "truncated record mark at byte {}",
                        self.offset
                    )))
                }
                n => got += n,
            }
        }
        if mark[0] & 0x80 == 0 {
            return Err(bad(format!("record at byte {} is not a last fragment (multi-fragment records are not supported)", self.offset)));
        }
        let len = (u32::from_be_bytes(mark) & 0x7fff_ffff) as usize;
        self.buf.resize(len, 0);
        self.r.read_exact(&mut self.buf).map_err(|_| {
            bad(format!(
                "truncated frame at byte {}: want {} bytes",
                self.offset + 4,
                len
            ))
        })?;
        self.offset += 4 + len as u64;
        Ok(Some(&self.buf))
    }
}

fn bad(msg: String) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, msg)
}

/// Counts for one decoder over one corpus.
#[derive(Debug, Default, Clone, PartialEq)]
pub struct Tally {
    pub files: u64,
    pub frames: u64,
    pub bytes: u64,
    pub mismatches: u64,
    pub decode_errors: u64,
    /// The first few problems, for diagnosis.
    pub examples: Vec<String>,
}

impl Tally {
    fn note(&mut self, msg: String) {
        if self.examples.len() < 5 {
            self.examples.push(msg);
        }
    }

    pub fn clean(&self) -> bool {
        self.mismatches == 0 && self.decode_errors == 0
    }
}

/// Outcome of one value's round trip.
#[derive(Debug, PartialEq)]
pub enum RoundTrip {
    Identical,
    Mismatch,
    DecodeError(String),
}

/// Decodes `bytes` as `variant`, re-encodes it and compares.
pub fn round_trip(variant: TypeVariant, bytes: &[u8]) -> RoundTrip {
    match Type::from_xdr(variant, bytes, limits()) {
        Err(e) => RoundTrip::DecodeError(e.to_string()),
        Ok(t) => match t.to_xdr(limits()) {
            Ok(out) if out == bytes => RoundTrip::Identical,
            Ok(_) => RoundTrip::Mismatch,
            Err(e) => RoundTrip::DecodeError(format!("re-encode: {e}")),
        },
    }
}

fn record(t: &mut Tally, where_: &str, len: usize, rt: RoundTrip) {
    t.frames += 1;
    t.bytes += len as u64;
    match rt {
        RoundTrip::Identical => {}
        RoundTrip::Mismatch => {
            t.mismatches += 1;
            t.note(format!("{where_}: re-encoded bytes differ"));
        }
        RoundTrip::DecodeError(e) => {
            t.decode_errors += 1;
            t.note(format!("{where_}: {e}"));
        }
    }
}

/// Round-trips every frame of a record-marked file into `t`.
pub fn verify_framed(path: &Path, variant: TypeVariant, t: &mut Tally) -> io::Result<()> {
    let mut frames = Frames::new(BufReader::new(File::open(path)?));
    let mut i = 0;
    loop {
        let frame = match frames.next_frame() {
            Ok(Some(f)) => f,
            Ok(None) => break,
            Err(e) => {
                t.decode_errors += 1;
                t.note(format!("{}: {e}", path.display()));
                break;
            }
        };
        let rt = round_trip(variant, frame);
        let len = frame.len();
        record(t, &format!("{} frame {i}", path.display()), len, rt);
        i += 1;
    }
    t.files += 1;
    Ok(())
}

/// `*.xdr` files under `dir`, sorted, skipping dotfiles (macOS AppleDouble `._x.xdr` twins).
pub fn xdr_files(dir: &Path) -> io::Result<Vec<PathBuf>> {
    let mut out = Vec::new();
    let mut stack = vec![dir.to_path_buf()];
    while let Some(d) = stack.pop() {
        for e in fs::read_dir(&d)? {
            let p = e?.path();
            let name = p.file_name().and_then(|n| n.to_str()).unwrap_or("");
            if name.starts_with('.') {
                continue;
            }
            if p.is_dir() {
                stack.push(p);
            } else if name.ends_with(".xdr") {
                out.push(p);
            }
        }
    }
    out.sort();
    Ok(out)
}

/// Round-trips every frame of every `*.xdr` file under `dir`.
pub fn verify_dir(dir: &Path, variant: TypeVariant) -> io::Result<Tally> {
    let mut t = Tally::default();
    for f in xdr_files(dir)? {
        verify_framed(&f, variant, &mut t)?;
    }
    Ok(t)
}

/// Result of checking a corpus manifest.
#[derive(Debug, Default)]
pub struct ManifestReport {
    pub schema_version: String,
    pub entries: usize,
    pub schema_errors: Vec<String>,
    pub sha256_mismatches: Vec<String>,
    pub missing: Vec<String>,
    pub tally: Tally,
}

impl ManifestReport {
    pub fn ok(&self) -> bool {
        self.schema_errors.is_empty()
            && self.sha256_mismatches.is_empty()
            && self.missing.is_empty()
            && self.tally.clean()
    }
}

/// The schema for a manifest: `manifest.schema.json` next to it, else in its parent directory.
pub fn default_schema(manifest: &Path) -> Option<PathBuf> {
    let dir = manifest.parent()?;
    [
        dir.join("manifest.schema.json"),
        dir.parent()?.join("manifest.schema.json"),
    ]
    .into_iter()
    .find(|p| p.exists())
}

fn read_json(path: &Path) -> Result<Value, String> {
    let s = fs::read_to_string(path).map_err(|e| format!("{}: {e}", path.display()))?;
    serde_json::from_str(&s).map_err(|e| format!("{}: {e}", path.display()))
}

/// Validates a manifest document against a JSON Schema, returning one message per violation.
pub fn schema_errors(schema: &Value, manifest: &Value) -> Result<Vec<String>, String> {
    let v = jsonschema::validator_for(schema).map_err(|e| format!("invalid schema: {e}"))?;
    Ok(v.iter_errors(manifest)
        .map(|e| format!("{}: {}", e.instance_path(), e))
        .collect())
}

/// Where an entry's bytes live: `file` relative to the manifest, or, for a core golden referenced by commit and
/// path, `<cache>/core-<commit7>/<path>` as laid out by scripts/fetch-inputs.sh.
pub fn entry_path(
    entry: &Value,
    manifest_dir: &Path,
    cache: Option<&Path>,
) -> Result<PathBuf, String> {
    let src = &entry["source"];
    if src["kind"] == "core-golden" {
        let commit = src["commit"]
            .as_str()
            .ok_or("core-golden entry without source.commit")?;
        let path = src["path"]
            .as_str()
            .ok_or("core-golden entry without source.path")?;
        let cache =
            cache.ok_or("core-golden entries need the input cache (--cache or HAMMING_CACHE)")?;
        return Ok(cache
            .join(format!("core-{}", &commit[..commit.len().min(7)]))
            .join(path));
    }
    let file = entry["file"].as_str().ok_or("entry without file")?;
    Ok(manifest_dir.join(file))
}

/// Checks a manifest: schema validity, then for every entry its sha256 and a byte-exact round trip of every
/// value it holds.
pub fn verify_manifest(
    manifest_path: &Path,
    schema_path: &Path,
    cache: Option<&Path>,
) -> Result<ManifestReport, String> {
    let manifest = read_json(manifest_path)?;
    let schema = read_json(schema_path)?;
    let mut rep = ManifestReport {
        schema_version: manifest["schema_version"]
            .as_str()
            .unwrap_or("")
            .to_string(),
        schema_errors: schema_errors(&schema, &manifest)?,
        ..Default::default()
    };
    let dir = manifest_path.parent().unwrap_or(Path::new("."));
    let entries = manifest["entries"].as_array().cloned().unwrap_or_default();
    rep.entries = entries.len();
    for e in &entries {
        let id = e["id"].as_str().unwrap_or("?").to_string();
        let path = match entry_path(e, dir, cache) {
            Ok(p) => p,
            Err(msg) => {
                rep.missing.push(format!("{id}: {msg}"));
                continue;
            }
        };
        let raw = match fs::read(&path) {
            Ok(b) => b,
            Err(err) => {
                rep.missing.push(format!("{id}: {}: {err}", path.display()));
                continue;
            }
        };
        let encoding = e["encoding"].as_str().unwrap_or("");
        let bytes = if encoding == "base64" {
            let text: Vec<u8> = raw
                .iter()
                .copied()
                .filter(|b| !b.is_ascii_whitespace())
                .collect();
            match base64::engine::general_purpose::STANDARD.decode(text) {
                Ok(b) => b,
                Err(err) => {
                    rep.tally.decode_errors += 1;
                    rep.tally.note(format!("{id}: base64: {err}"));
                    continue;
                }
            }
        } else {
            raw
        };
        let sum = hex(&Sha256::digest(&bytes));
        if e["sha256"].as_str() != Some(sum.as_str()) {
            rep.sha256_mismatches.push(format!(
                "{id}: manifest {} != file {sum}",
                e["sha256"].as_str().unwrap_or("(none)")
            ));
        }
        let variant = match variant_for(e["xdr_type"].as_str().unwrap_or("")) {
            Some(v) => v,
            None => {
                rep.tally.decode_errors += 1;
                rep.tally
                    .note(format!("{id}: unknown xdr_type {}", e["xdr_type"]));
                continue;
            }
        };
        if encoding == "xdr-framed" {
            verify_framed(&path, variant, &mut rep.tally).map_err(|err| format!("{id}: {err}"))?;
        } else {
            rep.tally.files += 1;
            let len = bytes.len();
            record(&mut rep.tally, &id, len, round_trip(variant, &bytes));
        }
    }
    Ok(rep)
}

pub fn hex(b: &[u8]) -> String {
    b.iter().map(|x| format!("{x:02x}")).collect()
}

/// Peak resident set size of this process, in bytes (getrusage: kilobytes on Linux, bytes on macOS).
pub fn peak_rss_bytes() -> u64 {
    let mut ru: libc::rusage = unsafe { std::mem::zeroed() };
    if unsafe { libc::getrusage(libc::RUSAGE_SELF, &mut ru) } != 0 {
        return 0;
    }
    let v = ru.ru_maxrss as u64;
    if cfg!(target_os = "macos") {
        v
    } else {
        v * 1024
    }
}

/// One decoder's row in results/decoders.json.
pub fn decoder_row(name: &str, version: &str, t: &Tally) -> Value {
    json!({"decoder": name, "version": version, "files": t.files, "frames": t.frames, "bytes": t.bytes,
           "mismatches": t.mismatches, "decode_errors": t.decode_errors})
}

/// The agreement table: decoders agree when each one decoded the same number of frames and re-encoded every
/// one to the input bytes.
pub fn agreement(corpus: &str, rows: &[Value]) -> Value {
    let frames: Vec<u64> = rows
        .iter()
        .map(|r| r["frames"].as_u64().unwrap_or(0))
        .collect();
    let same = frames.windows(2).all(|w| w[0] == w[1]);
    let clean = rows
        .iter()
        .all(|r| r["mismatches"] == 0 && r["decode_errors"] == 0);
    json!({
        "corpus": corpus,
        "decoders": rows,
        "agreement": {"frames": frames.first().copied().unwrap_or(0), "decoders": rows.len(),
                      "same_frame_count": same, "all_byte_identical": same && clean}
    })
}
