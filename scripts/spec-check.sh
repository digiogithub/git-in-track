#!/usr/bin/env bash
# spec-check.sh — the requirement-impact gate (GIT-US-0133, docs/09 §2).
#
# Runs the Go and Vitest suites with machine-readable output, records the
# results with `gintrack spec ingest`, then runs
#
#   gintrack spec impact --since "$SPEC_BASE" --tiers 1,2 --fail-on failing,suspect
#
# and exits with its code: 0 when nothing tripped, 7 when a requirement the diff
# touches is failing or suspect, anything else when the gate could not run.
# `make spec-check` calls it with GINTRACK pointing at a freshly built binary;
# the `spec-impact` job of .github/workflows/ci.yml runs that make target.
#
# Everything it writes goes to $SPEC_DIR (default bin/spec-check, ignored by
# git): the two reports, the impact report and a throwaway gintrack
# configuration registering only this repository, so the gate never reads or
# changes the user's own configuration, and the test-result cache lives next to
# it. Test failures do not stop the gate: they are recorded as failing results
# and surface as failing requirements; the `go` and `web` CI jobs own the plain
# pass/fail of the suites.

set -uo pipefail

GINTRACK=${GINTRACK:-bin/gintrack}
SPEC_BASE=${SPEC_BASE:-origin/main}
SPEC_DIR=${SPEC_DIR:-bin/spec-check}
SPEC_TIERS=${SPEC_TIERS:-1,2}
SPEC_FAIL_ON=${SPEC_FAIL_ON:-failing,suspect}
SPEC_BUDGET=${SPEC_BUDGET:-1500}

root=$(pwd)
mkdir -p "$SPEC_DIR"
dir=$(cd "$SPEC_DIR" && pwd)
gintrack=$(cd "$(dirname "$GINTRACK")" && pwd)/$(basename "$GINTRACK")
rm -f "$dir"/config.yaml "$dir"/go.json "$dir"/vitest.json "$dir"/impact.txt "$dir"/offenders.txt

# An isolated configuration: its directory is also the default index cache
# directory, so `spec ingest` and `spec impact` share one test-result cache.
export GINTRACK_CONFIG="$dir/config.yaml"
if ! "$gintrack" add "$root" >/dev/null; then
  echo "spec-check: could not register $root with gintrack" >&2
  exit 1
fi

echo "spec-check: go test -json -> $dir/go.json"
go_status=0
go list ./... | grep -v '/web/node_modules/' | xargs go test -json >"$dir/go.json" 2>&1 || go_status=$?

echo "spec-check: vitest --reporter=json -> $dir/vitest.json"
web_status=0
if [ -d web/node_modules ]; then
  (cd web && npx vitest run --reporter=dot --reporter=json --outputFile.json="$dir/vitest.json") || web_status=$?
else
  echo "spec-check: web/node_modules is missing; run \`make deps\` first" >&2
  exit 1
fi

[ "$go_status" -eq 0 ] || echo "spec-check: go test exited $go_status; failures are recorded as failing results" >&2
[ "$web_status" -eq 0 ] || echo "spec-check: vitest exited $web_status; failures are recorded as failing results" >&2

if [ -s "$dir/go.json" ]; then
  "$gintrack" spec ingest --repo "$root" --format go "$dir/go.json" || exit $?
fi
if [ -s "$dir/vitest.json" ]; then
  "$gintrack" spec ingest --repo "$root" --base web --format vitest "$dir/vitest.json" || exit $?
fi

echo "spec-check: gintrack spec impact --since $SPEC_BASE --tiers $SPEC_TIERS --fail-on $SPEC_FAIL_ON"
status=0
"$gintrack" spec impact --since "$SPEC_BASE" --tiers "$SPEC_TIERS" --budget "$SPEC_BUDGET" \
  --format text --fail-on "$SPEC_FAIL_ON" >"$dir/impact.txt" 2>"$dir/offenders.txt" || status=$?
cat "$dir/impact.txt"
cat "$dir/offenders.txt" >&2
case "$status" in
  0) echo "spec-check: ok — no requirement is in a --fail-on state ($SPEC_FAIL_ON)" ;;
  7) echo "spec-check: FAILED — see the offending requirements above" >&2 ;;
  *) echo "spec-check: the gate could not run (exit $status)" >&2 ;;
esac
exit "$status"
