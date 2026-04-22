import React, { useState, useCallback } from 'react'
import { Search, WandSparkles } from 'lucide-react'
import { recommend, RecommendResponse, Constraint } from '../api/client'

interface Props {
  onResult: (result: RecommendResponse) => void
  onLoading: (loading: boolean) => void
  constraints?: Constraint[]
  onSubmit?: (query: string, domain: string) => void
}

export const DOMAINS = [
  "Overall (Top 1000)",
  "Aerospace & Aviation",
  "Automotive & Transportation",
  "Marine & Naval",
  "Medical & Biomedical",
  "Electronics & Photonics",
  "Construction & Structural",
  "High-Temperature / Refractory",
  "Tooling & Wear-Resistant",
  "Plastics & Polymers",
  "Advanced Composites"
]

export const QueryInput: React.FC<Props> = ({ onResult, onLoading, constraints = [], onSubmit }) => {
  const [query, setQuery] = useState('')
  const [domain, setDomain] = useState(DOMAINS[0])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [charCount, setCharCount] = useState(0)

  const handleChange = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setQuery(e.target.value)
    setCharCount(e.target.value.length)
    setError(null)
  }, [])

  const handleSubmit = useCallback(async (e: React.FormEvent) => {
    e.preventDefault()
    if (!query.trim() || loading) return

    setLoading(true)
    setError(null)
    onLoading(true)
    onSubmit?.(query.trim(), domain)

    try {
      const result = await recommend(query.trim(), domain, constraints)
      onResult(result)
    } catch (err: any) {
      const msg = err?.response?.data?.error || err?.message || 'Request failed'
      setError(msg)
    } finally {
      setLoading(false)
      onLoading(false)
    }
  }, [query, domain, loading, onResult, onLoading, onSubmit, constraints])

  return (
    <div className="card fade-in-up" id="query-panel">
      <div className="panel-header">
        <div className="panel-icon">
          <Search size={18} />
        </div>
        <div>
          <h2 className="panel-title">Query materials</h2>
          <p className="panel-subtitle">Describe the requirement, select a domain, and run the recommendation.</p>
        </div>
      </div>

      <form onSubmit={handleSubmit} id="recommend-form">
        <div className="query-composer">
          <textarea
            id="query-textarea"
            className="textarea query-textarea"
            placeholder="Example: I need a lightweight material for an aircraft bracket with strong fatigue resistance, a melting point above 500°C, and good corrosion resistance."
            value={query}
            onChange={handleChange}
            disabled={loading}
            maxLength={2000}
            aria-label="Engineering problem statement"
          />

          <div className="query-toolbar">
            <select
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              className="select query-domain-select"
              disabled={loading}
            >
              {DOMAINS.map(d => (
                <option key={d} value={d}>{d}</option>
              ))}
            </select>

            <button
              id="analyze-btn"
              type="submit"
              className="btn btn--primary query-submit-btn"
              disabled={!query.trim() || loading}
            >
              {loading ? (
                <>
                  <span className="spinner" />
                  Running
                </>
              ) : (
                <>
                  <WandSparkles size={16} />
                  Run recommendation
                </>
              )}
            </button>
          </div>
        </div>

        <div className="query-meta-row">
          <span className="text-xs text-dim font-mono">{charCount}/2000 characters</span>
          {error && <span className="query-error">{error}</span>}
          <span className="query-note">
            The active session saves this query with any constraints you add.
          </span>
        </div>
      </form>
    </div>
  )
}
