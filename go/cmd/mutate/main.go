// Golden mutation (prototype): derive a CAP-40 signed-payload-signer ledger from a stellar-core golden
// LedgerCloseMeta. Finds a successful SetOptions that adds an ed25519 signer, rewrites the signer to a
// signed-payload signer over the same key in BOTH the envelope and the operation's ledger-entry changes,
// and re-hashes the transaction so the meta still matches its envelope. Output is flagged synthetic.
//
//   go run ./cmd/mutate <golden-root> <out.xdr> <out.json>
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stellar/go-stellar-sdk/network"
	"github.com/stellar/go-stellar-sdk/xdr"
)

const corePassphrase = "(V) (;,,;) (V)" // stellar-core src/test/test.cpp

func frames(b []byte) [][]byte {
	var out [][]byte
	for i := 0; i < len(b); {
		n := int(binary.BigEndian.Uint32(b[i:]) & 0x7fffffff)
		i += 4
		out = append(out, b[i:i+n])
		i += n
	}
	return out
}

func envelopes(lcm *xdr.LedgerCloseMeta) []*xdr.TransactionEnvelope {
	var out []*xdr.TransactionEnvelope
	var gts *xdr.GeneralizedTransactionSet
	switch lcm.V {
	case 0:
		for i := range lcm.V0.TxSet.Txs {
			out = append(out, &lcm.V0.TxSet.Txs[i])
		}
		return out
	case 1:
		gts = &lcm.V1.TxSet
	case 2:
		gts = &lcm.V2.TxSet
	}
	for pi := range gts.V1TxSet.Phases {
		ph := &gts.V1TxSet.Phases[pi]
		if ph.V0Components != nil {
			for ci := range *ph.V0Components {
				c := &(*ph.V0Components)[ci]
				if c.TxsMaybeDiscountedFee != nil {
					for ti := range c.TxsMaybeDiscountedFee.Txs {
						out = append(out, &c.TxsMaybeDiscountedFee.Txs[ti])
					}
				}
			}
		}
		if ph.ParallelTxsComponent != nil {
			for si := range ph.ParallelTxsComponent.ExecutionStages {
				for ki := range ph.ParallelTxsComponent.ExecutionStages[si] {
					cl := ph.ParallelTxsComponent.ExecutionStages[si][ki]
					for ti := range cl {
						out = append(out, &cl[ti])
					}
				}
			}
		}
	}
	return out
}

// processing returns (pointer to tx hash, tx meta, success) for the tx with hash h.
func processing(lcm *xdr.LedgerCloseMeta, h xdr.Hash) (*xdr.Hash, *xdr.TransactionMeta, bool) {
	ok := func(r xdr.TransactionResult) bool { return r.Successful() }
	switch lcm.V {
	case 0:
		for i := range lcm.V0.TxProcessing {
			p := &lcm.V0.TxProcessing[i]
			if p.Result.TransactionHash == h {
				return &p.Result.TransactionHash, &p.TxApplyProcessing, ok(p.Result.Result)
			}
		}
	case 1:
		for i := range lcm.V1.TxProcessing {
			p := &lcm.V1.TxProcessing[i]
			if p.Result.TransactionHash == h {
				return &p.Result.TransactionHash, &p.TxApplyProcessing, ok(p.Result.Result)
			}
		}
	case 2:
		for i := range lcm.V2.TxProcessing {
			p := &lcm.V2.TxProcessing[i]
			if p.Result.TransactionHash == h {
				return &p.Result.TransactionHash, &p.TxApplyProcessing, ok(p.Result.Result)
			}
		}
	}
	return nil, nil, false
}

func opChanges(m *xdr.TransactionMeta, opIdx int) xdr.LedgerEntryChanges {
	switch m.V {
	case 3:
		if opIdx < len(m.V3.Operations) {
			return m.V3.Operations[opIdx].Changes
		}
	case 4:
		if opIdx < len(m.V4.Operations) {
			return m.V4.Operations[opIdx].Changes
		}
	}
	return nil
}

func entryOf(c *xdr.LedgerEntryChange) *xdr.LedgerEntry {
	switch {
	case c.Created != nil:
		return c.Created
	case c.Updated != nil:
		return c.Updated
	case c.State != nil:
		return c.State
	case c.Restored != nil:
		return c.Restored
	}
	return nil
}

type report struct {
	SourceFile, OldTxHash, NewTxHash, SignedPayloadSigner string
	LedgerSeq                                              uint32
	OpIndex, EnvelopeEdits, MetaSignerEdits                int
	Synthetic                                              bool
	Method                                                 string
}

func main() {
	root, outX, outJ := os.Args[1], os.Args[2], os.Args[3]
	var files []string
	filepath.Walk(root, func(p string, i os.FileInfo, e error) error {
		if e == nil && strings.HasSuffix(p, ".xdr") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	payload := bytes.Repeat([]byte{0xAB}, 32)
	for _, f := range files {
		b, _ := os.ReadFile(f)
		for _, fr := range frames(b) {
			var lcm xdr.LedgerCloseMeta
			if xdr.SafeUnmarshal(fr, &lcm) != nil {
				continue
			}
			for _, env := range envelopes(&lcm) {
				ops := env.Operations()
				for oi := range ops {
					so := ops[oi].Body.SetOptionsOp
					if so == nil || so.Signer == nil || so.Signer.Key.Type != xdr.SignerKeyTypeSignerKeyTypeEd25519 || so.Signer.Weight == 0 {
						continue
					}
					oldH, err := network.HashTransactionInEnvelope(*env, corePassphrase)
					if err != nil {
						continue
					}
					hp, meta, success := processing(&lcm, xdr.Hash(oldH))
					if hp == nil || !success {
						continue
					}
					key := *so.Signer.Key.Ed25519
					newKey := xdr.SignerKey{Type: xdr.SignerKeyTypeSignerKeyTypeEd25519SignedPayload,
						Ed25519SignedPayload: &xdr.SignerKeyEd25519SignedPayload{Ed25519: key, Payload: payload}}
					// meta first: the account's signer list after this operation
					metaEdits := 0
					changes := opChanges(meta, oi)
					for ci := range changes {
						le := entryOf(&changes[ci])
						if le == nil || le.Data.Account == nil {
							continue
						}
						for si := range le.Data.Account.Signers {
							s := &le.Data.Account.Signers[si]
							if s.Key.Type == xdr.SignerKeyTypeSignerKeyTypeEd25519 && *s.Key.Ed25519 == key {
								s.Key = newKey
								metaEdits++
							}
						}
					}
					if metaEdits == 0 {
						continue
					}
					so.Signer.Key = newKey
					newH, err := network.HashTransactionInEnvelope(*env, corePassphrase)
					if err != nil {
						panic(err)
					}
					*hp = xdr.Hash(newH)
					out, err := lcm.MarshalBinary()
					if err != nil {
						panic(err)
					}
					var rt xdr.LedgerCloseMeta
					if err := xdr.SafeUnmarshal(out, &rt); err != nil {
						panic(fmt.Sprintf("mutated LCM does not decode: %v", err))
					}
					framed := make([]byte, 4+len(out))
					binary.BigEndian.PutUint32(framed, 0x80000000|uint32(len(out)))
					copy(framed[4:], out)
					if err := os.WriteFile(outX, framed, 0o644); err != nil {
						panic(err)
					}
					p, _ := newKey.GetAddress()
					rep := report{SourceFile: strings.TrimPrefix(f, root+"/"), OldTxHash: hex.EncodeToString(oldH[:]),
						NewTxHash: hex.EncodeToString(newH[:]), SignedPayloadSigner: p, LedgerSeq: lcm.LedgerSequence(),
						OpIndex: oi, EnvelopeEdits: 1, MetaSignerEdits: metaEdits, Synthetic: true,
						Method: "core-golden mutation: ed25519 signer -> CAP-40 signed-payload signer (same key, 32-byte payload) in envelope and op meta; tx hash recomputed with the core test passphrase"}
					j, _ := json.MarshalIndent(rep, "", " ")
					os.WriteFile(outJ, j, 0o644)
					fmt.Println(string(j))
					return
				}
			}
		}
	}
	fmt.Println("no candidate found")
	os.Exit(1)
}
