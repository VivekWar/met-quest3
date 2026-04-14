#!/usr/bin/env bash
# 🧪 Dispatcher API Test Suite
# Run tests against the new /api/v1/recommend/dispatcher endpoint

set -e

API_BASE="http://localhost:8080/api/v1"
TIMEOUT=30

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}LLM Dispatcher Test Suite${NC}"
echo -e "${BLUE}========================================${NC}\n"

# Helper function to make API calls
test_query() {
    local test_name="$1"
    local query="$2"
    local domain="${3:-Overall (Top 1000)}"
    
    echo -e "${YELLOW}📝 Test: $test_name${NC}"
    echo -e "   Query: \"$query\""
    echo -e "   Domain: $domain\n"
    
    # Make the request
    response=$(curl -s -X POST "$API_BASE/recommend/dispatcher" \
        -H "Content-Type: application/json" \
        --max-time $TIMEOUT \
        -d "{\"query\":\"$query\",\"domain\":\"$domain\"}")
    
    # Check if response is valid JSON
    if echo "$response" | jq . > /dev/null 2>&1; then
        echo -e "${GREEN}✅ Response received${NC}"
        
        # Extract key fields
        routed_category=$(echo "$response" | jq -r '.routed_category // "N/A"')
        top_candidate=$(echo "$response" | jq -r '.top_recommendation.name // "N/A"')
        num_candidates=$(echo "$response" | jq '.category_candidates | length')
        total_tokens=$(echo "$response" | jq -r '.total_tokens_used // 0')
        
        echo -e "   Routed to: ${BLUE}$routed_category${NC}"
        echo -e "   Top pick: ${GREEN}$top_candidate${NC}"
        echo -e "   Candidates found: $num_candidates"
        echo -e "   Tokens used: $total_tokens"
        
        # Show physics analysis if available
        physics_check=$(echo "$response" | jq '.physics_analysis.physics_verification // empty')
        if [ ! -z "$physics_check" ]; then
            echo -e "   ${BLUE}Physics verification:${NC}"
            echo "$physics_check" | jq . 2>/dev/null || echo "   (Physics data available)"
        fi
        
        echo ""
        return 0
    else
        echo -e "${RED}❌ Invalid response${NC}"
        echo "$response"
        echo ""
        return 1
    fi
}

# ─────────────────────────────────────────────────────────────
#  TEST SUITE 1: POLYMER QUERIES
# ─────────────────────────────────────────────────────────────
echo -e "${BLUE}TEST SUITE 1: POLYMER QUERIES${NC}\n"

test_query \
    "3D Printing (FDM) Selection" \
    "I need a polymer for desktop 3D printing with good print quality and low cost" \
    "Polymer Processing"

test_query \
    "High-Temperature Polymer" \
    "Looking for a polymer that can handle 150°C continuously without deformation. Used in automotive under-bonnet components." \
    "Automotive & Transportation"

test_query \
    "Lightweight, UV-Resistant Polymer" \
    "Outdoor enclosure polymer. Needs to resist UV, be lightweight, and have good impact resistance." \
    "Electronics & Photonics"

# ─────────────────────────────────────────────────────────────
#  TEST SUITE 2: ALLOY QUERIES
# ─────────────────────────────────────────────────────────────
echo -e "${BLUE}TEST SUITE 2: ALLOY QUERIES${NC}\n"

test_query \
    "Aerospace Alloy (Lightweight & Strong)" \
    "Aircraft wing components. Need high strength-to-weight ratio, good fatigue resistance, and corrosion resistance." \
    "Aerospace & Aviation"

test_query \
    "Automotive Structural Alloy" \
    "Car frame component. Needs yield strength minimum 300 MPa, easy to weld, and cost-effective." \
    "Automotive & Transportation"

test_query \
    "Marine Corrosion-Resistant Alloy" \
    "Saltwater exposure. Need excellent corrosion resistance and good strength." \
    "Marine & Naval"

# ─────────────────────────────────────────────────────────────
#  TEST SUITE 3: CERAMIC QUERIES
# ─────────────────────────────────────────────────────────────
echo -e "${BLUE}TEST SUITE 3: CERAMIC QUERIES${NC}\n"

test_query \
    "Cutting Tool Ceramic" \
    "High-speed cutting tool for cast iron. Needs extreme hardness and thermal shock resistance." \
    "Construction & Structural"

test_query \
    "Thermal Barrier Coating" \
    "Extreme temperature application (>1000°C). Need high melting point and low thermal conductivity." \
    "Aerospace & Aviation"

test_query \
    "Wear-Resistant Ceramic" \
    "Pump impeller. High hardness needed, but must resist fracture from cavitation impacts." \
    "Marine & Naval"

# ─────────────────────────────────────────────────────────────
#  TEST SUITE 4: COMPOSITE QUERIES
# ─────────────────────────────────────────────────────────────
echo -e "${BLUE}TEST SUITE 4: COMPOSITE QUERIES${NC}\n"

test_query \
    "Lightweight Aircraft Composite" \
    "Aircraft fuselage panel. Needs high specific modulus (E/ρ), low density, and excellent fatigue resistance." \
    "Aerospace & Aviation"

test_query \
    "Fiber-Reinforced Structural Composite" \
    "Wind turbine blade. High strength, low weight, good thermal stability, high fiber content." \
    "Construction & Structural"

test_query \
    "Impact-Resistant Composite" \
    "Protective equipment shell. Needs good impact absorption with high interlaminar shear strength." \
    "Medical & Biomedical"

# ─────────────────────────────────────────────────────────────
#  TEST SUITE 5: PURE METAL QUERIES
# ─────────────────────────────────────────────────────────────
echo -e "${BLUE}TEST SUITE 5: PURE METAL QUERIES${NC}\n"

test_query \
    "High-Conductivity Thermal Management" \
    "Heat sink for high-power electronics. Need pure metal with maximum thermal conductivity." \
    "Electronics & Photonics"

test_query \
    "Electrical Conductor (High Purity)" \
    "Electrical contacts and busses. Need pure metal with excellent electrical conductivity." \
    "Electronics & Photonics"

# ─────────────────────────────────────────────────────────────
#  TEST SUITE 6: EDGE CASES
# ─────────────────────────────────────────────────────────────
echo -e "${BLUE}TEST SUITE 6: EDGE CASES${NC}\n"

test_query \
    "Ambiguous Query (Multi-Category)" \
    "Strong and light material" \
    "Aerospace & Aviation"

test_query \
    "Vague Temperature Requirement" \
    "Material that works in hot environments" \
    "Overall (Top 1000)"

test_query \
    "Conflicting Requirements" \
    "Low cost, extreme performance, instant delivery" \
    "Overall (Top 1000)"

# ─────────────────────────────────────────────────────────────
#  TEST SUMMARY
# ─────────────────────────────────────────────────────────────
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Test Suite Complete${NC}"
echo -e "${BLUE}========================================${NC}\n"

echo -e "${GREEN}✅ All dispatcher endpoint tests executed.${NC}"
echo -e "Check the output above for results.\n"

echo -e "${YELLOW}Expected Behavior:${NC}"
echo -e "  1. RouteQuery should classify into: Polymers|Alloys|Pure_Metals|Ceramics|Composites"
echo -e "  2. Category-specific search should return 1-3 top candidates"
echo -e "  3. Physics analysis should verify first principles for that category"
echo -e "  4. Manufacturing feasibility should include actionable steps"
echo -e "  5. Token usage should be 1500-2200 per request\n"

echo -e "${YELLOW}Token Usage Estimate:${NC}"
echo -e "  • RouteQuery: 200-300 tokens"
echo -e "  • ExtractIntent: 300-400 tokens"
echo -e "  • ScientificAnalysis: 1200-1500 tokens"
echo -e "  • Total: 1700-2200 tokens per request\n"
