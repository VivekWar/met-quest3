import axios from 'axios'

const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL || 'https://vivekwa-met-quest-api.hf.space/api/v1',
  headers: { 'Content-Type': 'application/json' },
  timeout: 180000, // 150s — high-density long-context calls can be slow
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

interface DispatcherPhysicsAnalysis {
  top_candidate?: string
  physics_verification?: Record<string, string>
  merit_index_calculation?: string
  failure_rejection_reasons?: string[]
  manufacturing_feasibility?: string
  safety_margin?: string
}

interface DispatcherResponse {
  query: string
  routed_category: string
  category_candidates: Material[]
  physics_analysis?: DispatcherPhysicsAnalysis
  alloy_prediction?: InlineAlloyPrediction
  top_recommendation?: Material
  alternative_options?: Material[]
  total_tokens_used: number
  pipeline_explanation?: string
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

function buildDispatcherReport(data: DispatcherResponse): string {
  const topName = data.top_recommendation?.name || data.physics_analysis?.top_candidate || 'No feasible material'
  const topThree = (data.top_recommendation?.id ? [data.top_recommendation, ...(data.alternative_options || [])] : (data.category_candidates || [])).slice(0, 3)
  const top = data.top_recommendation
  const parts: string[] = []

  parts.push(`Recommendation: ${topName}.`)

  const propertySnippets: string[] = []
  if (top?.glass_transition_temp !== undefined) {
    propertySnippets.push(`Glass transition temperature ~${Math.round(top.glass_transition_temp - 273.15)}°C`)
  }
  if (top?.heat_deflection_temp !== undefined) {
    propertySnippets.push(`Heat deflection temperature ~${Math.round(top.heat_deflection_temp - 273.15)}°C`)
  }
  if (top?.yield_strength !== undefined) {
    propertySnippets.push(`Yield strength ~${Math.round(top.yield_strength)} MPa`)
  }
  if (top?.thermal_conductivity !== undefined) {
    propertySnippets.push(`Thermal conductivity ~${top.thermal_conductivity.toFixed(1)} W/mK`)
  }
  if (top?.density !== undefined) {
    propertySnippets.push(`Density ~${top.density.toFixed(2)} g/cm3`)
  }
  if (propertySnippets.length > 0) {
    parts.push(`● Properties Retrieved: ${propertySnippets.join(', ')}.`)
  }

  const explanationParts: string[] = []
  explanationParts.push(`${topName} was selected after routing this problem to ${data.routed_category}.`)
  if (data.physics_analysis?.merit_index_calculation) {
    explanationParts.push(data.physics_analysis.merit_index_calculation + '.')
  }
  if (data.physics_analysis?.safety_margin) {
    explanationParts.push(data.physics_analysis.safety_margin)
  }
  if (data.physics_analysis?.manufacturing_feasibility) {
    explanationParts.push(data.physics_analysis.manufacturing_feasibility)
  }
  parts.push(`● Explanation: "${explanationParts.join(' ')}"`)

  if (topThree.length > 0) {
    parts.push('')
    parts.push('Top 3 shortlist:')
    topThree.forEach((mat, idx) => {
      parts.push(`${idx + 1}. ${mat.name}`)
    })
  }

  const physics = data.physics_analysis?.physics_verification || {}
  if (Object.keys(physics).length > 0) {
    parts.push('')
    parts.push('## Physics and Engineering Notes')
    Object.entries(physics).forEach(([k, v]) => {
      const label = k.replace(/_/g, ' ').replace(/\b\w/g, (ch) => ch.toUpperCase())
      parts.push(`- ${label}: ${v}`)
    })
  }

  const failures = data.physics_analysis?.failure_rejection_reasons || []
  if (failures.length > 0) {
    parts.push('')
    parts.push('Why alternatives were rejected:')
    failures.slice(0, 6).forEach((r) => parts.push(`- ${r}`))
  }

  if (data.pipeline_explanation) {
    parts.push('')
    parts.push('Decision path (simplified):')
    const steps = data.pipeline_explanation
      .replace('Pipeline Steps:', '')
      .split('|')
      .map((s) => s.replace(/[✅🔍🧠🔬↩️↪️⚠️⛔]/g, '').trim())
      .filter(Boolean)
    steps.forEach((step, idx) => parts.push(`${idx + 1}. ${step}`))
  }

  if (data.alloy_prediction?.should_display) {
    parts.push('')
    parts.push('AI Alloy Performance Prediction:')
    parts.push(data.alloy_prediction.summary)
    if (data.alloy_prediction.key_findings && Object.keys(data.alloy_prediction.key_findings).length > 0) {
      parts.push('')
      parts.push('Key predicted findings:')
      Object.entries(data.alloy_prediction.key_findings).forEach(([k, v]) => {
        parts.push(`- ${k}: ${v}`)
      })
    }
    if (data.alloy_prediction.risk_flags && data.alloy_prediction.risk_flags.length > 0) {
      parts.push('')
      parts.push('Prediction risk flags:')
      data.alloy_prediction.risk_flags.forEach((item) => parts.push(`- ${item}`))
    }
  }

  return parts.join('\n')
}

export async function recommend(query: string, domain: string): Promise<RecommendResponse> {
  const { data } = await api.post<DispatcherResponse>('/recommend/dispatcher', { query, domain })

  const top = data.top_recommendation
  const alternatives = data.alternative_options || []
  const candidates = data.category_candidates || []
  const recommendations = top && top.id ? [top, ...alternatives] : candidates
  const topRecommendations = recommendations.slice(0, 3)

  return {
    query: data.query,
    extracted_intent: {
      category: data.routed_category,
      filters: {},
      sort_by: '',
      sort_dir: '',
    },
    recommendations,
    final_recommendation: top && top.id ? top : topRecommendations[0],
    top_recommendations: topRecommendations,
    routed_category: data.routed_category,
    inline_alloy_prediction: data.alloy_prediction,
    report: buildDispatcherReport(data),
    tokens_used: data.total_tokens_used || 0,
  }
}

export async function ping(): Promise<void> {
  // Simple health check to wake up the backend (cold start mitigation)
  // No response handling needed
  await api.get('/health').catch(() => { /* ignore */ })
}
