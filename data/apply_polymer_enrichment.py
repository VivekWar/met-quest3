#!/usr/bin/env python3
"""
Apply polymer enrichment (Tg/HDT/processing) onto existing materials_cleaned.csv
without refetching all API rows.
"""

from pathlib import Path
import pandas as pd
import re

DATA_DIR = Path(__file__).parent
SRC = DATA_DIR / "materials_cleaned.csv"
ENRICH = DATA_DIR / "polymer_enrichment.csv"


def normalize_key(s: str) -> str:
    if s is None:
        return ""
    s = str(s).strip().lower()
    return re.sub(r"[^a-z0-9]+", "", s)


def main():
    if not SRC.exists() or not ENRICH.exists():
        raise SystemExit("materials_cleaned.csv or polymer_enrichment.csv missing")

    df = pd.read_csv(SRC)
    e = pd.read_csv(ENRICH)

    for col in [
        "glass_transition_temp",
        "heat_deflection_temp",
        "processing_temp_min_c",
        "processing_temp_max_c",
        "crystallinity",
    ]:
        if col not in df.columns:
            df[col] = None

    df["name_key"] = df["name"].apply(normalize_key)
    df["formula_key"] = df["formula"].apply(normalize_key)
    e["name_key"] = e["name"].apply(normalize_key)
    e["formula_key"] = e["formula"].apply(normalize_key)

    e_by_name = e.set_index("name_key").to_dict("index")
    e_by_formula = e.set_index("formula_key").to_dict("index")

    updates = 0
    for i, row in df.iterrows():
        if str(row.get("category", "")).lower() != "polymer":
            continue
        src = None
        nk = row.get("name_key", "")
        fk = row.get("formula_key", "")
        if nk and nk in e_by_name:
            src = e_by_name[nk]
        elif fk and fk in e_by_formula:
            src = e_by_formula[fk]

        if not src:
            continue

        for col in ["glass_transition_temp", "heat_deflection_temp", "processing_temp_min_c", "processing_temp_max_c", "crystallinity"]:
            if col in src and pd.notna(src[col]):
                df.at[i, col] = src[col]
        updates += 1

    df.drop(columns=["name_key", "formula_key"], inplace=True, errors="ignore")
    df.to_csv(SRC, index=False)
    print(f"Applied enrichment to {updates} polymer rows. Saved {SRC}")


if __name__ == "__main__":
    main()
