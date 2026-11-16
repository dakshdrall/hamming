// hamming-mutate applies Hamming's mutation rules to stellar-core goldens and writes each output with a JSON
// provenance sidecar (synthetic: true, rule, source golden, core commit).
//
//	hamming-mutate -golden <core checkout>/test-lcm-current -core-commit <sha> -out corpus/mutated [-rule id]
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dakshdrall/hamming/go/mutate"
)

func main() {
	golden := flag.String("golden", "", "stellar-core test-lcm-current directory")
	commit := flag.String("core-commit", "", "full stellar-core commit SHA the goldens come from")
	out := flag.String("out", "", "output directory")
	only := flag.String("rule", "", "run only this rule id (default: all)")
	pass := flag.String("passphrase", mutate.CorePassphrase, "network passphrase of the goldens")
	flag.Parse()
	if *golden == "" || len(*commit) != 40 || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: hamming-mutate -golden DIR -core-commit FULL_SHA -out DIR [-rule ID]")
		os.Exit(2)
	}
	rules := mutate.Rules
	if *only != "" {
		r, ok := mutate.Lookup(*only)
		if !ok {
			fmt.Fprintf(os.Stderr, "hamming-mutate: unknown rule %q\n", *only)
			os.Exit(2)
		}
		rules = []*mutate.Rule{r}
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}
	for _, r := range rules {
		res, err := mutate.Run(r, *golden, *pass)
		if err != nil {
			fail(err)
		}
		side, err := res.Sidecar(*golden, *commit, *pass)
		if err != nil {
			fail(err)
		}
		// no HTML escaping: atom names such as payload<max:64> should read as written
		var j bytes.Buffer
		enc := json.NewEncoder(&j)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(side); err != nil {
			fail(err)
		}
		base := filepath.Join(*out, res.Name())
		if err := os.WriteFile(base+".xdr", res.Output, 0o644); err != nil {
			fail(err)
		}
		if err := os.WriteFile(base+".json", j.Bytes(), 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("rule %d %-28s %s  <- %s (ledger %d, op %d)\n", r.Number, r.ID, side.SHA256[:12], res.SourcePath, res.LedgerSeq, res.OpIndex)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "hamming-mutate:", err)
	os.Exit(1)
}
