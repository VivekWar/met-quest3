#!/bin/bash

# ============================================================================
# Material Dispatcher Validation Script - 10 Test Cases
# MET-QUEST '26 | Production Parity & Physics Guardrails
# ============================================================================

set -e

# Configuration
API_URL="${API_URL:-http://localhost:8080}"
DISPATCHER_ENDPOINT="${API_URL}/api/v1/recommend/dispatcher"
REPORT_FILE="/tmp/dispatcher_validation_report.txt"
PASSED=0
FAILED=0

parse_json_field() {
  local response_body=$1
  local field_path=$2

  printf '%s' "$response_body" | python3 -c '
import json
import sys

field_path = sys.argv[1].split(".")
try:
  data = json.load(sys.stdin)
  for part in field_path:
    if isinstance(data, dict):
      data = data.get(part)
    else:
      data = None
    if data is None:
      break
  if data is None:
    print("ERROR")
  else:
    print(data)
except Exception:
  print("PARSE_ERROR")
' "$field_path"
}

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo "🔬 Running Material Dispatcher Validation Suite..."
echo "   API Endpoint: $DISPATCHER_ENDPOINT"
echo "   Report: $REPORT_FILE"
echo ""

# Helper function to run a test case
run_test_case() {
  local test_num=$1
  local test_name=$2
  local query=$3
  local expected_category=$4
  local expected_contains=$5

  echo -n "Test $test_num: $test_name ... "
  
  local response=$(curl -s -X POST "$DISPATCHER_ENDPOINT" \
    -H "Content-Type: application/json" \
    -d "{\"query\": \"$query\", \"domain\": \"Overall (Top 1000)\"}")

  # Extract routed category
  local routed_category=$(parse_json_field "$response" "routed_category")
  
  # Extract top recommendation
  local top_recommendation=$(parse_json_field "$response" "top_recommendation.name")

  # Check if response contains expected strings
  local contains_check=$(echo "$response" | grep -q "$expected_contains" && echo "true" || echo "false")

  # Validate test
  if [[ "$routed_category" == "$expected_category" ]] && [[ "$contains_check" == "true" ]]; then
    echo -e "${GREEN}✓ PASS${NC}"
    echo "  Category: $routed_category | Top: $top_recommendation"
    ((PASSED+=1))
  else
    echo -e "${RED}✗ FAIL${NC}"
    echo "  Expected category: $expected_category, got: $routed_category"
    printf '  Response: %s\n' "$response"
    ((FAILED+=1))
  fi
  echo ""
}

# ============================================================================
# TEST CASES
# ============================================================================

# Test 1: Desktop FDM - Polymers Category Lock
run_test_case 1 \
  "Desktop FDM Query (Should lock to Polymers)" \
  "I need a strong plastic for desktop FDM printing on my Ender 3. Good layer adhesion is important." \
  "Polymers" \
  "Polymer"

# Test 2: High-Temperature Polymer for Aerospace
run_test_case 2 \
  "Aerospace Polymer (High Tg)" \
  "Need a high glass transition polymer for aircraft interior panels. Service temp is 150°C with good dimensional stability." \
  "Polymers" \
  "Polymer"

# Test 3: Alloy Selection for Aerospace Structures
run_test_case 3 \
  "Aerospace Alloy (Strength-to-weight)" \
  "Aircraft wing structure needs high strength-to-weight ratio with fatigue resistance. Service: 80°C, static loads 500 MPa." \
  "Alloys" \
  "Alloy"

# Test 4: Pure Copper for Maximum Conductivity
run_test_case 4 \
  "Pure Metal for Electrical Conductivity" \
  "Need maximum electrical conductivity for a heat sink. Pure metal preferred, high thermal conductivity essential." \
  "Pure_Metals" \
  "Metal"

# Test 5: Ceramic for High-Temperature Furnace
run_test_case 5 \
  "Ceramic for Extreme Heat (1000°C)" \
  "Furnace lining application. Must withstand 1000°C continuous service with thermal shock resistance. High hardness needed." \
  "Ceramics" \
  "Ceramic"

# Test 6: Composite for Lightweight Structure
run_test_case 6 \
  "Composite for Aerospace Structures" \
  "Carbon fiber reinforced composite for aircraft fuselage. Need high specific strength and excellent fatigue performance." \
  "Composites" \
  "Composite"

# Test 7: Impossible Desktop FDM Rejection (Should return NO_FEASIBLE_MATERIAL)
run_test_case 7 \
  "Impossible FDM Query (Rocket Nozzle)" \
  "I want to 3D print a rocket nozzle on my desktop printer that handles 2000°C continuous exposure with thermally resistant plastic filament." \
  "Polymers" \
  "NO_FEASIBLE_MATERIAL"

# Test 8: Aluminum Alloy for Cryogenic Service
run_test_case 8 \
  "Cryogenic Metal Selection" \
  "CNC-machined component for liquid nitrogen cryogenic service at -196°C. Need ductility and low notch sensitivity." \
  "Alloys" \
  "Alloy"

# Test 9: TPU Elastomer for Vibration Damping
run_test_case 9 \
  "Elastomer for Damping" \
  "Flexible component for vibration damping. High strain capacity and low stiffness needed. Temperature range -20°C to 60°C." \
  "Polymers" \
  "Polymer"

# Test 10: Wear-Resistant Ceramic
run_test_case 10 \
  "Ceramic for Abrasive Wear" \
  "Abrasion-resistant wear plates for industrial grinding mill. Must withstand constant particle impact and high hardness required." \
  "Ceramics" \
  "Ceramic"

# ============================================================================
# SUMMARY
# ============================================================================

echo "═════════════════════════════════════════════════════════════════"
echo -e "${BLUE}📊 VALIDATION SUMMARY${NC}"
echo "═════════════════════════════════════════════════════════════════"
echo -e "✓ Passed: ${GREEN}$PASSED/10${NC}"
echo -e "✗ Failed: ${RED}$FAILED/10${NC}"
echo -e "Score: ${BLUE}$((PASSED*10))%${NC}"
echo ""

if [[ $FAILED -eq 0 ]]; then
  echo -e "${GREEN}🎉 All tests PASSED! Dispatcher is production-ready.${NC}"
  exit 0
else
  echo -e "${RED}⚠️  $FAILED test(s) failed. Review the output above.${NC}"
  exit 1
fi
