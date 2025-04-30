#!/bin/bash
set -e

BASELINE="callvisdocs/callvis/baseline.dot"
CURRENT="callvisdocs/callvis/callgraph.dot"
DIFF="callvisdocs/callvis/diff.txt"

if [[ ! -f "$BASELINE" ]]; then
  echo "No baseline found at $BASELINE"
  exit 1
fi

echo "Comparing $CURRENT to baseline..."

diff -u "$BASELINE" "$CURRENT" > "$DIFF" || true

echo "Diff written to $DIFF"
cat "$DIFF"

