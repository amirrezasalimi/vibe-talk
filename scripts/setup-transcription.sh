#!/bin/sh
set -eu

# Native assets only; no package environments or capture helpers are installed.
if [ "$(uname -s)" != Darwin ] || [ "$(uname -m)" != arm64 ]; then
    echo 'Native Whistle requires macOS arm64 (Apple Silicon); run outside Rosetta.' >&2
    exit 1
fi
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
RUNTIME=${VIBE_TALK_RUNTIME:-"$ROOT/runtime"}
CACHE="$ROOT/.cache/whistle"
mkdir -p "$RUNTIME" "$CACHE"
RUNTIME=$(CDPATH= cd -- "$RUNTIME" && pwd)
for tool in curl shasum clang++ nm; do
    command -v "$tool" >/dev/null 2>&1 || {
        echo "Required tool missing: $tool (install Xcode Command Line Tools)." >&2
        exit 1
    }
done

# Revisions and LFS SHA-256 values verified against the official HF repository
# APIs with ?blobs=true.
ENGINE_REV=2ae11323dc000f5e70c49f7403efa6af12ba9e67
MODEL_REV=b358ddadd89b7a713b5aa131f23032d3cca1b251
ENGINE_URL="https://huggingface.co/Cactus-Compute/needle3/resolve/$ENGINE_REV/macos-arm64"
MODEL_URL="https://huggingface.co/Cactus-Compute/whistle/resolve/$MODEL_REV"
TEMP="$RUNTIME/.whistle-setup.$$"
mkdir "$TEMP"
trap 'rm -rf "$TEMP"' EXIT
trap 'exit 1' HUP INT TERM

verify() {
    printf '%s  %s\n' "$2" "$1" | shasum -a 256 -c - >/dev/null 2>&1
}
fetch() {
    name=$1
    url=$2
    checksum=$3
    destination=${4:-"$RUNTIME"}
    if [ -f "$destination/$name" ] && verify "$destination/$name" "$checksum"; then
        echo "Verified existing $destination/$name (SHA-256 $checksum)"
        return
    fi
    echo "Downloading $name from $url"
    curl --fail --location --show-error --retry 3 --connect-timeout 30 --max-time 300 \
        "$url" -o "$TEMP/$name"
    if ! verify "$TEMP/$name" "$checksum"; then
        echo "SHA-256 verification failed: $name" >&2
        exit 1
    fi
    mv "$TEMP/$name" "$destination/$name"
    echo "Verified $destination/$name (SHA-256 $checksum)"
}

# About 18.5 MB total; deliberately do not download safetensors or a runner.
fetch libneedle.a "$ENGINE_URL/libneedle.a" 98da47c15e1065b4cdc7ddc55e825be78414d4586832db3becaeded39a373df4 "$CACHE"
fetch whistle.cact "$MODEL_URL/whistle.cact" b6e02f048568ac5d01a2042556c658061e699acbc0aa2a1439f52f3d461dffeb

clang++ -arch arm64 -dynamiclib "-Wl,-force_load,$CACHE/libneedle.a" \
    -framework Accelerate -Wl,-install_name,@rpath/libwhistle.dylib \
    -o "$TEMP/libwhistle.dylib"
nm -gU "$TEMP/libwhistle.dylib" > "$TEMP/symbols"
for symbol in needle_load needle_last_error needle_stream_transcribe_process needle_stream_transcribe_stop; do
    if ! grep -Eq "[[:space:]]_$symbol$" "$TEMP/symbols"; then
        echo "Required native symbol missing: $symbol" >&2
        exit 1
    fi
done
mv "$TEMP/libwhistle.dylib" "$RUNTIME/libwhistle.dylib"
echo "Ready: $RUNTIME/libwhistle.dylib and $RUNTIME/whistle.cact"
echo 'Verified all four native API exports; setup-only archive cached in .cache/whistle/.'
