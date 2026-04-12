package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/vivek/met-quest/db"
	"github.com/vivek/met-quest/models"
)

func ptr[T any](v T) *T {
	return &v
}

// ──────────────────────────────────────────────────────────────────────────
//  Allowed columns for filtering / sorting (SQL injection prevention)
// ──────────────────────────────────────────────────────────────────────────

var allowedFilterCols = map[string]string{
	"density":                "density",
	"melting_point":          "melting_point",
	"thermal_conductivity":   "thermal_conductivity",
	"electrical_resistivity": "electrical_resistivity",
	"yield_strength":         "yield_strength",
	"tensile_strength":       "tensile_strength",
	"youngs_modulus":         "youngs_modulus",
	"hardness_vickers":       "hardness_vickers",
	"thermal_expansion":      "thermal_expansion",
	"specific_heat":          "specific_heat",
}

var allowedSortCols = allowedFilterCols

// ──────────────────────────────────────────────────────────────────────────
//  SearchMaterials — dynamic SQL from extracted intent
// ──────────────────────────────────────────────────────────────────────────

// SearchMaterials builds and executes a parameterised SQL query from the
// extracted intent, returning up to `limit` matching materials.
func SearchMaterials(ctx context.Context, intent models.IntentJSON, limit int) ([]models.Material, error) {
	if limit <= 0 || limit > 10 {
		limit = 3
	}

	if db.Pool == nil {
		// ── MOCK MODE: IN-MEMORY Filter ──
		return filterInMemory(intent, limit), nil
	}

	var (
		where  []string
		args   []interface{}
		argIdx = 1
	)

	// 1. Category filter
	if intent.Category != "" && intent.Category != "null" {
		where = append(where, fmt.Sprintf("category = $%d", argIdx))
		args = append(args, intent.Category)
		argIdx++
	}

	// 2. Property range filters
	for prop, rf := range intent.Filters {
		col, ok := allowedFilterCols[prop]
		if !ok {
			continue // Silently ignore unknown properties
		}
		if rf.Min != nil {
			where = append(where, fmt.Sprintf("%s >= $%d", col, argIdx))
			args = append(args, *rf.Min)
			argIdx++
		}
		if rf.Max != nil {
			where = append(where, fmt.Sprintf("%s <= $%d", col, argIdx))
			args = append(args, *rf.Max)
			argIdx++
		}
	}

	// 3. Only return rows that have at least density or melting_point (quality gate)
	where = append(where, "(density IS NOT NULL OR melting_point IS NOT NULL)")

	// Build WHERE clause
	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	// 4. Sort column
	sortCol := "density"
	sortDir := "ASC"
	if col, ok := allowedSortCols[intent.SortBy]; ok {
		sortCol = col
	}
	if strings.ToUpper(intent.SortDir) == "DESC" {
		sortDir = "DESC"
	}

	// Materials with null sort key go last
	orderClause := fmt.Sprintf("ORDER BY %s %s NULLS LAST", sortCol, sortDir)

	query := fmt.Sprintf(`
		SELECT
			id, name, formula, category, subcategory,
			density, melting_point, boiling_point,
			thermal_conductivity, specific_heat, thermal_expansion,
			electrical_resistivity,
			yield_strength, tensile_strength, youngs_modulus,
			hardness_vickers, poissons_ratio,
			source, mp_material_id
		FROM materials
		%s
		%s
		LIMIT $%d
	`, whereClause, orderClause, argIdx)

	args = append(args, limit)

	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search query failed: %w", err)
	}
	defer rows.Close()

	return scanMaterials(rows)
}

// ──────────────────────────────────────────────────────────────────────────
//  LookupElements — fetch element data for the predictor
// ──────────────────────────────────────────────────────────────────────────

// LookupElements fetches DB rows for the given element formulas.
// It tries exact formula match first, then falls back to a LIKE search.
func LookupElements(ctx context.Context, formulas []string) (map[string]models.Material, error) {
	result := make(map[string]models.Material)

	if db.Pool == nil {
		// ── MOCK MODE FALLBACK ──
		return lookupInMemory(formulas), nil
	}

	for _, formula := range formulas {
		// Try exact match on formula first
		row, err := lookupSingle(ctx, "formula = $1 AND source = 'Curated'", formula)
		if err != nil || row == nil {
			// Try curated name match
			row, err = lookupSingle(ctx, "LOWER(name) = LOWER($1)", formula)
		}
		if err != nil || row == nil {
			// Try MP formula match (any source)
			row, err = lookupSingle(ctx, "formula = $1", formula)
		}
		if err != nil {
			return nil, err
		}
		if row != nil {
			result[formula] = *row
		}
	}

	return result, nil
}

func lookupSingle(ctx context.Context, condition string, arg interface{}) (*models.Material, error) {
	query := fmt.Sprintf(`
		SELECT id, name, formula, category, subcategory,
			density, melting_point, boiling_point,
			thermal_conductivity, specific_heat, thermal_expansion,
			electrical_resistivity,
			yield_strength, tensile_strength, youngs_modulus,
			hardness_vickers, poissons_ratio, source, mp_material_id
		FROM materials
		WHERE %s
		LIMIT 1
	`, condition)

	rows, err := db.Pool.Query(ctx, query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	mats, err := scanMaterials(rows)
	if err != nil || len(mats) == 0 {
		return nil, err
	}
	return &mats[0], nil
}

// ──────────────────────────────────────────────────────────────────────────
//  Row scanning
// ──────────────────────────────────────────────────────────────────────────

func scanMaterials(rows pgx.Rows) ([]models.Material, error) {
	var results []models.Material

	for rows.Next() {
		var m models.Material
		err := rows.Scan(
			&m.ID, &m.Name, &m.Formula, &m.Category, &m.Subcategory,
			&m.Density, &m.MeltingPoint, &m.BoilingPoint,
			&m.ThermalConductivity, &m.SpecificHeat, &m.ThermalExpansion,
			&m.ElectricalResistivity,
			&m.YieldStrength, &m.TensileStrength, &m.YoungsModulus,
			&m.HardnessVickers, &m.PoissonsRatio,
			&m.Source, &m.MpMaterialID,
		)
		if err != nil {
			return nil, fmt.Errorf("row scan: %w", err)
		}
		results = append(results, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return results, nil
}
