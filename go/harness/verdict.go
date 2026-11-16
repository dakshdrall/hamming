package harness

import (
	"fmt"
	"regexp"
)

// Verdict is the harness's judgement of how a target handled one entry.
type Verdict string

const (
	// Pass: every frame decoded, indexed and persisted without error, and nothing was left out.
	Pass Verdict = "pass"
	// MishandledIndex: the target failed while decoding (for a reason other than an unknown protocol value),
	// indexing, or crashed.
	MishandledIndex Verdict = "mishandled(index)"
	// MishandledPersist: indexing succeeded but a buffered row cannot be encoded for the database.
	MishandledPersist Verdict = "mishandled(persist)"
	// Dropped: no error, but the target indexed fewer transactions or operations than the ledger holds.
	Dropped Verdict = "dropped"
	// UnsupportedProtocol: the target's XDR library does not know a discriminant the entry uses, i.e. the
	// entry is from a newer protocol than the target supports. Reported apart from failures: it is a
	// forward-compatibility signal, not a bug in handling data the target claims to support.
	UnsupportedProtocol Verdict = "unsupported-protocol"
)

// Verdicts lists every valid verdict.
var Verdicts = []Verdict{Pass, MishandledIndex, MishandledPersist, Dropped, UnsupportedProtocol}

// ParseVerdict validates a verdict string.
func ParseVerdict(s string) (Verdict, error) {
	for _, v := range Verdicts {
		if string(v) == s {
			return v, nil
		}
	}
	return "", fmt.Errorf("unknown verdict %q (want one of %v)", s, Verdicts)
}

// unknownDiscriminant matches XDR decode errors caused by an enum or union value the decoder does not know.
// Each alternative is wording observed from a real decoder (see verdict_test.go):
//   - go-stellar-sdk, enum:              "'3' is not a valid StellarValueType enum value"
//   - go-stellar-sdk, int-switched union: "union LedgerCloseMeta has invalid V (int32) switch value '7'"
//   - js-stellar-sdk, union:             "LedgerCloseMeta: no case for union discriminator -1"
var unknownDiscriminant = regexp.MustCompile(
	`is not a valid \w+ enum value|has invalid \w+ \(\w+\) switch value|no case for union discriminator`)

// IsUnknownDiscriminant reports whether a decode error means "value from a protocol this decoder predates".
func IsUnknownDiscriminant(msg string) bool { return unknownDiscriminant.MatchString(msg) }

// Classify turns an adapter's Facts into a Verdict and a one-line reason. Checks run in pipeline order:
// a target that cannot decode an entry never gets to index it, and so on.
func Classify(f Facts) (Verdict, string) {
	if f.Crash != "" {
		return MishandledIndex, "target process crashed: " + f.Crash
	}
	if f.DecodeErrorCount > 0 {
		for _, m := range f.DecodeErrors {
			if !IsUnknownDiscriminant(m) {
				return MishandledIndex, fmt.Sprintf("decode error (%d frames): %s", f.DecodeErrorCount, m)
			}
		}
		if len(f.DecodeErrors) == 0 {
			return MishandledIndex, fmt.Sprintf("%d decode errors with no message", f.DecodeErrorCount)
		}
		return UnsupportedProtocol, fmt.Sprintf("%d of %d frames use values the target's XDR does not know: %s",
			f.DecodeErrorCount, f.Frames, f.DecodeErrors[0])
	}
	if f.PanicCount > 0 {
		return MishandledIndex, fmt.Sprintf("panic (%d): %s", f.PanicCount, first(f.Panics))
	}
	if f.IndexErrorCount > 0 {
		return MishandledIndex, fmt.Sprintf("indexer error (%d): %s", f.IndexErrorCount, first(f.IndexErrors))
	}
	if f.PersistErrorCount > 0 {
		return MishandledPersist, fmt.Sprintf("cannot be persisted (%d): %s", f.PersistErrorCount, first(f.PersistErrors))
	}
	if f.Ledgers == 0 {
		return Dropped, "no ledger was processed"
	}
	if f.TxIndexed < f.TxInMeta || f.OpsIndexed < f.OpsInMeta {
		return Dropped, fmt.Sprintf("indexed %d of %d transactions and %d of %d operations",
			f.TxIndexed, f.TxInMeta, f.OpsIndexed, f.OpsInMeta)
	}
	reason := fmt.Sprintf("%d ledgers, %d transactions, %d operations, %d state changes",
		f.Ledgers, f.TxIndexed, f.OpsIndexed, f.StateChanges)
	if f.NormalizedCount > 0 {
		reason += fmt.Sprintf("; %d stored values read back normalized, e.g. %s", f.NormalizedCount, first(f.Normalized))
	}
	return Pass, reason
}

func first(msgs []string) string {
	if len(msgs) == 0 {
		return "(no message)"
	}
	return msgs[0]
}
