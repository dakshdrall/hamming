// Package harness replays Hamming corpus entries through indexers at pinned commits and reports, per entry,
// whether the indexer handled it.
//
// The harness orchestrates targets rather than importing them: an Adapter checks out the target, injects a
// small in-package test file and runs it with the Go toolchain. Adapters report Facts (what the target did);
// Classify turns Facts into a Verdict, so the rules for pass and failure live in one tested place instead of in
// each target's adapter.
package harness

import "context"

// Entry is one corpus file to replay.
type Entry struct {
	// Path is the absolute path of a record-marked XDR file of LedgerCloseMeta frames.
	Path string
	// Passphrase is the network passphrase the entry's transactions were hashed with.
	Passphrase string
}

// Adapter runs one target indexer, at a pinned commit, over corpus entries.
type Adapter interface {
	// Name identifies the target, e.g. "wallet-backend".
	Name() string
	// Run returns one Facts per entry, in entry order. It returns an error only when the target cannot be
	// run at all (checkout or build failure); a target that mishandles an entry is reported through Facts.
	Run(ctx context.Context, commit string, entries []Entry) ([]Facts, error)
}

// Facts is what an adapter observed while one entry went through the target. The JSON field names are the
// adapter's wire format (see adapters/wallet-backend/hamming_adapter_test.go.tmpl).
type Facts struct {
	Entry   string `json:"entry"`
	Frames  int    `json:"frames"`
	Ledgers int    `json:"ledgers"` // frames the target's own SDK decoded
	// Protocols lists the ledger protocol versions seen, as decoded by the target.
	Protocols []uint32 `json:"protocols,omitempty"`

	// Message lists hold at most a few distinct messages; the counts are complete.
	DecodeErrors      []string `json:"decode_errors,omitempty"`
	DecodeErrorCount  int      `json:"decode_error_count"`
	IndexErrors       []string `json:"index_errors,omitempty"`
	IndexErrorCount   int      `json:"index_error_count"`
	Panics            []string `json:"panics,omitempty"`
	PanicCount        int      `json:"panic_count"`
	PersistErrors     []string `json:"persist_errors,omitempty"`
	PersistErrorCount int      `json:"persist_error_count"`

	TxInMeta     int `json:"tx_in_meta"`
	TxIndexed    int `json:"tx_indexed"`
	OpsInMeta    int `json:"ops_in_meta"`
	OpsIndexed   int `json:"ops_indexed"`
	StateChanges int `json:"state_changes"`
	Participants int `json:"participants"`

	PersistedSignerIDs []string `json:"persisted_signer_ids,omitempty"`

	// Crash is set by the runner, not by the adapter: the target's process died while on this entry.
	Crash string `json:"crash,omitempty"`
}
