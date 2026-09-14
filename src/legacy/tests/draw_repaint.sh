#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
test_bin=$(mktemp /tmp/opennox-repaint.XXXXXX)
trap 'rm -f "$test_bin"' EXIT
${CC:-cc} -m32 -fshort-wchar -ffunction-sections -fdata-sections -I. tests/draw_repaint.c draw_phase.c \
    client__draw__bubbledraw.c client__draw__partscrn.c client__draw__plasma.c \
    -Wl,--gc-sections -o "$test_bin"
"$test_bin"
