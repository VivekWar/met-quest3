# Smart Alloy Selector & Material Recommendation: Tech Titans 🚀
**Needle in the Data-Stack: The AI-Powered Virtual Materials Scientist**

Built for the **MET-QUEST ’26** engineering competition, this platform is the official submission from **Team Tech Titans**. It features a high-performance **Material Recommendation** engine and a custom **Alloy Predictor** that replaces the grueling manual process of scraping disjointed tables (MatWeb, NASA TPSX) with a unified, **Long-Context RAG** engine.

---

## 🏗️ System Architecture

```mermaid
graph TD
    User([User Query]) --> React[React Frontend / Vite]
    React --> Go[Go Backend / Gin]
    Go --> Filter[Domain Segregation Engine]
    Filter --> CSV[(In-Memory CSV DB)]
    Filter --> Postgres[(Neon PostgreSQL)]
    Go --> LLM[OpenRouter / Gemini 1.5]
    LLM --> Report[Analytical Technical Report]
    Report --> User
```

---

## 📂 File-by-File Technical Analysis

### 🌐 Project Root
| File | Responsibility |
|------|----------------|
| `firebase.json` | Configuration for Firebase Hosting (Frontend targets `frontend/dist`). |
| `.env.example` | Template for environment variables (API Keys, DB URL). |
| `PROJECT_CHARTER.md` | Strategic overview of the project goals and architectural constraints. |
| `DEPLOYMENT.md` | Manual troubleshooting guide for Cloud Run and Firebase. |

### 🧠 Backend (`/backend`)
The core processing engine built in Golang.
| File | Logic |
|------|-------|
| `main.go` | Server entry point. Orchestrates the Gin router, CORS, and endpoint lifecycle. |
| `handlers/recommend.go` | Entry point for natural language **Material Recommendation** queries. |
| `handlers/predict.go` | Orchestrates the two-phase custom **Alloy Predictor**. |
| `services/llm.go` | **The AI Core.** Implements Intent Extraction and **Long-Context Analyze**. |
| `services/csv_db.go` | High-speed engine that parses 8k+ materials into RAM at startup. |
| `services/predictor.go` | Implements Rule-of-Mixtures (Phase 1) and LLM Refinement (Phase 2). |
| `db/postgres.go` | Manages the Neon PostgreSQL connection pool with automated mock fallback. |
| `models/material.go` | Central Go structs for Materials, Search Intents, and AI Reports. |
| `Dockerfile` | Multi-stage production build (Go / Alpine) with bundled data assets. |

### 🎨 Frontend (`/frontend`)
A modern React application built for speed and visual excellence.
| File | UI / UX Role |
|------|--------------|
| `src/App.tsx` | Main application shell. Manages state for the AI search results. |
| `src/components/QueryInput.tsx` | Specialized search interface for **Material Recommendation**. |
| `src/components/PredictorPanel.tsx` | Dynamic **Alloy Predictor** with real-time property charts. |
| `src/components/ReportCard.tsx` | Renders the AI's "Virtual Scientist" report with Markdown support. |
| `src/api/client.ts` | Type-safe Axios bridge for production and local backend calls. |
| `src/styles/index.css` | Custom CSS design system (Glassmorphism, Vibrant Dark Mode). |

### 📊 Data Pipeline (`/data`)
The lifecycle of the 8,759-entry materials database.
| File | Data Life Cycle |
|------|-----------------|
| `fetch_materials.py` | Python script to ingest 15,000+ raw records from Materials Project. |
| `seed_db.py` | Optimized bulk uploader for Neon/Postgres using `execute_values`. |
| `schema.sql` | DDL for the structured `materials` and `elements` tables. |
| `materials_cleaned.csv` | **Source of Truth.** The cleaned dataset used by the In-Memory engine. |

---

## ✨ Key Innovations (Brownie Points)

### 1. Long-Context RAG (LCR)
Traditional vector search loses the "holistic" engineering comparison. This project uses **LCR**, injecting up to 1,000 relevant materials directly into the LLM's context window. This allows the AI to "read" the entire catalog simultaneously, just like a human scientist.

### 2. Domain Segregation Engine
To maintain high precision without hitting token limits, we implemented **Domain Segregation**. The backend applies physics-based filters (e.g., *Aerospace*, *Biomedical*, *Plastics*) to mathematically narrow the search space before sending it to the AI.

### 3. Two-Phase Alloy Predictor
For alloys not in the database:
- **Phase 1**: Programmatic **Rule-of-Mixtures** calculation from elemental data.
- **Phase 2**: **Thermodynamic Refinement** via Gemini to account for crystalline phase stability and fatigue resistance.

---

## 🚀 Setup & Execution

### 1. Requirements
- **OpenRouter API Key**: This project uses **Google Gemini 1.5** via OpenRouter for high-speed, long-context reasoning.

#### 🔑 How to get an OpenRouter API Key:
1. Go to **[openrouter.ai](https://openrouter.ai/)**.
2. Sign up or log in.
3. Go to **[Settings → Keys](https://openrouter.ai/settings/keys)**.
4. Click **"Create Key"** and give it a name (e.g., *Met-Quest*).
5. Copy the key and paste it into your `.env` file!

### 2. Configure Environment
Create a `.env` file in the root directory:
```env
OPENROUTER_API_KEY=your_key_here
```

### 3. Run Backend
The backend automatically loads **8,759 materials** from the local CSV into RAM for sub-millisecond searching. No database setup is required!
```bash
cd backend
go run main.go
```

### 4. Run Frontend
```bash
cd frontend
npm install
npm run dev
```

---

## 🛠️ Advanced (Optional)
If you wish to re-ingest data or scale to a cloud-based PostgreSQL:

### Syncing to Cloud Database (Neon)
1. Add `DATABASE_URL` to your `.env`.
2. Sync the materials:
   ```bash
   cd data
   python3 seed_db.py
   ```

### Re-fetching from Materials Project API
1. Add `MP_API_KEY` to your `.env`.
2. Run the ingestion script:
   ```bash
   cd data
   python3 fetch_materials.py
   ```

---

## ☁️ Production Deployment (Free)

### Backend: Hugging Face Spaces
To host the Go backend for free without any credit card or prepayment:
1. Create a new **Space** on **[Hugging Face](https://huggingface.co/new-space)**.
2. Select **Docker** as the SDK and choose the **Blank** template.
3. Select the **Free (CPU Basic)** hardware.
4. Upload the files (you can link your GitHub or push manually).
5. In the Space **Settings**, add a Secret: `OPENROUTER_API_KEY`.
6. The backend will automatically start and listen on **Port 7860**.

### Frontend: Firebase Hosting
The frontend is hosted on Firebase:
```bash
firebase deploy --only hosting
```

---

## 🏆 Development Team
Designed and Engineered for **MET-QUEST ’26**. 
- **Team**: Tech Titans
- **AI Engine**: LCR-Node-L (Gemini-1.5 Optimized)
