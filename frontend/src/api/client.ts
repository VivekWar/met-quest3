import axios from 'axios'

const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL || 'https://vivekwa-met-quest-api.hf.space/api/v1',
  headers: { 'Content-Type': 'application/json' },
  timeout: 120000, // 120s — long-context Gemini calls can take 60-90s
})

// ── Types ─────────────────────────────────────────────────────────────────

export interface RangeFilter {
  min?: number
  max?: number
}

export interface IntentJSON {
  filters: Record<string, RangeFilter>
  category: string
  sort_by: string
  sort_dir: string
}

export interface Material {
  id: number
  name: string
  formula: string
  category: string
  subcategory?: string
  density?: number
  melting_point?: number
  boiling_point?: number
  thermal_conductivity?: number
  specific_heat?: number
  thermal_expansion?: number
  electrical_resistivity?: number
  yield_strength?: number
  tensile_strength?: number
  youngs_modulus?: number
  hardness_vickers?: number
  poissons_ratio?: number
  source: string
}

export interface RecommendResponse {
  query: string
  extracted_intent: IntentJSON
  recommendations: Material[]
  report: string
  tokens_used: number
}

export interface PredictResponse {
  composition: Record<string, number>
  predicted_name: string
  baseline_properties?: Record<string, number>
  density?: number
  melting_point?: number
  thermal_conductivity?: number
  electrical_resistivity?: number
  yield_strength?: number
  youngs_modulus?: number
  scientific_explanation?: string
  method: string
  notes?: string
}

// ── API calls ─────────────────────────────────────────────────────────────

export async function recommend(query: string, domain: string): Promise<RecommendResponse> {
  const { data } = await api.post<RecommendResponse>('/recommend', { query, domain })
  return data
}

export async function predict(composition: Record<string, number>): Promise<PredictResponse> {
  const { data } = await api.post<PredictResponse>('/predict', { composition })
  return data
}

export async function ping(): Promise<void> {
  // Simple health check to wake up the backend (cold start mitigation)
  // No response handling needed
  await api.get('/health').catch(() => { /* ignore */ })
}
