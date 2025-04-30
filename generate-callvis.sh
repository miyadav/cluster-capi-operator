#!/bin/bash
set -e

FOCUS_PKG="github.com/miyadav/cluster-capi-operator/cmd/machine-api-migration"
TARGET_PKG="./cmd/machine-api-migration"
OUTPUT_DIR="callvisdocs/callvis"
DOT_FILE="$OUTPUT_DIR/callgraph.dot"
SVG_FILE="$OUTPUT_DIR/callgraph.svg"

mkdir -p "$OUTPUT_DIR"

echo "Generating focused call graph..."
go-callvis -nostd -format dot -group pkg,type -focus "$FOCUS_PKG" -limit github.com/miyadav/cluster-capi-operator "$TARGET_PKG" > "$DOT_FILE"
#go-callvis -nostd -format dot -group pkg,type ./cmd/machine-api-migration > "$DOT_FILE"
dot -Tsvg "$DOT_FILE" -o "$SVG_FILE"

echo "Call graph generated at:"
echo "DOT:  $DOT_FILE"
echo "SVG:  $SVG_FILE"

