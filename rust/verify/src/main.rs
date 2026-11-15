// rs-stellar-xdr: byte-exact round-trip of every golden LedgerCloseMeta, streamed one frame at a time.
use std::{fs, path::Path, time::Instant};
use stellar_xdr::{LedgerCloseMeta, Limits, ReadXdr, WriteXdr};
fn walk(p: &Path, out: &mut Vec<std::path::PathBuf>) {
    for e in fs::read_dir(p).unwrap() { let e = e.unwrap().path();
        if e.is_dir() { walk(&e, out) } else if e.extension().map(|x| x == "xdr").unwrap_or(false) { out.push(e) } }
}
fn main() {
    let mut files = vec![]; walk(Path::new(&std::env::args().nth(1).unwrap()), &mut files); files.sort();
    let (mut n, mut bad, mut bytes) = (0u64, 0u64, 0usize);
    let lim = Limits { depth: 1000, len: 64 << 20 };
    let t = Instant::now();
    for f in files {
        let b = fs::read(&f).unwrap(); let mut i = 0;
        while i < b.len() {
            let l = (u32::from_be_bytes(b[i..i+4].try_into().unwrap()) & 0x7fff_ffff) as usize; i += 4;
            let fr = &b[i..i+l]; i += l;
            match LedgerCloseMeta::from_xdr(fr, lim.clone()) {
                Ok(m) => { let o = m.to_xdr(lim.clone()).unwrap(); if o != fr { bad += 1 } }
                Err(_) => bad += 1,
            }
            n += 1; bytes += l;
        }
    }
    let s = t.elapsed().as_secs_f64();
    println!("rs-stellar-xdr: {} LCMs, {:.1} MB, mismatches {}, {:.2}s, {:.0} LCM/s, {:.1} MB/s", n, bytes as f64/1e6, bad, s, n as f64/s, bytes as f64/1e6/s);
    if bad > 0 { std::process::exit(1) }
}
