# Smart Alloy Selector — Deployment Guide (Milestone 5)

This deployment guide outlines how to launch the Smart Alloy Selector stack for the MET-QUEST '26 competition. The system splits into three parts: Data, Backend, and Frontend.

---

## 1. Database (Neon PostgreSQL)

We use Neon's serverless PostgreSQL for ultra-fast RAG vector retrieval without managing hardware.

1. Create a free account at [neon.tech](https://neon.tech).
2. Create a new Project (name it `met-quest-db` or similar).
3. Copy the **Connection String** from the dashboard. It will look like: 
   `postgresql://[user]:[password]@[endpoint].neon.tech/neondb?sslmode=require`
4. Locally in the `Met-Quest` project folder, create a `.env` file from the sample:
   ```bash
   cp .env.example .env
   ```
5. Paste the connection string into the `DATABASE_URL` field in the `.env` file.
6. Populate the database by running our pre-built script:
   ```bash
   cd data
   python3 seed_db.py
   ```
   *(This ensures your DB has the required custom schema and all 455 curated aerospace materials).*

---

## 2. Backend (Google Cloud Run + Go)

The highly-concurrent Go backend calculates rule-of-mixtures and orchestrates the Gemini LLM. We will deploy it using a Docker container to Google Cloud Run for automatic TLS and scaling.

### Prerequisites
- Create a [Google Cloud Project](https://console.cloud.google.com/) and enable billing (Cloud Run free tier is generous).
- Enable the **Cloud Run API** and **Cloud Build API**.
- Install the Google Cloud CLI (`gcloud`).
- Ensure you have a valid Gemini API Key from Google AI Studio.

### Deployment Process

1. Authenticate with Google Cloud:
   ```bash
   gcloud auth login
   gcloud config set project YOUR_PROJECT_ID
   ```
2. Navigate to the backend folder:
   ```bash
   cd backend
   ```
3. Deploy directly from source (this leverages our custom multi-stage Dockerfile):
   ```bash
   gcloud run deploy met-quest-backend \
     --source . \
     --region us-central1 \
     --allow-unauthenticated \
     --set-env-vars="GIN_MODE=release,DATABASE_URL=your_neon_db_url,GEMINI_API_KEY=your_gemini_key,ALLOWED_ORIGINS=*"
   ```
4. Copy the Service URL provided in the terminal (e.g., `https://met-quest-backend-xxxxxx.run.app`). You will need this for the frontend configuration.

---

## 3. Frontend (Firebase Hosting + React)

The web UI provides the polished dark-theme "Virtual Scientist" experience.

### Prerequisites
- Install Firebase CLI globally (we recommend fetching it using `npm` or the binary curler):
  ```bash
  npm install -g firebase-tools
  ```

### Deployment Process

1. Log in to Firebase:
   ```bash
   firebase login
   ```
2. Initialize Firebase within your `/frontend` directory:
   ```bash
   cd frontend
   firebase init hosting
   ```
   * Choose to create a new project or select an existing one.
   * **What do you want to use as your public directory?** `dist`
   * **Configure as a single-page app (rewrite all urls to /index.html)?** `Yes`
   * **Set up automatic builds and deploys with GitHub?** `No` (for now)
   
3. **Crucial Setup**: Configure your production frontend to hit your Cloud Run Backend API URL! In `src/api/client.ts`, modify the `baseURL` dynamically, or hardcode it before build:
   
   ```typescript
   // src/api/client.ts
   const api = axios.create({
     baseURL: import.meta.env.VITE_API_URL || '/api/v1',
     // ...
   })
   ```
   *(If deploying strictly, create an `.env.production` file containing `VITE_API_URL=https://met-quest-backend-xxxxxx.run.app/api/v1`)*

4. Build the minimized React application:
   ```bash
   npm run build
   ```
5. Deploy to Firebase:
   ```bash
   firebase deploy --only hosting
   ```
6. Visit your live Firebase URL!

---

🎉 **You're Done!** Your RAG material selector is now fully serverless, highly-available globally, and ready for MET-QUEST '26 user scale!
