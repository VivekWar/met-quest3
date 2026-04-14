#!/usr/bin/env python3
"""
fetch_materials.py
==================
Fetches engineering materials by scraping public internet sources
(no Materials API dependency).

Supplements scraped data with a comprehensive curated table of
~70 common engineering materials with full property data.

Requirements:
    pip install requests pandas python-dotenv tqdm

Usage:
    python fetch_materials.py

Output:
    data/materials_cleaned.csv
"""

import os
import sys
import logging
import re
from io import StringIO
from pathlib import Path
from typing import Optional

import requests
import pandas as pd

try:
    from jarvis.db.figshare import data as jarvis_figshare_data
except Exception:
    jarvis_figshare_data = None

try:
    from bs4 import BeautifulSoup
except Exception:
    BeautifulSoup = None

# ── Logging ───────────────────────────────────────────────────────────────
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    handlers=[logging.StreamHandler(sys.stdout)],
)
log = logging.getLogger(__name__)

# ── Config ────────────────────────────────────────────────────────────────
# Load .env manually (avoid dotenv library issues)
env_file = Path(__file__).parent.parent / ".env"
if env_file.exists():
    for line in env_file.read_text().splitlines():
        line = line.strip()
        if line and not line.startswith("#") and "=" in line:
            k, v = line.split("=", 1)
            os.environ.setdefault(k.strip(), v.strip())

WIKI_ELEMENTS_URL = "https://en.wikipedia.org/wiki/List_of_chemical_elements"
PERIODIC_TABLE_JSON_URL = "https://raw.githubusercontent.com/Bowserinator/Periodic-Table-JSON/master/PeriodicTableJSON.json"
SCRAPE_MAX_ROWS = int(os.getenv("SCRAPE_MAX_ROWS", "6000"))
MAKEITFROM_GROUP_URLS = [
    "https://www.makeitfrom.com/material-group/Thermoplastic",
    "https://www.makeitfrom.com/material-group/Thermoset-Plastic",
    "https://www.makeitfrom.com/material-group/Nickel-Alloy",
    "https://www.makeitfrom.com/material-group/Aluminum-Alloy",
    "https://www.makeitfrom.com/material-group/Copper-Alloy",
    "https://www.makeitfrom.com/material-group/Iron-Alloy",
    "https://www.makeitfrom.com/material-group/Magnesium-Alloy",
    "https://www.makeitfrom.com/material-group/Titanium-Alloy",
    "https://www.makeitfrom.com/material-group/Cobalt-Alloy",
    "https://www.makeitfrom.com/material-group/Other-Metal-Alloy",
    "https://www.makeitfrom.com/material-group/Glass-and-Glass-Ceramic",
    "https://www.makeitfrom.com/material-group/Oxide-Based-Engineering-Ceramic",
    "https://www.makeitfrom.com/material-group/Non-Oxide-Engineering-Ceramic",
]
MAKEITFROM_HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
        "(KHTML, like Gecko) Chrome/124.0 Safari/537.36"
    )
}

DATA_DIR   = Path(__file__).parent
RAW_JSON   = DATA_DIR / "materials_raw.json"
OUTPUT_CSV = DATA_DIR / "materials_cleaned.csv"
POLYMER_ENRICHMENT_CSV = DATA_DIR / "polymer_enrichment.csv"

# ── Category Classifier ───────────────────────────────────────────────────
ELEMENT_METALS = {
    "Li","Be","Na","Mg","Al","K","Ca","Sc","Ti","V","Cr","Mn","Fe",
    "Co","Ni","Cu","Zn","Ga","Rb","Sr","Y","Zr","Nb","Mo","Tc","Ru",
    "Rh","Pd","Ag","Cd","In","Sn","Cs","Ba","La","Ce","Pr","Nd","Pm",
    "Sm","Eu","Gd","Tb","Dy","Ho","Er","Tm","Yb","Lu","Hf","Ta","W",
    "Re","Os","Ir","Pt","Au","Hg","Tl","Pb","Bi"
}
CERAMIC_NONMETALS = {"O", "N", "C", "Si", "B", "S", "P", "F", "Cl"}
SEMICONDUCTOR_ELEMENTS = {"Si", "Ge"}


def classify_category(formula: str, elements: list) -> tuple[str, str]:
    """Returns (category, subcategory)."""
    if not elements:
        return "Unknown", None

    elem_set = set(str(e) for e in elements)

    # Single element
    if len(elem_set) == 1:
        el = list(elem_set)[0]
        if el in SEMICONDUCTOR_ELEMENTS:
            return "Semiconductor", "Elemental"
        if el in ELEMENT_METALS:
            # Check if it's refractory
            refractory = {"W","Mo","Nb","Ta","Re","Hf","Zr","V","Cr","Ti"}
            return "Metal", "Refractory" if el in refractory else "Non-Ferrous"
        return "Ceramic", "Carbon" if el == "C" else "Elemental"

    metals_in = elem_set & ELEMENT_METALS
    nonmetals_in = elem_set & CERAMIC_NONMETALS

    # Iron-based → Ferrous
    if "Fe" in elem_set and not nonmetals_in - {"C"}:
        return "Metal", "Ferrous"

    # Mostly metals with no ceramics formers → Metal alloy
    if metals_in and not nonmetals_in:
        if "Ni" in elem_set and len(metals_in) > 2:
            return "Metal", "Superalloy"
        return "Metal", "Non-Ferrous"

    # Metals + ceramics formers → Ceramic (oxide, nitride, carbide…)
    if metals_in and nonmetals_in:
        if "O" in elem_set:
            return "Ceramic", "Oxide"
        if "N" in elem_set:
            return "Ceramic", "Nitride"
        if "C" in elem_set:
            return "Ceramic", "Carbide"
        if "B" in elem_set:
            return "Ceramic", "Boride"
        return "Ceramic", "Mixed"

    # All nonmetals
    if elem_set <= CERAMIC_NONMETALS:
        return "Ceramic", "Non-Oxide"

    return "Unknown", None


def safe_float(val) -> Optional[float]:
    if val is None:
        return None
    try:
        f = float(val)
        import math
        return None if math.isnan(f) else f
    except (TypeError, ValueError):
        return None


def parse_elements_from_formula(formula: str) -> list[str]:
    if not formula:
        return []
    found = re.findall(r"[A-Z][a-z]?", str(formula))
    if not found:
        return []
    # Keep order, unique symbols
    return list(dict.fromkeys(found))


def extract_first_number(val) -> Optional[float]:
    """Extract first numeric token from noisy scraped text."""
    if val is None:
        return None
    s = str(val).strip().replace(",", "")
    if not s or s.lower() in {"nan", "none", "unknown", "n/a", "na", "-"}:
        return None
    m = re.search(r"-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?", s)
    if not m:
        return None
    return safe_float(m.group(0))


def find_col(cols: list[str], hints: list[str]) -> str:
    lowered = {c.lower(): c for c in cols}
    for h in hints:
        for lc, original in lowered.items():
            if h in lc:
                return original
    return ""


def normalize_text_for_match(text: str) -> str:
    return re.sub(r"\s+", " ", str(text).strip().lower())


def midpoint_from_text(text: str) -> Optional[float]:
    if text is None:
        return None
    s = str(text).strip().replace(",", "")
    if not s:
        return None
    nums = re.findall(r"-?\d+(?:\.\d+)?", s)
    if not nums:
        return None
    values = [safe_float(n) for n in nums if safe_float(n) is not None]
    if not values:
        return None
    if len(values) >= 2 and "to" in s.lower():
        return sum(values[:2]) / 2.0
    return values[0]


def convert_makeitfrom_value(label: str, text: str) -> Optional[float]:
    if not text:
        return None
    label_n = normalize_text_for_match(label)
    text_n = normalize_text_for_match(text)
    value = midpoint_from_text(text_n)
    if value is None:
        return None

    # temperatures
    if any(k in label_n for k in ["temperature", "melting", "glass transition", "heat deflection", "maximum temperature"]):
        if "°c" in text_n or "c" in text_n:
            return value + 273.15
        return value

    # density -> kg/m3
    if "density" in label_n:
        if "g/cm" in text_n:
            return value * 1000.0
        return value

    # moduli -> Pa
    if any(k in label_n for k in ["modulus", "young", "flexural modulus", "shear modulus", "bulk modulus"]):
        if "gpa" in text_n:
            return value * 1e9
        if "mpa" in text_n:
            return value * 1e6
        return value

    # strengths -> Pa
    if any(k in label_n for k in ["strength", "hardness", "fatigue"]):
        if "gpa" in text_n:
            return value * 1e9
        if "mpa" in text_n:
            return value * 1e6
        return value

    # thermal expansion -> 1/K
    if "expansion" in label_n:
        if "µm/m-k" in text_n or "um/m-k" in text_n:
            return value * 1e-6
        return value

    # specific heat / latent heat -> J/kg-K or J/kg
    if "specific heat" in label_n:
        if "j/g" in text_n:
            return value * 1000.0
        return value
    if "latent heat" in label_n:
        if "j/g" in text_n:
            return value * 1000.0
        return value

    # electrical resistivity, dielectric strength, etc. keep numeric scale
    if "resistivity" in label_n:
        if "10x" in text_n:
            exp_vals = re.findall(r"\d+(?:\.\d+)?", text_n)
            if exp_vals:
                nums = [safe_float(v) for v in exp_vals if safe_float(v) is not None]
                if nums:
                    exp = sum(nums[:2]) / min(len(nums), 2)
                    return 10 ** exp
        return value

    return value


def fetch_html(url: str) -> Optional[str]:
    try:
        resp = requests.get(url, headers=MAKEITFROM_HEADERS, timeout=30)
        resp.raise_for_status()
        return resp.text
    except Exception as e:
        log.warning(f"Failed fetching {url}: {e}")
        return None


def soup_from_html(html: str):
    if BeautifulSoup is None:
        return None
    return BeautifulSoup(html, "html.parser")


def collect_makeitfrom_material_links(start_urls: list[str], max_pages: int = 400) -> list[str]:
    if BeautifulSoup is None:
        log.warning("beautifulsoup4 not available; skipping MakeItFrom crawl")
        return []

    from collections import deque
    from urllib.parse import urljoin

    queue = deque([(u, 0) for u in start_urls])
    seen_pages = set()
    material_links = []
    seen_materials = set()

    while queue and len(seen_pages) < max_pages:
        url, depth = queue.popleft()
        if url in seen_pages or depth > 2:
            continue
        seen_pages.add(url)
        html = fetch_html(url)
        if not html:
            continue
        soup = soup_from_html(html)
        if soup is None:
            continue

        for a in soup.find_all("a", href=True):
            href = a.get("href", "")
            if not href:
                continue
            full = urljoin(url, href)
            if "/material-properties/" in full:
                if full not in seen_materials:
                    seen_materials.add(full)
                    material_links.append(full)
            elif "/material-group/" in full and depth < 2:
                if full not in seen_pages:
                    queue.append((full, depth + 1))

    return material_links


def find_text_value(lines: list[str], labels: list[str], lookahead: int = 6) -> Optional[str]:
    normalized_labels = [normalize_text_for_match(l) for l in labels]
    for i, line in enumerate(lines):
        ln = normalize_text_for_match(line)
        if any(lab == ln or lab in ln for lab in normalized_labels):
            for j in range(i + 1, min(i + lookahead + 1, len(lines))):
                cand = lines[j].strip()
                if not cand:
                    continue
                if re.search(r"\d", cand):
                    return cand
    return None


def classify_makeitfrom_page(title: str, url: str) -> tuple[str, str]:
    t = normalize_text_for_match(title)
    u = normalize_text_for_match(url)
    if any(k in t or k in u for k in ["laminate", "fiber", "carbon-carbon", "carbon fiber", "gfrp", "cfrp", "nema"]):
        return "Composite", "Laminate"
    if any(k in t or k in u for k in ["thermoplastic", "plastic", "polyamide", "nylon", "polycarbonate", "pei", "ptfe", "pe", "pp", "abs", "peek", "pbi", "pom", "fluoroplastic", "thermoset"]):
        return "Polymer", "Thermoplastic"
    if any(k in t or k in u for k in ["ceramic", "glass", "silicon carbide", "silicon nitride", "boron carbide", "aluminum nitride", "alumina", "zro2", "cordierite", "sialon"]):
        return "Ceramic", "Engineering"
    return "Metal", "Alloy"


def parse_makeitfrom_material_page(url: str) -> Optional[dict]:
    html = fetch_html(url)
    if not html:
        return None
    soup = soup_from_html(html)
    if soup is None:
        return None

    title_el = soup.find("h1")
    title = title_el.get_text(" ", strip=True) if title_el else url.rsplit("/", 1)[-1]
    lines = [ln.strip() for ln in soup.get_text("\n").splitlines() if ln.strip()]

    cat, subcat = classify_makeitfrom_page(title, url)
    row = {
        "name": title,
        "formula": title,
        "elements": parse_elements_from_formula(title),
        "category": cat,
        "subcategory": subcat,
        "density": None,
        "melting_point": None,
        "boiling_point": None,
        "thermal_conductivity": None,
        "specific_heat": None,
        "thermal_expansion": None,
        "electrical_resistivity": None,
        "yield_strength": None,
        "tensile_strength": None,
        "youngs_modulus": None,
        "hardness_vickers": None,
        "poissons_ratio": None,
        "fracture_toughness": None,
        "weibull_modulus": None,
        "interlaminar_shear_strength": None,
        "fiber_volume_fraction": None,
        "processing_temp_min_c": None,
        "processing_temp_max_c": None,
        "glass_transition_temp": None,
        "heat_deflection_temp": None,
        "source": "MakeItFrom (scraped)",
        "notes": url,
        "mp_material_id": None,
    }

    property_map = [
        ("density", ["density"]),
        ("youngs_modulus", ["elastic (young's, tensile) modulus", "young's modulus", "elastic modulus", "flexural modulus"]),
        ("yield_strength", ["tensile strength: yield (proof)", "yield strength", "compressive strength"]),
        ("tensile_strength", ["tensile strength: ultimate (uts)", "tensile strength"]),
        ("thermal_conductivity", ["thermal conductivity"]),
        ("thermal_expansion", ["thermal expansion"]),
        ("specific_heat", ["specific heat capacity"]),
        ("melting_point", ["melting onset (solidus)", "melting completion (liquidus)"]),
        ("glass_transition_temp", ["glass transition temperature"]),
        ("heat_deflection_temp", ["maximum temperature: mechanical"]),
        ("poissons_ratio", ["poisson's ratio"]),
        ("fracture_toughness", ["fracture toughness"]),
        ("interlaminar_shear_strength", ["shear strength"]),
        ("fiber_volume_fraction", ["fiber volume fraction"]),
        ("electrical_resistivity", ["electrical resistivity order of magnitude", "electrical resistivity"]),
    ]

    for field, labels in property_map:
        text = find_text_value(lines, labels)
        if text:
            row[field] = convert_makeitfrom_value(field, text)

    # Add polymer processing temperatures from service temperature fields when present.
    if row["category"] == "Polymer":
        min_text = find_text_value(lines, ["maximum temperature: mechanical"])
        if min_text:
            row["processing_temp_min_c"] = convert_makeitfrom_value("maximum temperature: mechanical", min_text)
        max_text = find_text_value(lines, ["melting onset (solidus)", "maximum temperature: mechanical"])
        if max_text:
            row["processing_temp_max_c"] = convert_makeitfrom_value("melting onset (solidus)", max_text)

    # Use shear strength for composite laminates.
    if row["category"] == "Composite" and row.get("interlaminar_shear_strength") is None:
        shear = find_text_value(lines, ["shear strength"])
        if shear:
            row["interlaminar_shear_strength"] = convert_makeitfrom_value("shear strength", shear)

    # Keep only rows with at least one useful property.
    useful_fields = [
        "density", "melting_point", "thermal_conductivity", "thermal_expansion",
        "yield_strength", "tensile_strength", "youngs_modulus", "interlaminar_shear_strength",
        "glass_transition_temp", "heat_deflection_temp", "processing_temp_min_c", "processing_temp_max_c",
    ]
    if not any(row.get(f) is not None for f in useful_fields):
        return None

    return row


def scrape_makeitfrom_catalog(start_urls: list[str], max_materials: int = 500) -> list[dict]:
    links = collect_makeitfrom_material_links(start_urls)
    rows: list[dict] = []
    seen_names = set()
    for url in links:
        row = parse_makeitfrom_material_page(url)
        if not row:
            continue
        key = normalize_key(row["name"])
        if key in seen_names:
            continue
        seen_names.add(key)
        rows.append(row)
        if len(rows) >= max_materials:
            break

    log.info(f"Scraped {len(rows)} rows from MakeItFrom")
    return rows


def scrape_wikipedia_elements() -> list[dict]:
    """Scrape elemental material properties from Wikipedia table."""
    try:
        resp = requests.get(
            WIKI_ELEMENTS_URL,
            headers={
                "User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
                              "(KHTML, like Gecko) Chrome/124.0 Safari/537.36"
            },
            timeout=30,
        )
        resp.raise_for_status()
        tables = pd.read_html(StringIO(resp.text))
    except Exception as e:
        log.warning(f"Failed scraping {WIKI_ELEMENTS_URL}: {e}")
        return []

    selected = None
    for t in tables:
        cols = [str(c) for c in t.columns]
        has_symbol = bool(find_col(cols, ["symbol", "sym.", "sym"]))
        has_density = bool(find_col(cols, ["density"]))
        has_melting = bool(find_col(cols, ["melting"]))
        if has_symbol and (has_density or has_melting):
            selected = t
            break

    if selected is None:
        log.warning("Could not locate a usable elements table on Wikipedia")
        return []

    cols = [str(c) for c in selected.columns]
    c_symbol = find_col(cols, ["symbol", "sym.", "sym"])
    c_name = find_col(cols, ["name"])
    c_density = find_col(cols, ["density"])
    c_melting = find_col(cols, ["melting"])
    c_boiling = find_col(cols, ["boiling"])

    rows: list[dict] = []
    for _, r in selected.iterrows():
        symbol = str(r.get(c_symbol, "")).strip() if c_symbol else ""
        if not symbol or len(symbol) > 3:
            continue

        name = str(r.get(c_name, symbol)).strip() if c_name else symbol
        density = extract_first_number(r.get(c_density)) if c_density else None
        melting = extract_first_number(r.get(c_melting)) if c_melting else None
        boiling = extract_first_number(r.get(c_boiling)) if c_boiling else None

        rows.append({
            "name": name,
            "formula": symbol,
            "elements": [symbol],
            "density": density,
            "melting_point": melting,
            "boiling_point": boiling,
            "source": "Wikipedia (scraped)",
            "notes": "Scraped from List of chemical elements",
        })

    log.info(f"Scraped {len(rows)} elemental rows from Wikipedia")
    return rows


def scrape_periodic_table_json() -> list[dict]:
    """Scrape elemental properties from a public GitHub-hosted JSON dataset."""
    try:
        resp = requests.get(
            PERIODIC_TABLE_JSON_URL,
            headers={"User-Agent": "Mozilla/5.0"},
            timeout=30,
        )
        resp.raise_for_status()
        payload = resp.json()
    except Exception as e:
        log.warning(f"Failed scraping periodic dataset: {e}")
        return []

    elements = payload.get("elements") if isinstance(payload, dict) else None
    if not isinstance(elements, list):
        return []

    rows: list[dict] = []
    for el in elements:
        symbol = str(el.get("symbol", "")).strip()
        if not symbol:
            continue
        rows.append({
            "name": str(el.get("name", symbol)).strip(),
            "formula": symbol,
            "elements": [symbol],
            "density": safe_float(el.get("density")),
            "melting_point": safe_float(el.get("melt")),
            "boiling_point": safe_float(el.get("boil")),
            "source": "Periodic-Table-JSON (scraped)",
            "notes": "Scraped from public GitHub dataset",
        })

    log.info(f"Scraped {len(rows)} elemental rows from Periodic-Table-JSON")
    return rows


def scrape_jarvis_3d(max_rows: int = SCRAPE_MAX_ROWS) -> list[dict]:
    """Scrape a large public materials dataset from JARVIS Figshare."""
    if jarvis_figshare_data is None:
        log.warning("jarvis-tools not installed; skipping JARVIS scrape source")
        return []

    try:
        raw = jarvis_figshare_data("dft_3d")
    except Exception as e:
        log.warning(f"Failed scraping JARVIS dataset: {e}")
        return []

    rows: list[dict] = []
    for r in raw:
        formula = str(r.get("formula", "")).strip()
        if not formula:
            continue

        density = safe_float(r.get("density"))
        if density is None:
            # Keep only entries with at least one key property to avoid dropping later.
            continue

        k = safe_float(r.get("bulk_modulus_kv"))
        g = safe_float(r.get("shear_modulus_gv"))
        youngs = None
        if k is not None and g is not None and (3 * k + g) != 0:
            # Isotropic estimate: E = 9KG/(3K+G)
            youngs = (9.0 * k * g) / (3.0 * k + g)

        rows.append({
            "name": formula,
            "formula": formula,
            "elements": parse_elements_from_formula(formula),
            "density": density,
            "melting_point": None,
            "boiling_point": None,
            "youngs_modulus": youngs,
            "source": "JARVIS dft_3d (scraped)",
            "notes": str(r.get("jid", "")),
        })

        if len(rows) >= max_rows:
            break

    log.info(f"Scraped {len(rows)} rows from JARVIS dft_3d")
    return rows


def fetch_from_web() -> list[dict]:
    """Fetch internet-scraped rows only (no API calls)."""
    rows = []
    rows.extend(scrape_jarvis_3d())
    rows.extend(scrape_periodic_table_json())
    # MakeItFrom scraper disabled due to network connectivity issues
    # rows.extend(scrape_makeitfrom_catalog(MAKEITFROM_GROUP_URLS, max_materials=700))
    if not rows:
        rows.extend(scrape_wikipedia_elements())
    return rows


def normalize_key(s: str) -> str:
    if s is None:
        return ""
    s = str(s).strip().lower()
    s = re.sub(r"[^a-z0-9]+", "", s)
    return s


def load_polymer_enrichment() -> pd.DataFrame:
    """Load optional polymer Tg/HDT enrichment table. Now handles multiple category enrichment."""
    # Try loading comprehensive enrichment first (all categories), then fallback to polymer-only
    enrichment_files = [
        Path(__file__).parent / "comprehensive_enrichment.csv",
        POLYMER_ENRICHMENT_CSV,
    ]
    
    df = pd.DataFrame()
    for csv_path in enrichment_files:
        if csv_path.exists():
            try:
                df = pd.read_csv(csv_path)
                log.info(f"Loaded enrichment from {csv_path.name}: {len(df)} rows")
                break
            except Exception as e:
                log.warning(f"Failed to load {csv_path}: {e}")
    
    if df.empty:
        log.warning("No enrichment files found")
        return df
    
    # Normalize numeric columns
    for col in ["density", "melting_point", "thermal_conductivity", "specific_heat", "thermal_expansion",
                "yield_strength", "tensile_strength", "youngs_modulus", "hardness_vickers",
                "glass_transition_temp", "heat_deflection_temp", "processing_temp_min_c", 
                "processing_temp_max_c", "crystallinity", "interlaminar_shear_strength", 
                "fiber_volume_fraction", "fracture_toughness"]:
        if col in df.columns:
            df[col] = pd.to_numeric(df[col], errors="coerce")
    
    # Convert processing temps from Celsius to Kelvin
    for col in ["processing_temp_min_c", "processing_temp_max_c"]:
        if col in df.columns:
            df[col] = df[col].apply(lambda v: v + 273.15 if pd.notna(v) and v < 200 else v)
    
    # Only convert Tg/HDT if they're in Celsius (< 200K means unrealistic)
    for col in ["glass_transition_temp", "heat_deflection_temp"]:
        if col in df.columns:
            df[col] = df[col].apply(lambda v: v + 273.15 if pd.notna(v) and v < 200 else v)
    
    # Normalize keys for deduplication
    df["name_key"] = df["name"].apply(normalize_key) if "name" in df.columns else ""
    df["formula_key"] = df["formula"].apply(normalize_key) if "formula" in df.columns else ""
    log.info(f"Loaded enrichment with {len(df)} rows (all categories)")
    return df


def _canonicalize_enrichment_df(df: pd.DataFrame, source_name: str) -> pd.DataFrame:
    """Normalize enrichment schema from CSV/API into canonical columns."""
    if df.empty:
        return df

    alias = {
        "name": ["name", "material", "material_name", "polymer_name"],
        "formula": ["formula", "chemical_formula", "grade", "type"],
        "glass_transition_temp": [
            "glass_transition_temp", "tg", "tg_k", "tg_kelvin", "glass_transition_temperature",
        ],
        "heat_deflection_temp": [
            "heat_deflection_temp", "hdt", "hdt_k", "hdt_kelvin", "heat_deflection_temperature",
        ],
        "processing_temp_min_c": ["processing_temp_min_c", "processing_min_c", "print_temp_min_c"],
        "processing_temp_max_c": ["processing_temp_max_c", "processing_max_c", "print_temp_max_c"],
        "crystallinity": ["crystallinity", "crystallinity_percent", "x_c"],
    }

    renames = {}
    lower_to_col = {str(c).strip().lower(): c for c in df.columns}
    for canonical, choices in alias.items():
        for c in choices:
            if c.lower() in lower_to_col:
                renames[lower_to_col[c.lower()]] = canonical
                break
    df = df.rename(columns=renames)

    for required in ["name", "formula", "glass_transition_temp", "heat_deflection_temp"]:
        if required not in df.columns:
            df[required] = None

    for col in [
        "glass_transition_temp",
        "heat_deflection_temp",
        "processing_temp_min_c",
        "processing_temp_max_c",
        "crystallinity",
    ]:
        if col not in df.columns:
            df[col] = None
        df[col] = pd.to_numeric(df[col], errors="coerce")

    df["name"] = df["name"].fillna("").astype(str)
    df["formula"] = df["formula"].fillna("").astype(str)
    df["name_key"] = df["name"].apply(normalize_key)
    df["formula_key"] = df["formula"].apply(normalize_key)
    df["source"] = source_name

    keep_cols = [
        "name",
        "formula",
        "category",
        "subcategory",
        "glass_transition_temp",
        "heat_deflection_temp",
        "processing_temp_min_c",
        "processing_temp_max_c",
        "crystallinity",
        "density",
        "melting_point",
        "boiling_point",
        "thermal_conductivity",
        "specific_heat",
        "thermal_expansion",
        "electrical_resistivity",
        "yield_strength",
        "tensile_strength",
        "youngs_modulus",
        "hardness_vickers",
        "poissons_ratio",
        "interlaminar_shear_strength",
        "fiber_volume_fraction",
        "fracture_toughness",
        "name_key",
        "formula_key",
        "source",
    ]
    available_cols = [c for c in keep_cols if c in df.columns]
    return df[available_cols]


# ── Curated Dataset ───────────────────────────────────────────────────────
def load_curated_supplement() -> pd.DataFrame:
    """
    High-quality curated dataset of common engineering materials.
    All values from peer-reviewed engineering references (MatWeb, ASM, etc.)
    """
    data = [
        # ── Pure Elements / Metals ──────────────────────────────────────
        {"name":"Aluminum","formula":"Al","category":"Metal","subcategory":"Non-Ferrous",
         "density":2.70,"melting_point":933,"boiling_point":2792,
         "thermal_conductivity":237,"specific_heat":897,"thermal_expansion":23.1,
         "electrical_resistivity":2.65e-8,"yield_strength":35,"tensile_strength":90,
         "youngs_modulus":70,"hardness_vickers":17,"poissons_ratio":0.33},
        {"name":"Copper","formula":"Cu","category":"Metal","subcategory":"Non-Ferrous",
         "density":8.96,"melting_point":1358,"boiling_point":2835,
         "thermal_conductivity":401,"specific_heat":385,"thermal_expansion":16.5,
         "electrical_resistivity":1.68e-8,"yield_strength":70,"tensile_strength":220,
         "youngs_modulus":120,"hardness_vickers":35,"poissons_ratio":0.34},
        {"name":"Iron","formula":"Fe","category":"Metal","subcategory":"Ferrous",
         "density":7.87,"melting_point":1811,"boiling_point":3134,
         "thermal_conductivity":80,"specific_heat":449,"thermal_expansion":11.8,
         "electrical_resistivity":9.71e-8,"yield_strength":80,"tensile_strength":400,
         "youngs_modulus":211,"hardness_vickers":60,"poissons_ratio":0.29},
        {"name":"Titanium","formula":"Ti","category":"Metal","subcategory":"Non-Ferrous",
         "density":4.51,"melting_point":1941,"boiling_point":3560,
         "thermal_conductivity":21.9,"specific_heat":520,"thermal_expansion":8.6,
         "electrical_resistivity":4.20e-7,"yield_strength":140,"tensile_strength":220,
         "youngs_modulus":116,"hardness_vickers":70,"poissons_ratio":0.32},
        {"name":"Nickel","formula":"Ni","category":"Metal","subcategory":"Non-Ferrous",
         "density":8.91,"melting_point":1728,"boiling_point":3003,
         "thermal_conductivity":91,"specific_heat":444,"thermal_expansion":13.4,
         "electrical_resistivity":6.99e-8,"yield_strength":59,"tensile_strength":317,
         "youngs_modulus":200,"hardness_vickers":64,"poissons_ratio":0.31},
        {"name":"Zinc","formula":"Zn","category":"Metal","subcategory":"Non-Ferrous",
         "density":7.13,"melting_point":693,"boiling_point":1180,
         "thermal_conductivity":116,"specific_heat":388,"thermal_expansion":30.2,
         "electrical_resistivity":5.96e-8,"yield_strength":37,"tensile_strength":100,
         "youngs_modulus":108,"hardness_vickers":30,"poissons_ratio":0.25},
        {"name":"Lead","formula":"Pb","category":"Metal","subcategory":"Non-Ferrous",
         "density":11.34,"melting_point":601,"boiling_point":2022,
         "thermal_conductivity":35.3,"specific_heat":128,"thermal_expansion":28.9,
         "electrical_resistivity":2.08e-7,"yield_strength":11,"tensile_strength":17,
         "youngs_modulus":16,"hardness_vickers":5,"poissons_ratio":0.44},
        {"name":"Magnesium","formula":"Mg","category":"Metal","subcategory":"Non-Ferrous",
         "density":1.74,"melting_point":923,"boiling_point":1363,
         "thermal_conductivity":156,"specific_heat":1020,"thermal_expansion":25.2,
         "electrical_resistivity":4.39e-8,"yield_strength":20,"tensile_strength":165,
         "youngs_modulus":45,"hardness_vickers":30,"poissons_ratio":0.29},
        {"name":"Silver","formula":"Ag","category":"Metal","subcategory":"Precious",
         "density":10.49,"melting_point":1235,"boiling_point":2435,
         "thermal_conductivity":429,"specific_heat":235,"thermal_expansion":18.9,
         "electrical_resistivity":1.59e-8,"yield_strength":55,"tensile_strength":170,
         "youngs_modulus":83,"hardness_vickers":25,"poissons_ratio":0.37},
        {"name":"Gold","formula":"Au","category":"Metal","subcategory":"Precious",
         "density":19.32,"melting_point":1337,"boiling_point":3129,
         "thermal_conductivity":318,"specific_heat":129,"thermal_expansion":14.2,
         "electrical_resistivity":2.44e-8,"yield_strength":100,"tensile_strength":120,
         "youngs_modulus":79,"hardness_vickers":25,"poissons_ratio":0.44},
        {"name":"Chromium","formula":"Cr","category":"Metal","subcategory":"Refractory",
         "density":7.19,"melting_point":2180,"boiling_point":2944,
         "thermal_conductivity":94,"specific_heat":449,"thermal_expansion":4.9,
         "electrical_resistivity":1.25e-7,"yield_strength":279,"tensile_strength":418,
         "youngs_modulus":248,"hardness_vickers":1060,"poissons_ratio":0.21},
        {"name":"Tungsten","formula":"W","category":"Metal","subcategory":"Refractory",
         "density":19.25,"melting_point":3695,"boiling_point":5828,
         "thermal_conductivity":173,"specific_heat":134,"thermal_expansion":4.5,
         "electrical_resistivity":5.28e-8,"yield_strength":750,"tensile_strength":1500,
         "youngs_modulus":411,"hardness_vickers":360,"poissons_ratio":0.28},
        {"name":"Molybdenum","formula":"Mo","category":"Metal","subcategory":"Refractory",
         "density":10.28,"melting_point":2896,"boiling_point":4912,
         "thermal_conductivity":138,"specific_heat":251,"thermal_expansion":4.8,
         "electrical_resistivity":5.34e-8,"yield_strength":324,"tensile_strength":630,
         "youngs_modulus":329,"hardness_vickers":230,"poissons_ratio":0.31},
        {"name":"Platinum","formula":"Pt","category":"Metal","subcategory":"Precious",
         "density":21.45,"melting_point":2041,"boiling_point":4098,
         "thermal_conductivity":72,"specific_heat":133,"thermal_expansion":8.8,
         "electrical_resistivity":1.05e-7,"yield_strength":95,"tensile_strength":125,
         "youngs_modulus":168,"hardness_vickers":56,"poissons_ratio":0.38},
        {"name":"Cobalt","formula":"Co","category":"Metal","subcategory":"Non-Ferrous",
         "density":8.90,"melting_point":1768,"boiling_point":3200,
         "thermal_conductivity":100,"specific_heat":421,"thermal_expansion":13.0,
         "electrical_resistivity":6.24e-8,"yield_strength":276,"tensile_strength":760,
         "youngs_modulus":209,"hardness_vickers":1043,"poissons_ratio":0.31},
        {"name":"Tin","formula":"Sn","category":"Metal","subcategory":"Non-Ferrous",
         "density":7.26,"melting_point":505,"boiling_point":2875,
         "thermal_conductivity":67,"specific_heat":228,"thermal_expansion":22.0,
         "electrical_resistivity":1.15e-7,"yield_strength":14,"tensile_strength":23,
         "youngs_modulus":50,"hardness_vickers":5,"poissons_ratio":0.36},
        {"name":"Vanadium","formula":"V","category":"Metal","subcategory":"Refractory",
         "density":6.11,"melting_point":2183,"boiling_point":3680,
         "thermal_conductivity":31,"specific_heat":489,"thermal_expansion":8.4,
         "electrical_resistivity":1.97e-7,"yield_strength":220,"tensile_strength":310,
         "youngs_modulus":128,"hardness_vickers":628,"poissons_ratio":0.37},
        {"name":"Niobium","formula":"Nb","category":"Metal","subcategory":"Refractory",
         "density":8.57,"melting_point":2750,"boiling_point":5017,
         "thermal_conductivity":54,"specific_heat":265,"thermal_expansion":7.3,
         "electrical_resistivity":1.52e-7,"yield_strength":105,"tensile_strength":275,
         "youngs_modulus":105,"hardness_vickers":132,"poissons_ratio":0.40},
        {"name":"Tantalum","formula":"Ta","category":"Metal","subcategory":"Refractory",
         "density":16.65,"melting_point":3290,"boiling_point":5731,
         "thermal_conductivity":57,"specific_heat":140,"thermal_expansion":6.3,
         "electrical_resistivity":1.31e-7,"yield_strength":170,"tensile_strength":270,
         "youngs_modulus":186,"hardness_vickers":873,"poissons_ratio":0.34},
        {"name":"Palladium","formula":"Pd","category":"Metal","subcategory":"Precious",
         "density":12.02,"melting_point":1828,"boiling_point":3236,
         "thermal_conductivity":72,"specific_heat":246,"thermal_expansion":11.8,
         "electrical_resistivity":1.08e-7,"yield_strength":53,"tensile_strength":180,
         "youngs_modulus":121,"hardness_vickers":37,"poissons_ratio":0.39},
        {"name":"Beryllium","formula":"Be","category":"Metal","subcategory":"Non-Ferrous",
         "density":1.85,"melting_point":1560,"boiling_point":2742,
         "thermal_conductivity":200,"specific_heat":1825,"thermal_expansion":11.3,
         "electrical_resistivity":4e-8,"yield_strength":240,"tensile_strength":400,
         "youngs_modulus":287,"hardness_vickers":130,"poissons_ratio":0.08},
        {"name":"Rhenium","formula":"Re","category":"Metal","subcategory":"Refractory",
         "density":21.02,"melting_point":3459,"boiling_point":5869,
         "thermal_conductivity":48,"specific_heat":138,"thermal_expansion":6.7,
         "electrical_resistivity":1.93e-7,"yield_strength":290,"tensile_strength":1170,
         "youngs_modulus":463,"hardness_vickers":2450,"poissons_ratio":0.30},
        {"name":"Hafnium","formula":"Hf","category":"Metal","subcategory":"Refractory",
         "density":13.31,"melting_point":2506,"boiling_point":4876,
         "thermal_conductivity":23,"specific_heat":144,"thermal_expansion":5.9,
         "electrical_resistivity":3.31e-7,"yield_strength":None,"tensile_strength":485,
         "youngs_modulus":141,"hardness_vickers":None,"poissons_ratio":0.37},
        {"name":"Zirconium","formula":"Zr","category":"Metal","subcategory":"Refractory",
         "density":6.51,"melting_point":2128,"boiling_point":4682,
         "thermal_conductivity":22.7,"specific_heat":278,"thermal_expansion":5.7,
         "electrical_resistivity":4.21e-7,"yield_strength":45,"tensile_strength":170,
         "youngs_modulus":88,"hardness_vickers":None,"poissons_ratio":0.34},
        # ── Engineering Alloys ──────────────────────────────────────────
        {"name":"Steel AISI 1020","formula":"Fe-C","category":"Metal","subcategory":"Ferrous",
         "density":7.85,"melting_point":1773,"boiling_point":None,
         "thermal_conductivity":51,"specific_heat":486,"thermal_expansion":12.0,
         "electrical_resistivity":1.60e-7,"yield_strength":380,"tensile_strength":470,
         "youngs_modulus":200,"hardness_vickers":130,"poissons_ratio":0.29},
        {"name":"Steel AISI 4340","formula":"Fe-Ni-Cr-Mo","category":"Metal","subcategory":"Ferrous",
         "density":7.85,"melting_point":1705,"boiling_point":None,
         "thermal_conductivity":44,"specific_heat":475,"thermal_expansion":12.3,
         "electrical_resistivity":2.48e-7,"yield_strength":793,"tensile_strength":1000,
         "youngs_modulus":205,"hardness_vickers":300,"poissons_ratio":0.29},
        {"name":"Stainless Steel 304","formula":"Fe-Cr-Ni","category":"Metal","subcategory":"Ferrous",
         "density":8.00,"melting_point":1673,"boiling_point":None,
         "thermal_conductivity":16,"specific_heat":500,"thermal_expansion":17.2,
         "electrical_resistivity":7.20e-7,"yield_strength":215,"tensile_strength":505,
         "youngs_modulus":193,"hardness_vickers":129,"poissons_ratio":0.29},
        {"name":"Stainless Steel 316","formula":"Fe-Cr-Ni-Mo","category":"Metal","subcategory":"Ferrous",
         "density":8.00,"melting_point":1648,"boiling_point":None,
         "thermal_conductivity":16,"specific_heat":500,"thermal_expansion":16.0,
         "electrical_resistivity":7.40e-7,"yield_strength":290,"tensile_strength":580,
         "youngs_modulus":193,"hardness_vickers":170,"poissons_ratio":0.27},
        {"name":"Stainless Steel 316L","formula":"Fe-Cr-Ni-Mo-C","category":"Metal","subcategory":"Ferrous",
         "density":8.00,"melting_point":1648,"boiling_point":None,
         "thermal_conductivity":16,"specific_heat":500,"thermal_expansion":16.0,
         "electrical_resistivity":7.40e-7,"yield_strength":170,"tensile_strength":485,
         "youngs_modulus":193,"hardness_vickers":150,"poissons_ratio":0.27},
        {"name":"Duplex Steel 2205","formula":"Fe-Cr-Ni-Mo-N","category":"Metal","subcategory":"Ferrous",
         "density":7.78,"melting_point":1475,"boiling_point":None,
         "thermal_conductivity":19,"specific_heat":500,"thermal_expansion":13.7,
         "electrical_resistivity":8.50e-7,"yield_strength":450,"tensile_strength":620,
         "youngs_modulus":200,"hardness_vickers":290,"poissons_ratio":0.30},
        {"name":"Brass 70Cu-30Zn","formula":"Cu-Zn","category":"Metal","subcategory":"Non-Ferrous",
         "density":8.53,"melting_point":1178,"boiling_point":None,
         "thermal_conductivity":120,"specific_heat":380,"thermal_expansion":19.9,
         "electrical_resistivity":6.20e-8,"yield_strength":200,"tensile_strength":500,
         "youngs_modulus":105,"hardness_vickers":160,"poissons_ratio":0.34},
        {"name":"Bronze 90Cu-10Sn","formula":"Cu-Sn","category":"Metal","subcategory":"Non-Ferrous",
         "density":8.78,"melting_point":1223,"boiling_point":None,
         "thermal_conductivity":50,"specific_heat":380,"thermal_expansion":18.0,
         "electrical_resistivity":1.30e-7,"yield_strength":195,"tensile_strength":350,
         "youngs_modulus":110,"hardness_vickers":100,"poissons_ratio":0.34},
        {"name":"Cupronickel 90-10","formula":"Cu-Ni","category":"Metal","subcategory":"Non-Ferrous",
         "density":8.90,"melting_point":1468,"boiling_point":None,
         "thermal_conductivity":45,"specific_heat":377,"thermal_expansion":17.1,
         "electrical_resistivity":1.90e-7,"yield_strength":105,"tensile_strength":300,
         "youngs_modulus":150,"hardness_vickers":80,"poissons_ratio":0.35},
        {"name":"Aluminum 6061-T6","formula":"Al-Mg-Si","category":"Metal","subcategory":"Non-Ferrous",
         "density":2.70,"melting_point":855,"boiling_point":None,
         "thermal_conductivity":167,"specific_heat":896,"thermal_expansion":23.6,
         "electrical_resistivity":3.99e-8,"yield_strength":276,"tensile_strength":310,
         "youngs_modulus":69,"hardness_vickers":107,"poissons_ratio":0.33},
        {"name":"Aluminum 7075-T6","formula":"Al-Zn-Mg-Cu","category":"Metal","subcategory":"Non-Ferrous",
         "density":2.81,"melting_point":750,"boiling_point":None,
         "thermal_conductivity":130,"specific_heat":960,"thermal_expansion":23.4,
         "electrical_resistivity":5.15e-8,"yield_strength":503,"tensile_strength":572,
         "youngs_modulus":72,"hardness_vickers":175,"poissons_ratio":0.33},
        {"name":"Aluminum 2024-T3","formula":"Al-Cu-Mg","category":"Metal","subcategory":"Non-Ferrous",
         "density":2.78,"melting_point":911,"boiling_point":None,
         "thermal_conductivity":121,"specific_heat":875,"thermal_expansion":23.2,
         "electrical_resistivity":5.82e-8,"yield_strength":345,"tensile_strength":483,
         "youngs_modulus":73,"hardness_vickers":130,"poissons_ratio":0.33},
        {"name":"Ti-6Al-4V (Grade 5)","formula":"Ti-Al-V","category":"Metal","subcategory":"Non-Ferrous",
         "density":4.43,"melting_point":1877,"boiling_point":None,
         "thermal_conductivity":6.7,"specific_heat":560,"thermal_expansion":8.6,
         "electrical_resistivity":1.71e-6,"yield_strength":880,"tensile_strength":950,
         "youngs_modulus":114,"hardness_vickers":349,"poissons_ratio":0.34},
        {"name":"Ti-3Al-2.5V","formula":"Ti-Al-V","category":"Metal","subcategory":"Non-Ferrous",
         "density":4.48,"melting_point":1883,"boiling_point":None,
         "thermal_conductivity":7.5,"specific_heat":544,"thermal_expansion":8.6,
         "electrical_resistivity":1.57e-6,"yield_strength":585,"tensile_strength":690,
         "youngs_modulus":107,"hardness_vickers":250,"poissons_ratio":0.33},
        {"name":"Inconel 718","formula":"Ni-Cr-Fe-Nb","category":"Metal","subcategory":"Superalloy",
         "density":8.19,"melting_point":1609,"boiling_point":None,
         "thermal_conductivity":11.2,"specific_heat":435,"thermal_expansion":13.0,
         "electrical_resistivity":1.25e-6,"yield_strength":1034,"tensile_strength":1241,
         "youngs_modulus":200,"hardness_vickers":350,"poissons_ratio":0.29},
        {"name":"Inconel 625","formula":"Ni-Cr-Mo-Nb","category":"Metal","subcategory":"Superalloy",
         "density":8.44,"melting_point":1623,"boiling_point":None,
         "thermal_conductivity":9.8,"specific_heat":410,"thermal_expansion":12.8,
         "electrical_resistivity":1.29e-6,"yield_strength":415,"tensile_strength":930,
         "youngs_modulus":207,"hardness_vickers":200,"poissons_ratio":0.28},
        {"name":"Hastelloy C-276","formula":"Ni-Mo-Cr","category":"Metal","subcategory":"Superalloy",
         "density":8.89,"melting_point":1598,"boiling_point":None,
         "thermal_conductivity":10.2,"specific_heat":427,"thermal_expansion":11.2,
         "electrical_resistivity":1.25e-6,"yield_strength":414,"tensile_strength":790,
         "youngs_modulus":205,"hardness_vickers":210,"poissons_ratio":0.30},
        {"name":"Monel 400","formula":"Ni-Cu","category":"Metal","subcategory":"Non-Ferrous",
         "density":8.80,"melting_point":1573,"boiling_point":None,
         "thermal_conductivity":21.8,"specific_heat":427,"thermal_expansion":13.9,
         "electrical_resistivity":5.47e-7,"yield_strength":240,"tensile_strength":550,
         "youngs_modulus":179,"hardness_vickers":130,"poissons_ratio":0.32},
        {"name":"Nitinol NiTi","formula":"Ni-Ti","category":"Metal","subcategory":"Shape-Memory",
         "density":6.45,"melting_point":1583,"boiling_point":None,
         "thermal_conductivity":8.6,"specific_heat":490,"thermal_expansion":11.0,
         "electrical_resistivity":8.20e-7,"yield_strength":195,"tensile_strength":895,
         "youngs_modulus":75,"hardness_vickers":250,"poissons_ratio":0.33},
        {"name":"Solder 60Sn-40Pb","formula":"Sn-Pb","category":"Metal","subcategory":"Non-Ferrous",
         "density":8.52,"melting_point":456,"boiling_point":None,
         "thermal_conductivity":50,"specific_heat":168,"thermal_expansion":24.0,
         "electrical_resistivity":1.5e-7,"yield_strength":None,"tensile_strength":60,
         "youngs_modulus":32,"hardness_vickers":14,"poissons_ratio":0.40},
        {"name":"Cast Iron (Gray)","formula":"Fe-C-Si","category":"Metal","subcategory":"Ferrous",
         "density":7.15,"melting_point":1423,"boiling_point":None,
         "thermal_conductivity":51,"specific_heat":490,"thermal_expansion":10.8,
         "electrical_resistivity":1.0e-6,"yield_strength":None,"tensile_strength":250,
         "youngs_modulus":100,"hardness_vickers":200,"poissons_ratio":0.26},
        {"name":"Maraging Steel 300","formula":"Fe-Ni-Co-Mo","category":"Metal","subcategory":"Ferrous",
         "density":8.00,"melting_point":1723,"boiling_point":None,
         "thermal_conductivity":25,"specific_heat":460,"thermal_expansion":10.4,
         "electrical_resistivity":6.0e-7,"yield_strength":1900,"tensile_strength":1965,
         "youngs_modulus":190,"hardness_vickers":600,"poissons_ratio":0.30},
        # ── Ceramics ────────────────────────────────────────────────────
        {"name":"Alumina Al2O3 99%","formula":"Al2O3","category":"Ceramic","subcategory":"Oxide",
         "density":3.95,"melting_point":2345,"boiling_point":3250,
         "thermal_conductivity":30,"specific_heat":880,"thermal_expansion":8.1,
         "electrical_resistivity":1e10,"yield_strength":None,"tensile_strength":260,
         "youngs_modulus":370,"hardness_vickers":1500,"poissons_ratio":0.22},
        {"name":"Silicon Carbide SiC","formula":"SiC","category":"Ceramic","subcategory":"Carbide",
         "density":3.21,"melting_point":3003,"boiling_point":None,
         "thermal_conductivity":120,"specific_heat":750,"thermal_expansion":4.0,
         "electrical_resistivity":1e4,"yield_strength":None,"tensile_strength":400,
         "youngs_modulus":410,"hardness_vickers":2800,"poissons_ratio":0.16},
        {"name":"Silicon Nitride Si3N4","formula":"Si3N4","category":"Ceramic","subcategory":"Nitride",
         "density":3.19,"melting_point":2173,"boiling_point":None,
         "thermal_conductivity":25,"specific_heat":700,"thermal_expansion":3.2,
         "electrical_resistivity":1e12,"yield_strength":None,"tensile_strength":600,
         "youngs_modulus":300,"hardness_vickers":1700,"poissons_ratio":0.27},
        {"name":"Zirconia ZrO2 (Stabilized)","formula":"ZrO2","category":"Ceramic","subcategory":"Oxide",
         "density":6.05,"melting_point":2988,"boiling_point":None,
         "thermal_conductivity":2.0,"specific_heat":400,"thermal_expansion":10.5,
         "electrical_resistivity":1e12,"yield_strength":None,"tensile_strength":800,
         "youngs_modulus":210,"hardness_vickers":1200,"poissons_ratio":0.31},
        {"name":"Boron Carbide B4C","formula":"B4C","category":"Ceramic","subcategory":"Carbide",
         "density":2.52,"melting_point":2763,"boiling_point":None,
         "thermal_conductivity":30,"specific_heat":950,"thermal_expansion":5.0,
         "electrical_resistivity":0.3,"yield_strength":None,"tensile_strength":350,
         "youngs_modulus":450,"hardness_vickers":3000,"poissons_ratio":0.17},
        {"name":"Magnesia MgO","formula":"MgO","category":"Ceramic","subcategory":"Oxide",
         "density":3.58,"melting_point":3098,"boiling_point":3873,
         "thermal_conductivity":48,"specific_heat":877,"thermal_expansion":13.5,
         "electrical_resistivity":1e13,"yield_strength":None,"tensile_strength":100,
         "youngs_modulus":248,"hardness_vickers":750,"poissons_ratio":0.18},
        {"name":"Tungsten Carbide WC","formula":"WC","category":"Ceramic","subcategory":"Carbide",
         "density":15.63,"melting_point":3058,"boiling_point":None,
         "thermal_conductivity":110,"specific_heat":203,"thermal_expansion":5.5,
         "electrical_resistivity":2e-7,"yield_strength":None,"tensile_strength":1700,
         "youngs_modulus":696,"hardness_vickers":2400,"poissons_ratio":0.24},
        {"name":"Aluminium Nitride AlN","formula":"AlN","category":"Ceramic","subcategory":"Nitride",
         "density":3.26,"melting_point":2473,"boiling_point":None,
         "thermal_conductivity":170,"specific_heat":780,"thermal_expansion":4.6,
         "electrical_resistivity":1e11,"yield_strength":None,"tensile_strength":300,
         "youngs_modulus":320,"hardness_vickers":1200,"poissons_ratio":0.24},
        {"name":"Titanium Nitride TiN","formula":"TiN","category":"Ceramic","subcategory":"Nitride",
         "density":5.22,"melting_point":3220,"boiling_point":None,
         "thermal_conductivity":19,"specific_heat":616,"thermal_expansion":9.4,
         "electrical_resistivity":2.5e-7,"yield_strength":None,"tensile_strength":None,
         "youngs_modulus":600,"hardness_vickers":2100,"poissons_ratio":0.25},
        {"name":"Titania TiO2 (Rutile)","formula":"TiO2","category":"Ceramic","subcategory":"Oxide",
         "density":4.26,"melting_point":2116,"boiling_point":None,
         "thermal_conductivity":8.0,"specific_heat":690,"thermal_expansion":8.2,
         "electrical_resistivity":1e6,"yield_strength":None,"tensile_strength":100,
         "youngs_modulus":290,"hardness_vickers":1100,"poissons_ratio":0.27},
        {"name":"Cordierite Mg2Al4Si5O18","formula":"Mg2Al4Si5O18","category":"Ceramic","subcategory":"Oxide",
         "density":2.51,"melting_point":1733,"boiling_point":None,
         "thermal_conductivity":3.5,"specific_heat":790,"thermal_expansion":1.5,
         "electrical_resistivity":1e12,"yield_strength":None,"tensile_strength":None,
         "youngs_modulus":135,"hardness_vickers":None,"poissons_ratio":0.25},
        {"name":"Borosilicate Glass","formula":"SiO2-B2O3","category":"Ceramic","subcategory":"Glass",
         "density":2.23,"melting_point":1098,"boiling_point":None,
         "thermal_conductivity":1.2,"specific_heat":830,"thermal_expansion":3.3,
         "electrical_resistivity":1e8,"yield_strength":None,"tensile_strength":60,
         "youngs_modulus":63,"hardness_vickers":520,"poissons_ratio":0.20},
        {"name":"Fused Silica SiO2","formula":"SiO2","category":"Ceramic","subcategory":"Glass",
         "density":2.20,"melting_point":1983,"boiling_point":None,
         "thermal_conductivity":1.38,"specific_heat":740,"thermal_expansion":0.55,
         "electrical_resistivity":1e16,"yield_strength":None,"tensile_strength":50,
         "youngs_modulus":72,"hardness_vickers":1100,"poissons_ratio":0.17},
        {"name":"Hydroxyapatite","formula":"Ca10(PO4)6(OH)2","category":"Ceramic","subcategory":"Bioceramic",
         "density":3.16,"melting_point":1823,"boiling_point":None,
         "thermal_conductivity":1.3,"specific_heat":700,"thermal_expansion":11.5,
         "electrical_resistivity":1e10,"yield_strength":None,"tensile_strength":40,
         "youngs_modulus":117,"hardness_vickers":600,"poissons_ratio":0.28},
        # ── Semiconductors ───────────────────────────────────────────────
        {"name":"Silicon","formula":"Si","category":"Semiconductor","subcategory":"Elemental",
         "density":2.33,"melting_point":1687,"boiling_point":3538,
         "thermal_conductivity":150,"specific_heat":712,"thermal_expansion":2.6,
         "electrical_resistivity":640,"yield_strength":None,"tensile_strength":7000,
         "youngs_modulus":185,"hardness_vickers":1000,"poissons_ratio":0.28},
        {"name":"Germanium","formula":"Ge","category":"Semiconductor","subcategory":"Elemental",
         "density":5.32,"melting_point":1211,"boiling_point":3106,
         "thermal_conductivity":60,"specific_heat":321,"thermal_expansion":5.8,
         "electrical_resistivity":4.6e-1,"yield_strength":None,"tensile_strength":None,
         "youngs_modulus":130,"hardness_vickers":692,"poissons_ratio":0.26},
        {"name":"Gallium Arsenide GaAs","formula":"GaAs","category":"Semiconductor","subcategory":"III-V",
         "density":5.32,"melting_point":1511,"boiling_point":None,
         "thermal_conductivity":46,"specific_heat":330,"thermal_expansion":6.0,
         "electrical_resistivity":1e-3,"yield_strength":None,"tensile_strength":None,
         "youngs_modulus":86,"hardness_vickers":750,"poissons_ratio":0.31},
        {"name":"Gallium Nitride GaN","formula":"GaN","category":"Semiconductor","subcategory":"III-V",
         "density":6.15,"melting_point":2773,"boiling_point":None,
         "thermal_conductivity":130,"specific_heat":490,"thermal_expansion":5.6,
         "electrical_resistivity":1e-3,"yield_strength":None,"tensile_strength":None,
         "youngs_modulus":181,"hardness_vickers":1580,"poissons_ratio":0.35},
        {"name":"Silicon Carbide 4H-SiC","formula":"SiC","category":"Semiconductor","subcategory":"Wide-Bandgap",
         "density":3.21,"melting_point":3003,"boiling_point":None,
         "thermal_conductivity":370,"specific_heat":750,"thermal_expansion":4.2,
         "electrical_resistivity":1e-2,"yield_strength":None,"tensile_strength":None,
         "youngs_modulus":694,"hardness_vickers":2800,"poissons_ratio":0.16},
        # ── Polymers ────────────────────────────────────────────────────
        {"name":"PTFE Teflon","formula":"(C2F4)n","category":"Polymer","subcategory":"Fluoropolymer",
         "density":2.20,"melting_point":600,"boiling_point":None,
         "thermal_conductivity":0.25,"specific_heat":1000,"thermal_expansion":135,
         "electrical_resistivity":1e16,"yield_strength":None,"tensile_strength":31,
         "youngs_modulus":0.5,"hardness_vickers":None,"poissons_ratio":0.46},
        {"name":"PEEK","formula":"(C19H12O3)n","category":"Polymer","subcategory":"Thermoplastic",
         "density":1.32,"melting_point":616,"boiling_point":None,
         "thermal_conductivity":0.25,"specific_heat":320,"thermal_expansion":47,
         "electrical_resistivity":4.9e13,"yield_strength":91,"tensile_strength":100,
         "youngs_modulus":3.6,"hardness_vickers":None,"poissons_ratio":0.40},
        {"name":"Nylon 6/6 PA66","formula":"(C12H22N2O2)n","category":"Polymer","subcategory":"Polyamide",
         "density":1.14,"melting_point":538,"boiling_point":None,
         "thermal_conductivity":0.25,"specific_heat":1700,"thermal_expansion":80,
         "electrical_resistivity":1e12,"yield_strength":60,"tensile_strength":83,
         "youngs_modulus":3.0,"hardness_vickers":None,"poissons_ratio":0.40},
        {"name":"Polycarbonate PC","formula":"(C15H16O2)n","category":"Polymer","subcategory":"Thermoplastic",
         "density":1.20,"melting_point":423,"boiling_point":None,
         "thermal_conductivity":0.20,"specific_heat":1200,"thermal_expansion":65,
         "electrical_resistivity":1e14,"yield_strength":55,"tensile_strength":65,
         "youngs_modulus":2.6,"hardness_vickers":None,"poissons_ratio":0.37},
        {"name":"Polypropylene PP","formula":"(C3H6)n","category":"Polymer","subcategory":"Thermoplastic",
         "density":0.90,"melting_point":433,"boiling_point":None,
         "thermal_conductivity":0.22,"specific_heat":1700,"thermal_expansion":100,
         "electrical_resistivity":1e14,"yield_strength":30,"tensile_strength":35,
         "youngs_modulus":1.4,"hardness_vickers":None,"poissons_ratio":0.42},
        {"name":"Ultem PEI","formula":"(C37H24N2O6)n","category":"Polymer","subcategory":"Thermoplastic",
         "density":1.27,"melting_point":490,"boiling_point":None,
         "thermal_conductivity":0.22,"specific_heat":1100,"thermal_expansion":56,
         "electrical_resistivity":1e15,"yield_strength":105,"tensile_strength":105,
         "youngs_modulus":3.3,"hardness_vickers":None,"poissons_ratio":0.36},
        # ── Composites ───────────────────────────────────────────────────
        {"name":"CFRP Unidirectional","formula":"C/Epoxy","category":"Composite","subcategory":"PMC",
         "density":1.55,"melting_point":None,"boiling_point":None,
         "thermal_conductivity":5,"specific_heat":1050,"thermal_expansion":0.5,
         "electrical_resistivity":1.7e-5,"yield_strength":None,"tensile_strength":1500,
         "youngs_modulus":135,"hardness_vickers":None,"poissons_ratio":0.30},
        {"name":"GFRP Woven","formula":"SiO2/Epoxy","category":"Composite","subcategory":"PMC",
         "density":1.80,"melting_point":None,"boiling_point":None,
         "thermal_conductivity":0.35,"specific_heat":1000,"thermal_expansion":11,
         "electrical_resistivity":1e12,"yield_strength":None,"tensile_strength":900,
         "youngs_modulus":40,"hardness_vickers":None,"poissons_ratio":0.25},
        {"name":"Al-SiC Metal Matrix Composite","formula":"Al-SiC","category":"Composite","subcategory":"MMC",
         "density":2.90,"melting_point":None,"boiling_point":None,
         "thermal_conductivity":170,"specific_heat":864,"thermal_expansion":12.4,
         "electrical_resistivity":4e-8,"yield_strength":310,"tensile_strength":400,
         "youngs_modulus":115,"hardness_vickers":130,"poissons_ratio":0.30},
        # ── Special Materials ────────────────────────────────────────────
        {"name":"Graphite Isotropic","formula":"C","category":"Ceramic","subcategory":"Carbon",
         "density":1.81,"melting_point":3948,"boiling_point":None,
         "thermal_conductivity":120,"specific_heat":720,"thermal_expansion":4.0,
         "electrical_resistivity":1.3e-5,"yield_strength":None,"tensile_strength":50,
         "youngs_modulus":10,"hardness_vickers":None,"poissons_ratio":0.20},
        {"name":"Diamond CVD","formula":"C","category":"Ceramic","subcategory":"Carbon",
         "density":3.51,"melting_point":4300,"boiling_point":None,
         "thermal_conductivity":2000,"specific_heat":502,"thermal_expansion":1.0,
         "electrical_resistivity":1e12,"yield_strength":None,"tensile_strength":2800,
         "youngs_modulus":1000,"hardness_vickers":10000,"poissons_ratio":0.07},
        {"name":"Graphene (monolayer)","formula":"C","category":"Semiconductor","subcategory":"2D Material",
         "density":0.77,"melting_point":None,"boiling_point":None,
         "thermal_conductivity":5000,"specific_heat":700,"thermal_expansion":None,
         "electrical_resistivity":1e-6,"yield_strength":None,"tensile_strength":130000,
         "youngs_modulus":1000,"hardness_vickers":None,"poissons_ratio":0.16},
        {"name":"Aerogel Silica","formula":"SiO2","category":"Ceramic","subcategory":"Nanomaterial",
         "density":0.10,"melting_point":None,"boiling_point":None,
         "thermal_conductivity":0.015,"specific_heat":1000,"thermal_expansion":None,
         "electrical_resistivity":1e12,"yield_strength":None,"tensile_strength":None,
         "youngs_modulus":0.002,"hardness_vickers":None,"poissons_ratio":None},
    ]

    df = pd.DataFrame(data)
    df["source"] = "Curated"
    df["mp_material_id"] = None
    df["notes"] = None
    if "glass_transition_temp" not in df.columns:
        df["glass_transition_temp"] = None
    if "heat_deflection_temp" not in df.columns:
        df["heat_deflection_temp"] = None
    df["boiling_point"] = df.get("boiling_point")
    return df


def build_api_rows(raw_rows: list[dict]) -> pd.DataFrame:
    """Convert raw scraped rows to clean DataFrame."""
    rows = []
    for r in raw_rows:
        elements = r.get("elements", [])
        formula  = r.get("formula", "") or ""
        name = r.get("name", formula) or formula
        category, subcategory = classify_category(formula, elements)

        rows.append({
            "mp_material_id"        : r.get("mp_material_id"),
            "name"                  : name,
            "formula"               : formula,
            "category"              : category,
            "subcategory"           : subcategory,
            "density"               : safe_float(r.get("density")),
            "glass_transition_temp" : None,
            "heat_deflection_temp"  : None,
            "melting_point"         : safe_float(r.get("melting_point")),
            "boiling_point"         : safe_float(r.get("boiling_point")),
            "thermal_conductivity"  : None,
            "specific_heat"         : None,
            "thermal_expansion"     : None,
            "electrical_resistivity": None,
            "yield_strength"        : None,
            "tensile_strength"      : None,
            "youngs_modulus"        : safe_float(r.get("youngs_modulus")),
            "hardness_vickers"      : None,
            "poissons_ratio"        : None,
            "processing_temp_min_c" : safe_float(r.get("processing_temp_min_c")),
            "processing_temp_max_c" : safe_float(r.get("processing_temp_max_c")),
            "crystallinity"         : safe_float(r.get("crystallinity")),
            "fracture_toughness"    : safe_float(r.get("fracture_toughness")),
            "weibull_modulus"       : safe_float(r.get("weibull_modulus")),
            "interlaminar_shear_strength": safe_float(r.get("interlaminar_shear_strength")),
            "fiber_volume_fraction"  : safe_float(r.get("fiber_volume_fraction")),
            "source"                : r.get("source", "Web Scraped"),
            "notes"                 : r.get("notes"),
        })
    return pd.DataFrame(rows)


def merge_datasets(api_df: pd.DataFrame, curated_df: pd.DataFrame) -> pd.DataFrame:
    """Merge API + curated, dedup by formula, curated takes priority."""
    if api_df.empty:
        log.info("Using curated data only (no API rows)")
        return curated_df.copy()

    combined = pd.concat([curated_df, api_df], ignore_index=True)

    # Drop exact formula duplicates (curated first = kept)
    combined.drop_duplicates(subset=["formula"], keep="first", inplace=True)

    # Drop rows with NO useful properties
    prop_cols = [
        "density","glass_transition_temp","heat_deflection_temp","melting_point",
        "thermal_conductivity","electrical_resistivity","youngs_modulus",
        "tensile_strength","yield_strength","fracture_toughness",
        "interlaminar_shear_strength","specific_heat","thermal_expansion",
    ]
    mask = combined[prop_cols].notna().any(axis=1)
    combined = combined[mask].copy()

    combined.reset_index(drop=True, inplace=True)
    log.info(f"Merged dataset: {len(combined)} materials")
    return combined


def apply_polymer_enrichment(df: pd.DataFrame, enrich_df: pd.DataFrame) -> pd.DataFrame:
    """Apply enrichment to rows by exact name/formula match (supports all categories, handles duplicates)."""
    if enrich_df.empty:
        return df
    
    required_cols = [
        "glass_transition_temp", "heat_deflection_temp", "processing_temp_min_c", "processing_temp_max_c",
        "crystallinity", "interlaminar_shear_strength", "fiber_volume_fraction", "fracture_toughness"
    ]
    for col in required_cols:
        if col not in df.columns:
            df[col] = None
    
    df["name_key"] = df["name"].apply(normalize_key)
    df["formula_key"] = df["formula"].apply(normalize_key)
    enrich_df_local = enrich_df.copy()
    enrich_df_local["name_key"] = enrich_df_local["name"].apply(normalize_key)
    enrich_df_local["formula_key"] = enrich_df_local["formula"].apply(normalize_key)
    
    updates = 0
    for i, row in df.iterrows():
        key_name = row.get("name_key", "")
        key_formula = row.get("formula_key", "")
        row_cat = str(row.get("category", "")).lower()
        
        matches = enrich_df_local[enrich_df_local["name_key"] == key_name] if key_name else pd.DataFrame()
        
        if matches.empty and key_formula and "category" in enrich_df_local.columns:
            matches = enrich_df_local[
                (enrich_df_local["formula_key"] == key_formula) & 
                (enrich_df_local["category"].str.lower() == row_cat)
            ]
        elif matches.empty and key_formula:
            matches = enrich_df_local[enrich_df_local["formula_key"] == key_formula]
        
        if not matches.empty:
            src = matches.iloc[0].to_dict()
            for col in required_cols:
                if col in src and pd.notna(src[col]):
                    df.at[i, col] = src[col]
            updates += 1
    
    df.drop(columns=["name_key", "formula_key"], inplace=True, errors="ignore")
    log.info(f"Applied enrichment to {updates} rows (all categories)")
    return df


def main():
    log.info("=" * 60)
    log.info("Smart Alloy Selector — Materials Data Fetcher v2")
    log.info("=" * 60)

    # 1. Fetch from internet scraping only (no API)
    scraped_rows = []
    try:
        scraped_rows = fetch_from_web()
    except Exception as e:
        log.warning(f"Web scraping fetch error: {e} — using curated only")

    scraped_df = build_api_rows(scraped_rows) if scraped_rows else pd.DataFrame()

    # 2. Load curated supplement
    log.info("Loading curated dataset …")
    curated_df = load_curated_supplement()
    log.info(f"Curated: {len(curated_df)} materials")

    # 2b. Load enrichment materials (these get added as new rows + used for enrichment)
    enrich_df = _canonicalize_enrichment_df(load_polymer_enrichment(), "CSV")

    # 3. Merge scraped + curated
    final_df = merge_datasets(scraped_df, curated_df)
    
    # 3b. Add enrichment materials that don't exist in final_df (expand database)
    before_enrich = len(final_df)
    if not enrich_df.empty:
        final_df["name_key"] = final_df["name"].apply(normalize_key)
        for _, erow in enrich_df.iterrows():
            ekey = normalize_key(erow.get("name", ""))
            if ekey and ekey not in final_df["name_key"].values:
                # Add this enrichment row as a new material
                erow_dict = erow.to_dict()
                erow_dict["source"] = erow.get("source", "Enrichment")
                final_df = pd.concat([final_df, pd.DataFrame([erow_dict])], ignore_index=True)
        final_df.drop(columns=["name_key"], inplace=True, errors="ignore")
        post_enrich = len(final_df)
        log.info(f"Added {post_enrich - before_enrich} new materials from enrichment")
    
    # 3c. Enrich existing rows with enrichment data (fill in missing fields)
    final_df = apply_polymer_enrichment(final_df, enrich_df)

    # 4. Save
    final_df.to_csv(OUTPUT_CSV, index=False)
    log.info(f"✅  Saved {len(final_df)} rows → {OUTPUT_CSV}")

    # 5. Summary
    print("\n── Category Breakdown ──────────────────────────────────")
    print(final_df["category"].value_counts().to_string())
    print("\n── Source Breakdown ────────────────────────────────────")
    print(final_df["source"].value_counts().to_string())
    print("\n── Sample (Curated) ────────────────────────────────────")
    sample = final_df[final_df["source"]=="Curated"][
        ["name","category","density","melting_point","thermal_conductivity","youngs_modulus"]
    ].head(10)
    print(sample.to_string(index=False))
    print("────────────────────────────────────────────────────────\n")


if __name__ == "__main__":
    main()
