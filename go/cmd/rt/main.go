// Go decoder: byte-exact round-trip of every golden LedgerCloseMeta with go-stellar-sdk.
//
//	rt [-json result.json] <dir>
//
// -json writes the decoder's row for hamming-verify's agreement table (results/decoders.json).
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/stellar/go-stellar-sdk/xdr"
)

type row struct {
	Decoder      string `json:"decoder"`
	Version      string `json:"version"`
	Files        int    `json:"files"`
	Frames       int    `json:"frames"`
	Bytes        int    `json:"bytes"`
	Mismatches   int    `json:"mismatches"`
	DecodeErrors int    `json:"decode_errors"`
}

func sdkVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, m := range bi.Deps {
			if m.Path == "github.com/stellar/go-stellar-sdk" {
				return m.Version
			}
		}
	}
	return "unknown"
}

func main() {
	out := flag.String("json", "", "write the result row to this file")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: rt [-json result.json] <dir>")
		os.Exit(2)
	}
	var files []string
	filepath.Walk(flag.Arg(0), func(p string, i os.FileInfo, e error) error {
		// skip dotfiles: macOS writes AppleDouble "._x.xdr" twins on non-APFS volumes
		if e == nil && !i.IsDir() && strings.HasSuffix(p, ".xdr") && !strings.HasPrefix(i.Name(), ".") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	r := row{Decoder: "go-stellar-sdk", Version: sdkVersion()}
	t0 := time.Now()
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		r.Files++
		for i := 0; i < len(b); {
			if len(b)-i < 4 {
				r.DecodeErrors++
				break
			}
			l := int(binary.BigEndian.Uint32(b[i:]) & 0x7fffffff)
			i += 4
			if l > len(b)-i {
				r.DecodeErrors++
				break
			}
			fr := b[i : i+l]
			i += l
			r.Frames++
			r.Bytes += l
			var m xdr.LedgerCloseMeta
			if err := xdr.SafeUnmarshal(fr, &m); err != nil {
				r.DecodeErrors++
				continue
			}
			if enc, err := m.MarshalBinary(); err != nil || !bytes.Equal(enc, fr) {
				r.Mismatches++
			}
		}
	}
	s := time.Since(t0).Seconds()
	fmt.Printf("go-stellar-sdk %s: %d files, %d frames, %.1f MB, mismatches %d, decode errors %d, %.2fs, %.0f frames/s\n",
		r.Version, r.Files, r.Frames, float64(r.Bytes)/1e6, r.Mismatches, r.DecodeErrors, s, float64(r.Frames)/s)
	if *out != "" {
		j, _ := json.MarshalIndent(r, "", "  ")
		if err := os.WriteFile(*out, append(j, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	if r.Mismatches > 0 || r.DecodeErrors > 0 {
		os.Exit(1)
	}
}
