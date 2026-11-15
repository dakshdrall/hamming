#!/usr/bin/env bash
# Fetch Hamming's pinned upstream inputs into $HAMMING_CACHE (default: <repo>/.cache, git-ignored).
#
#   core-f2b9194/test-lcm-current   stellar-core protocol 30 goldens (sparse, blob-filtered)
#   core-abe1c55/test-lcm-current   stellar-core protocol 28 goldens (sparse, blob-filtered)
#   stellar-xdr/                    stellar-xdr at tag v30.0
#   wallet-backend/                 full clone; the harness makes one worktree per pinned commit
#
# Every pin is a full commit SHA (docs/FACTS.md records where each comes from). The script exits non-zero,
# naming the ref, if any pinned commit or tag is missing upstream or resolves to something else. Re-running it
# reuses what is already in the cache.
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
CACHE=${HAMMING_CACHE:-$ROOT/.cache}

CORE_URL=https://github.com/stellar/stellar-core
CORE_P30=f2b91947fb20fc8174cca38bf320eda7baa3edbb # f2b9194
CORE_P28=abe1c5541827caaafe023954c224cadeb5dddc44 # abe1c55
XDR_URL=https://github.com/stellar/stellar-xdr
XDR_TAG=v30.0
XDR_COMMIT=4f524bbac80c781c06e4fb5fc93a2d0d331d6705
WB_URL=https://github.com/stellar/wallet-backend
WB_COMMITS=(
  2a0a84ea12523b6c92e278c0c84b443041240095 # 2a0a84e: before the #729 fix
  2c873353c1f0f3c9b145e25a7d009023ad104a1c # 2c87335: the #729 fix
  3e22515a44979fdc3a558fda3991ca2b48e38cef # 3e22515: HEAD on 2026-11-12
)

die() {
  echo "fetch-inputs: ERROR: $*" >&2
  exit 1
}

# core_goldens SHA: test-lcm-current at one stellar-core commit, without the rest of the tree or history.
core_goldens() {
  local sha=$1 dir="$CACHE/core-${1:0:7}"
  if [ -d "$dir/test-lcm-current" ] && [ "$(git -C "$dir" rev-parse HEAD 2>/dev/null)" = "$sha" ]; then
    echo "stellar-core ${sha:0:7}: cached"
    return
  fi
  rm -rf "$dir"
  git init -q "$dir"
  git -C "$dir" remote add origin "$CORE_URL"
  git -C "$dir" sparse-checkout set test-lcm-current
  git -C "$dir" fetch -q --depth 1 --filter=blob:none origin "$sha" ||
    die "stellar-core commit $sha cannot be fetched from $CORE_URL"
  git -C "$dir" checkout -q --detach FETCH_HEAD
  [ "$(git -C "$dir" rev-parse HEAD)" = "$sha" ] || die "stellar-core checkout is not $sha"
  [ -d "$dir/test-lcm-current" ] || die "stellar-core $sha has no test-lcm-current/"
  echo "stellar-core ${sha:0:7}: $(find "$dir/test-lcm-current" -name '*.xdr' | wc -l | tr -d ' ') golden files"
}

xdr_spec() {
  local dir="$CACHE/stellar-xdr"
  if [ "$(git -C "$dir" rev-parse HEAD 2>/dev/null)" != "$XDR_COMMIT" ]; then
    rm -rf "$dir"
    git -c advice.detachedHead=false clone -q --depth 1 --branch "$XDR_TAG" "$XDR_URL" "$dir" || die "stellar-xdr tag $XDR_TAG not found"
  fi
  local got
  got=$(git -C "$dir" rev-parse HEAD)
  [ "$got" = "$XDR_COMMIT" ] || die "stellar-xdr $XDR_TAG resolves to $got, pinned $XDR_COMMIT"
  ls "$dir"/*.x >/dev/null 2>&1 || die "stellar-xdr $XDR_TAG has no .x files at its root"
  echo "stellar-xdr $XDR_TAG: ${got:0:7}"
}

wallet_backend() {
  local dir="$CACHE/wallet-backend" missing=0 sha
  [ -d "$dir/.git" ] || git clone -q "$WB_URL" "$dir" || die "cannot clone $WB_URL"
  for sha in "${WB_COMMITS[@]}"; do
    git -C "$dir" cat-file -e "$sha^{commit}" 2>/dev/null || missing=1
  done
  [ $missing = 0 ] || git -C "$dir" fetch -q origin || die "cannot fetch $WB_URL"
  for sha in "${WB_COMMITS[@]}"; do
    git -C "$dir" cat-file -e "$sha^{commit}" 2>/dev/null || die "wallet-backend commit $sha not found in $WB_URL"
  done
  echo "wallet-backend: ${#WB_COMMITS[@]} pinned commits present"
}

mkdir -p "$CACHE"
core_goldens "$CORE_P30"
core_goldens "$CORE_P28"
xdr_spec
wallet_backend
