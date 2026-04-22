import React, { useState, useEffect } from 'react'
import { ChevronDown, ChevronUp, Cpu, Layers3, Sparkles, TriangleAlert } from 'lucide-react'
import { RecommendResponse } from '../api/client'

interface Props {
  result: RecommendResponse
}

// Render LaTeX math expressions using simple KaTeX-like substitutions
const renderLatex = (text: string): string => {
  if (!text) return text
  // Replace common LaTeX patterns with Unicode/HTML equivalents for display
  return text
    // Greek letters
    .replace(/\\sigma/g, 'σ')
    .replace(/\\rho/g, 'ρ')
    .replace(/\\tau/g, 'τ')
    .replace(/\\alpha/g, 'α')
    .replace(/\\beta/g, 'β')
    .replace(/\\gamma/g, 'γ')
    .replace(/\\pi/g, 'π')
    // Superscripts and subscripts (basic)
    .replace(/\^2/g, '²')
    .replace(/\^3/g, '³')
    .replace(/_{]/g, '<sub>')
    .replace(/}/g, '</sub>')
    .replace(/_([a-zA-Z0-9]+)/g, '<sub>$1</sub>')
}

// Enhanced markdown renderer with LaTeX support
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
    // LaTeX inline math (preserve before other replacements)
    .replace(/\$([^\$]+)\$/g, '<code class="latex-math">$1</code>')
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
        return `<p>${renderLatex(match)}</p>`
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
  const [showFullAnalysis, setShowFullAnalysis] = useState(false)

  const hasFilters = Object.keys(result.extracted_intent.filters || {}).length > 0
  const finalRecommendation = result.final_recommendation || result.recommendations[0]
  const topThree = (result.top_recommendations || result.recommendations).slice(0, 3)
  const inlinePrediction = result.inline_alloy_prediction

  // Load KaTeX for LaTeX rendering if available
  useEffect(() => {
    // Attempt to load KaTeX if script not already present
    if (typeof window !== 'undefined' && !(window as any).KaTeX) {
      const script = document.createElement('script')
      script.src = 'https://cdn.jsdelivr.net/npm/katex@0.16.0/dist/katex.min.js'
      script.async = true
      document.head.appendChild(script)

      const link = document.createElement('link')
      link.rel = 'stylesheet'
      link.href = 'https://cdn.jsdelivr.net/npm/katex@0.16.0/dist/katex.min.css'
      document.head.appendChild(link)
    }
  }, [])

  return (
    <div className="fade-in-up" id="report-section">
      <div className="report-hero">
        <div className="report-hero-icon">
          <Cpu size={22} />
        </div>
        <div>
          <div className="eyebrow">
            Generated report
          </div>
          <h2 className="report-title">
            {result.recommendations.length} material{result.recommendations.length !== 1 ? 's' : ''} found
          </h2>
          <p className="report-query">
            {result.query.slice(0, 80)}{result.query.length > 80 ? '…' : ''}
          </p>
        </div>
        {result.tokens_used > 0 && (
          <div className="report-tokens">
            <div className="text-xs text-dim">Tokens used</div>
            <div className="font-mono token-value">
              {result.tokens_used.toLocaleString()}
            </div>
          </div>
        )}
      </div>

      {finalRecommendation && (
        <div className="card card--success report-highlight">
          <div className="eyebrow">
            <Sparkles size={13} />
            Top recommendation
          </div>
          <h3 className="report-highlight-title">{finalRecommendation.name}</h3>
          <p className="report-highlight-copy">
            This candidate is the first match returned by the recommendation response.
          </p>
        </div>
      )}

      {topThree.length > 0 && (
        <div className="recommendation-grid report-grid">
          {topThree.map((mat, idx) => (
            <div
              key={mat.id}
              className={`card ${idx === 0 ? 'card--accent' : ''}`}
              style={{ padding: '16px 18px' }}
            >
              <div className="eyebrow">Rank #{idx + 1}</div>
              <div className="report-card-title">{mat.name}</div>
              <div className="text-xs text-muted">{mat.category}{mat.subcategory ? ` • ${mat.subcategory}` : ''}</div>
              {mat.density && (
                <div className="text-xs text-muted mt-sm">ρ: {mat.density?.toFixed(1)} kg/m³</div>
              )}
            </div>
          ))}
        </div>
      )}

      {inlinePrediction?.should_display && (
        <div className="card card--accent report-section-card">
          <div className="eyebrow">
            <Layers3 size={13} />
            Prediction summary
          </div>
          <p className="text-sm report-copy">
            {inlinePrediction.summary}
          </p>

          {inlinePrediction.key_findings && Object.keys(inlinePrediction.key_findings).length > 0 && (
            <div className="section-block">
              <div className="text-xs text-dim section-label">Key findings</div>
              {Object.entries(inlinePrediction.key_findings).map(([k, v]) => (
                <div key={k} className="text-xs report-kv">
                  <strong style={{ color: 'var(--color-text)' }}>{k}:</strong> {v}
                </div>
              ))}
            </div>
          )}

          {inlinePrediction.risk_flags && inlinePrediction.risk_flags.length > 0 && (
            <div>
              <div className="text-xs section-label section-label-warn">
                <TriangleAlert size={13} />
                Risk flags
              </div>
              {inlinePrediction.risk_flags.slice(0, 3).map((risk, idx) => (
                <div key={idx} className="text-xs report-risk-item">
                  {risk}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {(result as any).physics_analysis && (
        <div className="card card--info report-section-card">
          <button
            className="btn btn--outline btn--sm"
            onClick={() => setShowFullAnalysis(v => !v)}
            style={{ width: '100%', justifyContent: 'space-between', marginBottom: 12 }}
          >
            <span className="button-label">Detailed analysis</span>
            <span>{showFullAnalysis ? <ChevronUp size={16} /> : <ChevronDown size={16} />}</span>
          </button>
          
          {showFullAnalysis && (
            <div className="fade-in-up">
              {(result as any).physics_analysis.recommendation_narrative && (
                <div className="analysis-block">
                  <div className="text-xs text-dim section-label">Recommendation rationale</div>
                  <p className="text-sm">{(result as any).physics_analysis.recommendation_narrative}</p>
                </div>
              )}
              
              {(result as any).physics_analysis.merit_index_calculation && (
                <div className="analysis-block">
                  <div className="text-xs text-dim section-label">Merit index</div>
                  <code style={{ fontSize: '0.85rem', color: 'var(--color-accent)', whiteSpace: 'pre-wrap' }}>
                    {(result as any).physics_analysis.merit_index_calculation}
                  </code>
                </div>
              )}
              
              {(result as any).physics_analysis.failure_rejection_reasons && (result as any).physics_analysis.failure_rejection_reasons.length > 0 && (
                <div className="analysis-block">
                  <div className="text-xs text-dim section-label">Rejected alternatives</div>
                  <ul style={{ marginLeft: '1.2rem', color: 'var(--color-text-muted)' }}>
                    {(result as any).physics_analysis.failure_rejection_reasons.slice(0, 5).map((reason: string, idx: number) => (
                      <li key={idx} className="text-xs" style={{ marginBottom: 4 }}>{reason}</li>
                    ))}
                  </ul>
                </div>
              )}
              
              {(result as any).physics_analysis.manufacturing_feasibility && (
                <div className="analysis-block">
                  <div className="text-xs text-dim section-label">Manufacturing feasibility</div>
                  <p className="text-sm" style={{ color: 'var(--color-text-muted)' }}>{(result as any).physics_analysis.manufacturing_feasibility}</p>
                </div>
              )}
              
              {(result as any).physics_analysis.safety_margin && (
                <div>
                  <div className="text-xs text-dim section-label">Safety margin</div>
                  <p className="text-sm" style={{ color: 'var(--color-success)' }}>{(result as any).physics_analysis.safety_margin}</p>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      <div className="card">
        <div
          className="markdown"
          style={{ lineHeight: 1.75 }}
          dangerouslySetInnerHTML={{ __html: renderMarkdown(result.report) }}
        />
      </div>

      {hasFilters && (
        <div className="intent-block">
          <button
            className="btn btn--outline btn--sm"
            onClick={() => setShowIntent(v => !v)}
            id="toggle-intent-btn"
            style={{ width: '100%', justifyContent: 'space-between' }}
          >
            <span className="button-label">Extracted intent</span>
            <span>{showIntent ? <ChevronUp size={16} /> : <ChevronDown size={16} />}</span>
          </button>
          {showIntent && (
            <div className="card mt-sm fade-in-up intent-card">
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
