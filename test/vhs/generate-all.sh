#!/bin/bash
# generate-all.sh - Generate all demo recordings using VHS
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
OUTPUT_DIR="${PROJECT_ROOT}/docs/examples"

# Ensure output directory exists
mkdir -p "$OUTPUT_DIR"

# Track failures
FAILED=0

run_tape() {
    local tape="$1"
    local name
    name="$(basename "$tape" .tape)"
    local out_name="$name"
    if [ "$name" = "full-demo" ]; then
        out_name="demo"
    fi

    echo "==> Recording $name..."
    if vhs -o "$OUTPUT_DIR/$out_name.gif" "$tape"; then
        echo "    [OK] $OUTPUT_DIR/$out_name.gif"
    else
        echo "    [FAIL] $name"
        FAILED=$((FAILED + 1))
    fi
}

# Setup environment first (not recorded)
echo "==> Setting up demo environment..."
"$SCRIPT_DIR/setup-env.sh"

echo ""
echo "==> Recording demos..."

# Run each demo tape
run_tape "$SCRIPT_DIR/full-demo.tape"

echo ""
if [ $FAILED -gt 0 ]; then
    echo "ERROR: $FAILED demo(s) failed"
    exit 1
else
    echo "All demos recorded successfully!"
    echo "Output: $OUTPUT_DIR/"
    ls -la "$OUTPUT_DIR"/*.gif 2>/dev/null || true
fi
