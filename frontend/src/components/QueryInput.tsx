import React, { useState, useCallback } from 'react'
import { recommend, RecommendResponse } from '../api/client'

interface Props {
  onResult: (result: RecommendResponse) => void
  onLoading: (loading: boolean) => void
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

export const QueryInput: React.FC<Props> = ({ onResult, onLoading }) => {
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

    try {
      const result = await recommend(query.trim(), domain)
      onResult(result)
    } catch (err: any) {
      const msg = err?.response?.data?.error || err?.message || 'Request failed'
      setError(msg)
    } finally {
      setLoading(false)
      onLoading(false)
    }
  }, [query, domain, loading, onResult, onLoading])

  return (
    <div className="card fade-in-up" id="query-panel">
      {/* Header */}
      <div className="flex items-center gap-md mb-md">
        <div style={{
          width: 44, height: 44, borderRadius: 12,
          background: 'linear-gradient(135deg, #00d4ff22, #0080ff22)',
          border: '1px solid rgba(0,212,255,0.3)',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          fontSize: '1.25rem', flexShrink: 0,
        }}>🧪</div>
        <div>
          <h2 style={{ fontSize: '1.1rem', marginBottom: 2 }}>Problem Statement</h2>
          <p className="text-sm text-muted">Describe your engineering requirements in plain language</p>
        </div>
      </div>

      <form onSubmit={handleSubmit} id="recommend-form">
        <textarea
          id="query-textarea"
          className="textarea"
          style={{ minHeight: 140 }}
          placeholder="e.g. I need a lightweight material for an aircraft wing spar bracket. It must withstand cyclic loading, have a melting point above 500°C, and ideally be corrosion resistant..."
          value={query}
          onChange={handleChange}
          disabled={loading}
          maxLength={2000}
          aria-label="Engineering problem statement"
        />

        <div className="flex justify-between items-center mt-sm">
          <span className="text-xs text-dim font-mono">{charCount}/2000</span>
          {error && (
            <span className="text-sm" style={{ color: '#ff4757' }}>
              ⚠️ {error}
            </span>
          )}
        </div>

        <div className="flex items-center justify-end mt-md query-actions">
          <div className="query-controls" style={{ display: 'flex', gap: '12px' }}>
            <select
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              className="textarea"
              style={{ minHeight: 'auto', padding: '6px 12px', fontSize: '0.85rem', width: '220px' }}
              disabled={loading}
            >
              {DOMAINS.map(d => (
                <option key={d} value={d}>📂 {d}</option>
              ))}
            </select>

            <button
              id="analyze-btn"
              type="submit"
              className="btn btn--primary"
              disabled={!query.trim() || loading}
              style={{ flexShrink: 0 }}
            >
              {loading ? (
                <>
                  <span style={{
                    width: 16, height: 16, border: '2px solid #080c18',
                    borderTopColor: 'transparent', borderRadius: '50%',
                    animation: 'spin 0.8s linear infinite', display: 'inline-block',
                    flexShrink: 0,
                  }} />
                  Analyzing…
                </>
              ) : (
                <>⚡ Analyze</>
              )}
            </button>
          </div>
        </div>
      </form>
    </div>
  )
}
