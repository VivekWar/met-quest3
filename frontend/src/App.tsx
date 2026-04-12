import React, { useState, useCallback } from 'react'
import './styles/index.css'
import { QueryInput }    from './components/QueryInput'
import { ReportCard }    from './components/ReportCard'
import { PropertyTable } from './components/PropertyTable'
import { PredictorPanel } from './components/PredictorPanel'
import { RecommendResponse } from './api/client'

type Tab = 'recommend' | 'predict'

const App: React.FC = () => {
  const [activeTab, setActiveTab]       = useState<Tab>('recommend')
  const [result, setResult]             = useState<RecommendResponse | null>(null)
  const [loading, setLoading]           = useState(false)

  const handleResult = useCallback((res: RecommendResponse) => {
    setResult(res)
    // Smooth scroll to results
    setTimeout(() => {
      document.getElementById('report-section')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    }, 100)
  }, [])

  return (
    <div>
      {/* ── Navigation ──────────────────────────────────────────── */}
      <nav style={{
        position: 'sticky', top: 0, zIndex: 100,
        background: 'rgba(8,12,24,0.9)',
        backdropFilter: 'blur(16px)',
        borderBottom: '1px solid var(--color-border)',
        padding: '0 24px',
      }}>
        <div className="container" style={{ display: 'flex', alignItems: 'center', height: 64 }}>
          {/* Logo */}
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginRight: 48 }}>
            <div style={{
              width: 36, height: 36, borderRadius: 9,
              background: 'linear-gradient(135deg, #00d4ff, #0080ff)',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              fontSize: '1.1rem', fontWeight: 800,
              boxShadow: '0 4px 16px rgba(0,212,255,0.35)',
            }}>⚛</div>
            <div>
              <div style={{ fontWeight: 800, fontSize: '0.95rem', letterSpacing: '-0.02em' }}>
                Smart Alloy Selector
              </div>
              <div className="text-xs text-dim" style={{ fontWeight: 500 }}>MET-QUEST '26</div>
            </div>
          </div>

          {/* Tabs */}
          <div style={{ display: 'flex', gap: 4, flex: 1 }}>
            {([
              { key: 'recommend', label: '🔍 Material Recommender', id: 'tab-recommend' },
              { key: 'predict',   label: '⚗️ Alloy Predictor',       id: 'tab-predict'   },
            ] as const).map(tab => (
              <button
                key={tab.key}
                id={tab.id}
                className="btn btn--sm"
                onClick={() => setActiveTab(tab.key)}
                style={{
                  background: activeTab === tab.key ? 'rgba(0,212,255,0.12)' : 'transparent',
                  color: activeTab === tab.key ? 'var(--color-primary)' : 'var(--color-text-muted)',
                  border: `1px solid ${activeTab === tab.key ? 'rgba(0,212,255,0.3)' : 'transparent'}`,
                  fontSize: '0.85rem',
                }}
              >{tab.label}</button>
            ))}
          </div>

          {/* DB badge */}
          <div style={{
            display: 'flex', alignItems: 'center', gap: 8,
            padding: '6px 14px',
            background: 'rgba(0,255,159,0.08)',
            border: '1px solid rgba(0,255,159,0.2)',
            borderRadius: 20,
            fontSize: '0.75rem',
          }}>
            <span style={{
              width: 7, height: 7, borderRadius: '50%',
              background: '#00ff9f',
              boxShadow: '0 0 6px #00ff9f',
              display: 'inline-block',
            }} />
            <span style={{ color: '#00ff9f', fontWeight: 600 }}>455 Materials Loaded</span>
          </div>
        </div>
      </nav>

      {/* ── Hero ────────────────────────────────────────────────── */}
      {activeTab === 'recommend' && !result && (
        <div style={{
          textAlign: 'center', padding: '72px 24px 48px',
          background: 'radial-gradient(ellipse at 50% 0%, rgba(0,212,255,0.07) 0%, transparent 65%)',
        }}>
          <div
            className="text-xs font-mono"
            style={{
              color: 'var(--color-primary)', letterSpacing: '0.15em',
              textTransform: 'uppercase', marginBottom: 20,
              display: 'inline-flex', alignItems: 'center', gap: 8,
              padding: '5px 16px',
              background: 'rgba(0,212,255,0.08)',
              border: '1px solid rgba(0,212,255,0.2)',
              borderRadius: 20,
            }}
          >
            <span style={{ width: 6, height: 6, borderRadius: '50%', background: 'var(--color-primary)', display: 'inline-block', animation: 'pulse-glow 2s ease-in-out infinite' }} />
            Powered by Gemini + Local PostgreSQL RAG
          </div>

          <h1 style={{ maxWidth: 640, margin: '0 auto 16px' }}>
            <span className="gradient-text">AI-Powered</span> Material Selection
          </h1>
          <p style={{ maxWidth: 540, margin: '0 auto 40px', fontSize: '1.05rem', color: 'var(--color-text-muted)' }}>
            Describe your engineering challenge. Our AI extracts your requirements, queries 455+ materials,
            and delivers a <strong style={{ color: 'var(--color-text)' }}>Virtual Scientist report</strong> with deep technical analysis.
          </p>

          {/* Feature pills */}
          <div style={{ display: 'flex', justifyContent: 'center', gap: 12, flexWrap: 'wrap', marginBottom: 48 }}>
            {[
              ['🧠', 'Gemini Intent Extraction'],
              ['🗄️', 'Local PostgreSQL RAG'],
              ['📋', 'Virtual Scientist Report'],
              ['🔬', 'LLM Alloy Predictor'],
            ].map(([icon, label]) => (
              <div key={label as string} style={{
                display: 'flex', alignItems: 'center', gap: 8,
                padding: '8px 16px',
                background: 'rgba(255,255,255,0.03)',
                border: '1px solid var(--color-border)',
                borderRadius: 20,
                fontSize: '0.8125rem',
                color: 'var(--color-text-muted)',
              }}>
                <span>{icon}</span>{label}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* ── Main Content ─────────────────────────────────────────── */}
      <div className="container" style={{ paddingBottom: 64 }}>

        {/* Recommend Tab */}
        {activeTab === 'recommend' && (
          <div style={{ maxWidth: 900, margin: '0 auto' }}>
            <div style={{ marginBottom: 24 }}>
              <QueryInput onResult={handleResult} onLoading={setLoading} />
            </div>

            {/* Loading skeleton */}
            {loading && (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
                {[160, 300, 80].map((h, i) => (
                  <div key={i} className="skeleton" style={{ height: h, borderRadius: 18 }} />
                ))}
              </div>
            )}

            {/* Results */}
            {result && !loading && (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
                <ReportCard result={result} />
                {result.recommendations.length > 0 && (
                  <PropertyTable materials={result.recommendations} />
                )}

                {/* New search CTA */}
                <div style={{ textAlign: 'center' }}>
                  <button
                    className="btn btn--outline"
                    onClick={() => setResult(null)}
                    id="new-search-btn"
                  >
                    ← New Search
                  </button>
                </div>
              </div>
            )}
          </div>
        )}

        {/* Predict Tab */}
        {activeTab === 'predict' && (
          <div style={{ maxWidth: 800, margin: '0 auto' }}>
            <div style={{ marginBottom: 32, textAlign: 'center', padding: '32px 0 16px' }}>
              <h1 style={{ fontSize: '1.75rem', marginBottom: 8 }}>
                <span className="gradient-text">LLM-Enhanced</span> Alloy Predictor
              </h1>
              <p className="text-muted">
                Define a custom alloy composition. Phase 1 computes rule-of-mixtures from our DB.
                Phase 2 sends it to Gemini for thermodynamic refinement with phase diagram knowledge.
              </p>
            </div>
            <PredictorPanel />
          </div>
        )}
      </div>

      {/* ── Footer ──────────────────────────────────────────────── */}
      <footer style={{
        borderTop: '1px solid var(--color-border)',
        padding: '20px 24px',
        textAlign: 'center',
      }}>
        <p className="text-xs text-dim">
          Smart Alloy Selector · MET-QUEST '26 ·{' '}
          <span className="font-mono" style={{ color: 'var(--color-text-dim)' }}>
            Go/Gin + Gemini 2.0 Flash + Neon PostgreSQL + React
          </span>
        </p>
      </footer>
    </div>
  )
}

export default App
