import React, { useState } from 'react'
import { RecommendResponse } from '../api/client'

interface Props {
  result: RecommendResponse
}

// Minimal markdown renderer (no library needed)
const renderMarkdown = (md: string): string => {
  return md
    // Headings
    .replace(/^## (.+)$/gm, '<h3 class="md-h2">$1</h3>')
    .replace(/^### (.+)$/gm, '<h4 class="md-h3">$1</h4>')
    // Bold
    .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
    // Italic
    .replace(/\*(.+?)\*/g, '<em>$1</em>')
    // Code inline
    .replace(/`(.+?)`/g, '<code>$1</code>')
    // Table rows — simplified
    .replace(/^\|(.+)\|$/gm, (match) => {
      const cells = match.slice(1, -1).split('|').map(c => c.trim())
      if (cells.every(c => /^[-:]+$/.test(c))) {
        return '' // separator row
      }
      const tag = cells[0].includes('**') || match.includes('---|') ? 'th' : 'td'
      return `<tr>${cells.map(c => `<${tag}>${c}</${tag}>`).join('')}</tr>`
    })
    // Wrap table rows
    .replace(/((<tr>.*<\/tr>\n?)+)/g, '<div class="table-wrapper"><table>$1</table></div>')
    // Bullet lists
    .replace(/^[-*] (.+)$/gm, '<li>$1</li>')
    .replace(/((<li>.*<\/li>\n?)+)/g, '<ul>$1</ul>')
    // Numbered lists
    .replace(/^\d+\. (.+)$/gm, '<li>$1</li>')
    // Paragraphs
    .replace(/\n\n/g, '</p><p>')
    .replace(/^(?!<[hultd])(.+)$/gm, (match) => {
      if (match.trim() && !match.startsWith('<') && !match.startsWith('---')) {
        return `<p>${match}</p>`
      }
      return match
    })
    // Horizontal rules
    .replace(/^---+$/gm, '<hr class="divider" />')
    // Line breaks
    .replace(/\n/g, '')
}

export const ReportCard: React.FC<Props> = ({ result }) => {
  const [showIntent, setShowIntent] = useState(false)

  const hasFilters = Object.keys(result.extracted_intent.filters || {}).length > 0
  const finalRecommendation = result.final_recommendation || result.recommendations[0]
  const topThree = (result.top_recommendations || result.recommendations).slice(0, 3)
  const inlinePrediction = result.inline_alloy_prediction

  return (
    <div className="fade-in-up" id="report-section">
      {/* Header banner */}
      <div style={{
        background: 'linear-gradient(135deg, rgba(0,212,255,0.1), rgba(168,85,247,0.1), rgba(255,215,0,0.05))',
        border: '1px solid rgba(0,212,255,0.2)',
        borderRadius: 'var(--radius-lg)',
        padding: '20px 24px',
        marginBottom: 20,
        display: 'flex',
        flexWrap: 'wrap',
        alignItems: 'center',
        gap: 16,
      }}>
        <div style={{
          fontSize: '2rem',
          width: 52, height: 52,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          background: 'rgba(0,212,255,0.1)',
          borderRadius: 12,
          border: '1px solid rgba(0,212,255,0.25)',
          flexShrink: 0,
        }}>🧬</div>
        <div>
          <div className="text-xs text-muted" style={{ letterSpacing: '0.1em', textTransform: 'uppercase', marginBottom: 4 }}>
            Virtual Scientist Report
          </div>
          <h2 style={{ fontSize: '1.1rem' }}>
            {result.recommendations.length} material{result.recommendations.length !== 1 ? 's' : ''} found
          </h2>
          <p className="text-xs text-muted mt-sm font-mono">
            "{result.query.slice(0, 80)}{result.query.length > 80 ? '…' : ''}"
          </p>
        </div>
        {result.tokens_used > 0 && (
          <div className="report-tokens" style={{ marginLeft: 'auto', textAlign: 'right', flexShrink: 0 }}>
            <div className="text-xs text-dim">Tokens used</div>
            <div className="font-mono" style={{ color: 'var(--color-accent)', fontSize: '0.9rem' }}>
              {result.tokens_used.toLocaleString()}
            </div>
          </div>
        )}
      </div>

      {/* Final Recommendation Highlight */}
      {finalRecommendation && (
        <div className="card card--success" style={{ marginBottom: 20, padding: '20px 24px' }}>
          <div className="text-xs text-muted" style={{ letterSpacing: '0.08em', textTransform: 'uppercase', marginBottom: 8 }}>
            Final Recommendation
          </div>
          <h3 style={{ marginBottom: 6, color: 'var(--color-success)' }}>{finalRecommendation.name}</h3>
          <p className="text-sm text-muted">
            This candidate scored highest after process feasibility checks, physics constraints, and category routing.
          </p>
        </div>
      )}

      {/* Top 3 Recommendations */}
      {topThree.length > 0 && (
        <div className="recommendation-grid" style={{ marginBottom: 20 }}>
          {topThree.map((mat, idx) => (
            <div
              key={mat.id}
              className={`card ${idx === 0 ? 'card--accent' : ''}`}
              style={{ padding: '16px 18px' }}
            >
              <div className="text-xs text-dim" style={{ marginBottom: 6 }}>Rank #{idx + 1}</div>
              <div style={{ fontWeight: 700, marginBottom: 6 }}>{mat.name}</div>
              <div className="text-xs text-muted">{mat.category}{mat.subcategory ? ` • ${mat.subcategory}` : ''}</div>
            </div>
          ))}
        </div>
      )}

      {inlinePrediction?.should_display && (
        <div className="card card--accent" style={{ marginBottom: 20, padding: '18px 20px' }}>
          <div className="text-xs text-muted" style={{ letterSpacing: '0.08em', textTransform: 'uppercase', marginBottom: 8 }}>
            AI Alloy Prediction
          </div>
          <p className="text-sm" style={{ color: 'var(--color-text-muted)', marginBottom: 10 }}>
            {inlinePrediction.summary}
          </p>

          {inlinePrediction.key_findings && Object.keys(inlinePrediction.key_findings).length > 0 && (
            <div style={{ marginBottom: 10 }}>
              {Object.entries(inlinePrediction.key_findings).map(([k, v]) => (
                <div key={k} className="text-xs" style={{ color: 'var(--color-text-muted)', marginBottom: 4 }}>
                  <strong style={{ color: 'var(--color-text)' }}>{k}:</strong> {v}
                </div>
              ))}
            </div>
          )}

          {inlinePrediction.risk_flags && inlinePrediction.risk_flags.length > 0 && (
            <div>
              <div className="text-xs" style={{ color: 'var(--color-warning)', marginBottom: 4 }}>Watch-outs:</div>
              {inlinePrediction.risk_flags.slice(0, 3).map((risk, idx) => (
                <div key={idx} className="text-xs" style={{ color: 'var(--color-text-muted)', marginBottom: 2 }}>
                  - {risk}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Main report */}
      <div className="card">
        <div
          className="markdown"
          style={{ lineHeight: 1.75 }}
          dangerouslySetInnerHTML={{ __html: renderMarkdown(result.report) }}
        />
      </div>

      {/* Debug: Extracted Intent */}
      {hasFilters && (
        <div style={{ marginTop: 12 }}>
          <button
            className="btn btn--outline btn--sm"
            onClick={() => setShowIntent(v => !v)}
            id="toggle-intent-btn"
            style={{ width: '100%', justifyContent: 'center' }}
          >
            {showIntent ? '▲' : '▼'} AI Extracted Constraints
          </button>
          {showIntent && (
            <div className="card mt-sm fade-in-up" style={{ padding: '16px 20px' }}>
              <div className="flex gap-md" style={{ flexWrap: 'wrap' }}>
                {result.extracted_intent.category && result.extracted_intent.category !== 'null' && (
                  <div>
                    <div className="label">Category</div>
                    <span className={`tag tag--${result.extracted_intent.category.toLowerCase()}`}>
                      {result.extracted_intent.category}
                    </span>
                  </div>
                )}
                {Object.entries(result.extracted_intent.filters || {}).map(([prop, filter]) => (
                  <div key={prop}>
                    <div className="label">{prop.replace(/_/g, ' ')}</div>
                    <code style={{ fontSize: '0.8rem', color: 'var(--color-primary)' }}>
                      {filter.min !== undefined && filter.min !== null ? `≥ ${filter.min}` : ''}
                      {filter.min !== undefined && filter.max !== undefined ? ' … ' : ''}
                      {filter.max !== undefined && filter.max !== null ? `≤ ${filter.max}` : ''}
                    </code>
                  </div>
                ))}
                {result.extracted_intent.sort_by && (
                  <div>
                    <div className="label">Sort by</div>
                    <code style={{ fontSize: '0.8rem', color: 'var(--color-accent)' }}>
                      {result.extracted_intent.sort_by} {result.extracted_intent.sort_dir}
                    </code>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
