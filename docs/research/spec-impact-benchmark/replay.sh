#!/usr/bin/env bash
# replay.sh — replay benchmark PRs on the base tree and measure each one.
#
#   BENCH_REPO=/path/to/clone-at-7e60c1c4 GINTRACK=/path/to/gintrack \
#   GINTRACK_CONFIG=/path/to/throwaway/config.yaml OUT=/path/to/out \
#   docs/research/spec-impact-benchmark/replay.sh P1 P2 S1 C1 ...
#
# The clone must be checked out at the base commit with no local changes; it
# is restored after every PR. See README.md for the full procedure.
set -u
: "${BENCH_REPO:?set BENCH_REPO to a clone checked out at the base commit}"
: "${GINTRACK:?set GINTRACK to the gintrack binary under test}"
: "${GINTRACK_CONFIG:?set GINTRACK_CONFIG to a throwaway config registering only BENCH_REPO}"
: "${OUT:?set OUT to the directory the results are written to}"
export GINTRACK_CONFIG
BASE=${BASE:-7e60c1c4}
PATCHES=${PATCHES:-$(cd "$(dirname "$0")" && pwd)/patches}
cd "$BENCH_REPO" || exit 1

# settle waits until tier 3 answers, i.e. Pando has stopped re-importing the
# documents the replay touched; it prints the seconds waited.
settle() {
  local n=0
  while [ $n -lt 120 ]; do
    if "$GINTRACK" spec impact --since "$BASE" --tiers 3 --json 2>/dev/null | grep -q '"tier":3,"status":"ok"'; then echo $n; return; fi
    sleep 3; n=$((n+3))
  done
  echo "$n(timeout)"
}

for pr in "$@"; do
  out=$OUT/$pr; rm -rf "$out"; mkdir -p "$out"
  case $pr in
    P*|C1) patch -R -F3 -p1 -s -r /dev/null --no-backup-if-mismatch < "$PATCHES/$pr.patch" >/dev/null 2>&1 ;;  # reverse diff of a real commit
    *)     patch -p1 -s < "$PATCHES/$pr.patch" >/dev/null 2>&1 ;;                                              # hand edit
  esac
  echo "$pr settle_after_apply=$(settle)" | tee "$out/settle.txt"
  for t in 1 1,2 1,2,3 3; do
    tag=t${t//,/}
    for i in 1 2; do
      s=$(date +%s.%N)
      "$GINTRACK" spec impact --since "$BASE" --tiers "$t" --json > "$out/$tag.$i.json" 2>"$out/$tag.$i.err"
      e=$(date +%s.%N); echo "$e - $s" | bc > "$out/$tag.$i.secs"
    done
    "$GINTRACK" spec impact --since "$BASE" --tiers "$t" --format text > "$out/$tag.txt" 2>/dev/null
    if cmp -s "$out/$tag.1.json" "$out/$tag.2.json"; then echo same > "$out/$tag.cmp"; else echo DIFF > "$out/$tag.cmp"; fi
  done
  git checkout -q .; git clean -fdq -e node_modules
  echo "$pr settle_after_restore=$(settle)" | tee -a "$out/settle.txt"
done
