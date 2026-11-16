package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummarizeAndFinish(t *testing.T) {
	ok := Facts{Entry: "/c/g/a.xdr", Frames: 2, Ledgers: 2, TxInMeta: 3, TxIndexed: 3, OpsInMeta: 3, OpsIndexed: 3}
	bad := Facts{Entry: "/c/g/b.xdr", Frames: 1, Ledgers: 1, IndexErrors: []string{"boom"}, IndexErrorCount: 1}
	rel := func(p string) string { return strings.TrimPrefix(p, "/c/g/") }

	g := Summarize(GoldenReplay{Commit: "3e22515", Root: "cache:g", Expect: Pass}, []Facts{ok, ok}, rel)
	if !g.Met || g.Files != 2 || g.Ledgers != 4 || g.Verdicts[Pass] != 2 {
		t.Errorf("clean replay: %+v", g)
	}
	g = Summarize(GoldenReplay{Commit: "3e22515", Root: "cache:g", Expect: Pass}, []Facts{ok, bad}, rel)
	if g.Met || g.IndexErrors != 1 || len(g.Unexpected) != 1 || g.Unexpected[0].File != "b.xdr" {
		t.Errorf("replay with an indexer error: %+v", g)
	}
	if Summarize(GoldenReplay{Expect: Pass}, nil, rel).Met {
		t.Error("an empty replay counted as met")
	}

	r := &Report{
		Entries: []EntryResult{{Entry: "x.xdr", Commit: "2a0a84e", Expected: MishandledPersist, Verdict: Pass, Reason: "r"}},
		Golden:  g,
	}
	r.Finish()
	if r.ExpectationsMet || len(r.Mismatches) != 2 {
		t.Errorf("Finish: met=%v mismatches=%v", r.ExpectationsMet, r.Mismatches)
	}
}

func TestInspect(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, _, err := Inspect(write("empty.xdr", nil)); err == nil {
		t.Error("empty entry accepted")
	}
	if _, _, err := Inspect(write("trunc.xdr", []byte{0x80, 0, 0, 8, 0, 0})); err == nil {
		t.Error("truncated frame accepted")
	}
	if _, _, err := Inspect(write("bad.xdr", []byte{0x80, 0, 0, 4, 0, 0, 0, 7})); err == nil {
		t.Error("undecodable frame accepted")
	}
	p, frames, err := Inspect(filepath.Join("..", "..", "corpus", "mutated", "cap40-signed-payload-signer-p28.xdr"))
	if err != nil || frames != 1 || len(p) != 1 || p[0] != 28 {
		t.Errorf("cap40 entry: protocols=%v frames=%d err=%v", p, frames, err)
	}
}
