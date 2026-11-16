// Package mutate derives rare but valid ledgers from stellar-core's golden LedgerCloseMeta files.
//
// A rule rewrites one structure of one successful transaction to a rare shape, in the envelope and, where the
// shape is also recorded there, in the meta. The transaction is then re-hashed and its result's hash updated,
// so the meta still matches its envelope. Output is XDR-valid and re-decodes, but signatures and the ledger
// header's txSetHash no longer match, so every output is flagged synthetic until a local-core capture
// replaces it.
package mutate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// CorePassphrase is stellar-core's test network passphrase (src/test/test.cpp), which the goldens' hashes use.
const CorePassphrase = "(V) (;,,;) (V)"

// ErrNoCandidate means no transaction in the goldens fits the rule.
var ErrNoCandidate = errors.New("no candidate transaction in the goldens")

// Candidate is one successful, non-fee-bump transaction a rule may rewrite. Pointers alias the decoded ledger.
type Candidate struct {
	LCM  *xdr.LedgerCloseMeta
	Env  *xdr.TransactionEnvelope
	Meta *xdr.TransactionMeta
}

// Rule rewrites a candidate. Apply must leave the candidate untouched when it returns ok == false.
type Rule struct {
	ID      string
	Number  int
	Summary string
	// Level is the corpus level of the output: structural, value or relational (docs/manifest.md).
	Level string
	// Caps are the CAPs the output exercises.
	Caps []string
	// Method is recorded in the sidecar: what was rewritten and what was preserved.
	Method string
	// Atoms are coverage atoms (python/xdr_coverage.py names) the output must contain.
	Atoms []string
	// NotPreserved lists what the mutation knowingly leaves inconsistent.
	NotPreserved []string
	Apply        func(c *Candidate) (op int, details map[string]any, ok bool)
}

// Result is a successful mutation.
type Result struct {
	Rule       *Rule
	SourcePath string // golden file, relative to the golden root
	Output     []byte // one record-marked LedgerCloseMeta
	LedgerSeq  uint32
	Protocol   uint32
	OpIndex    int
	OldTxHash  string
	NewTxHash  string
	Details    map[string]any
}

// Run applies the rule to the first fitting transaction in the goldens under root, in sorted file order, frame
// order and transaction-set order, so the same goldens always give the same output.
func Run(r *Rule, root, passphrase string) (*Result, error) {
	files, err := goldenFiles(root)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		frames, err := Frames(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for _, fr := range frames {
			var lcm xdr.LedgerCloseMeta
			if err := xdr.SafeUnmarshal(fr, &lcm); err != nil {
				continue // another protocol's golden; not a candidate
			}
			byHash := map[xdr.Hash]applied{}
			for _, a := range processing(&lcm) {
				byHash[a.Result.TransactionHash] = a
			}
			for _, env := range envelopes(&lcm) {
				if env.Type == xdr.EnvelopeTypeEnvelopeTypeTxFeeBump {
					continue // the inner result pair would need re-hashing too
				}
				oldH, err := HashEnvelope(*env, passphrase)
				if err != nil {
					continue
				}
				a, ok := byHash[oldH]
				if !ok || !a.Result.Result.Successful() {
					continue
				}
				op, details, ok := r.Apply(&Candidate{LCM: &lcm, Env: env, Meta: a.Meta})
				if !ok {
					continue
				}
				newH, err := HashEnvelope(*env, passphrase)
				if err != nil {
					return nil, err
				}
				a.Result.TransactionHash = newH
				out, err := lcm.MarshalBinary()
				if err != nil {
					return nil, err
				}
				var rt xdr.LedgerCloseMeta
				if err := xdr.SafeUnmarshal(out, &rt); err != nil {
					return nil, fmt.Errorf("rule %s: output does not decode: %w", r.ID, err)
				}
				if err := CheckHashes(&rt, passphrase); err != nil {
					return nil, fmt.Errorf("rule %s: %w", r.ID, err)
				}
				rel, _ := filepath.Rel(root, f)
				return &Result{Rule: r, SourcePath: rel, Output: Frame(out), LedgerSeq: lcm.LedgerSequence(),
					Protocol: lcm.ProtocolVersion(), OpIndex: op, OldTxHash: hex.EncodeToString(oldH[:]),
					NewTxHash: hex.EncodeToString(newH[:]), Details: details}, nil
			}
		}
	}
	return nil, fmt.Errorf("rule %s: %w", r.ID, ErrNoCandidate)
}

// Sidecar is the JSON written next to each output.
type Sidecar struct {
	ID                string         `json:"id"`
	Rule              string         `json:"rule"`
	RuleNumber        int            `json:"rule_number"`
	Summary           string         `json:"summary"`
	Level             string         `json:"level"`
	Caps              []string       `json:"caps"`
	Synthetic         bool           `json:"synthetic"`
	Method            string         `json:"method"`
	Source            Source         `json:"source"`
	NetworkPassphrase string         `json:"network_passphrase"`
	LedgerSeq         uint32         `json:"ledger_seq"`
	Protocol          uint32         `json:"protocol"`
	OpIndex           int            `json:"op_index"`
	OldTxHash         string         `json:"old_tx_hash"`
	NewTxHash         string         `json:"new_tx_hash"`
	Atoms             []string       `json:"atoms"`
	NotPreserved      []string       `json:"not_preserved"`
	Details           map[string]any `json:"details"`
	SHA256            string         `json:"sha256"`
	Bytes             int            `json:"bytes"`
}

// Source identifies the golden file a mutation started from.
type Source struct {
	Kind   string `json:"kind"`
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Sidecar builds the provenance record. goldenDir is the directory holding SourcePath; its base name and the
// core commit locate the file in stellar-core.
func (res *Result) Sidecar(goldenDir, coreCommit, passphrase string) (Sidecar, error) {
	src, err := os.ReadFile(filepath.Join(goldenDir, res.SourcePath))
	if err != nil {
		return Sidecar{}, err
	}
	srcSum, outSum := sha256.Sum256(src), sha256.Sum256(res.Output)
	return Sidecar{
		ID: res.Name(), Rule: res.Rule.ID, RuleNumber: res.Rule.Number, Summary: res.Rule.Summary,
		Level: res.Rule.Level, Caps: res.Rule.Caps, Synthetic: true, Method: res.Rule.Method,
		Source: Source{Kind: "core-golden", Repo: "stellar/stellar-core", Commit: coreCommit,
			Path: filepath.ToSlash(filepath.Join(filepath.Base(goldenDir), res.SourcePath)), SHA256: hex.EncodeToString(srcSum[:])},
		NetworkPassphrase: passphrase, LedgerSeq: res.LedgerSeq, Protocol: res.Protocol, OpIndex: res.OpIndex,
		OldTxHash: res.OldTxHash, NewTxHash: res.NewTxHash, Atoms: res.Rule.Atoms, NotPreserved: res.Rule.NotPreserved,
		Details: res.Details, SHA256: hex.EncodeToString(outSum[:]), Bytes: len(res.Output),
	}, nil
}

// Name is the output's file stem: rule id and protocol.
func (res *Result) Name() string { return fmt.Sprintf("%s-p%d", res.Rule.ID, res.Protocol) }

// ---- rules

var notPreservedCommon = []string{
	"transaction signatures (they sign the original transaction)",
	"the ledger header's scpValue.txSetHash (it hashes the original transaction set)",
}

// payloadByte fills signed payloads; any fixed value works, this one is easy to spot in hex dumps.
const payloadByte = 0xAB

// Rules is the rule table, in the order hamming-mutate runs it.
var Rules = []*Rule{
	{
		ID: "cap40-signed-payload-signer", Number: 1,
		Level: "structural", Caps: []string{"CAP-0040"},
		Summary: "ed25519 signer -> CAP-40 signed-payload signer over the same key (32-byte payload)",
		Method: "core-golden mutation: a successful SetOptions that adds an ed25519 signer is rewritten to add a CAP-40 " +
			"signed-payload signer over the same key with a 32-byte payload, in the envelope and in the operation's " +
			"account-entry changes; tx hash recomputed with the core test passphrase",
		Atoms:        []string{"SignerKey|SIGNER_KEY_TYPE_ED25519_SIGNED_PAYLOAD", "SignerKey|SIGNER_KEY_TYPE_ED25519_SIGNED_PAYLOAD.payload<nonempty>"},
		NotPreserved: notPreservedCommon,
		Apply:        signedPayloadSigner(32),
	},
	{
		ID: "muxed-payment-destination", Number: 2,
		Level: "structural", Caps: []string{"CAP-0027"},
		Summary: "G -> M payment destination (muxed id 2^53+1), envelope only",
		Method: "core-golden mutation: the destination of a successful Payment is rewritten from an ed25519 account (G) to " +
			"a muxed account (M) over the same key with id 2^53+1, in the envelope only; tx hash recomputed with the core " +
			"test passphrase",
		// xdr_coverage.py names integer boundary atoms after the typedef (uint64), not the field
		Atoms: []string{"MuxedAccount|KEY_TYPE_MUXED_ED25519", "uint64#2^53+1"},
		NotPreserved: append([]string{"the transaction meta, including any operation events, is unchanged and still " +
			"names the unmuxed destination"}, notPreservedCommon...),
		Apply: muxedPaymentDestination(1<<53 + 1),
	},
	{
		ID: "cap40-signed-payload-len1", Number: 3,
		Level: "structural", Caps: []string{"CAP-0040"},
		Summary:      "CAP-40 signed-payload signer with a 1-byte payload (shortest non-empty)",
		Method:       "core-golden mutation: as rule 1, with a 1-byte payload; tx hash recomputed with the core test passphrase",
		Atoms:        []string{"SignerKey|SIGNER_KEY_TYPE_ED25519_SIGNED_PAYLOAD", "SignerKey|SIGNER_KEY_TYPE_ED25519_SIGNED_PAYLOAD.payload<nonempty>"},
		NotPreserved: notPreservedCommon,
		Apply:        signedPayloadSigner(1),
	},
	{
		ID: "cap40-signed-payload-len64", Number: 3,
		Level: "structural", Caps: []string{"CAP-0040"},
		Summary: "CAP-40 signed-payload signer with a 64-byte payload (the XDR maximum)",
		Method: "core-golden mutation: as rule 1, with a 64-byte payload (opaque<64> maximum); tx hash recomputed with the " +
			"core test passphrase",
		Atoms:        []string{"SignerKey|SIGNER_KEY_TYPE_ED25519_SIGNED_PAYLOAD", "SignerKey|SIGNER_KEY_TYPE_ED25519_SIGNED_PAYLOAD.payload<max:64>"},
		NotPreserved: notPreservedCommon,
		Apply:        signedPayloadSigner(64),
	},
	{
		ID: "signer-g-and-p-same-key", Number: 4,
		Level: "relational", Caps: []string{"CAP-0040"},
		Summary: "one account holds a G signer and a P signer over the same ed25519 key",
		Method: "core-golden mutation: a successful SetOptions adds an ed25519 signer K2 to an account that already has an " +
			"ed25519 signer K1; K2 is rewritten to a CAP-40 signed-payload signer over K1 (32-byte payload) in the envelope " +
			"and in every entry of that account in the ledger's meta, with signers (and signerSponsoringIDs) re-sorted in " +
			"core's SignerKey order; tx hash recomputed with the core test passphrase",
		Atoms:        []string{"SignerKey|SIGNER_KEY_TYPE_ED25519_SIGNED_PAYLOAD", "AccountEntry.signers[len>1]"},
		NotPreserved: notPreservedCommon,
		Apply:        gAndPSameKey(32),
	},
}

// Lookup returns the rule with the given id.
func Lookup(id string) (*Rule, bool) {
	for _, r := range Rules {
		if r.ID == id {
			return r, true
		}
	}
	return nil, false
}

func signedPayloadKey(key xdr.Uint256, n int) xdr.SignerKey {
	return xdr.SignerKey{Type: xdr.SignerKeyTypeSignerKeyTypeEd25519SignedPayload,
		Ed25519SignedPayload: &xdr.SignerKeyEd25519SignedPayload{Ed25519: key, Payload: bytes.Repeat([]byte{payloadByte}, n)}}
}

// strkeyPayloadLen is the length of the raw strkey payload of a signer key's address.
func strkeyPayloadLen(k xdr.SignerKey) (string, int, error) {
	addr, err := k.GetAddress()
	if err != nil {
		return "", 0, err
	}
	raw, err := strkey.Decode(strkey.VersionByteSignedPayload, addr)
	return addr, len(raw), err
}

// signedPayloadSigner is rules 1 and 3: replace the added ed25519 signer with a signed-payload signer over the
// same key, in the envelope and in the operation's account-entry changes.
func signedPayloadSigner(n int) func(c *Candidate) (int, map[string]any, bool) {
	return func(c *Candidate) (int, map[string]any, bool) {
		ops := c.Env.Operations()
		for oi := range ops {
			so := ops[oi].Body.SetOptionsOp
			if so == nil || so.Signer == nil || so.Signer.Key.Type != xdr.SignerKeyTypeSignerKeyTypeEd25519 || so.Signer.Weight == 0 {
				continue
			}
			key := *so.Signer.Key.Ed25519
			newKey := signedPayloadKey(key, n)
			addr, plen, err := strkeyPayloadLen(newKey)
			if err != nil {
				continue
			}
			edits := 0
			changes := opChanges(c.Meta, oi)
			for ci := range changes {
				le := entryOf(&changes[ci])
				if le == nil || le.Data.Account == nil {
					continue
				}
				for si := range le.Data.Account.Signers {
					s := &le.Data.Account.Signers[si]
					if s.Key.Type == xdr.SignerKeyTypeSignerKeyTypeEd25519 && *s.Key.Ed25519 == key {
						s.Key = newKey
						edits++
					}
				}
			}
			if edits == 0 {
				continue
			}
			so.Signer.Key = newKey
			return oi, map[string]any{"signed_payload_signer": addr, "payload_bytes": n,
				"strkey_payload_bytes": plen, "meta_signer_edits": edits}, true
		}
		return 0, nil, false
	}
}

// muxedPaymentDestination is rule 2.
func muxedPaymentDestination(id uint64) func(c *Candidate) (int, map[string]any, bool) {
	return func(c *Candidate) (int, map[string]any, bool) {
		ops := c.Env.Operations()
		for oi := range ops {
			p := ops[oi].Body.PaymentOp
			if p == nil || p.Destination.Type != xdr.CryptoKeyTypeKeyTypeEd25519 {
				continue
			}
			before := p.Destination.Address()
			p.Destination = xdr.MuxedAccount{Type: xdr.CryptoKeyTypeKeyTypeMuxedEd25519,
				Med25519: &xdr.MuxedAccountMed25519{Id: xdr.Uint64(id), Ed25519: *p.Destination.Ed25519}}
			after := p.Destination.Address()
			raw, err := strkey.Decode(strkey.VersionByteMuxedAccount, after)
			if err != nil {
				panic(fmt.Sprintf("muxed address %s does not decode: %v", after, err))
			}
			return oi, map[string]any{"destination_before": before, "destination_after": after, "muxed_id": id,
				"strkey_payload_bytes": len(raw)}, true
		}
		return 0, nil, false
	}
}

// gAndPSameKey is rule 4.
func gAndPSameKey(n int) func(c *Candidate) (int, map[string]any, bool) {
	return func(c *Candidate) (int, map[string]any, bool) {
		ops := c.Env.Operations()
		for oi := range ops {
			so := ops[oi].Body.SetOptionsOp
			if so == nil || so.Signer == nil || so.Signer.Key.Type != xdr.SignerKeyTypeSignerKeyTypeEd25519 || so.Signer.Weight == 0 {
				continue
			}
			k2 := *so.Signer.Key.Ed25519
			// The account the signer is added to: its pre-state (State) lacks K2 and its post-state has it.
			var account *xdr.AccountId
			var k1 *xdr.Uint256
			changes := opChanges(c.Meta, oi)
			for ci := range changes {
				st := changes[ci].State
				if st == nil || st.Data.Account == nil {
					continue
				}
				acc := st.Data.Account
				hasK2 := false
				var first *xdr.Uint256
				for si := range acc.Signers {
					s := acc.Signers[si].Key
					if s.Type != xdr.SignerKeyTypeSignerKeyTypeEd25519 {
						continue
					}
					if *s.Ed25519 == k2 {
						hasK2 = true
					} else if first == nil {
						v := *s.Ed25519
						first = &v
					}
				}
				if !hasK2 && first != nil {
					id := acc.AccountId
					account, k1 = &id, first
					break
				}
			}
			if account == nil {
				continue
			}
			newKey := signedPayloadKey(*k1, n)
			addr, plen, err := strkeyPayloadLen(newKey)
			if err != nil {
				continue
			}
			// Rewrite K2 -> P(K1) in every entry of this account in the ledger. K2 is absent from the account
			// before this operation, so every entry holding it comes from this operation or later.
			edits := 0
			for _, a := range processing(c.LCM) {
				lists := append([]xdr.LedgerEntryChanges{a.Fee}, txChanges(a.Meta)...)
				for _, list := range lists {
					for ci := range list {
						le := entryOf(&list[ci])
						if le == nil || le.Data.Account == nil || !le.Data.Account.AccountId.Equals(*account) {
							continue
						}
						changed := false
						for si := range le.Data.Account.Signers {
							s := &le.Data.Account.Signers[si]
							if s.Key.Type == xdr.SignerKeyTypeSignerKeyTypeEd25519 && *s.Key.Ed25519 == k2 {
								s.Key = newKey
								changed = true
								edits++
							}
						}
						if changed {
							sortSigners(le.Data.Account)
						}
					}
				}
			}
			if edits == 0 {
				continue
			}
			so.Signer.Key = newKey
			g, _ := strkey.Encode(strkey.VersionByteAccountID, k1[:])
			return oi, map[string]any{"account": account.Address(), "g_signer": g, "p_signer": addr,
				"payload_bytes": n, "strkey_payload_bytes": plen, "replaced_ed25519_signer_key": hex.EncodeToString(k2[:]),
				"meta_signer_edits": edits}, true
		}
		return 0, nil, false
	}
}
