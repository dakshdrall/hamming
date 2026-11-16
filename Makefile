# Hamming pipeline. Every file under results/ that a target names is written by that target; nothing in
# results/ is edited by hand.
#
#   make inputs     fetch pinned upstream inputs into $(HAMMING_CACHE)
#   make verify     decoder agreement over every golden (results/decoders.json) and the samples manifest check
#   make coverage   grammar-decoder atom coverage and the greedy golden-file set cover
#   make corpus     per-golden-file manifest rows (atoms, sha256, core test) for the corpus builder
#   make mutated    regenerate corpus/mutated (synthetic entries) from the protocol 28 goldens
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
CORE_P28_COMMIT := abe1c5541827caaafe023954c224cadeb5dddc44
GOLDEN_P30 := $(HAMMING_CACHE)/core-f2b9194/test-lcm-current
GOLDEN_P28 := $(HAMMING_CACHE)/core-abe1c55/test-lcm-current
XDR := $(HAMMING_CACHE)/stellar-xdr
RESULTS := $(CURDIR)/results

.PHONY: all inputs verify coverage corpus mutated harness conform test

all: inputs verify coverage corpus harness conform

inputs:
	scripts/fetch-inputs.sh

# Each decoder round-trips every f2b9194 golden; hamming-verify merges the three rows into one agreement table
# (results/decoders.json) and exits 1 unless all agree. Then the sample corpus manifest is checked.
verify:
	cargo build --release --locked --quiet -p hamming-verify
	cd go && go run ./cmd/rt -json $(HAMMING_CACHE)/decoders-go.json $(GOLDEN_P30)
	cd js && npm ci --no-audit --no-fund --silent
	node js/lcm_rt.js --json $(HAMMING_CACHE)/decoders-js.json $(GOLDEN_P30)
	target/release/hamming-verify --label core-f2b9194/test-lcm-current --out $(RESULTS)/decoders.json \
		--merge $(HAMMING_CACHE)/decoders-go.json --merge $(HAMMING_CACHE)/decoders-js.json $(GOLDEN_P30)
	target/release/hamming-verify corpus/samples/manifest.json

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

# Regenerates corpus/mutated from the protocol 28 goldens. Deterministic: CI checks the output matches git.
mutated:
	cd go && go run ./cmd/hamming-mutate -golden $(GOLDEN_P28) -core-commit $(CORE_P28_COMMIT) -out $(CURDIR)/corpus/mutated

harness:
	cd go && go run ./cmd/hamming-harness -root $(CURDIR) -cache $(HAMMING_CACHE)

conform:
	@echo "conform: the SDK conformance runner is not in the imported prototype (docs/PLAN.md says it lives" >&2
	@echo "         in the evidence bundle). Add it before enabling this target." >&2
	@exit 1

test:
	cd go && go test ./...
	cargo test --locked --quiet
