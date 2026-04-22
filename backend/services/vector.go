package services

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/vivek/met-quest/models"
)

const (
	geminiEmbeddingURL = "https://generativelanguage.googleapis.com/v1beta/models/text-embedding-004:embedContent"
	hashEmbedDim       = 128
)

type vectorIndex struct {
	key       string
	materials []models.Material
	vectors   map[int][]float64
}

var vectorCache = struct {
	sync.RWMutex
	idx *vectorIndex
}{}

type geminiEmbeddingRequest struct {
	Model   string `json:"model"`
	Content struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	} `json:"content"`
}

type geminiEmbeddingResponse struct {
	Embedding struct {
		Values []float64 `json:"values"`
	} `json:"embedding"`
}

type scoredMaterial struct {
	m     models.Material
	score float64
}

func HybridVectorRetrieve(ctx context.Context, query string, routedCategory string, materials []models.Material, limit int) []models.Material {
	return IntentVectorRetrieve(ctx, query, models.IntentJSON{}, routedCategory, materials, limit)
}

func IntentVectorRetrieve(ctx context.Context, query string, intent models.IntentJSON, routedCategory string, materials []models.Material, limit int) []models.Material {
	if limit <= 0 {
		limit = 25
	}

	candidates := categorySubset(routedCategory, materials)
	if len(candidates) == 0 {
		candidates = materials
	}

	queryVec := embeddingForIntent(ctx, query, intent, routedCategory)
	idx := getOrBuildVectorIndex(candidates)
	if idx == nil || len(idx.vectors) == 0 {
		return nil
	}

	intentText := strings.ToLower(buildIntentRetrievalText(query, intent, routedCategory))

	scored := make([]scoredMaterial, 0, len(candidates))
	for _, m := range idx.materials {
		vec, ok := idx.vectors[m.ID]
		if !ok || len(vec) == 0 {
			continue
		}
		s := cosineSimilarity(queryVec, vec)
		if s <= 0 {
			continue
		}
		s += 0.18 * nameHeuristicBoost(intentText, strings.ToLower(m.Name))
		s += 0.22 * semanticIntentBoost(m, intent, routedCategory, query)
		scored = append(scored, scoredMaterial{m: m, score: s})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	out := make([]models.Material, 0, len(scored))
	for _, entry := range scored {
		out = append(out, entry.m)
	}
	return out
}

func embeddingForIntent(ctx context.Context, query string, intent models.IntentJSON, routedCategory string) []float64 {
	intentText := strings.TrimSpace(buildIntentRetrievalText(query, intent, routedCategory))
	if intentText == "" {
		intentText = strings.TrimSpace(query)
	}
	if intentText == "" {
		intentText = "general engineering material selection intent"
	}

	if v, err := geminiTextEmbedding(ctx, intentText); err == nil && len(v) > 0 {
		return normalizeVector(v)
	}
	return normalizeVector(hashEmbedding(intentText))
}

func getOrBuildVectorIndex(materials []models.Material) *vectorIndex {
	if len(materials) == 0 {
		return nil
	}

	key := fmt.Sprintf("%d:%d:%d", len(materials), materials[0].ID, materials[len(materials)-1].ID)

	vectorCache.RLock()
	if vectorCache.idx != nil && vectorCache.idx.key == key {
		cached := vectorCache.idx
		vectorCache.RUnlock()
		return cached
	}
	vectorCache.RUnlock()

	vectors := make(map[int][]float64, len(materials))
	for _, m := range materials {
		text := materialEmbeddingText(m)
		vec := hashEmbedding(text)
		if len(vec) == 0 {
			continue
		}
		vectors[m.ID] = normalizeVector(vec)
	}

	idx := &vectorIndex{
		key:       key,
		materials: materials,
		vectors:   vectors,
	}

	vectorCache.Lock()
	vectorCache.idx = idx
	vectorCache.Unlock()
	return idx
}

func embeddingForQuery(ctx context.Context, query string) []float64 {
	q := strings.TrimSpace(query)
	if q == "" {
		return hashEmbedding("default query")
	}

	if v, err := geminiTextEmbedding(ctx, q); err == nil && len(v) > 0 {
		return normalizeVector(v)
	}
	return normalizeVector(hashEmbedding(q))
}

func geminiTextEmbedding(ctx context.Context, text string) ([]float64, error) {
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" || strings.Contains(strings.ToLower(apiKey), "dummy") || strings.Contains(strings.ToLower(apiKey), "your_") {
		return nil, fmt.Errorf("gemini key unavailable")
	}

	payload := geminiEmbeddingRequest{Model: "models/text-embedding-004"}
	payload.Content.Parts = []struct {
		Text string `json:"text"`
	}{{Text: text}}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s?key=%s", geminiEmbeddingURL, apiKey)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding status %d: %s", resp.StatusCode, string(b))
	}

	var out geminiEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Embedding.Values) == 0 {
		return nil, fmt.Errorf("empty embedding values")
	}
	return out.Embedding.Values, nil
}

func materialEmbeddingText(m models.Material) string {
	parts := []string{m.Name, m.Category}
	if m.Subcategory != nil {
		parts = append(parts, *m.Subcategory)
	}
	parts = append(parts, materialSemanticDescriptors(m)...)
	return strings.ToLower(strings.Join(parts, " "))
}

func buildIntentRetrievalText(query string, intent models.IntentJSON, routedCategory string) string {
	s := extractQuerySignals(query)
	parts := []string{
		"user intent",
		strings.TrimSpace(query),
	}

	if routedCategory != "" {
		parts = append(parts, fmt.Sprintf("target family %s", routedCategory))
	}
	if intent.Category != "" {
		parts = append(parts, fmt.Sprintf("intent category %s", intent.Category))
	}

	switch {
	case s.desktopFDM:
		parts = append(parts, "desktop fdm printable realistic hobby setup low warp process feasibility matters")
	case s.requiresCNC:
		parts = append(parts, "machined from stock cnc manufacturable practical shop floor selection")
	case s.requiresConductivity:
		parts = append(parts, "heat transfer thermal path conductivity driven application")
	case s.requiresWear:
		parts = append(parts, "wear resistance abrasion hardness and durability")
	case s.requiresExtremeHeat || s.requiresHotSection:
		parts = append(parts, "high temperature refractory survivability and thermal stability")
	case s.requiresAerospace:
		parts = append(parts, "lightweight structural specific performance and fatigue")
	}

	for prop, rf := range intent.Filters {
		switch prop {
		case "thermal_conductivity":
			parts = append(parts, "prefer very high thermal conductivity")
		case "yield_strength":
			parts = append(parts, "prefer strong structural material")
		case "density":
			if rf.Max != nil {
				parts = append(parts, "prefer lightweight material")
			}
		case "processing_temp":
			parts = append(parts, "process limit and manufacturability important")
		case "electrical_resistivity":
			parts = append(parts, "prefer electrically conductive material")
		}
	}

	if strings.EqualFold(intent.SortBy, "thermal_conductivity") {
		parts = append(parts, "rank by thermal conductivity")
	}
	if strings.EqualFold(intent.SortBy, "yield_strength") {
		parts = append(parts, "rank by strength")
	}

	return strings.Join(parts, ". ")
}

func materialSemanticDescriptors(m models.Material) []string {
	parts := []string{}
	cat := strings.ToLower(m.Category)
	name := strings.ToLower(m.Name)
	sub := ""
	if m.Subcategory != nil {
		sub = strings.ToLower(*m.Subcategory)
	}

	switch cat {
	case "polymer":
		parts = append(parts, "polymer printable plastic candidate")
	case "metal":
		parts = append(parts, "metal machinable engineering material")
	case "ceramic":
		parts = append(parts, "ceramic hard high temperature material")
	case "composite":
		parts = append(parts, "composite lightweight anisotropic material")
	}

	if strings.Contains(name, "copper") {
		parts = append(parts, "excellent heat sink busbar thermal path electrical conductor")
	}
	if strings.Contains(name, "aluminum") || strings.Contains(name, "6061") || strings.Contains(name, "7075") {
		parts = append(parts, "lightweight machinable structural metal")
	}
	if strings.Contains(name, "petg") || strings.Contains(name, "abs") || strings.Contains(name, "pla") || strings.Contains(name, "nylon") {
		parts = append(parts, "fdm printable polymer filament")
	}
	if strings.Contains(name, "peek") || strings.Contains(name, "ultem") {
		parts = append(parts, "high performance polymer difficult hobby printing")
	}
	if sub != "" {
		parts = append(parts, sub)
	}

	if m.ThermalConductivity != nil {
		switch {
		case *m.ThermalConductivity >= 300:
			parts = append(parts, "ultra high thermal conductivity")
		case *m.ThermalConductivity >= 150:
			parts = append(parts, "high thermal conductivity")
		case *m.ThermalConductivity >= 20:
			parts = append(parts, "moderate thermal conductivity")
		}
	}
	if m.ElectricalResistivity != nil && *m.ElectricalResistivity < 2e-8 {
		parts = append(parts, "excellent electrical conductor")
	}
	if m.YieldStrength != nil {
		switch {
		case *m.YieldStrength >= 1200:
			parts = append(parts, "ultra high strength")
		case *m.YieldStrength >= 500:
			parts = append(parts, "high strength")
		case *m.YieldStrength >= 200:
			parts = append(parts, "moderate structural strength")
		}
	}
	if m.Density != nil {
		switch {
		case *m.Density <= 2.2:
			parts = append(parts, "very lightweight")
		case *m.Density <= 5.0:
			parts = append(parts, "lightweight")
		case *m.Density >= 8.0:
			parts = append(parts, "dense metal")
		}
	}
	if m.GlassTransitionTemp != nil && (*m.GlassTransitionTemp-273.15) >= 100 {
		parts = append(parts, "good thermal polymer stability")
	}
	if m.HeatDeflectionTemp != nil && (*m.HeatDeflectionTemp-273.15) >= 100 {
		parts = append(parts, "heat resistant under load")
	}
	if m.MeltingPoint != nil && (*m.MeltingPoint-273.15) >= 1200 {
		parts = append(parts, "refractory or high temperature capable")
	}
	if m.ProcessingTempMaxC != nil && *m.ProcessingTempMaxC <= 270 {
		parts = append(parts, "desktop fdm friendly")
	}
	if m.ProcessingTempMaxC != nil && *m.ProcessingTempMaxC > 340 {
		parts = append(parts, "needs industrial processing temperatures")
	}

	return parts
}

func semanticIntentBoost(m models.Material, intent models.IntentJSON, routedCategory string, query string) float64 {
	score := 0.0
	s := extractQuerySignals(query)

	if matchesRoutedCategory(routedCategory, m) {
		score += 0.15
	}

	if s.requiresConductivity {
		if m.ThermalConductivity != nil {
			score += math.Min(*m.ThermalConductivity/450.0, 0.45)
		}
		if m.ElectricalResistivity != nil && *m.ElectricalResistivity > 0 {
			score += math.Min(2.0e-8/(*m.ElectricalResistivity), 1.5) * 0.08
		}
	}
	if s.desktopFDM {
		if strings.EqualFold(m.Category, "Polymer") || strings.EqualFold(m.Category, "Composite") {
			score += 0.18
		}
		if m.ProcessingTempMaxC != nil {
			switch {
			case *m.ProcessingTempMaxC <= 270:
				score += 0.18
			case *m.ProcessingTempMaxC <= 320:
				score += 0.06
			default:
				score -= 0.16
			}
		}
	}
	if s.requiresCNC && strings.EqualFold(m.Category, "Metal") {
		score += 0.12
	}
	if s.requiresWear && m.HardnessVickers != nil {
		score += math.Min(*m.HardnessVickers/1200.0, 1.0) * 0.18
	}
	if s.requiresAerospace {
		if m.Density != nil {
			score += math.Max(0, 6.0-*m.Density) * 0.03
		}
		if m.YieldStrength != nil {
			score += math.Min(*m.YieldStrength/1200.0, 1.0) * 0.16
		}
	}

	for prop, rf := range intent.Filters {
		switch prop {
		case "thermal_conductivity":
			if rf.Min != nil && m.ThermalConductivity != nil {
				score += clamp01(*m.ThermalConductivity / math.Max(*rf.Min, 1)) * 0.12
			}
		case "yield_strength":
			if rf.Min != nil && m.YieldStrength != nil {
				score += clamp01(*m.YieldStrength / math.Max(*rf.Min, 1)) * 0.1
			}
		case "density":
			if rf.Max != nil && m.Density != nil && *m.Density <= *rf.Max {
				score += 0.08
			}
		}
	}

	return score
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func MergePrimaryCandidates(primary, fallback []models.Material, limit int) []models.Material {
	if limit <= 0 {
		limit = 40
	}
	seen := map[int]bool{}
	out := make([]models.Material, 0, limit)
	for _, group := range [][]models.Material{primary, fallback} {
		for _, m := range group {
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			out = append(out, m)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func hashEmbedding(text string) []float64 {
	vec := make([]float64, hashEmbedDim)
	tokens := strings.Fields(strings.ToLower(text))
	if len(tokens) == 0 {
		return vec
	}

	for _, tok := range tokens {
		h := sha1.Sum([]byte(tok))
		x := binary.BigEndian.Uint64(h[:8])
		y := binary.BigEndian.Uint64(h[8:16])
		idx := int(x % uint64(hashEmbedDim))
		sign := 1.0
		if y%2 == 1 {
			sign = -1.0
		}
		vec[idx] += sign
	}
	return vec
}

func normalizeVector(v []float64) []float64 {
	norm := 0.0
	for _, x := range v {
		norm += x * x
	}
	if norm == 0 {
		return v
	}
	norm = math.Sqrt(norm)
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = x / norm
	}
	return out
}

func cosineSimilarity(a, b []float64) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return 0
	}
	acc := 0.0
	for i := 0; i < n; i++ {
		acc += a[i] * b[i]
	}
	return acc
}

func categorySubset(routedCategory string, materials []models.Material) []models.Material {
	out := make([]models.Material, 0, len(materials))
	for _, m := range materials {
		if matchesRoutedCategory(routedCategory, m) {
			out = append(out, m)
		}
	}
	return out
}

func matchesRoutedCategory(routedCategory string, m models.Material) bool {
	route := strings.ToLower(strings.TrimSpace(routedCategory))
	cat := strings.ToLower(m.Category)
	sub := ""
	if m.Subcategory != nil {
		sub = strings.ToLower(*m.Subcategory)
	}

	switch route {
	case "polymers", "polymer":
		return cat == "polymer"
	case "alloys", "alloy", "metal", "metals":
		return cat == "metal" || strings.Contains(cat, "alloy")
	case "pure_metals", "pure_metal", "pure metals", "pure metal":
		name := strings.ToLower(m.Name)
		if cat != "metal" {
			return false
		}
		return !strings.Contains(name, "alloy") && !strings.Contains(name, "steel") && sub != "ferrous"
	case "ceramics", "ceramic":
		return cat == "ceramic"
	case "composites", "composite":
		return cat == "composite"
	default:
		return true
	}
}

func maxDensityFromConstraints(constraints map[string]interface{}) float64 {
	v, ok := constraints["max_density"].(float64)
	if !ok {
		return 0
	}
	return v
}

func nameHeuristicBoost(query string, materialName string) float64 {
	boost := 0.0
	keywords := []string{"petg", "pla", "polycarbonate", "pc", "tpu", "elastomer", "ptfe", "teflon", "copper", "alumina", "zirconia", "silicon carbide", "7075", "6061"}
	for _, kw := range keywords {
		if strings.Contains(query, kw) && strings.Contains(materialName, kw) {
			boost += 1.0
		}
	}
	return boost
}
