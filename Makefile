# Hamming pipeline. Every file under results/ that a target names is written by that target; nothing in
# results/ is edited by hand.
#
#   make inputs     fetch pinned upstream inputs into $(HAMMING_CACHE)
#   make verify     decoder agreement: rs-stellar-xdr, go-stellar-sdk, js-stellar-sdk round-trip every golden
#   make coverage   grammar-decoder atom coverage and the greedy golden-file set cover
#   make corpus     per-golden-file manifest rows (atoms, sha256, core test) for the corpus builder
#   make harness    replay the corpus through pinned wallet-backend commits and check expectations
#   make conform    SDK conformance matrix
#   make test       unit tests
#   make all        everything above except test, in order

SHELL := bash
.SHELLFLAGS := -euo pipefail -c

HAMMING_CACHE ?= $(CURDIR)/.cache
override HAMMING_CACHE := $(abspath $(HAMMING_CACHE))
export HAMMING_CACHE
PYTHON ?= python3

CORE_P30_COMMIT := f2b91947fb20fc8174cca38bf320eda7baa3edbb
GOLDEN_P30 := $(HAMMING_CACHE)/core-f2b9194/test-lcm-current
XDR := $(HAMMING_CACHE)/stellar-xdr
RESULTS := $(CURDIR)/results

.PHONY: all inputs verify coverage corpus harness conform test

all: inputs verify coverage corpus harness conform

inputs:
	scripts/fetch-inputs.sh

verify:
	cd rust/verify && cargo build --release --locked --quiet
	rust/verify/target/release/hamming-verify $(GOLDEN_P30)
	cd go && go run ./cmd/rt $(GOLDEN_P30)
	cd js && npm ci --no-audit --no-fund --silent
	node js/lcm_rt.js $(GOLDEN_P30)

coverage:
	# run from the cache so the summary records core-f2b9194/test-lcm-current, not this machine's path
	cd $(HAMMING_CACHE) && $(PYTHON) $(CURDIR)/python/xdr_coverage.py $(XDR) core-f2b9194/test-lcm-current \
		--out $(RESULTS)/coverage_test-lcm-current.json > $(RESULTS)/coverage_summary.json
	cd $(RESULTS) && $(PYTHON) ../python/setcover.py $(XDR) $(GOLDEN_P30) > setcover_summary.json
	@$(PYTHON) -c 'import json; c = json.load(open("$(RESULTS)/coverage_summary.json")); \
		s = json.load(open("$(RESULTS)/setcover_summary.json")); \
		print("coverage: %d/%d atoms, %d decode errors; set cover: %d of %d files" % (c["atoms_covered"], \
		      c["atoms_total"], c["decode_errors"], s["greedy_cover_files"], s["files"]))'

corpus:
	cd $(RESULTS) && $(PYTHON) ../python/golden_manifest.py $(XDR) $(GOLDEN_P30) $(CORE_P30_COMMIT)

harness:
	@echo "harness: not implemented yet; go/cmd/hamming-harness replaces the prototype's test-file steps" >&2
	@exit 1

conform:
	@echo "conform: the SDK conformance runner is not in the imported prototype (docs/PLAN.md says it lives" >&2
	@echo "         in the evidence bundle). Add it before enabling this target." >&2
	@exit 1

test:
	cd go && go test ./...
	cd rust/verify && cargo test --locked --quiet
