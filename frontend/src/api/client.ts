import axios from 'axios'

const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL || 'https://vivekwa-met-quest-api.hf.space/api/v1',
  headers: { 'Content-Type': 'application/json' },
  timeout: 180000,
})

export interface Constraint {
  id?: string
  key: string
  operator: 'min' | 'max' | 'equals' | 'contains'
  value: string | number
  label?: string
}

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
  glass_transition_temp?: number
  heat_deflection_temp?: number
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
  processing_temp_min_c?: number
  processing_temp_max_c?: number
  crystallinity?: number
  source: string
}

export interface RecommendResponse {
  query: string
  extracted_intent: IntentJSON
  recommendations: Material[]
  final_recommendation?: Material
  top_recommendations?: Material[]
  routed_category?: string
  inline_alloy_prediction?: InlineAlloyPrediction
  report: string
  tokens_used: number
}

export interface InlineAlloyPrediction {
  summary: string
  key_findings?: Record<string, string>
  risk_flags?: string[]
  confidence?: string
  should_display: boolean
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

export interface ChatTurn {
  role: 'user' | 'assistant'
  content: string
}

export interface FollowUpChatRequest {
  message: string
  history?: ChatTurn[]
  initial_report?: string
  top_recommendations?: string[]
}

export interface FollowUpChatResponse {
  reply: string
  tokens_used: number
}

export async function recommend(query: string, domain: string, constraints?: Constraint[]): Promise<RecommendResponse> {
  const payload: any = { query, domain }
  if (constraints && constraints.length > 0) {
    payload.constraints = constraints.map(c => ({
      key: c.key,
      operator: c.operator,
      value: c.value,
    }))
  }
  const { data } = await api.post<RecommendResponse>('/recommend', payload)

  const recommendations = data.recommendations || []
  const topRecommendations = recommendations.slice(0, 3)

  return {
    query: data.query,
    extracted_intent: data.extracted_intent,
    recommendations,
    final_recommendation: data.final_recommendation || topRecommendations[0],
    top_recommendations: topRecommendations,
    routed_category: data.extracted_intent?.category,
    inline_alloy_prediction: data.inline_alloy_prediction,
    report: data.report,
    tokens_used: data.tokens_used || 0,
  }
}

export async function chatFollowup(payload: FollowUpChatRequest): Promise<FollowUpChatResponse> {
  const { data } = await api.post<FollowUpChatResponse>('/chat/followup', payload)
  return data
}

export async function ping(): Promise<void> {
  await pingStatus()
}

export async function pingStatus(): Promise<boolean> {
  try {
    await api.get('/health')
    return true
  } catch {
    return false
  }
}
