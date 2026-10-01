#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
test_bin=$(mktemp /tmp/opennox-repaint.XXXXXX)
trap 'rm -f "$test_bin"' EXIT
# Same compiler and warning flags as cgo_common.go.
cflags="-fshort-wchar -fno-strict-aliasing -fno-strict-overflow
    -Werror=return-type -Werror=implicit-function-declaration -Werror=pointer-arith -Werror=implicit-int
    -Werror=unused-label -Werror=address -Werror=stringop-overflow -Werror=uninitialized
    -Wno-pointer-to-int-cast -Wno-int-to-pointer-cast -Wno-incompatible-pointer-types -Wno-int-conversion
    -Wno-format -Wno-shift-count-overflow -Wno-pedantic -Wno-bad-function-cast -Wno-strict-prototypes
    -Wno-discarded-qualifiers -Wno-return-local-addr -Wno-unused-result"
${CC:-cc} -m32 $cflags -ffunction-sections -fdata-sections -I. tests/draw_repaint.c draw_phase.c \
    client__draw__bubbledraw.c client__draw__partscrn.c client__draw__plasma.c client__draw__arrowdraw.c GAME3_1.c \
    -Wl,--gc-sections -lm -o "$test_bin"
"$test_bin"
