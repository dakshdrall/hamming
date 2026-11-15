package indexer

// Hamming reference adapter (prototype): replays stellar-core's committed golden
// LedgerCloseMeta files through wallet-backend's real indexer, in-process, no DB.
// Run: HAMMING_GOLDEN=/path/to/test-lcm-current go test ./internal/indexer -run TestHammingGolden -v

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alitto/pond/v2"
	"github.com/stellar/go-stellar-sdk/xdr"
)

const coreTestPassphrase = "(V) (;,,;) (V)" // stellar-core src/test/test.cpp

func readFrames(path string) ([][]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for i := 0; i < len(b); {
		n := int(binary.BigEndian.Uint32(b[i:]) & 0x7fffffff)
		i += 4
		out = append(out, b[i:i+n])
		i += n
	}
	return out, nil
}

type hammingResult struct {
	Files, Ledgers, OK int
	Errors, Panics     map[string]int
	Examples           map[string]string
	Participants       int
	Seconds            float64
}

func TestHammingGolden(t *testing.T) {
	root := os.Getenv("HAMMING_GOLDEN")
	if root == "" {
		t.Skip("HAMMING_GOLDEN not set")
	}
	pool := pond.NewPool(runtime.NumCPU())
	res := hammingResult{Errors: map[string]int{}, Panics: map[string]int{}, Examples: map[string]string{}}
	var files []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(p, ".xdr") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	start := time.Now()
	for _, f := range files {
		res.Files++
		frames, err := readFrames(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, fr := range frames {
			var lcm xdr.LedgerCloseMeta
			if err := xdr.SafeUnmarshal(fr, &lcm); err != nil {
				res.Errors["decode: "+err.Error()]++
				continue
			}
			res.Ledgers++
			func() {
				defer func() {
					if r := recover(); r != nil {
						key := fmt.Sprint(r)
						if len(key) > 120 {
							key = key[:120]
						}
						res.Panics[key]++
						if _, ok := res.Examples["panic: "+key]; !ok {
							res.Examples["panic: "+key] = f
						}
					}
				}()
				idx, err := NewIndexer(coreTestPassphrase, pool, nil, true)
				if err != nil {
					t.Fatal(err)
				}
				n, err := ProcessLedger(context.Background(), coreTestPassphrase, lcm, idx, NewIndexerBuffer())
				if err != nil {
					msg := err.Error()
					// normalise ledger numbers / hashes so identical failures group together
					key := msg
					if i := strings.Index(key, ": "); i > 0 && strings.HasPrefix(key, "getting transactions for ledger") || strings.HasPrefix(key, "processing transactions for ledger") {
						key = key[strings.Index(key, ": ")+2:]
					}
					if len(key) > 140 {
						key = key[:140]
					}
					res.Errors[key]++
					if _, ok := res.Examples[key]; !ok {
						res.Examples[key] = f
					}
					return
				}
				res.OK++
				res.Participants += n
			}()
		}
	}
	res.Seconds = time.Since(start).Seconds()
	out, _ := json.MarshalIndent(res, "", " ")
	_ = os.WriteFile(filepath.Join(os.TempDir(), "hamming_wallet_backend.json"), out, 0o644)
	fmt.Println(string(out))
}
