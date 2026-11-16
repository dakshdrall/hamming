package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadExpectationsRepoFile(t *testing.T) {
	e, err := LoadExpectations(filepath.Join("..", "..", "adapters", "wallet-backend", "expectations.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Entries[0].Verdicts["2a0a84e"]; got != MishandledPersist {
		t.Errorf("#729 pre-fix expectation = %q", got)
	}
	if e.Golden == nil || e.Golden.Expect != Pass {
		t.Errorf("golden replay = %+v", e.Golden)
	}
}

func TestLoadExpectationsRejects(t *testing.T) {
	const commits = `"commits": {"2a0a84e": "2a0a84ea12523b6c92e278c0c84b443041240095"}`
	cases := map[string]string{
		"unknown verdict":        `{"target": "t", ` + commits + `, "entries": [{"entry": "a.xdr", "why": "w", "verdicts": {"2a0a84e": "fail"}}]}`,
		"unknown commit":         `{"target": "t", ` + commits + `, "entries": [{"entry": "a.xdr", "why": "w", "verdicts": {"deadbee": "pass"}}]}`,
		"short sha":              `{"target": "t", "commits": {"2a0a84e": "2a0a84ea"}}`,
		"label mismatch":         `{"target": "t", "commits": {"2c87335": "2a0a84ea12523b6c92e278c0c84b443041240095"}}`,
		"missing why":            `{"target": "t", ` + commits + `, "entries": [{"entry": "a.xdr", "verdicts": {"2a0a84e": "pass"}}]}`,
		"duplicate entry":        `{"target": "t", ` + commits + `, "entries": [{"entry": "a.xdr", "why": "w", "verdicts": {"2a0a84e": "pass"}}, {"entry": "a.xdr", "why": "w", "verdicts": {"2a0a84e": "pass"}}]}`,
		"unknown field":          `{"target": "t", ` + commits + `, "expected": []}`,
		"golden root":            `{"target": "t", ` + commits + `, "golden_replay": {"commit": "2a0a84e", "root": "/abs", "expect": "pass"}}`,
		"reason without verdict": `{"target": "t", ` + commits + `, "entries": [{"entry": "a.xdr", "why": "w", "verdicts": {"2a0a84e": "pass"}, "reason_contains": {"x": "y"}}]}`,
		"empty reason":           `{"target": "t", ` + commits + `, "entries": [{"entry": "a.xdr", "why": "w", "verdicts": {"2a0a84e": "pass"}, "reason_contains": {"2a0a84e": ""}}]}`,
		"golden bad commit":      `{"target": "t", ` + commits + `, "golden_replay": {"commit": "x", "root": "cache:d", "expect": "pass"}}`,
	}
	for name, body := range cases {
		path := filepath.Join(t.TempDir(), "e.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadExpectations(path); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestResolve(t *testing.T) {
	if got := Resolve("cache:core/x.xdr", "/repo", "/cache"); got != "/cache/core/x.xdr" {
		t.Errorf("cache entry -> %s", got)
	}
	if got := Resolve("corpus/x.xdr", "/repo", "/cache"); !strings.HasPrefix(got, "/repo/") {
		t.Errorf("repo entry -> %s", got)
	}
}

func TestExpectedEntryMet(t *testing.T) {
	x := ExpectedEntry{Verdicts: map[string]Verdict{"a": MishandledPersist, "b": Pass}, Reasons: map[string]string{"a": "stored lossily"}}
	if !x.Met("a", MishandledPersist, "StateChange.SignerAccountID: P... is stored lossily and reads back as ...") {
		t.Error("matching verdict and reason not met")
	}
	if x.Met("a", MishandledPersist, "StateChange.SignerAccountID: has a 68-byte key payload") {
		t.Error("a different persist failure met an expectation pinned to the lossy write")
	}
	if !x.Met("b", Pass, "anything") || x.Met("b", Dropped, "anything") {
		t.Error("verdict-only expectation misjudged")
	}
}
