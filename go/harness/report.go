package harness

import (
	"encoding/binary"
	"fmt"
	"os"
	"sort"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// Method states, in the report itself, what the harness does and does not exercise.
const Method = "In-process: each target commit runs its own NewIndexer and ProcessLedger on every frame, then " +
	"persistence is emulated by calling every driver.Valuer field of each buffered transaction, operation and " +
	"state change (the encoders the database COPY uses). No database is involved."

// Report is results/harness.json.
type Report struct {
	Target          string            `json:"target"`
	Commits         map[string]string `json:"commits"`
	Method          string            `json:"method"`
	Entries         []EntryResult     `json:"entries"`
	Golden          *GoldenResult     `json:"golden_replay,omitempty"`
	Mismatches      []string          `json:"mismatches"`
	ExpectationsMet bool              `json:"expectations_met"`
}

// EntryResult is one (entry, commit) pair from the expectations file.
type EntryResult struct {
	Entry     string   `json:"entry"`
	Issue     string   `json:"issue,omitempty"`
	Commit    string   `json:"commit"`
	Protocols []uint32 `json:"protocols"` // from Hamming's own decode, not the target's
	Expected  Verdict  `json:"expected"`
	Verdict   Verdict  `json:"verdict"`
	Met       bool     `json:"met"`
	Reason    string   `json:"reason"`
}

// GoldenResult summarises a golden replay.
type GoldenResult struct {
	Commit        string          `json:"commit"`
	Root          string          `json:"root"`
	Expect        Verdict         `json:"expect"`
	Files         int             `json:"files"`
	Ledgers       int             `json:"ledgers"`
	Transactions  int             `json:"transactions"`
	Operations    int             `json:"operations"`
	StateChanges  int             `json:"state_changes"`
	Participants  int             `json:"participants"`
	DecodeErrors  int             `json:"decode_errors"`
	IndexErrors   int             `json:"index_errors"`
	Panics        int             `json:"panics"`
	PersistErrors int             `json:"persist_errors"`
	Crashes       int             `json:"crashes"`
	Verdicts      map[Verdict]int `json:"verdicts"`
	Unexpected    []GoldenFile    `json:"unexpected,omitempty"` // at most maxUnexpected
	Met           bool            `json:"met"`
}

// GoldenFile is a golden file whose verdict differed from the expectation.
type GoldenFile struct {
	File    string  `json:"file"`
	Verdict Verdict `json:"verdict"`
	Reason  string  `json:"reason"`
}

const maxUnexpected = 20

// Summarize aggregates golden replay facts. rel maps an absolute entry path to the name shown in the report.
func Summarize(g GoldenReplay, facts []Facts, rel func(string) string) *GoldenResult {
	r := &GoldenResult{Commit: g.Commit, Root: g.Root, Expect: g.Expect, Verdicts: map[Verdict]int{}, Met: true}
	for _, f := range facts {
		r.Files++
		r.Ledgers += f.Ledgers
		r.Transactions += f.TxIndexed
		r.Operations += f.OpsIndexed
		r.StateChanges += f.StateChanges
		r.Participants += f.Participants
		r.DecodeErrors += f.DecodeErrorCount
		r.IndexErrors += f.IndexErrorCount
		r.Panics += f.PanicCount
		r.PersistErrors += f.PersistErrorCount
		if f.Crash != "" {
			r.Crashes++
		}
		v, reason := Classify(f)
		r.Verdicts[v]++
		if v != g.Expect {
			r.Met = false
			if len(r.Unexpected) < maxUnexpected {
				r.Unexpected = append(r.Unexpected, GoldenFile{File: rel(f.Entry), Verdict: v, Reason: reason})
			}
		}
	}
	if r.Files == 0 {
		r.Met = false
	}
	return r
}

// Finish fills Mismatches and ExpectationsMet from the entry and golden results.
func (r *Report) Finish() {
	r.Mismatches = []string{}
	for _, e := range r.Entries {
		if !e.Met {
			r.Mismatches = append(r.Mismatches, fmt.Sprintf("%s at %s: got %s, expected %s (%s)",
				e.Entry, e.Commit, e.Verdict, e.Expected, e.Reason))
		}
	}
	if g := r.Golden; g != nil && !g.Met {
		var counts []string
		for _, v := range Verdicts {
			if n := g.Verdicts[v]; n > 0 {
				counts = append(counts, fmt.Sprintf("%s %d", v, n))
			}
		}
		sort.Strings(counts)
		r.Mismatches = append(r.Mismatches, fmt.Sprintf("golden replay at %s: %d files, expected all %s, got %v",
			g.Commit, g.Files, g.Expect, counts))
	}
	r.ExpectationsMet = len(r.Mismatches) == 0
}

// Inspect decodes an entry with Hamming's own go-stellar-sdk and returns the protocol versions it contains.
// An entry Hamming itself cannot decode is a corpus error, never a target verdict.
func Inspect(path string) (protocols []uint32, frames int, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	seen := map[uint32]bool{}
	for i := 0; i < len(b); {
		if len(b)-i < 4 {
			return nil, frames, fmt.Errorf("%s: truncated record mark at byte %d", path, i)
		}
		n := int(binary.BigEndian.Uint32(b[i:]) & 0x7fffffff)
		i += 4
		if n > len(b)-i {
			return nil, frames, fmt.Errorf("%s: truncated frame at byte %d", path, i)
		}
		var lcm xdr.LedgerCloseMeta
		if err := xdr.SafeUnmarshal(b[i:i+n], &lcm); err != nil {
			return nil, frames, fmt.Errorf("%s: frame %d does not decode with Hamming's go-stellar-sdk: %w", path, frames, err)
		}
		if p := lcm.ProtocolVersion(); !seen[p] {
			seen[p] = true
			protocols = append(protocols, p)
		}
		frames++
		i += n
	}
	if frames == 0 {
		return nil, 0, fmt.Errorf("%s: no frames", path)
	}
	return protocols, frames, nil
}
