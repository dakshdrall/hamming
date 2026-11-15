package harness

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// Messages captured from real runs (wallet-backend 2a0a84e, go-stellar-sdk v0.7.3; js-stellar-sdk 17.4.0).
const (
	msgP30OnOldSDK = "decoding LedgerCloseMetaV2: decoding LedgerHeaderHistoryEntry: decoding LedgerHeader: " +
		"decoding StellarValue: decoding StellarValueExt: decoding StellarValueType: " +
		"'3' is not a valid StellarValueType enum value"
	msg729 = "StateChange.SignerAccountID: stellar address PCEDCRZF3WHS3R6CWJ3WFILROPR6U6KR27B5SW6XYISELPLKW347MAAAAAQKXK5LVOV2XK5LVOV2XK5LVOV2XK5LVOV2XK5LVOV2XK5LVOV2XK2COE " +
		"has a 68-byte key payload; the account_id column stores only 32-byte account/contract keys"
	msgJSUnion = "LedgerCloseMeta: no case for union discriminator -1"
)

func TestUnknownDiscriminantMatchesRealDecoders(t *testing.T) {
	// Decode bytes with go-stellar-sdk itself so the test tracks the library's wording, not a copy of it.
	var sk xdr.SignerKey
	enumErr := xdr.SafeUnmarshal(append([]byte{0, 0, 0, 9}, make([]byte, 32)...), &sk)
	var lcm xdr.LedgerCloseMeta
	unionErr := xdr.SafeUnmarshal([]byte{0, 0, 0, 7}, &lcm)
	if enumErr == nil || unionErr == nil {
		t.Fatalf("expected decode errors, got %v / %v", enumErr, unionErr)
	}
	for _, msg := range []string{enumErr.Error(), unionErr.Error(), msgP30OnOldSDK, msgJSUnion} {
		if !IsUnknownDiscriminant(msg) {
			t.Errorf("not recognised as an unknown discriminant: %q", msg)
		}
	}
	for _, msg := range []string{"truncated frame at byte 4: want 8 bytes, have 2", "xdr:DecodeUint: EOF", msg729} {
		if IsUnknownDiscriminant(msg) {
			t.Errorf("wrongly recognised as an unknown discriminant: %q", msg)
		}
	}
}

func TestClassify(t *testing.T) {
	ok := Facts{Frames: 1, Ledgers: 1, TxInMeta: 1, TxIndexed: 1, OpsInMeta: 3, OpsIndexed: 3, StateChanges: 2}
	with := func(edit func(*Facts)) Facts { f := ok; edit(&f); return f }

	cases := []struct {
		name   string
		facts  Facts
		want   Verdict
		reason string // substring of the reason
	}{
		{"clean entry", ok, Pass, "1 ledgers"},
		{"#729 on 2a0a84e", with(func(f *Facts) { f.PersistErrors, f.PersistErrorCount = []string{msg729}, 1 }),
			MishandledPersist, "68-byte key payload"},
		{"P30 golden on v0.7.3", Facts{Frames: 2, DecodeErrors: []string{msgP30OnOldSDK}, DecodeErrorCount: 2},
			UnsupportedProtocol, "2 of 2 frames"},
		{"decode error that is not a protocol gap", Facts{Frames: 1, DecodeErrors: []string{"xdr:DecodeUint: EOF"}, DecodeErrorCount: 1},
			MishandledIndex, "decode error"},
		{"mixed decode errors count as mishandled", Facts{Frames: 2, DecodeErrorCount: 2,
			DecodeErrors: []string{msgP30OnOldSDK, "xdr:DecodeUint: EOF"}}, MishandledIndex, "EOF"},
		{"decode count without messages", Facts{Frames: 1, DecodeErrorCount: 1}, MishandledIndex, "no message"},
		{"indexer error", with(func(f *Facts) {
			f.IndexErrors, f.IndexErrorCount = []string{"processing transactions for ledger 4: boom"}, 1
		}),
			MishandledIndex, "indexer error"},
		{"recovered panic", with(func(f *Facts) { f.Panics, f.PanicCount = []string{"index out of range"}, 1 }),
			MishandledIndex, "panic"},
		{"crash", with(func(f *Facts) { f.Crash = "fatal error: concurrent map writes" }), MishandledIndex, "crashed"},
		{"panic outranks persist error", with(func(f *Facts) {
			f.Panics, f.PanicCount = []string{"nil map"}, 1
			f.PersistErrors, f.PersistErrorCount = []string{msg729}, 1
		}), MishandledIndex, "panic"},
		{"missing transaction", with(func(f *Facts) { f.TxIndexed = 0 }), Dropped, "0 of 1 transactions"},
		{"missing operation", with(func(f *Facts) { f.OpsIndexed = 2 }), Dropped, "2 of 3 operations"},
		{"nothing processed", Facts{}, Dropped, "no ledger"},
	}
	for _, c := range cases {
		got, reason := Classify(c.facts)
		if got != c.want || !strings.Contains(reason, c.reason) {
			t.Errorf("%s: got %s (%q), want %s containing %q", c.name, got, reason, c.want, c.reason)
		}
	}
}

func TestParseVerdict(t *testing.T) {
	for _, v := range Verdicts {
		if got, err := ParseVerdict(string(v)); err != nil || got != v {
			t.Errorf("ParseVerdict(%q) = %q, %v", v, got, err)
		}
	}
	if _, err := ParseVerdict("mishandled"); err == nil {
		t.Error("ParseVerdict accepted a verdict without its stage")
	}
}
