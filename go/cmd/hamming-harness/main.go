// hamming-harness replays corpus entries through pinned commits of a target indexer, checks every verdict
// against the target's expectations file, and writes results/harness.json. It exits 1 if any verdict differs
// from its expectation, and 2 if the harness itself cannot run.
//
//	hamming-harness -root <repo> [-cache $HAMMING_CACHE] [-expectations adapters/wallet-backend/expectations.json]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dakshdrall/hamming/go/harness"
)

func main() {
	root := flag.String("root", ".", "repository root")
	cache := flag.String("cache", "", "input cache from scripts/fetch-inputs.sh (default $HAMMING_CACHE, else <root>/.cache)")
	expPath := flag.String("expectations", "adapters/wallet-backend/expectations.json", "expectations file, relative to -root")
	outPath := flag.String("out", "results/harness.json", "report path, relative to -root")
	golden := flag.Bool("golden", true, "run the golden replay")
	flag.Parse()

	met, err := run(*root, *cache, *expPath, *outPath, *golden)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hamming-harness:", err)
		os.Exit(2)
	}
	if !met {
		os.Exit(1)
	}
}

func run(root, cache, expPath, outPath string, golden bool) (bool, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	if cache == "" {
		cache = os.Getenv("HAMMING_CACHE")
	}
	if cache == "" {
		cache = filepath.Join(root, ".cache")
	}
	exp, err := harness.LoadExpectations(filepath.Join(root, expPath))
	if err != nil {
		return false, err
	}
	if exp.Target != "wallet-backend" {
		return false, fmt.Errorf("no adapter for target %q", exp.Target)
	}
	adapter := &harness.WalletBackend{
		Repo:     filepath.Join(cache, "wallet-backend"),
		Cache:    cache,
		Template: filepath.Join(root, "adapters", "wallet-backend", "hamming_adapter_test.go.tmpl"),
		Log:      os.Stderr,
	}
	ctx := context.Background()
	report := &harness.Report{Target: exp.Target, Commits: exp.Commits, Method: harness.Method}

	// Hamming decodes every entry itself first: protocol tags for the report, and a corpus error stops the run
	// instead of being blamed on the target.
	protocols := map[string][]uint32{}
	for _, x := range exp.Entries {
		p, _, err := harness.Inspect(harness.Resolve(x.Entry, root, cache))
		if err != nil {
			return false, err
		}
		protocols[x.Entry] = p
	}

	for _, label := range exp.Labels() {
		var specs []harness.ExpectedEntry
		var entries []harness.Entry
		for _, x := range exp.Entries {
			if _, ok := x.Verdicts[label]; ok {
				specs = append(specs, x)
				entries = append(entries, entry(x.Entry, x.Passphrase, root, cache))
			}
		}
		if len(entries) == 0 {
			continue
		}
		facts, err := adapter.Run(ctx, exp.Commits[label], entries)
		if err != nil {
			return false, err
		}
		for i, f := range facts {
			v, reason := harness.Classify(f)
			report.Entries = append(report.Entries, harness.EntryResult{
				Entry: specs[i].Entry, Issue: specs[i].Issue, Commit: label, Protocols: protocols[specs[i].Entry],
				Expected: specs[i].Verdicts[label], ExpectedReason: specs[i].Reasons[label], Verdict: v,
				Met: specs[i].Met(label, v, reason), Reason: reason,
			})
		}
	}

	if g := exp.Golden; g != nil && golden {
		dir := harness.Resolve(g.Root, root, cache)
		files, err := xdrFiles(dir)
		if err != nil {
			return false, err
		}
		entries := make([]harness.Entry, len(files))
		for i, f := range files {
			entries[i] = harness.Entry{Path: f, Passphrase: harness.CoreTestPassphrase}
		}
		facts, err := adapter.Run(ctx, exp.Commits[g.Commit], entries)
		if err != nil {
			return false, err
		}
		report.Golden = harness.Summarize(*g, facts, func(p string) string {
			if r, err := filepath.Rel(dir, p); err == nil {
				return r
			}
			return p
		})
	}

	// results/ is committed: keep this machine's paths out of it
	scrub := strings.NewReplacer(cache, "$HAMMING_CACHE", root, "$REPO").Replace
	for i := range report.Entries {
		report.Entries[i].Reason = scrub(report.Entries[i].Reason)
	}
	if report.Golden != nil {
		for i := range report.Golden.Unexpected {
			report.Golden.Unexpected[i].Reason = scrub(report.Golden.Unexpected[i].Reason)
		}
	}
	report.Finish()
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(filepath.Join(root, outPath), append(b, '\n'), 0o644); err != nil {
		return false, err
	}
	printSummary(report)
	return report.ExpectationsMet, nil
}

func entry(spec, passphrase, root, cache string) harness.Entry {
	if passphrase == "" {
		passphrase = harness.CoreTestPassphrase
	}
	return harness.Entry{Path: harness.Resolve(spec, root, cache), Passphrase: passphrase}
}

// xdrFiles lists *.xdr under dir in sorted order, skipping dotfiles (macOS AppleDouble "._x.xdr" twins).
func xdrFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".xdr") && !strings.HasPrefix(d.Name(), ".") {
			out = append(out, p)
		}
		return nil
	})
	if err == nil && len(out) == 0 {
		err = fmt.Errorf("no .xdr files under %s; run make inputs", dir)
	}
	sort.Strings(out)
	return out, err
}

func printSummary(r *harness.Report) {
	fmt.Printf("%s harness\n", r.Target)
	for _, e := range r.Entries {
		mark := "ok  "
		if !e.Met {
			mark = "FAIL"
		}
		fmt.Printf("  %s %s  %-22s expected %-22s %s\n", mark, e.Commit, e.Verdict, e.Expected, e.Entry)
	}
	if g := r.Golden; g != nil {
		fmt.Printf("  golden replay at %s (%s): %d files, %d ledgers, %d transactions, %d operations\n",
			g.Commit, g.Root, g.Files, g.Ledgers, g.Transactions, g.Operations)
		fmt.Printf("    verdicts %v; decode errors %d, indexer errors %d, panics %d, persist errors %d, crashes %d; %d values normalized on read-back\n",
			g.Verdicts, g.DecodeErrors, g.IndexErrors, g.Panics, g.PersistErrors, g.Crashes, g.Normalized)
	}
	if r.ExpectationsMet {
		fmt.Println("expectations: all met")
		return
	}
	fmt.Printf("expectations: %d not met\n", len(r.Mismatches))
	for _, m := range r.Mismatches {
		fmt.Println("  -", m)
	}
}
