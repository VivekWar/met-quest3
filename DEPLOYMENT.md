# Smart Alloy Selector — Deployment Guide

This guide matches the current chat-first app: a session sidebar, a single conversation surface, and follow-up chat that continues from the first recommendation instead of rerunning the full pipeline.

## 0. Pre-Deployment Validation

Run the validation suite before you deploy changes to the recommendation pipeline or follow-up chat flow.

```bash
chmod +x test_dispatcher_validation.sh
API_URL=http://localhost:8080 ./test_dispatcher_validation.sh
```

Expected coverage:
- Desktop FDM routing to Polymers
- Aerospace alloy selection
- Pure metals for conductivity
- Ceramics at high temperature
- Impossible FDM rejection
- Cryogenic ductility
- Elastomer damping
- Wear resistance

## 1. Backend

The backend is the Go service in `backend/`. It serves the recommendation API, the follow-up chat endpoint, and the health check.

### Local Run

```bash
cd backend
go run main.go
```

### Environment

Use the root `.env` file created from `.env.example`. The important values are:

- `GEMINI_API_KEY`
- `DATABASE_URL` if you are using PostgreSQL / Neon
- `ALLOWED_ORIGINS`
- `PORT`

### Production Deploy

Deploy the backend to your hosting target with the same environment variables you use locally.

If you are using Google Cloud Run, a typical source deploy looks like this:

```bash
cd backend
gcloud run deploy met-quest-backend \
  --source . \
  --region us-central1 \
  --allow-unauthenticated \
  --set-env-vars="GIN_MODE=release,DATABASE_URL=your_db_url,GEMINI_API_KEY=your_key,ALLOWED_ORIGINS=https://met-quest.web.app"
```

### Smoke Tests

```bash
curl https://YOUR_BACKEND_URL/health
```

```bash
curl -X POST https://YOUR_BACKEND_URL/api/v1/recommend \
  -H "Content-Type: application/json" \
  -d '{"query":"Need a lightweight alloy for aircraft wings","domain":"Overall (Top 1000)"}'
```

```bash
curl -X POST https://YOUR_BACKEND_URL/api/v1/chat/followup \
  -H "Content-Type: application/json" \
  -d '{"message":"Can you narrow it down for better corrosion resistance?","history":[],"initial_report":"...","top_recommendations":["7075 Aluminum"]}'
```

## 2. Frontend

The frontend is the Vite app in `frontend/` and deploys to Firebase Hosting from `frontend/dist`.

### Local Run

```bash
cd frontend
npm install
npm run dev
```

### Production API URL

The frontend client uses `VITE_API_URL` when present, otherwise it falls back to the deployed API URL in `frontend/src/api/client.ts`.

For production builds, set `VITE_API_URL` to your backend base URL before building:

```bash
export VITE_API_URL=https://YOUR_BACKEND_URL/api/v1
cd frontend
npm run build
```

### Firebase Hosting Deploy

```bash
firebase login
cd frontend
firebase deploy --only hosting
```

If you have not already configured Hosting, use `dist` as the public directory and enable SPA rewrites to `index.html`.

## 3. Production Checks

After deployment, verify the live app and API together.

```bash
curl https://YOUR_BACKEND_URL/health
```

```bash
API_URL=https://YOUR_BACKEND_URL ./test_dispatcher_validation.sh
```

What to verify in the UI:

- The sidebar shows saved sessions.
- The first message creates a recommendation report.
- Follow-up messages use the existing chat context.
- Copy and expand actions work on assistant messages.
- The app remains readable on mobile widths.

## 4. Data

The app can run from CSV data alone, but PostgreSQL / Neon improves retrieval and consistency for larger deployments.

If you use the data scripts, keep the seed process aligned with the active schema in `data/schema.sql`.

## 5. Monitoring

Watch for backend health and conversation flow issues after release.

- Health check failures usually mean the backend is not reachable or the API URL is wrong.
- Follow-up issues usually mean the chat endpoint cannot find the initial report or history payload.
- If the validation script starts failing, check category routing and rejection handling first.

---

## 6. Scaling & Cost Optimization

### Backend

- **Cold starts**: Google Cloud Run typically <1s for warm instances
- **Concurrency**: Set `--concurrency 100` if expected traffic is high
- **Memory**: 2GB is usually sufficient; monitor CPU utilization in Cloud Run metrics

### Frontend

- Firebase Hosting is globally CDN-distributed; no scaling needed
- Build output gzips to ~50–80KB

### Database

- Neon Serverless: Compute scales automatically; storage is pay-as-you-go
- Connection pooling: Go backend uses pgx with a pool size of 8–16

---

🎉 **You're Done!** Your RAG material selector is now fully serverless, highly-available globally, humanized with first-principles explanations, and ready for MET-QUEST '26 user scale!

---

## Troubleshooting

| Issue | Solution |
| --- | --- |
| `NO_FEASIBLE_MATERIAL` for valid queries | Verify query doesn't trigger impossible combination detection (e.g., desktop FDM + rocket nozzle). Check backend logs for signal extraction details. |
| Frontend cannot reach backend API | Verify `VITE_API_URL` environment variable and CORS headers in `main.go`. Check `ALLOWED_ORIGINS` matches frontend domain. |
| Slow response times (>5s) | Check Gemini API quota; may be hitting rate limits. Verify database connection (test `DATABASE_URL`). |
| Physics analysis not appearing | Set `ENABLE_LLM_SCIENTIFIC_ANALYSIS=1` in backend environment. Falls back to deterministic analysis if LLM is unavailable. |
| LaTeX formulas not rendering | Ensure KaTeX is loaded in frontend (check console for script errors). Formulas should render inline within 1-2s of page load. |
