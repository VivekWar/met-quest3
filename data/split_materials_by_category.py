#!/usr/bin/env python3
"""
split_materials_by_category.py

Creates modular category CSVs from data/materials_cleaned.csv:
- polymers.csv
- metals.csv
- ceramics.csv
- composites.csv
"""

from pathlib import Path
import pandas as pd

DATA_DIR = Path(__file__).parent
SRC = DATA_DIR / "materials_cleaned.csv"


def ensure_cols(df: pd.DataFrame, cols):
    for c in cols:
        if c not in df.columns:
            df[c] = None
    return df


def write(df: pd.DataFrame, name: str, cols):
    out = DATA_DIR / name
    df = ensure_cols(df.copy(), cols)
    df = df[cols]
    df.to_csv(out, index=False)
    print(f"Wrote {len(df)} rows -> {out}")


def main():
    if not SRC.exists():
        raise SystemExit(f"Missing source CSV: {SRC}")

    df = pd.read_csv(SRC)

    common = [
        "name",
        "formula",
        "category",
        "subcategory",
        "density",
        "thermal_conductivity",
        "yield_strength",
        "tensile_strength",
        "youngs_modulus",
        "hardness_vickers",
        "thermal_expansion",
        "electrical_resistivity",
        "notes",
        "source",
        "mp_material_id",
    ]

    polymer_cols = common + [
        "glass_transition_temp",
        "heat_deflection_temp",
        "processing_temp_min_c",
        "processing_temp_max_c",
        "crystallinity",
    ]

    metal_cols = common + [
        "crystal_system",
    ]

    ceramic_cols = common + [
        "fracture_toughness",
        "weibull_modulus",
    ]

    composite_cols = common + [
        "interlaminar_shear_strength",
        "fiber_volume_fraction",
    ]

    poly = df[df["category"].astype(str).str.lower() == "polymer"]
    met = df[df["category"].astype(str).str.lower() == "metal"]
    cer = df[df["category"].astype(str).str.lower() == "ceramic"]
    comp = df[df["category"].astype(str).str.lower() == "composite"]

    write(poly, "polymers.csv", polymer_cols)
    write(met, "metals.csv", metal_cols)
    write(cer, "ceramics.csv", ceramic_cols)
    write(comp, "composites.csv", composite_cols)


if __name__ == "__main__":
    main()
