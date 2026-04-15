#!/usr/bin/env bash
set -euo pipefail

API_BASE="${API_BASE:-http://localhost:8080/api/v1/recommend/dispatcher}"
TIMEOUT="${TIMEOUT:-45}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

pass_count=0
fail_count=0

run_case() {
  local id="$1"
  local name="$2"
  local query="$3"
  local domain="$4"
  local expect_regex="$5"
  local mode="${6:-top}"

  echo -e "${BLUE}[$id] $name${NC}"

  local payload
  payload=$(python3 - <<PY
import json
print(json.dumps({"query": """$query""", "domain": """$domain"""}))
PY
)

  local response
  response=$(curl -sS --max-time "$TIMEOUT" -X POST "$API_BASE" -H "Content-Type: application/json" -d "$payload")
  local tmp_json
  tmp_json=$(mktemp)
  printf '%s' "$response" > "$tmp_json"

  if ! python3 -m json.tool "$tmp_json" >/dev/null 2>&1; then
    echo -e "${RED}  FAIL${NC} invalid JSON response"
    fail_count=$((fail_count+1))
    rm -f "$tmp_json"
    return
  fi

  local routed
  routed=$(python3 - <<PY
import json
d=json.load(open('$tmp_json'))
print(d.get('routed_category',''))
PY
)
  local top
  top=$(python3 - <<PY
import json
d=json.load(open('$tmp_json'))
top=(d.get('top_recommendation') or {}).get('name') or (d.get('physics_analysis') or {}).get('top_candidate') or ''
print(top)
PY
)
  local report
  report=$(python3 - <<PY
import json
d=json.load(open('$tmp_json'))
print(d.get('pipeline_explanation',''))
PY
)

  local haystack="$top"
  if [[ "$mode" == "reject" ]]; then
    haystack="$(python3 - <<PY
import json
d=json.load(open('$tmp_json'))
p=d.get('physics_analysis') or {}
print((p.get('top_candidate') or '') + ' ' + (p.get('manufacturing_feasibility') or ''))
PY
)"
  fi

  echo "  Routed: $routed"
  echo "  Top:    $top"

  if echo "$haystack" | grep -Eiq "$expect_regex"; then
    echo -e "${GREEN}  PASS${NC}"
    pass_count=$((pass_count+1))
  else
    echo -e "${RED}  FAIL${NC} expected pattern /$expect_regex/"
    fail_count=$((fail_count+1))
  fi
  rm -f "$tmp_json"
  echo
}

run_case 1 "Middle Ground PETG" \
  "Designing a motor mount for a 20-minute robotics run. Needs to be easy to print on an Ender 3 without an enclosure, but must not soften when the motor gets hot (70-80C)." \
  "Plastics & Polymers" \
  "petg|pc|polycarbonate"

run_case 2 "Aesthetic PLA" \
  "Need a high-resolution architectural scale model for an indoor display. Dimensional accuracy and surface finish are the only priorities. No heat or mechanical load." \
  "Plastics & Polymers" \
  "pla"

run_case 3 "High Performance PC/Nylon-CF" \
  "Building a structural bracket for an engine bay. Constant exposure to 105C. I have an industrial-grade printer with a heated chamber (90C) and 300C nozzle." \
  "Plastics & Polymers" \
  "polycarbonate|pc|nylon"

run_case 4 "Cryogenic Alloy" \
  "Need a material for a liquid nitrogen pipe fitting. Must be non-porous and machined via CNC. Needs to stay ductile at -196C." \
  "Automotive & Transportation" \
  "6061|aluminum|aluminium"

run_case 5 "Energy Absorber" \
  "Need to print soft, energy-absorbing gaskets for a drone camera mount to reduce jelly-effect vibrations." \
  "Plastics & Polymers" \
  "tpu|elastomer|urethane|rubber|tpe"

run_case 6 "Precision Ceramic" \
  "High-wear industrial nozzle for abrasive slurry. Must handle 800C and extreme friction without losing shape." \
  "Tooling & Wear-Resistant" \
  "zirconia|silicon carbide|sic|alumina"

run_case 7 "Conductivity King" \
  "Custom heat sink for a high-power LED array. Maximum thermal conductivity is required. Will be machined from a block." \
  "Electronics & Photonics" \
  "copper|c101|c110"

run_case 8 "Aerospace Wing" \
  "Wing spar for a UAV. Needs the absolute highest strength-to-weight ratio available in a machinable metal." \
  "Aerospace & Aviation" \
  "7075|carbon|cfrp"

run_case 9 "Chemical Manifold" \
  "Fluid manifold for transporting corrosive acids at 150C. Must be chemically inert." \
  "Plastics & Polymers" \
  "ptfe|teflon|peek"

run_case 10 "Safety Valve Rejection" \
  "I want to 3D print a rocket nozzle that survives 2000C on my desktop hobby printer using plastic filament." \
  "Plastics & Polymers" \
  "no_feasible_material|reject|no desktop fdm polymer" \
  "reject"

echo -e "${BLUE}================================${NC}"
echo -e "Passed: ${GREEN}${pass_count}${NC}"
echo -e "Failed: ${RED}${fail_count}${NC}"
echo -e "${BLUE}================================${NC}"

if [[ $fail_count -gt 0 ]]; then
  exit 1
fi
