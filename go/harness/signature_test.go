package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePkg(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "indexer.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// a test file with a different NewIndexer must be ignored
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte("package indexer\nfunc NewIndexer(x int) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNewIndexerCall(t *testing.T) {
	cases := []struct {
		name, decl, want, err string
	}{
		{ // signature at wallet-backend 2a0a84e and 2c87335 (internal/indexer/indexer.go:119)
			"3 params", "func NewIndexer(networkPassphrase string, pool pond.Pool, ingestionMetrics *metrics.IngestionMetrics) (*Indexer, error)",
			"NewIndexer(pass, pool, nil)", ""},
		{ // signature at wallet-backend 3e22515 (internal/indexer/indexer.go:113)
			"4 params", "func NewIndexer(networkPassphrase string, pool pond.Pool, ingestionMetrics *metrics.IngestionMetrics, collectContractData bool) (*Indexer, error)",
			"NewIndexer(pass, pool, nil, true)", ""},
		{"grouped names", "func NewIndexer(a, b string) (*Indexer, error)", "NewIndexer(pass, pass)", ""},
		{"unknown bool", "func NewIndexer(p string, strict bool) (*Indexer, error)", "", `unknown bool parameter "strict"`},
		{"unknown type", "func NewIndexer(p string, n int) (*Indexer, error)", "", "type int"},
	}
	for _, c := range cases {
		dir := writePkg(t, "package indexer\n\n"+c.decl+" { return nil, nil }\n\nfunc (i *Indexer) NewIndexer() {}\n")
		got, err := NewIndexerCall(dir)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: err = %v, want containing %q", c.name, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.name, got, err, c.want)
		}
	}
}

func TestNewIndexerCallMissing(t *testing.T) {
	if _, err := NewIndexerCall(writePkg(t, "package indexer\n")); err == nil {
		t.Fatal("found a NewIndexer that does not exist")
	}
}
