#!/usr/bin/env bash
# Run TLC on every model in formal/ and check its broken variants.
#
#   formal/<name>.tla + .cfg                  model: TLC must pass it
#   formal/<name>_broken_<label>.tla + .cfg   broken variant: TLC must reject it
#                                             with a deadlock, invariant,
#                                             property or assertion violation
#
# A parse or configuration error proves nothing and fails the check. Every
# model needs at least one broken variant. PlusCal sources must commit a
# translation that matches the pinned tla2tools.jar.
#
# Needs bash, java (11+), curl and sha256sum or shasum. CI and local runs use
# this same script. The JAR is downloaded once, verified by SHA-256 and cached
# in $TLA_JAR_CACHE (default: $XDG_CACHE_HOME/tuckerbox-tla or ~/.cache/tuckerbox-tla).
#
# Environment:
#   TLA_MODEL_TIMEOUT  seconds per model or variant (default 300)
#   TLA_JAR_CACHE      where the verified JAR is kept
#   TLA_REPORT         JUnit report path (default: none)
set -euo pipefail

TLA_TOOLS_VERSION="1.7.4"
TLA_TOOLS_SHA256="936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88"
TLA_MODEL_TIMEOUT="${TLA_MODEL_TIMEOUT:-300}"

cd "$(dirname "$0")/.."

digest() { if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1; }

work="" dl=""
trap 'rm -rf -- ${work:+"$work"} ${dl:+"$dl"}' EXIT

cache="${TLA_JAR_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/tuckerbox-tla}"
mkdir -p "$cache"
jar="$cache/tla2tools-${TLA_TOOLS_VERSION}.jar"
if [ ! -f "$jar" ] || [ "$(digest "$jar")" != "$TLA_TOOLS_SHA256" ]; then
  # Download to a private file and rename it into place only once verified,
  # so a concurrent run never sees a partial or unverified JAR.
  dl="$(mktemp "$cache/tla2tools.XXXXXX")"
  curl -fsSL -o "$dl" "https://github.com/tlaplus/tlaplus/releases/download/v${TLA_TOOLS_VERSION}/tla2tools.jar"
  if [ "$(digest "$dl")" != "$TLA_TOOLS_SHA256" ]; then
    echo "FAIL: downloaded tla2tools.jar does not match TLA_TOOLS_SHA256; no model was run" >&2
    exit 1
  fi
  chmod 0644 "$dl"
  mv -f "$dl" "$jar"; dl=""
fi

limit=""
if command -v timeout >/dev/null; then limit="timeout $TLA_MODEL_TIMEOUT"
elif command -v gtimeout >/dev/null; then limit="gtimeout $TLA_MODEL_TIMEOUT"
else echo "warning: no timeout command; TLA_MODEL_TIMEOUT is not enforced" >&2; fi

work="$(mktemp -d)"
cases=""; failures=0; total=0
record() {  # record <file> <role> <ok|fail> <message>
  total=$((total + 1))
  local esc; esc="$(printf '%s' "$4" | sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g; s/"/\&quot;/g')"
  if [ "$3" = ok ]; then
    echo "PASS $1 ($2): $4"
    cases="$cases<testcase classname=\"$1\" name=\"$2\"/>"
  else
    failures=$((failures + 1))
    echo "FAIL $1 ($2): $4" >&2
    cases="$cases<testcase classname=\"$1\" name=\"$2\"><failure message=\"$esc\"/></testcase>"
  fi
}
tlc() {  # tlc <dir> <base>: run TLC in <dir>, keep the log, return its exit code
  local code=0
  (cd "$1" && $limit java -XX:+UseParallelGC -cp "$jar" tlc2.TLC -workers auto \
    -metadir "$work/meta/$2" -cleanup -config "$2.cfg" "$2.tla") > "$work/$2.log" 2>&1 || code=$?
  return "$code"
}

while IFS= read -r cfg; do
  dir="$(dirname "$cfg")"; base="$(basename "$cfg" .cfg)"
  case "$base" in *_broken_*) [ -f "$dir/${base%%_broken_*}.cfg" ] ||
    record "$cfg" variant fail "broken variant has no model ${base%%_broken_*}.cfg"; continue ;; esac
  code=0; tlc "$dir" "$base" || code=$?
  if [ "$code" = 0 ]; then record "$dir/$base.tla" model ok "TLC passed"
  else
    cat "$work/$base.log" >&2
    if [ "$code" = 124 ]; then record "$dir/$base.tla" model fail "exceeded TLA_MODEL_TIMEOUT ($TLA_MODEL_TIMEOUT s)"
    else record "$dir/$base.tla" model fail "TLC exit $code (see output above)"; fi
  fi
  variants=0
  for variant in "$dir/${base}"_broken_*.cfg; do
    [ -f "$variant" ] || continue
    variants=$((variants + 1)); vbase="$(basename "$variant" .cfg)"
    code=0; tlc "$dir" "$vbase" || code=$?
    case "$code" in
      # TLC exit codes 11-14: deadlock, safety (invariant), liveness, assertion.
      11|12|13|14)
        record "$dir/$vbase.tla" variant ok "rejected with TLC exit $code ($(grep -m1 '^Error:' "$work/$vbase.log" || true))"
        echo "--- TLC trace: $dir/$vbase.tla"; cat "$work/$vbase.log"; echo "--- end of TLC trace: $dir/$vbase.tla" ;;
      0) record "$dir/$vbase.tla" variant fail "vacuous: TLC passed a broken variant, so the model cannot detect the bug it was written for" ;;
      124) record "$dir/$vbase.tla" variant fail "exceeded TLA_MODEL_TIMEOUT ($TLA_MODEL_TIMEOUT s)" ;;
      *) cat "$work/$vbase.log" >&2
         record "$dir/$vbase.tla" variant fail "TLC exit $code is not a violation (parse or configuration error proves nothing)" ;;
    esac
  done
  [ "$variants" -gt 0 ] || record "$dir/$base.tla" model fail "no broken variant (${base}_broken_<label>.cfg)"
done < <(find formal -name '*.cfg' | LC_ALL=C sort)

while IFS= read -r tla; do
  grep -qE -- '--(fair )?algorithm' "$tla" || continue
  rel="${tla#formal/}"; mkdir -p "$work/pcal/$(dirname "$rel")"; cp "$tla" "$work/pcal/$rel"
  code=0; (cd "$work/pcal" && java -cp "$jar" pcal.trans -nocfg "$rel") > "$work/pcal.log" 2>&1 || code=$?
  if [ "$code" != 0 ]; then cat "$work/pcal.log" >&2; record "$tla" pluscal fail "PlusCal translation failed (exit $code)"
  elif diff -u "$tla" "$work/pcal/$rel" >&2; then record "$tla" pluscal ok "committed translation is current"
  else record "$tla" pluscal fail "committed TLA+ differs from the PlusCal translation (diff above)"; fi
done < <(find formal -name '*.tla' | LC_ALL=C sort)

if [ -n "${TLA_REPORT:-}" ]; then
  printf '<?xml version="1.0" encoding="UTF-8"?>\n<testsuite name="tla" tests="%s" failures="%s">%s</testsuite>\n' \
    "$total" "$failures" "$cases" > "$TLA_REPORT"
fi
if [ "$total" = 0 ]; then
  echo "FAIL: no models found under formal/" >&2
  exit 1
fi
echo "TLC: $total checks, $failures failed"
[ "$failures" = 0 ]
