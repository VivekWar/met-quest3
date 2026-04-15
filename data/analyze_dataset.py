#!/usr/bin/env python3
"""Quick audit for dataset coverage and null-property rates.

Usage:
  python3 data/analyze_dataset.py
"""

from __future__ import annotations

import csv
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parent
FILES = [
    ROOT / "materials_cleaned.csv",
    ROOT / "metals.csv",
    ROOT / "polymers.csv",
    ROOT / "ceramics.csv",
    ROOT / "composites.csv",
]

PROPS = [
    "yield_strength",
    "glass_transition_temp",
    "heat_deflection_temp",
    "melting_point",
    "thermal_conductivity",
    "electrical_resistivity",
    "processing_temp_max_c",
    "hardness_vickers",
    "fracture_toughness",
]

KEY_TERMS = [
    "petg",
    "pla",
    "polycarbonate",
    "pc-pbt",
    "tpu",
    "elastomer",
    "alumina",
    "zirconia",
    "silicon carbide",
    "7075",
    "6061",
    "copper",
    "ptfe",
    "teflon",
    "peek",
]


def load_rows(path: Path) -> list[dict[str, str]]:
    with path.open(newline="", encoding="utf-8", errors="ignore") as f:
        return list(csv.DictReader(f))


def null_rate(rows: list[dict[str, str]], column: str) -> tuple[int, float]:
    total = len(rows)
    if total == 0:
        return 0, 0.0
    missing = sum(1 for row in rows if not (row.get(column) or "").strip())
    return missing, (missing / total) * 100.0


def term_hits(path: Path, terms: list[str]) -> dict[str, int]:
    text = path.read_text(encoding="utf-8", errors="ignore").lower()
    return {term: (1 if term in text else 0) for term in terms}


def main() -> None:
    print("MET-QUEST Dataset Audit")
    print("=" * 60)

    global_hits = Counter()

    for path in FILES:
        if not path.exists():
            continue

        rows = load_rows(path)
        print(f"\n== {path.name} ({len(rows)} rows) ==")

        categories = Counter((row.get("category") or "").strip() for row in rows)
        print("Top categories:", categories.most_common(8))

        for prop in PROPS:
            missing, pct = null_rate(rows, prop)
            print(f"{prop:24s} missing {missing:5d} ({pct:5.1f}%)")

        hits = term_hits(path, KEY_TERMS)
        for term, count in hits.items():
            global_hits[term] += count

    print("\n== Benchmark term coverage across CSV files ==")
    for term in KEY_TERMS:
        print(f"{term:16s} files_with_term={global_hits[term]}")


if __name__ == "__main__":
    main()
