# Milestone 1 — Data Ingestion Guide

This folder contains everything needed to build and populate the
`materials` table in Neon PostgreSQL.

---

## Step 1 — Set Up Neon PostgreSQL (Free Tier)

1. Go to **[neon.tech](https://neon.tech)** and sign up (free).
2. Click **"New Project"** → give it a name (e.g. `met-quest`).
3. Choose a region close to your deployment (e.g. `ap-south-1` for India).
4. After creation, go to **Dashboard → Connection Details**.
5. Copy the connection string — it looks like:
   ```
   postgres://vivek:password@ep-cold-forest-123456.ap-south-1.aws.neon.tech/neondb?sslmode=require
   ```
6. Paste it into your `.env` file at the project root:
   ```env
   DATABASE_URL=postgres://vivek:password@ep-cold-forest-123456.ap-south-1.aws.neon.tech/neondb?sslmode=require
   ```

> **Note**: Neon's free tier gives you 0.5 GB storage and auto-suspends
> the compute after 5 minutes of inactivity. Auto-resume on first query
> takes ~1 second — fine for a competition demo.

---

## Step 2 — Set Up Python Environment

```bash
cd Met-Quest/data

# Create virtual environment
python3 -m venv .venv
source .venv/bin/activate        # Windows: .venv\Scripts\activate

# Install dependencies
pip install -r requirements.txt
```

---

## Step 3 — Configure Environment

Copy `.env.example` to `.env` in the project root and fill it in:

```bash
cd ..   # go to Met-Quest/
cp .env.example .env
nano .env   # or use your editor
```

Required variables for this milestone:
```env
DATABASE_URL=postgres://user:pass@ep-xxx.neon.tech/neondb?sslmode=require
```

No materials API key is needed. The script scrapes public internet datasets and merges with curated data.

---

## Step 4 — Fetch Materials Data

```bash
cd data/
python fetch_materials.py
```

**What it does:**
- Scrapes public internet datasets:
   - JARVIS dft_3d (Figshare) for large-scale materials rows
   - Periodic-Table-JSON and Wikipedia element fallback
- Supplements with a curated table of 60+ common engineering materials
  (with full thermal, electrical, and mechanical properties)
- Outputs `materials_cleaned.csv`

**Expected output:**
```
[INFO] Scraped 6000 rows from JARVIS dft_3d
[INFO] Scraped 119 elemental rows from Periodic-Table-JSON
[INFO] Curated: 70+ materials
[INFO] Merged dataset: ... materials
✅  Cleaned CSV saved to: data/materials_cleaned.csv
```

Optional row limit for scraping:
```env
SCRAPE_MAX_ROWS=6000
```

---

## Step 5 — Seed the Database

```bash
python seed_db.py
```

**What it does:**
1. Reads `materials_cleaned.csv`
2. Connects to your Neon instance
3. Applies `schema.sql` (creates tables + indexes if not exist)
4. Bulk-inserts all rows (idempotent — safe to re-run)
5. Prints a verification summary

**Expected output:**
```
✅  Connected to PostgreSQL
✅  Schema applied
Seeding materials: 100%|████████████| 2060/2060
✅  Inserted: 2060 | Skipped: 0

── Database Summary ───────────────
   Total materials: 2060

── By Category ────────────────────
   Metal                 1820 rows
   Ceramic                 95 rows
   Semiconductor           75 rows
   Polymer                 30 rows
   Composite               20 rows
   Unknown                 20 rows
```

---

## Verify in Neon Console

You can run SQL directly in the Neon web console:

```sql
-- Count all materials
SELECT COUNT(*) FROM materials;

-- Find lightweight metals with high melting points
SELECT name, density, melting_point, thermal_conductivity
FROM materials
WHERE category = 'Metal'
  AND density < 5.0
  AND melting_point > 1500
ORDER BY melting_point DESC
LIMIT 5;

-- Check for aerospace alloys
SELECT name, density, yield_strength, youngs_modulus
FROM materials
WHERE density < 5.0 AND yield_strength > 500
ORDER BY yield_strength DESC;
```

---

## Files in this folder

| File | Purpose |
|------|---------|
| `fetch_materials.py` | Fetch data from Materials Project API + curated supplement |
| `seed_db.py` | Apply schema and bulk-insert CSV into PostgreSQL |
| `schema.sql` | PostgreSQL DDL (tables + indexes) |
| `requirements.txt` | Python package dependencies |
| `materials_cleaned.csv` | Output CSV (gitignored if >10MB) |
| `materials_raw.json` | Raw API backup (gitignored) |
