package harness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Lines the in-package adapter prints, one pair per entry.
const (
	beginMarker = "HAMMING_BEGIN "
	factsMarker = "HAMMING_FACTS "
)

// GoTestRun is what one `go test -json` invocation of an adapter produced.
type GoTestRun struct {
	Facts       []Facts  // one per completed entry, in output order
	Begun       []string // entry paths the adapter started, in order
	BuildFailed bool
	BuildOutput string
	Passed      bool   // the package's final action was "pass"
	Tail        string // last part of the test output, for diagnosing a crash
}

// testEvent is the subset of cmd/test2json's event format the harness reads.
type testEvent struct {
	Action     string
	Package    string
	ImportPath string // set on build-output and build-fail events
	Test       string
	Output     string
}

const tailBytes = 4000

// ParseGoTestJSON reads the event stream of `go test -json`. Output events are joined before splitting into
// lines, because test2json may split one long line across several events.
func ParseGoTestJSON(r io.Reader) (*GoTestRun, error) {
	run := &GoTestRun{}
	var pending, tail, build strings.Builder
	handleLine := func(line string) error {
		switch {
		case strings.HasPrefix(line, beginMarker):
			run.Begun = append(run.Begun, strings.TrimPrefix(line, beginMarker))
		case strings.HasPrefix(line, factsMarker):
			var f Facts
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, factsMarker)), &f); err != nil {
				return fmt.Errorf("adapter printed malformed facts: %w", err)
			}
			run.Facts = append(run.Facts, f)
		}
		return nil
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		raw := sc.Bytes()
		if len(raw) == 0 || raw[0] != '{' {
			// go test prints some build errors as plain text even with -json
			build.Write(raw)
			build.WriteByte('\n')
			continue
		}
		var ev testEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return nil, fmt.Errorf("go test -json: %w in %q", err, raw)
		}
		switch ev.Action {
		case "build-output":
			build.WriteString(ev.Output)
		case "build-fail":
			run.BuildFailed = true
		case "output":
			tail.WriteString(ev.Output)
			pending.WriteString(ev.Output)
			s := pending.String()
			for {
				i := strings.IndexByte(s, '\n')
				if i < 0 {
					break
				}
				if err := handleLine(s[:i]); err != nil {
					return nil, err
				}
				s = s[i+1:]
			}
			pending.Reset()
			pending.WriteString(s)
		case "pass", "fail", "skip":
			if ev.Test == "" {
				run.Passed = ev.Action == "pass"
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if pending.Len() > 0 {
		if err := handleLine(pending.String()); err != nil {
			return nil, err
		}
	}
	run.BuildOutput = build.String()
	t := tail.String()
	if len(t) > tailBytes {
		t = t[len(t)-tailBytes:]
	}
	run.Tail = t
	return run, nil
}

// Unfinished returns the entry the adapter began but never reported, if any: the entry a crash happened on.
func (r *GoTestRun) Unfinished() (string, bool) {
	if len(r.Begun) > len(r.Facts) {
		return r.Begun[len(r.Facts)], true
	}
	return "", false
}
