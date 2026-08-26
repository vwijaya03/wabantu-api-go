#!/usr/bin/env bash
# Generate internal/apitest/catalog_snapshot.json from //encore:api annotations.
# Run from api-go root after adding/changing endpoints.
set -euo pipefail
cd "$(dirname "$0")/.."

OUT="internal/apitest/catalog_snapshot.json"
TMP="$(mktemp)"

python3 - <<'PY' >"$TMP"
import json, re, subprocess, sys
from collections import defaultdict

root = "."
pat = re.compile(r"//encore:api\s+(.+)$")
entries = []

proc = subprocess.run(
    ["rg", "-n", "encore:api", "--glob", "*.go", root],
    capture_output=True, text=True, check=False,
)
if proc.returncode not in (0, 1):
    print(proc.stderr, file=sys.stderr)
    sys.exit(proc.returncode)

for line in proc.stdout.splitlines():
    if not line.strip():
        continue
    path, rest = line.split(":", 1)
    lineno_str, _, annotation = rest.partition(":")
    path = path.removeprefix("./")
    if path.startswith("internal/apitest/"):
        continue
    m = pat.search(annotation)
    if not m:
        continue
    parts = path.split("/")
    service = parts[0] if parts else "unknown"
    entries.append({
        "service": service,
        "file": path,
        "line": int(lineno_str),
        "annotation": m.group(1).strip(),
    })

by_service = defaultdict(list)
for e in entries:
    by_service[e["service"]].append(e)

snapshot = {
    "generatedAt": subprocess.check_output(["date", "-u", "+%Y-%m-%dT%H:%M:%SZ"], text=True).strip(),
    "endpointCount": len(entries),
    "serviceCount": len(by_service),
    "services": {
        svc: {"endpointCount": len(items), "endpoints": items}
        for svc, items in sorted(by_service.items())
    },
}
print(json.dumps(snapshot, indent=2, ensure_ascii=False))
PY

mv "$TMP" "$OUT"
echo "Wrote $OUT ($(python3 -c "import json; d=json.load(open('$OUT')); print(d['endpointCount'], 'endpoints,', d['serviceCount'], 'services')"))"
