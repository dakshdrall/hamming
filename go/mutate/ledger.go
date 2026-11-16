package mutate

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stellar/go-stellar-sdk/network"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// Frames splits a record-marked XDR stream (4-byte big-endian length, high bit set) into frames.
func Frames(b []byte) ([][]byte, error) {
	var out [][]byte
	for i := 0; i < len(b); {
		if len(b)-i < 4 {
			return nil, fmt.Errorf("truncated record mark at byte %d", i)
		}
		n := int(binary.BigEndian.Uint32(b[i:]) & 0x7fffffff)
		i += 4
		if n > len(b)-i {
			return nil, fmt.Errorf("truncated frame at byte %d", i)
		}
		out = append(out, b[i:i+n])
		i += n
	}
	return out, nil
}

// Frame adds a record mark to one XDR value.
func Frame(b []byte) []byte {
	out := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(out, 0x80000000|uint32(len(b)))
	copy(out[4:], b)
	return out
}

// goldenFiles lists *.xdr under root in sorted order, skipping dotfiles (macOS AppleDouble twins).
func goldenFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".xdr") && !strings.HasPrefix(d.Name(), ".") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// envelopes returns pointers to every transaction envelope in the ledger's transaction set.
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

// applied is one transaction's result and meta, in apply order.
type applied struct {
	Result *xdr.TransactionResultPair
	Meta   *xdr.TransactionMeta
	Fee    xdr.LedgerEntryChanges
}

func processing(lcm *xdr.LedgerCloseMeta) []applied {
	var out []applied
	switch lcm.V {
	case 0:
		for i := range lcm.V0.TxProcessing {
			p := &lcm.V0.TxProcessing[i]
			out = append(out, applied{&p.Result, &p.TxApplyProcessing, p.FeeProcessing})
		}
	case 1:
		for i := range lcm.V1.TxProcessing {
			p := &lcm.V1.TxProcessing[i]
			out = append(out, applied{&p.Result, &p.TxApplyProcessing, p.FeeProcessing})
		}
	case 2:
		for i := range lcm.V2.TxProcessing {
			p := &lcm.V2.TxProcessing[i]
			out = append(out, applied{&p.Result, &p.TxApplyProcessing, p.FeeProcessing})
		}
	}
	return out
}

// opChanges returns operation opIdx's ledger-entry changes (a slice sharing the meta's backing array).
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

// txChanges returns every change list of one transaction's meta (before, each operation, after).
func txChanges(m *xdr.TransactionMeta) []xdr.LedgerEntryChanges {
	var out []xdr.LedgerEntryChanges
	switch m.V {
	case 1:
		out = append(out, m.V1.TxChanges)
		for _, op := range m.V1.Operations {
			out = append(out, op.Changes)
		}
	case 2:
		out = append(out, m.V2.TxChangesBefore)
		for _, op := range m.V2.Operations {
			out = append(out, op.Changes)
		}
		out = append(out, m.V2.TxChangesAfter)
	case 3:
		out = append(out, m.V3.TxChangesBefore)
		for _, op := range m.V3.Operations {
			out = append(out, op.Changes)
		}
		out = append(out, m.V3.TxChangesAfter)
	case 4:
		out = append(out, m.V4.TxChangesBefore)
		for _, op := range m.V4.Operations {
			out = append(out, op.Changes)
		}
		out = append(out, m.V4.TxChangesAfter)
	}
	return out
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

// HashEnvelope is the transaction hash core records in the result for this envelope.
func HashEnvelope(env xdr.TransactionEnvelope, passphrase string) (xdr.Hash, error) {
	h, err := network.HashTransactionInEnvelope(env, passphrase)
	return xdr.Hash(h), err
}

// CheckHashes verifies that the envelopes and results of a ledger still describe the same transactions: the
// multiset of envelope hashes equals the multiset of result hashes, and each fee bump's inner result pair
// carries its inner transaction's hash.
func CheckHashes(lcm *xdr.LedgerCloseMeta, passphrase string) error {
	count := map[xdr.Hash]int{}
	inner := map[xdr.Hash]xdr.Hash{} // fee-bump hash -> inner tx hash
	for _, env := range envelopes(lcm) {
		h, err := HashEnvelope(*env, passphrase)
		if err != nil {
			return err
		}
		count[h]++
		if env.Type == xdr.EnvelopeTypeEnvelopeTypeTxFeeBump {
			ih, err := network.HashTransaction(env.FeeBump.Tx.InnerTx.V1.Tx, passphrase)
			if err != nil {
				return err
			}
			inner[h] = xdr.Hash(ih)
		}
	}
	for _, a := range processing(lcm) {
		count[a.Result.TransactionHash]--
		if want, ok := inner[a.Result.TransactionHash]; ok {
			pair, ok := a.Result.Result.Result.GetInnerResultPair()
			if !ok || pair.TransactionHash != want {
				return fmt.Errorf("fee bump %x: inner result hash does not match its inner transaction", a.Result.TransactionHash)
			}
		}
	}
	for h, n := range count {
		if n != 0 {
			return fmt.Errorf("transaction hash %x: %d more envelopes than results", h, n)
		}
	}
	return nil
}

// signerKeyLess is stellar-core's SignerKey operator< (xdrpp): discriminant first, then the arm's fields in
// order, with variable-length opaque compared like std::vector (bytewise, shorter prefix first).
func signerKeyLess(a, b xdr.SignerKey) bool {
	if a.Type != b.Type {
		return a.Type < b.Type
	}
	switch a.Type {
	case xdr.SignerKeyTypeSignerKeyTypeEd25519:
		return bytes.Compare(a.Ed25519[:], b.Ed25519[:]) < 0
	case xdr.SignerKeyTypeSignerKeyTypePreAuthTx:
		return bytes.Compare(a.PreAuthTx[:], b.PreAuthTx[:]) < 0
	case xdr.SignerKeyTypeSignerKeyTypeHashX:
		return bytes.Compare(a.HashX[:], b.HashX[:]) < 0
	case xdr.SignerKeyTypeSignerKeyTypeEd25519SignedPayload:
		pa, pb := a.Ed25519SignedPayload, b.Ed25519SignedPayload
		if c := bytes.Compare(pa.Ed25519[:], pb.Ed25519[:]); c != 0 {
			return c < 0
		}
		return bytes.Compare(pa.Payload, pb.Payload) < 0
	}
	return false
}

// sortSigners restores core's signer order, moving each signerSponsoringIDs element with its signer.
func sortSigners(acc *xdr.AccountEntry) {
	var ids []xdr.SponsorshipDescriptor
	if v2, ok := acc.Ext.GetV1(); ok {
		if e2, ok := v2.Ext.GetV2(); ok {
			ids = e2.SignerSponsoringIDs
		}
	}
	idx := make([]int, len(acc.Signers))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool { return signerKeyLess(acc.Signers[idx[i]].Key, acc.Signers[idx[j]].Key) })
	signers := make([]xdr.Signer, len(idx))
	for i, k := range idx {
		signers[i] = acc.Signers[k]
	}
	copy(acc.Signers, signers)
	if len(ids) == len(idx) {
		sorted := make([]xdr.SponsorshipDescriptor, len(idx))
		for i, k := range idx {
			sorted[i] = ids[k]
		}
		copy(ids, sorted)
	}
}
