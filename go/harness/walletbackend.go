package harness

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const newIndexerPlaceholder = "{{NEW_INDEXER_CALL}}"

// WalletBackend runs stellar/wallet-backend's indexer. Its indexer package is internal, so for each commit
// the adapter makes a worktree, renders adapters/wallet-backend/hamming_adapter_test.go.tmpl into
// internal/indexer, and runs that test with `go test -json`.
type WalletBackend struct {
	Repo     string    // clone of stellar/wallet-backend (from scripts/fetch-inputs.sh)
	Cache    string    // directory for wb-<sha7> worktrees
	Template string    // path of hamming_adapter_test.go.tmpl
	Log      io.Writer // progress and go test stderr; nil discards
}

func (w *WalletBackend) Name() string { return "wallet-backend" }

func (w *WalletBackend) logf(format string, args ...any) {
	if w.Log != nil {
		fmt.Fprintf(w.Log, format, args...)
	}
}

// Prepare checks out commit and injects the adapter, returning the worktree directory.
func (w *WalletBackend) Prepare(ctx context.Context, commit string) (string, error) {
	dir := filepath.Join(w.Cache, "wb-"+commit[:7])
	if err := EnsureWorktree(ctx, w.Repo, dir, commit); err != nil {
		return "", err
	}
	pkg := filepath.Join(dir, "internal", "indexer")
	call, err := NewIndexerCall(pkg)
	if err != nil {
		return "", fmt.Errorf("wallet-backend %s: %w", commit[:7], err)
	}
	tmpl, err := os.ReadFile(w.Template)
	if err != nil {
		return "", err
	}
	if n := bytes.Count(tmpl, []byte(newIndexerPlaceholder)); n != 1 {
		return "", fmt.Errorf("%s: want exactly one %s, found %d", w.Template, newIndexerPlaceholder, n)
	}
	src := bytes.Replace(tmpl, []byte(newIndexerPlaceholder), []byte(call), 1)
	if err := os.WriteFile(filepath.Join(pkg, "hamming_adapter_test.go"), src, 0o644); err != nil {
		return "", err
	}
	w.logf("wallet-backend %s: adapter calls %s\n", commit[:7], call)
	return dir, nil
}

// Run replays entries through wallet-backend at commit. If the test process dies on an entry, that entry gets
// Facts with Crash set and the run resumes with the next entry in a fresh process.
func (w *WalletBackend) Run(ctx context.Context, commit string, entries []Entry) ([]Facts, error) {
	dir, err := w.Prepare(ctx, commit)
	if err != nil {
		return nil, err
	}
	byPath := map[string]Facts{}
	remaining := entries
	for len(remaining) > 0 {
		run, err := w.goTest(ctx, dir, remaining)
		if err != nil {
			return nil, err
		}
		if run.BuildFailed {
			return nil, fmt.Errorf("adapter does not build against wallet-backend %s:\n%s", commit[:7], run.BuildOutput)
		}
		for _, f := range run.Facts {
			byPath[f.Entry] = f
		}
		done := len(run.Facts)
		if done == len(remaining) {
			break
		}
		crashed, ok := run.Unfinished()
		if !ok || crashed != remaining[done].Path {
			return nil, fmt.Errorf("wallet-backend %s: adapter stopped after %d of %d entries without starting the next one:\n%s",
				commit[:7], done, len(remaining), run.Tail)
		}
		w.logf("wallet-backend %s: process died on %s; resuming after it\n", commit[:7], crashed)
		byPath[crashed] = Facts{Entry: crashed, Crash: lastLines(run.Tail, 12)}
		remaining = remaining[done+1:]
	}
	out := make([]Facts, len(entries))
	for i, e := range entries {
		f, ok := byPath[e.Path]
		if !ok {
			return nil, fmt.Errorf("no facts for %s", e.Path)
		}
		out[i] = f
	}
	return out, nil
}

func (w *WalletBackend) goTest(ctx context.Context, dir string, entries []Entry) (*GoTestRun, error) {
	list, err := os.CreateTemp("", "hamming-entries-*.txt")
	if err != nil {
		return nil, err
	}
	defer os.Remove(list.Name())
	for _, e := range entries {
		if strings.ContainsAny(e.Path, "\t\n") || strings.ContainsAny(e.Passphrase, "\t\n") {
			return nil, fmt.Errorf("entry %q: paths and passphrases cannot contain tabs or newlines", e.Path)
		}
		fmt.Fprintf(list, "%s\t%s\n", e.Path, e.Passphrase)
	}
	if err := list.Close(); err != nil {
		return nil, err
	}
	// -vet=off: vet findings in the target's own code are not what is being tested.
	cmd := exec.CommandContext(ctx, "go", "test", "./internal/indexer", "-run", "^TestHammingPersist$",
		"-count=1", "-vet=off", "-timeout=30m", "-json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HAMMING_LEDGER=@"+list.Name())
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if w.Log != nil {
		cmd.Stderr = w.Log
	}
	_ = cmd.Run() // a failing or crashing test exits non-zero; the event stream says what happened
	return ParseGoTestJSON(&stdout)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
