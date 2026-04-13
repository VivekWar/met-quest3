-- ================================================================
--  Smart Alloy Selector — PostgreSQL Schema
--  MET-QUEST '26 | Neon Serverless PostgreSQL
-- ================================================================

-- Enable extensions
CREATE EXTENSION IF NOT EXISTS pg_trgm;   -- for fuzzy name search
CREATE EXTENSION IF NOT EXISTS unaccent;  -- for accent-insensitive search

-- ----------------------------------------------------------------
--  Main materials table
-- ----------------------------------------------------------------
CREATE TABLE IF NOT EXISTS materials (
    id                       SERIAL PRIMARY KEY,
    name                     TEXT NOT NULL,
    formula                  TEXT,
    category                 TEXT,              -- Metal | Ceramic | Polymer | Composite | Semiconductor
    subcategory              TEXT,              -- e.g. Ferrous, Non-Ferrous, Oxide Ceramic …

    -- Core physical properties
    density                  FLOAT,             -- g/cm³
    glass_transition_temp    FLOAT,             -- Kelvin (polymers)
    heat_deflection_temp     FLOAT,             -- Kelvin (polymers)
    melting_point            FLOAT,             -- Kelvin
    boiling_point            FLOAT,             -- Kelvin (if available)
    thermal_conductivity     FLOAT,             -- W/(m·K)
    specific_heat            FLOAT,             -- J/(kg·K)
    thermal_expansion        FLOAT,             -- 10⁻⁶ /K (CTE)

    -- Electrical properties
    electrical_resistivity   FLOAT,             -- Ω·m (×10⁻⁸)

    -- Mechanical properties
    yield_strength           FLOAT,             -- MPa
    tensile_strength         FLOAT,             -- MPa
    youngs_modulus           FLOAT,             -- GPa
    hardness_vickers         FLOAT,             -- HV
    poissons_ratio           FLOAT,

    -- Metadata
    source                   TEXT DEFAULT 'Materials Project',
    mp_material_id           TEXT UNIQUE,       -- e.g. "mp-66"
    notes                    TEXT,
    created_at               TIMESTAMPTZ DEFAULT NOW()
);

-- ----------------------------------------------------------------
--  Indexes for fast range queries (the RAG retrieval layer)
-- ----------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_mat_density         ON materials(density);
CREATE INDEX IF NOT EXISTS idx_mat_tg              ON materials(glass_transition_temp);
CREATE INDEX IF NOT EXISTS idx_mat_hdt             ON materials(heat_deflection_temp);
CREATE INDEX IF NOT EXISTS idx_mat_melting_pt       ON materials(melting_point);
CREATE INDEX IF NOT EXISTS idx_mat_thermal_cond     ON materials(thermal_conductivity);
CREATE INDEX IF NOT EXISTS idx_mat_resistivity      ON materials(electrical_resistivity);
CREATE INDEX IF NOT EXISTS idx_mat_yield_strength   ON materials(yield_strength);
CREATE INDEX IF NOT EXISTS idx_mat_youngs_modulus   ON materials(youngs_modulus);
CREATE INDEX IF NOT EXISTS idx_mat_category         ON materials(category);
CREATE INDEX IF NOT EXISTS idx_mat_formula          ON materials(formula);

-- Full-text / trigram index for name search
CREATE INDEX IF NOT EXISTS idx_mat_name_trgm 
    ON materials USING GIN (name gin_trgm_ops);

-- Backward-compatible migration for existing DBs
ALTER TABLE materials ADD COLUMN IF NOT EXISTS glass_transition_temp FLOAT;
ALTER TABLE materials ADD COLUMN IF NOT EXISTS heat_deflection_temp FLOAT;

-- ----------------------------------------------------------------
--  Query log — track what users ask (useful for evaluation)
-- ----------------------------------------------------------------
CREATE TABLE IF NOT EXISTS query_log (
    id              SERIAL PRIMARY KEY,
    raw_query       TEXT NOT NULL,
    extracted_json  JSONB,
    result_ids      INT[],
    created_at      TIMESTAMPTZ DEFAULT NOW()
);
