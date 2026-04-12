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

// GetAllMaterials safely returns the fully loaded CSV DB array 
func GetAllMaterials() []models.Material {
	return inMemDB
}

// LoadCSVDB parses the full CSV into memory for blazing-fast local testing without Postgres
func LoadCSVDB() error {
	file, err := os.Open("../data/materials_cleaned.csv")
	if err != nil {
		file, err = os.Open("/home/vivek/Met-Quest/data/materials_cleaned.csv")
		if err != nil {
			return fmt.Errorf("could not open CSV: %w", err)
		}
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return err
	}

	for i, row := range records {
		if i == 0 {
			continue // skip header
		}

		m := models.Material{
			ID:       i,
			Name:     row[0],
			Formula:  row[1],
			Category: row[2],
			Source:   row[16],
		}

		if row[3] != "" {
			sub := row[3]
			m.Subcategory = &sub
		}
		if row[17] != "" {
			mpid := row[17]
			m.MpMaterialID = &mpid
		}

		m.Density = parseFloatOpt(row[4])
		m.MeltingPoint = parseFloatOpt(row[5])
		m.BoilingPoint = parseFloatOpt(row[6])
		m.ThermalConductivity = parseFloatOpt(row[7])
		m.SpecificHeat = parseFloatOpt(row[8])
		m.ThermalExpansion = parseFloatOpt(row[9])
		m.ElectricalResistivity = parseFloatOpt(row[10])
		m.YieldStrength = parseFloatOpt(row[11])
		m.TensileStrength = parseFloatOpt(row[12])
		m.YoungsModulus = parseFloatOpt(row[13])
		m.HardnessVickers = parseFloatOpt(row[14])
		m.PoissonsRatio = parseFloatOpt(row[15])

		inMemDB = append(inMemDB, m)
	}

	log.Printf("📦 Loaded %d materials into High-Speed In-Memory DB", len(inMemDB))
	return nil
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
