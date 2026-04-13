package services

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/vivek/met-quest/models"
)

var inMemDB []models.Material
var inMemByClass = map[string][]models.Material{}

// GetAllMaterials safely returns the fully loaded CSV DB array
func GetAllMaterials() []models.Material {
	return inMemDB
}

// GetMaterialsForClass returns the category-specific catalog when available.
func GetMaterialsForClass(class string) []models.Material {
	class = normalizeClass(class)
	if class == "" {
		return inMemDB
	}
	if mats, ok := inMemByClass[class]; ok && len(mats) > 0 {
		return mats
	}
	return inMemDB
}

// RouteMaterialClass determines the best modular catalog from domain + intent + query.
func RouteMaterialClass(domain string, intentCategory string, query string) string {
	if d := strings.ToLower(strings.TrimSpace(domain)); d != "" {
		switch d {
		case strings.ToLower("Plastics & Polymers"):
			return "Polymer"
		case strings.ToLower("Advanced Composites"):
			return "Composite"
		}
	}

	if c := normalizeClass(intentCategory); c != "" {
		return c
	}

	q := strings.ToLower(query)
	switch {
	case strings.Contains(q, "3d print") || strings.Contains(q, "fdm") || strings.Contains(q, "filament") || strings.Contains(q, "resin"):
		return "Polymer"
	case strings.Contains(q, "composite") || strings.Contains(q, "laminate") || strings.Contains(q, "fiber"):
		return "Composite"
	case strings.Contains(q, "ceramic") || strings.Contains(q, "glass") || strings.Contains(q, "refractory"):
		return "Ceramic"
	case strings.Contains(q, "metal") || strings.Contains(q, "alloy") || strings.Contains(q, "steel") || strings.Contains(q, "machin"):
		return "Metal"
	default:
		return ""
	}
}

func normalizeClass(category string) string {
	c := strings.ToLower(strings.TrimSpace(category))
	switch c {
	case "polymer", "polymers", "plastic", "plastics":
		return "Polymer"
	case "metal", "metals", "alloy", "alloys":
		return "Metal"
	case "ceramic", "ceramics", "glass":
		return "Ceramic"
	case "composite", "composites":
		return "Composite"
	default:
		return ""
	}
}

// LoadCSVDB parses the full CSV into memory for blazing-fast local testing without Postgres
func LoadCSVDB() error {
	inMemDB = []models.Material{}
	inMemByClass = map[string][]models.Material{}

	modularFiles := map[string][]string{
		"Polymer":  {"data/polymers.csv", "/app/data/polymers.csv", "../data/polymers.csv"},
		"Metal":    {"data/metals.csv", "/app/data/metals.csv", "../data/metals.csv"},
		"Ceramic":  {"data/ceramics.csv", "/app/data/ceramics.csv", "../data/ceramics.csv"},
		"Composite": {"data/composites.csv", "/app/data/composites.csv", "../data/composites.csv"},
	}

	idCounter := 1
	loadedAnyModular := false
	for class, paths := range modularFiles {
		rows, path, err := loadCSVFromPaths(paths, idCounter)
		if err != nil {
			continue
		}
		loadedAnyModular = true
		for i := range rows {
			if rows[i].Category == "" {
				rows[i].Category = class
			}
		}
		idCounter += len(rows)
		inMemByClass[class] = rows
		inMemDB = append(inMemDB, rows...)
		log.Printf("📂 Modular CSV Loader: %s -> %d rows (%s)", class, len(rows), path)
	}

	if loadedAnyModular {
		log.Printf("📦 Loaded %d materials into modular in-memory catalogs", len(inMemDB))
		return nil
	}

	fallbackPaths := []string{
		"data/materials_cleaned.csv",
		"/app/data/materials_cleaned.csv",
		"../data/materials_cleaned.csv",
		"materials_cleaned.csv",
	}
	rows, foundPath, err := loadCSVFromPaths(fallbackPaths, idCounter)
	if err != nil {
		for _, p := range fallbackPaths {
			log.Printf("❌ CSV Search: Failed to open %s", p)
		}
		return fmt.Errorf("CRITICAL: Materials catalog (CSV) not found. Searched paths: %v. Current working directory: %s", fallbackPaths, func() string { cwd, _ := os.Getwd(); return cwd }())
	}

	inMemDB = rows
	for _, m := range rows {
		class := normalizeClass(m.Category)
		if class != "" {
			inMemByClass[class] = append(inMemByClass[class], m)
		}
	}
	log.Printf("📂 CSV Loader: Successfully opened %s", foundPath)
	log.Printf("📦 Loaded %d materials into High-Speed In-Memory DB", len(inMemDB))
	return nil
}

func loadCSVFromPaths(paths []string, startID int) ([]models.Material, string, error) {
	var file *os.File
	var err error
	var foundPath string
	for _, p := range paths {
		file, err = os.Open(p)
		if err == nil {
			foundPath = p
			break
		}
	}
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, foundPath, err
	}
	if len(records) == 0 {
		return nil, foundPath, fmt.Errorf("CSV is empty: %s", foundPath)
	}

	headerIdx := map[string]int{}
	for i, h := range records[0] {
		headerIdx[strings.TrimSpace(strings.ToLower(h))] = i
	}
	get := func(row []string, key string) string {
		idx, ok := headerIdx[key]
		if !ok || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	materials := make([]models.Material, 0, len(records)-1)
	for i, row := range records {
		if i == 0 {
			continue
		}
		m := models.Material{
			ID:       startID + len(materials),
			Name:     get(row, "name"),
			Formula:  get(row, "formula"),
			Category: get(row, "category"),
			Source:   get(row, "source"),
		}
		if sub := get(row, "subcategory"); sub != "" {
			m.Subcategory = &sub
		}
		if mpid := get(row, "mp_material_id"); mpid != "" {
			m.MpMaterialID = &mpid
		}
		if cs := get(row, "crystal_system"); cs != "" {
			m.CrystalSystem = &cs
		}

		m.Density = parseFloatOpt(get(row, "density"))
		m.GlassTransitionTemp = parseFloatOpt(get(row, "glass_transition_temp"))
		m.HeatDeflectionTemp = parseFloatOpt(get(row, "heat_deflection_temp"))
		m.MeltingPoint = parseFloatOpt(get(row, "melting_point"))
		m.BoilingPoint = parseFloatOpt(get(row, "boiling_point"))
		m.ThermalConductivity = parseFloatOpt(get(row, "thermal_conductivity"))
		m.SpecificHeat = parseFloatOpt(get(row, "specific_heat"))
		m.ThermalExpansion = parseFloatOpt(get(row, "thermal_expansion"))
		m.ElectricalResistivity = parseFloatOpt(get(row, "electrical_resistivity"))
		m.YieldStrength = parseFloatOpt(get(row, "yield_strength"))
		m.TensileStrength = parseFloatOpt(get(row, "tensile_strength"))
		m.YoungsModulus = parseFloatOpt(get(row, "youngs_modulus"))
		m.HardnessVickers = parseFloatOpt(get(row, "hardness_vickers"))
		m.PoissonsRatio = parseFloatOpt(get(row, "poissons_ratio"))
		m.ProcessingTempMinC = parseFloatOpt(get(row, "processing_temp_min_c"))
		m.ProcessingTempMaxC = parseFloatOpt(get(row, "processing_temp_max_c"))
		m.Crystallinity = parseFloatOpt(get(row, "crystallinity"))
		m.FractureToughness = parseFloatOpt(get(row, "fracture_toughness"))
		m.WeibullModulus = parseFloatOpt(get(row, "weibull_modulus"))
		m.InterlaminarShear = parseFloatOpt(get(row, "interlaminar_shear_strength"))
		m.FiberVolumeFraction = parseFloatOpt(get(row, "fiber_volume_fraction"))

		materials = append(materials, m)
	}

	return materials, foundPath, nil
}

func parseFloatOpt(s string) *float64 {
	if s == "" {
		return nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil
	}
	return &f
}

// filterInMemory performs the logic of PostgreSQL natively in Go
func filterInMemory(intent models.IntentJSON, limit int) []models.Material {
	var candidates []models.Material

	for _, m := range inMemDB {
		// Category match
		if intent.Category != "" && intent.Category != "null" {
			if !strings.EqualFold(m.Category, intent.Category) {
				continue
			}
		}

		// Filters match processing map
		props := map[string]*float64{
			"density":                m.Density,
			"melting_point":          m.MeltingPoint,
			"thermal_conductivity":   m.ThermalConductivity,
			"electrical_resistivity": m.ElectricalResistivity,
			"yield_strength":         m.YieldStrength,
			"tensile_strength":       m.TensileStrength,
			"youngs_modulus":         m.YoungsModulus,
			"hardness_vickers":       m.HardnessVickers,
			"thermal_expansion":      m.ThermalExpansion,
			"specific_heat":          m.SpecificHeat,
		}

		passed := true
		for pName, rf := range intent.Filters {
			val, ok := props[pName]
			if !ok {
				continue // unrecognized filter
			}
			if val == nil {
				passed = false // Required field is null on this material
				break
			}
			if rf.Min != nil && *val < *rf.Min {
				passed = false
				break
			}
			if rf.Max != nil && *val > *rf.Max {
				passed = false
				break
			}
		}

		if passed {
			candidates = append(candidates, m)
		}
	}

	// Sort logic
	sortVal := func(m models.Material, col string) float64 {
		switch col {
		case "yield_strength":
			if m.YieldStrength != nil {
				return *m.YieldStrength
			}
		case "density":
			if m.Density != nil {
				return *m.Density
			}
		case "melting_point":
			if m.MeltingPoint != nil {
				return *m.MeltingPoint
			}
		case "thermal_conductivity":
			if m.ThermalConductivity != nil {
				return *m.ThermalConductivity
			}
		case "youngs_modulus":
			if m.YoungsModulus != nil {
				return *m.YoungsModulus
			}
		}
		if intent.SortDir == "DESC" {
			return -999999999.0
		}
		return 999999999.0
	}

	sortCol := intent.SortBy
	if sortCol == "" {
		sortCol = "density"
	}

	sort.Slice(candidates, func(i, j int) bool {
		vi := sortVal(candidates[i], sortCol)
		vj := sortVal(candidates[j], sortCol)
		if intent.SortDir == "DESC" {
			return vi > vj
		}
		return vi < vj
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	return candidates
}

// lookupInMemory replaces LookupElements for Predictor
func lookupInMemory(formulas []string) map[string]models.Material {
	res := make(map[string]models.Material)
	for _, f := range formulas {
		for _, m := range inMemDB {
			if strings.EqualFold(m.Formula, f) || strings.EqualFold(m.Name, f) {
				res[f] = m
				break
			}
		}
	}
	return res
}
