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
	if limit <= 0 {
		limit = 25
	}

	candidates := categorySubset(routedCategory, materials)
	if len(candidates) == 0 {
		candidates = materials
	}

	queryVec := embeddingForQuery(ctx, query)
	idx := getOrBuildVectorIndex(candidates)
	if idx == nil || len(idx.vectors) == 0 {
		return nil
	}

	maxDensity := maxDensityFromConstraints(BuildHeuristicConstraints(query, routedCategory))

	scored := make([]scoredMaterial, 0, len(candidates))
	for _, m := range idx.materials {
		if maxDensity > 0 && m.Density != nil && *m.Density > maxDensity {
			continue
		}
		vec, ok := idx.vectors[m.ID]
		if !ok || len(vec) == 0 {
			continue
		}
		s := cosineSimilarity(queryVec, vec)
		if s <= 0 {
			continue
		}
		s += 0.15 * nameHeuristicBoost(strings.ToLower(query), strings.ToLower(m.Name))
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

	if m.GlassTransitionTemp != nil {
		parts = append(parts, fmt.Sprintf("tg_%.1f", *m.GlassTransitionTemp))
	}
	if m.HeatDeflectionTemp != nil {
		parts = append(parts, fmt.Sprintf("hdt_%.1f", *m.HeatDeflectionTemp))
	}
	if m.MeltingPoint != nil {
		parts = append(parts, fmt.Sprintf("melt_%.1f", *m.MeltingPoint))
	}
	if m.YieldStrength != nil {
		parts = append(parts, fmt.Sprintf("ys_%.1f", *m.YieldStrength))
	}
	if m.ThermalConductivity != nil {
		parts = append(parts, fmt.Sprintf("k_%.2f", *m.ThermalConductivity))
	}
	if m.ElectricalResistivity != nil {
		parts = append(parts, fmt.Sprintf("rho_e_%.6g", *m.ElectricalResistivity))
	}

	return strings.ToLower(strings.Join(parts, " "))
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
	cat := strings.ToLower(m.Category)
	sub := ""
	if m.Subcategory != nil {
		sub = strings.ToLower(*m.Subcategory)
	}

	switch routedCategory {
	case "Polymers":
		return cat == "polymer"
	case "Alloys":
		return cat == "metal" || strings.Contains(cat, "alloy")
	case "Pure_Metals":
		name := strings.ToLower(m.Name)
		if cat != "metal" {
			return false
		}
		return !strings.Contains(name, "alloy") && !strings.Contains(name, "steel") && sub != "ferrous"
	case "Ceramics":
		return cat == "ceramic"
	case "Composites":
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
