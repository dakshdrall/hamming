package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CoreTestPassphrase is the network passphrase stellar-core's tests (and so its golden files) use
// (src/test/test.cpp; see docs/FACTS.md).
const CoreTestPassphrase = "(V) (;,,;) (V)"

// cachePrefix marks an entry path as relative to HAMMING_CACHE instead of the repository root.
const cachePrefix = "cache:"

// Expectations is a target's expectations file: which verdict each corpus entry must get at each pinned
// commit, and the golden replay that must stay clean.
type Expectations struct {
	Target  string            `json:"target"`
	Repo    string            `json:"repo"`
	Commits map[string]string `json:"commits"` // label (short SHA) -> full SHA
	Entries []ExpectedEntry   `json:"entries"`
	Golden  *GoldenReplay     `json:"golden_replay,omitempty"`
}

// ExpectedEntry pins the verdicts of one corpus entry.
type ExpectedEntry struct {
	Entry      string             `json:"entry"` // repo-relative path, or "cache:<path under HAMMING_CACHE>"
	Passphrase string             `json:"passphrase,omitempty"`
	Issue      string             `json:"issue,omitempty"`
	Why        string             `json:"why"`
	Verdicts   map[string]Verdict `json:"verdicts"` // commit label -> verdict
	// Reasons optionally pins which failure a verdict must be, by a substring of its reason (commit label ->
	// substring). Two different bugs can share a verdict: #716's lossy write and #729's rejected write are both
	// mishandled(persist).
	Reasons map[string]string `json:"reason_contains,omitempty"`
}

// Met reports whether a verdict and its reason satisfy the expectation at a commit label.
func (x ExpectedEntry) Met(label string, v Verdict, reason string) bool {
	if v != x.Verdicts[label] {
		return false
	}
	want, ok := x.Reasons[label]
	return !ok || strings.Contains(reason, want)
}

// GoldenReplay runs every .xdr file under Root through one commit. Every file must get the Expect verdict.
type GoldenReplay struct {
	Commit string  `json:"commit"` // label
	Root   string  `json:"root"`   // "cache:<dir>"
	Expect Verdict `json:"expect"`
	Why    string  `json:"why"`
}

// LoadExpectations reads and validates an expectations file.
func LoadExpectations(path string) (*Expectations, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var e Expectations
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := e.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &e, nil
}

func (e *Expectations) validate() error {
	if e.Target == "" || len(e.Commits) == 0 {
		return fmt.Errorf("target and commits are required")
	}
	for label, sha := range e.Commits {
		if !fullSHA.MatchString(sha) || !strings.HasPrefix(sha, label) {
			return fmt.Errorf("commit %q -> %q: want a full SHA starting with its label", label, sha)
		}
	}
	checkVerdict := func(where string, v Verdict) error {
		if _, err := ParseVerdict(string(v)); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		return nil
	}
	seen := map[string]bool{}
	for _, x := range e.Entries {
		if x.Entry == "" || x.Why == "" || len(x.Verdicts) == 0 {
			return fmt.Errorf("entry %q: entry, why and verdicts are required", x.Entry)
		}
		if seen[x.Entry] {
			return fmt.Errorf("entry %q listed twice", x.Entry)
		}
		seen[x.Entry] = true
		for label, v := range x.Verdicts {
			if _, ok := e.Commits[label]; !ok {
				return fmt.Errorf("entry %q: commit %q is not in commits", x.Entry, label)
			}
			if err := checkVerdict("entry "+x.Entry, v); err != nil {
				return err
			}
		}
		for label, sub := range x.Reasons {
			if _, ok := x.Verdicts[label]; !ok || sub == "" {
				return fmt.Errorf("entry %q: reason_contains for %q needs a verdict for that commit and a non-empty substring", x.Entry, label)
			}
		}
	}
	if g := e.Golden; g != nil {
		if _, ok := e.Commits[g.Commit]; !ok {
			return fmt.Errorf("golden_replay: commit %q is not in commits", g.Commit)
		}
		if !strings.HasPrefix(g.Root, cachePrefix) {
			return fmt.Errorf("golden_replay: root %q must start with %q", g.Root, cachePrefix)
		}
		if err := checkVerdict("golden_replay", g.Expect); err != nil {
			return err
		}
	}
	return nil
}

// Resolve maps an entry path from the expectations file to an absolute path.
func Resolve(spec, repoRoot, cache string) string {
	if strings.HasPrefix(spec, cachePrefix) {
		return filepath.Join(cache, strings.TrimPrefix(spec, cachePrefix))
	}
	return filepath.Join(repoRoot, spec)
}

// Labels returns the commit labels in a stable order.
func (e *Expectations) Labels() []string {
	labels := make([]string, 0, len(e.Commits))
	for l := range e.Commits {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	return labels
}
