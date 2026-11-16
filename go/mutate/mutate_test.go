package mutate

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

const coreP28 = "abe1c5541827caaafe023954c224cadeb5dddc44"

// inputs returns the protocol 28 goldens and the stellar-xdr checkout from scripts/fetch-inputs.sh. Without them
// the test is skipped, unless HAMMING_REQUIRE_INPUTS is set (CI), where a skip would hide a broken setup.
func inputs(t *testing.T) (golden, xdrDir string) {
	t.Helper()
	cache := os.Getenv("HAMMING_CACHE")
	if cache == "" {
		cache = filepath.Join("..", "..", ".cache")
	}
	golden = filepath.Join(cache, "core-abe1c55", "test-lcm-current")
	xdrDir = filepath.Join(cache, "stellar-xdr")
	for _, p := range []string{golden, xdrDir} {
		if _, err := os.Stat(p); err != nil {
			if os.Getenv("HAMMING_REQUIRE_INPUTS") != "" {
				t.Fatalf("missing input %s (run make inputs)", p)
			}
			t.Skipf("missing input %s (run make inputs)", p)
		}
	}
	return golden, xdrDir
}

// coverage runs the Python coverage meter over a directory of framed files and returns the covered atoms.
func coverage(t *testing.T, xdrDir, dir string) map[string]bool {
	t.Helper()
	py := os.Getenv("PYTHON")
	if py == "" {
		py = "python3"
	}
	out := filepath.Join(t.TempDir(), "cov.json")
	cmd := exec.Command(py, filepath.Join("..", "..", "python", "xdr_coverage.py"), xdrDir, dir, "--out", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("xdr_coverage.py: %v\n%s", err, b)
	}
	var res struct{ Covered []string }
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal(err)
	}
	set := map[string]bool{}
	for _, a := range res.Covered {
		set[a] = true
	}
	return set
}

func writeDir(t *testing.T, name string, b []byte) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRulesDeclareLevelAndCaps(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range Rules {
		if r.Level != "structural" && r.Level != "value" && r.Level != "relational" {
			t.Errorf("rule %s: level %q", r.ID, r.Level)
		}
		if len(r.Caps) == 0 {
			t.Errorf("rule %s: no CAPs", r.ID)
		}
		if ids[r.ID] {
			t.Errorf("rule id %s used twice", r.ID)
		}
		ids[r.ID] = true
	}
}

func TestRule1ReproducesCommittedEntry(t *testing.T) {
	golden, _ := inputs(t)
	res, err := Run(Rules[0], golden, CorePassphrase)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "corpus", "mutated", "cap40-signed-payload-signer-p28.xdr"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(res.Output, want) {
		t.Fatal("rule 1 no longer reproduces corpus/mutated/cap40-signed-payload-signer-p28.xdr (the #729 entry)")
	}
}

func TestRules(t *testing.T) {
	golden, xdrDir := inputs(t)
	for _, r := range Rules {
		t.Run(r.ID, func(t *testing.T) {
			res, err := Run(r, golden, CorePassphrase)
			if err != nil {
				t.Fatal(err)
			}
			// (a) the output is one frame that re-decodes
			frames, err := Frames(res.Output)
			if err != nil || len(frames) != 1 {
				t.Fatalf("frames: %d, %v", len(frames), err)
			}
			var lcm xdr.LedgerCloseMeta
			if err := xdr.SafeUnmarshal(frames[0], &lcm); err != nil {
				t.Fatalf("output does not decode: %v", err)
			}
			// (b) every envelope hash matches its result hash, and the mutated transaction's hash changed
			if err := CheckHashes(&lcm, CorePassphrase); err != nil {
				t.Fatal(err)
			}
			if res.NewTxHash == res.OldTxHash {
				t.Error("transaction hash unchanged: nothing was rewritten")
			}
			// (c) the intended atoms appear in the output, and at least one was absent from the source golden
			got := coverage(t, xdrDir, writeDir(t, "out.xdr", res.Output))
			src, err := os.ReadFile(filepath.Join(golden, res.SourcePath))
			if err != nil {
				t.Fatal(err)
			}
			before := coverage(t, xdrDir, writeDir(t, "src.xdr", src))
			added := false
			for _, a := range r.Atoms {
				if !got[a] {
					t.Errorf("atom %s missing from the output", a)
				}
				if !before[a] {
					added = true
				}
			}
			if !added {
				t.Errorf("source golden %s already had every atom %v: the rule added nothing", res.SourcePath, r.Atoms)
			}
		})
	}
}

// Strkey signed payloads are 32 (key) + 4 (length) + n rounded up to a multiple of 4. The prompt's "36 and 100"
// for n = 1 and 64 was off for n = 1: 36 bytes is n = 0, which go-stellar-sdk's strkey decoder rejects.
func TestSignedPayloadStrkeyLengths(t *testing.T) {
	var key xdr.Uint256
	for n, want := range map[int]int{1: 40, 4: 40, 5: 44, 32: 68, 64: 100} {
		_, got, err := strkeyPayloadLen(signedPayloadKey(key, n))
		if err != nil || got != want {
			t.Errorf("payload %d bytes: strkey payload %d bytes (err %v), want %d", n, got, err, want)
		}
	}
	if _, _, err := strkeyPayloadLen(signedPayloadKey(key, 0)); err == nil {
		t.Error("an empty signed payload decoded; the strkey range would start at 36, not 40")
	}
}

func TestRule4HoldsGAndPOverOneKey(t *testing.T) {
	golden, _ := inputs(t)
	r, _ := Lookup("signer-g-and-p-same-key")
	res, err := Run(r, golden, CorePassphrase)
	if err != nil {
		t.Fatal(err)
	}
	frames, _ := Frames(res.Output)
	var lcm xdr.LedgerCloseMeta
	if err := xdr.SafeUnmarshal(frames[0], &lcm); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range processing(&lcm) {
		for _, list := range txChanges(a.Meta) {
			for ci := range list {
				le := entryOf(&list[ci])
				if le == nil || le.Data.Account == nil {
					continue
				}
				acc := le.Data.Account
				g := map[xdr.Uint256]bool{}
				for _, s := range acc.Signers {
					if s.Key.Type == xdr.SignerKeyTypeSignerKeyTypeEd25519 {
						g[*s.Key.Ed25519] = true
					}
				}
				for i, s := range acc.Signers {
					if i > 0 && signerKeyLess(s.Key, acc.Signers[i-1].Key) {
						t.Errorf("account %s: signers out of core's order", acc.AccountId.Address())
					}
					if p := s.Key.Ed25519SignedPayload; p != nil && g[p.Ed25519] {
						found = true
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("no account entry holds a G signer and a P signer over the same key")
	}
}

func TestSortSignersMovesSponsoringIDs(t *testing.T) {
	k := func(b byte) xdr.Uint256 { var u xdr.Uint256; u[0] = b; return u }
	ed := func(b byte) xdr.SignerKey {
		u := k(b)
		return xdr.SignerKey{Type: xdr.SignerKeyTypeSignerKeyTypeEd25519, Ed25519: &u}
	}
	sponsor := xdr.MustAddress("GBIFX2QKGC6J4VRBEQDJZJSFZA43IQK2CYR3BF473UFC7VMSOOM2Z7SX")
	acc := xdr.AccountEntry{
		Signers: []xdr.Signer{{Key: signedPayloadKey(k(1), 4), Weight: 1}, {Key: ed(9), Weight: 2}, {Key: ed(3), Weight: 3}},
		Ext: xdr.AccountEntryExt{V: 1, V1: &xdr.AccountEntryExtensionV1{Ext: xdr.AccountEntryExtensionV1Ext{V: 2,
			V2: &xdr.AccountEntryExtensionV2{SignerSponsoringIDs: []xdr.SponsorshipDescriptor{nil, &sponsor, nil}}}}},
	}
	sortSigners(&acc)
	if w := []xdr.Uint32{3, 2, 1}; acc.Signers[0].Weight != w[0] || acc.Signers[1].Weight != w[1] || acc.Signers[2].Weight != w[2] {
		t.Fatalf("order by weight marker: %d %d %d, want 3 2 1", acc.Signers[0].Weight, acc.Signers[1].Weight, acc.Signers[2].Weight)
	}
	ids := acc.Ext.V1.Ext.V2.SignerSponsoringIDs
	if ids[0] != nil || ids[1] == nil || ids[2] != nil {
		t.Errorf("sponsoring IDs did not move with their signers: %v", ids)
	}
}

func TestCheckHashesDetectsStaleResultHash(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "corpus", "mutated", "cap40-signed-payload-signer-p28.xdr"))
	if err != nil {
		t.Fatal(err)
	}
	frames, err := Frames(b)
	if err != nil {
		t.Fatal(err)
	}
	var lcm xdr.LedgerCloseMeta
	if err := xdr.SafeUnmarshal(frames[0], &lcm); err != nil {
		t.Fatal(err)
	}
	if err := CheckHashes(&lcm, CorePassphrase); err != nil {
		t.Fatalf("committed entry: %v", err)
	}
	processing(&lcm)[0].Result.TransactionHash[0] ^= 0xff
	if CheckHashes(&lcm, CorePassphrase) == nil {
		t.Fatal("a stale result hash went unnoticed")
	}
	if CheckHashes(&lcm, "Test SDF Network ; September 2015") == nil {
		t.Fatal("hashes matched under the wrong passphrase")
	}
}
