#!/usr/bin/env bash
set -euo pipefail

API_BASE="${API_BASE:-http://localhost:8080/api/v1/recommend/dispatcher}"
TIMEOUT="${TIMEOUT:-60}"

pass=0
fail=0

run_case() {
  local id="$1"
  local query="$2"
  local domain="$3"
  local expect="$4"

  local payload
  payload=$(python3 - <<PY
import json
print(json.dumps({"query": """$query""", "domain": """$domain"""}))
PY
)

  local response
  response=$(curl -sS --max-time "$TIMEOUT" -X POST "$API_BASE" -H "Content-Type: application/json" -d "$payload")

  local tmp
  tmp=$(mktemp)
  printf '%s' "$response" > "$tmp"

  local top
  top=$(python3 - <<PY
import json
j=json.load(open('$tmp'))
print((j.get('top_recommendation') or {}).get('name') or (j.get('physics_analysis') or {}).get('top_candidate') or '')
PY
)

  echo "[$id] top: $top"

  if echo "$top" | grep -Eiq "$expect"; then
    echo "  PASS"
    pass=$((pass+1))
  else
    echo "  FAIL expected /$expect/"
    fail=$((fail+1))
  fi

  rm -f "$tmp"
}

run_case 11 "Designing a water-cooled block for a high-density server rack. I need the absolute highest thermal conductivity possible to prevent CPU throttling." "Electronics & Photonics" "ofhc|oxygen-free|copper"
run_case 12 "Need a manifold for a chemical processing plant. It will be exposed to hot sulfuric acid at 120C. Can I 3D print this on a desktop setup?" "Plastics & Polymers" "peek|ptfe|teflon"
run_case 13 "Building a drone arm for high-speed racing. It must be as stiff as possible to prevent propeller flutter but weight is my biggest enemy." "Aerospace & Aviation" "cfrp|carbon"
run_case 14 "Exhaust manifold for a turbocharger. Temps reach 950C. Must resist oxidation and should not deform under high pressure." "Automotive & Transportation" "inconel|hastelloy"
run_case 15 "Need a material for a dental implant post. Must be non-magnetic, lightweight, and won't be rejected by the human body." "Medical & Biomedical" "ti-6al-4v|titanium"
run_case 16 "I need a compact housing for a radioactive isotope source used in industrial X-raying. It needs to be the smallest footprint possible while blocking radiation." "Tooling & Wear-Resistant" "tungsten"
run_case 17 "Need a transparent guard for a CNC machine. It needs to stop a flying metal shard if a tool breaks. It cannot be brittle." "Construction & Structural" "polycarbonate|pc"
run_case 18 "Induction heating crucible. I will be dropping cold metal into it while it's at 1500C. It must not shatter." "High-Temperature / Refractory" "zirconia|alumina"
run_case 19 "I need a wire that can be bent into a complex shape but will remember its original straight form when I dip it in hot water." "Automotive & Transportation" "nitinol|ni-ti"
run_case 20 "Supporting frame for a high-precision space telescope mirror. As the satellite moves from sun to shadow, the frame cannot expand or contract at all." "Aerospace & Aviation" "invar"

echo "Passed: $pass"
echo "Failed: $fail"

if [[ $fail -gt 0 ]]; then
  exit 1
fi
