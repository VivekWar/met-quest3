# Production Parity Checklist (Firebase + Backend)

Use this checklist before every production deploy to prevent frontend/backend drift.

## 1. Source of Truth

Keep one canonical mapping of environments:

- Frontend host: Firebase project `met-quest`
- Frontend URL: `https://met-quest.web.app`
- Backend host: Hugging Face Space `vivekwa/met-quest-api`
- Backend URL: `https://vivekwa-met-quest-api.hf.space`
- API base used by frontend: `https://vivekwa-met-quest-api.hf.space/api/v1`

## 2. Required Variables by Layer

### Backend (Hugging Face Space Secrets)

Set these in Space Settings -> Secrets:

- `OPENROUTER_API_KEY` (or valid provider key used by the backend)
- `GEMINI_API_KEY` (if Google provider path is used)
- `DATABASE_URL` (only if running Postgres-backed mode)
- `ALLOWED_ORIGINS` (for production, set to `https://met-quest.web.app`)
- `PORT` (optional on Spaces, platform usually injects this)

### Frontend (build-time)

- `frontend/.env.production` must contain:

```env
VITE_API_URL=https://vivekwa-met-quest-api.hf.space/api/v1
```

Note: `VITE_*` vars are compiled at build time. Changing backend URL requires rebuilding and redeploying frontend.

## 3. Pre-Deploy Validation (Local)

Run from repo root:

```bash
# Confirm production API URL baked for frontend builds
cat frontend/.env.production

# Confirm Firebase target project
cat .firebaserc

# Backend health check
curl -i -m 20 https://vivekwa-met-quest-api.hf.space/health

# API smoke test (recommend)
curl -s -m 60 -X POST 'https://vivekwa-met-quest-api.hf.space/api/v1/recommend' \
  -H 'Content-Type: application/json' \
  -d '{"query":"Need lightweight high-strength aerospace bracket","domain":"Aerospace & Aviation"}'
```

Pass criteria:

- `/health` returns HTTP 200
- `/recommend` returns JSON with `recommendations` as an array
- Response includes `report` and `tokens_used`

## 4. Deploy Order (Do Not Swap)

1. Deploy backend first.
2. Verify backend with `/health` and one `/recommend` call.
3. Build frontend with production env.
4. Deploy frontend to Firebase.
5. Verify end-to-end in browser and with direct API call.

## 5. Frontend Deploy Commands

Run from repo root:

```bash
cd frontend
npm install
npm run build
cd ..
firebase deploy --only hosting
```

## 6. Post-Deploy E2E Checks

### Browser checks

- Open `https://met-quest.web.app`
- Submit two different prompts in two different domains
- Verify results are not identical and table renders correctly

### API checks

```bash
# Query 1
curl -s -m 60 -X POST 'https://vivekwa-met-quest-api.hf.space/api/v1/recommend' \
  -H 'Content-Type: application/json' \
  -d '{"query":"Need corrosion-resistant marine shaft material","domain":"Marine & Naval"}'

# Query 2
curl -s -m 60 -X POST 'https://vivekwa-met-quest-api.hf.space/api/v1/recommend' \
  -H 'Content-Type: application/json' \
  -d '{"query":"Need high-temperature nozzle liner","domain":"High-Temperature / Refractory"}'
```

Pass criteria:

- Both responses are HTTP 200
- `recommendations` arrays are present
- Top IDs/names differ between clearly different domains

## 7. Common Drift Failures and Fixes

- Symptom: Frontend calls old API URL.
  - Fix: Update `frontend/.env.production`, rebuild, redeploy.

- Symptom: API returns same output frequently.
  - Fix: Verify domain parameter is being sent and backend received it.

- Symptom: CORS failures in browser only.
  - Fix: Set backend `ALLOWED_ORIGINS=https://met-quest.web.app`.

- Symptom: Backend "works" but behaves like fallback mode.
  - Fix: Verify provider keys and database mode logs in backend runtime.

## 8. Security Hygiene (Mandatory)

- Never commit real secrets in `.env`.
- Rotate any key that was pasted in terminal/chat logs.
- Keep `.env` and secrets manager values separate by environment.
