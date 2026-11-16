package harness

import (
	"encoding/json"
	"strings"
	"testing"
)

const pkg = "github.com/stellar/wallet-backend/internal/indexer"

// ev renders one test2json event in the shape go test -json emits (checked against Go 1.27.4 output).
func ev(action, test, output string) string {
	m := map[string]any{"Action": action, "Package": pkg}
	if test != "" {
		m["Test"] = test
	}
	if output != "" {
		m["Output"] = output
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func stream(lines ...string) *strings.Reader {
	return strings.NewReader(strings.Join(lines, "\n") + "\n")
}

func TestParseGoTestJSONPass(t *testing.T) {
	facts := `HAMMING_FACTS {"entry":"/c/a.xdr","frames":1,"ledgers":1,"persist_errors":["StateChange.SignerAccountID: 68-byte"],"persist_error_count":1,"tx_in_meta":1,"tx_indexed":1}`
	run, err := ParseGoTestJSON(stream(
		ev("start", "", ""),
		ev("run", "TestHammingPersist", ""),
		ev("output", "TestHammingPersist", "=== RUN   TestHammingPersist\n"),
		ev("output", "TestHammingPersist", "HAMMING_BEGIN /c/a.xdr\n"),
		// test2json may split a long line across events: the parser must join them
		ev("output", "TestHammingPersist", facts[:40]),
		ev("output", "TestHammingPersist", facts[40:]+"\n"),
		ev("output", "TestHammingPersist", "--- PASS: TestHammingPersist (0.00s)\n"),
		ev("pass", "TestHammingPersist", ""),
		ev("output", "", "PASS\n"),
		ev("pass", "", ""),
	))
	if err != nil {
		t.Fatal(err)
	}
	if !run.Passed || run.BuildFailed {
		t.Errorf("Passed=%v BuildFailed=%v", run.Passed, run.BuildFailed)
	}
	if len(run.Facts) != 1 || run.Facts[0].Entry != "/c/a.xdr" || run.Facts[0].PersistErrorCount != 1 {
		t.Fatalf("facts = %+v", run.Facts)
	}
	if _, crashed := run.Unfinished(); crashed {
		t.Error("a completed run reported an unfinished entry")
	}
}

func TestParseGoTestJSONCrash(t *testing.T) {
	run, err := ParseGoTestJSON(stream(
		ev("output", "TestHammingPersist", "HAMMING_BEGIN /c/a.xdr\n"),
		ev("output", "TestHammingPersist", `HAMMING_FACTS {"entry":"/c/a.xdr","frames":1,"ledgers":1}`+"\n"),
		ev("output", "TestHammingPersist", "HAMMING_BEGIN /c/b.xdr\n"),
		ev("output", "", "fatal error: concurrent map writes\n"),
		ev("output", "", "FAIL\t"+pkg+"\t0.2s\n"),
		ev("fail", "", ""),
	))
	if err != nil {
		t.Fatal(err)
	}
	if run.Passed {
		t.Error("a crashed run reported Passed")
	}
	if got, ok := run.Unfinished(); !ok || got != "/c/b.xdr" {
		t.Errorf("Unfinished() = %q, %v; want /c/b.xdr", got, ok)
	}
	if !strings.Contains(run.Tail, "concurrent map writes") {
		t.Errorf("tail lost the crash message: %q", run.Tail)
	}
}

func TestParseGoTestJSONBuildFailure(t *testing.T) {
	imp := pkg + " [" + pkg + ".test]"
	build := func(action, out string) string {
		b, _ := json.Marshal(map[string]string{"ImportPath": imp, "Action": action, "Output": out})
		return string(b)
	}
	run, err := ParseGoTestJSON(stream(
		build("build-output", "# "+imp+"\n"),
		build("build-output", "internal/indexer/hamming_adapter_test.go:12:3: undefined: NewIndexer\n"),
		build("build-fail", ""),
		ev("start", "", ""),
		ev("output", "", "FAIL\t"+pkg+" [build failed]\n"),
		ev("fail", "", ""),
	))
	if err != nil {
		t.Fatal(err)
	}
	if !run.BuildFailed || !strings.Contains(run.BuildOutput, "undefined: NewIndexer") {
		t.Errorf("BuildFailed=%v output=%q", run.BuildFailed, run.BuildOutput)
	}
}

func TestParseGoTestJSONMalformedFacts(t *testing.T) {
	_, err := ParseGoTestJSON(stream(ev("output", "TestHammingPersist", "HAMMING_FACTS {not json\n")))
	if err == nil {
		t.Fatal("malformed facts were accepted")
	}
}
