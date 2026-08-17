#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
CHECK="$ROOT/scripts/check-json-mode.sh"
TMP=$(mktemp -d "${TMPDIR:-/tmp}/shellbeam-json-mode.XXXXXX")
trap 'rm -rf "$TMP"' EXIT INT TERM
run_case() {
  name=$1; gov=$2; exp=$3; mod=$4; want=$5
  mkdir -p "$TMP/$name"
  cat > "$TMP/$name/go" <<SH
#!/bin/sh
if [ "\$1" = env ] && [ "\$2" = GOVERSION ]; then printf '%s\\n' '$gov'; exit 0; fi
if [ "\$1" = env ] && [ "\$2" = GOEXPERIMENT ]; then printf '%s\\n' '$exp'; exit 0; fi
if [ "\$1" = list ]; then printf '%s\\n' '$mod'; exit 0; fi
exit 99
SH
  chmod +x "$TMP/$name/go"
  set +e
  PATH="$TMP/$name:$PATH" "$CHECK" >/dev/null 2>&1
  rc=$?
  set -e
  if [ "$want" = pass ]; then [ "$rc" -eq 0 ] || { echo "$name rc=$rc want pass"; exit 1; }
  else [ "$rc" -ne 0 ] || { echo "$name unexpectedly passed"; exit 1; }; fi
}
V=v0.0.0-20260623181947-01eb4420fa68
run_case baseline go1.26.5 '' "$V" pass
run_case global-experiment go1.26.5 jsonv2 "$V" fail
run_case old-go go1.25.9 '' "$V" fail
run_case module-drift go1.26.5 '' v0.0.0-deadbeef fail
echo PASS
