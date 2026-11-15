// Go decoder: byte-exact round-trip of every golden LedgerCloseMeta.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stellar/go-stellar-sdk/xdr"
)

func main() {
	var files []string
	filepath.Walk(os.Args[1], func(p string, i os.FileInfo, e error) error {
		if e == nil && strings.HasSuffix(p, ".xdr") { files = append(files, p) }
		return nil
	})
	n, bad, total := 0, 0, 0
	t0 := time.Now()
	for _, f := range files {
		b, _ := os.ReadFile(f)
		for i := 0; i < len(b); {
			l := int(binary.BigEndian.Uint32(b[i:]) & 0x7fffffff); i += 4
			fr := b[i : i+l]; i += l
			var m xdr.LedgerCloseMeta
			if err := xdr.SafeUnmarshal(fr, &m); err != nil { bad++; continue }
			out, err := m.MarshalBinary()
			if err != nil || !bytes.Equal(out, fr) { bad++ }
			n++; total += l
		}
	}
	s := time.Since(t0).Seconds()
	fmt.Printf("go-stellar-sdk: %d LCMs, %.1f MB, mismatches %d, %.2fs, %.0f LCM/s, %.1f MB/s\n", n, float64(total)/1e6, bad, s, float64(n)/s, float64(total)/1e6/s)
}
