#!/usr/bin/env python3
"""
seed_db.py
==========
Reads materials_cleaned.csv and bulk-inserts into a Neon PostgreSQL instance.
Also creates the schema if it doesn't exist.

Requirements:
    pip install psycopg2-binary pandas python-dotenv tqdm

Usage:
    export DATABASE_URL=postgres://user:pass@ep-xxx.neon.tech/neondb?sslmode=require
    python seed_db.py
    
    # Or with a .env file in the project root:
    python seed_db.py
"""

import os
import sys
import logging
from pathlib import Path

import pandas as pd
import psycopg2
import psycopg2.extras
from tqdm import tqdm

# ── Load env manually (no dotenv dependency) ─────────────────────────────────
_env_file = Path(__file__).parent.parent / ".env"
if _env_file.exists():
    for _line in _env_file.read_text().splitlines():
        _line = _line.strip()
        if _line and not _line.startswith("#") and "=" in _line:
            _k, _v = _line.split("=", 1)
            os.environ.setdefault(_k.strip(), _v.strip())

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    handlers=[logging.StreamHandler(sys.stdout)],
)
log = logging.getLogger(__name__)

# ── Paths ─────────────────────────────────────────────────────────────────
DATA_DIR   = Path(__file__).parent
SCHEMA_SQL = DATA_DIR / "schema.sql"
CSV_PATH   = DATA_DIR / "materials_cleaned.csv"

# ── DB ────────────────────────────────────────────────────────────────────
DATABASE_URL = os.getenv("DATABASE_URL")


def get_connection():
    if not DATABASE_URL:
        log.error("DATABASE_URL environment variable is not set.")
        log.error("  → Set it in your .env file or export it directly.")
        sys.exit(1)
    try:
        conn = psycopg2.connect(DATABASE_URL)
        log.info("✅  Connected to PostgreSQL")
        return conn
    except psycopg2.OperationalError as e:
        log.error(f"Connection failed: {e}")
        sys.exit(1)


def apply_schema(conn):
    """Apply schema.sql DDL — creates tables and indexes if not exist."""
    if not SCHEMA_SQL.exists():
        log.error(f"Schema file not found: {SCHEMA_SQL}")
        sys.exit(1)

    schema_sql = SCHEMA_SQL.read_text()
    log.info("Applying schema …")
    with conn.cursor() as cur:
        cur.execute(schema_sql)
    conn.commit()
    log.info("✅  Schema applied")


def safe_val(val):
    """Convert NaN/NA → None for psycopg2."""
    if val is None:
        return None
    try:
        import math
        if isinstance(val, float) and math.isnan(val):
            return None
    except (TypeError, ValueError):
        pass
    if pd.isna(val):
        return None
    return val


def seed_materials(conn, df: pd.DataFrame):
    """Bulk insert materials using execute_values for high speed (>100x faster)."""
    from psycopg2.extras import execute_values

    # 1. Prepare Columns and Clean Data
    columns_full = [
        "mp_material_id", "name", "formula", "category", "subcategory",
        "density", "glass_transition_temp", "heat_deflection_temp", "melting_point", "boiling_point",
        "thermal_conductivity", "specific_heat", "thermal_expansion",
        "electrical_resistivity",
        "yield_strength", "tensile_strength", "youngs_modulus",
        "hardness_vickers", "poissons_ratio",
        "processing_temp_min_c", "processing_temp_max_c", "crystallinity",
        "crystal_system", "fracture_toughness", "weibull_modulus",
        "interlaminar_shear_strength", "fiber_volume_fraction",
        "source", "notes",
    ]

    # Pre-clean the whole dataframe for Postgres
    for col in df.columns:
        df[col] = df[col].apply(safe_val)

    # 2. Split into blocks with and without mp_material_id
    mask_has_id = df["mp_material_id"].notna()
    df_with_id = df[mask_has_id]
    df_no_id = df[~mask_has_id]

    log.info(f"Preparing sync: {len(df_with_id)} items with IDs, {len(df_no_id)} without.")

    with conn.cursor() as cur:
        # --- PHASE A: Bulk UPSERT for Materials with IDs ---
        if not df_with_id.empty:
            log.info("Executing Bulk UPSERT (Chunking 1000) ...")
            upsert_sql = """
                INSERT INTO materials (mp_material_id, name, formula, category, subcategory,
                    density, glass_transition_temp, heat_deflection_temp, melting_point, boiling_point, thermal_conductivity,
                    specific_heat, thermal_expansion, electrical_resistivity,
                    yield_strength, tensile_strength, youngs_modulus,
                    hardness_vickers, poissons_ratio,
                    processing_temp_min_c, processing_temp_max_c, crystallinity,
                    crystal_system, fracture_toughness, weibull_modulus,
                    interlaminar_shear_strength, fiber_volume_fraction,
                    source, notes)
                VALUES %s
                ON CONFLICT (mp_material_id) DO UPDATE SET
                    density               = EXCLUDED.density,
                    glass_transition_temp = EXCLUDED.glass_transition_temp,
                    heat_deflection_temp  = EXCLUDED.heat_deflection_temp,
                    melting_point         = EXCLUDED.melting_point,
                    thermal_conductivity  = EXCLUDED.thermal_conductivity,
                    electrical_resistivity= EXCLUDED.electrical_resistivity,
                    yield_strength        = EXCLUDED.yield_strength,
                    youngs_modulus        = EXCLUDED.youngs_modulus,
                    source                = EXCLUDED.source
            """
            data_with_id = [tuple(row[col] for col in columns_full) for _, row in df_with_id.iterrows()]
            execute_values(cur, upsert_sql, data_with_id, page_size=1000)
            log.info(f"✅  Sync complete for {len(data_with_id)} items.")

        # --- PHASE B: Bulk INSERT for Materials without IDs ---
        if not df_no_id.empty:
            log.info("Executing Bulk INSERT for custom curated items ...")
            cols_no_id = [c for c in columns_full if c != "mp_material_id"]
            insert_no_id_sql = f"""
                INSERT INTO materials ({', '.join(cols_no_id)})
                VALUES %s
                ON CONFLICT DO NOTHING
            """
            data_no_id = [tuple(row[col] for col in cols_no_id) for _, row in df_no_id.iterrows()]
            execute_values(cur, insert_no_id_sql, data_no_id, page_size=1000)
            log.info(f"✅  Sync complete for {len(data_no_id)} items.")

        conn.commit()

    log.info("✅  Database Sync Operations Finished Successfully")


def verify(conn):
    """Print a summary of what's in the DB."""
    with conn.cursor() as cur:
        cur.execute("SELECT COUNT(*) FROM materials")
        total = cur.fetchone()[0]

        cur.execute("SELECT category, COUNT(*) FROM materials GROUP BY category ORDER BY COUNT(*) DESC")
        cats = cur.fetchall()

        cur.execute("""
            SELECT name, formula, density, melting_point, thermal_conductivity
            FROM materials
            ORDER BY RANDOM()
            LIMIT 5
        """)
        samples = cur.fetchall()

    print(f"\n── Database Summary ───────────────────────────────")
    print(f"   Total materials: {total}")
    print(f"\n── By Category ────────────────────────────────────")
    for cat, cnt in cats:
        print(f"   {cat or 'Unknown':<20} {cnt:>5} rows")
    print(f"\n── Random Sample ──────────────────────────────────")
    print(f"{'Name':<40} {'Formula':<15} {'Density':>8} {'Melting':>8} {'ThermCond':>10}")
    for row in samples:
        name, formula, d, m, tc = row
        print(f"  {(name or ''):<38} {(formula or ''):<15} {d or '—':>8} {m or '—':>8} {tc or '—':>10}")
    print("────────────────────────────────────────────────\n")


def main():
    log.info("=" * 60)
    log.info("Smart Alloy Selector — Database Seeder")
    log.info("=" * 60)

    # 1. Load CSV
    if not CSV_PATH.exists():
        log.error(f"CSV not found: {CSV_PATH}")
        log.error("  → Run fetch_materials.py first.")
        sys.exit(1)

    df = pd.read_csv(CSV_PATH)
    log.info(f"Loaded {len(df)} rows from {CSV_PATH.name}")

    # 2. Connect to DB
    conn = get_connection()

    # 3. Apply schema
    apply_schema(conn)

    # 4. Seed data
    seed_materials(conn, df)

    # 5. Verify
    verify(conn)

    conn.close()
    log.info("Done! 🎉")


if __name__ == "__main__":
    main()
