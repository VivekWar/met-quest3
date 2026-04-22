import React, { useState, useCallback } from 'react'
import { BadgePlus, Beaker, Sparkles, SlidersHorizontal, Trash2 } from 'lucide-react'
import { predict, PredictResponse } from '../api/client'

const COMMON_ELEMENTS = [
  'Al','Cu','Fe','Ti','Ni','Zn','Cr','Mo','W','Co',
  'Mg','Pb','Sn','Au','Ag','Mn','Si','C','Nb','Ta',
]

interface ElementEntry {
  symbol: string
  percent: number
}

export const PredictorPanel: React.FC = () => {
  const [elements, setElements] = useState<ElementEntry[]>([
    { symbol: 'Cu', percent: 70 },
    { symbol: 'Zn', percent: 30 },
  ])
  const [customSymbol, setCustomSymbol] = useState('')
  const [loading, setLoading] = useState(false)
  const [result, setResult]   = useState<PredictResponse | null>(null)
  const [error, setError]     = useState<string | null>(null)

  const total = elements.reduce((s, e) => s + e.percent, 0)
  const isValid = elements.length >= 1 && Math.abs(total - 100) <= 5

  const addElement = (symbol: string) => {
    const sym = symbol.trim()
    if (!sym) return
    if (elements.find(e => e.symbol.toLowerCase() === sym.toLowerCase())) return
    const remaining = Math.max(0, 100 - total)
    setElements(prev => [...prev, { symbol: sym, percent: remaining }])
    setCustomSymbol('')
    setResult(null)
  }

  const removeElement = (idx: number) => {
    setElements(prev => prev.filter((_, i) => i !== idx))
    setResult(null)
  }

  const updatePercent = (idx: number, val: number) => {
    setElements(prev => prev.map((e, i) => i === idx ? { ...e, percent: val } : e))
    setResult(null)
  }

  const normalize = () => {
    if (total === 0) return
    setElements(prev => prev.map(e => ({
      ...e,
      percent: parseFloat(((e.percent / total) * 100).toFixed(1))
    })))
  }

  const handlePredict = useCallback(async () => {
    if (!isValid || loading) return
    setLoading(true)
    setError(null)
    setResult(null)

    const composition: Record<string, number> = {}
    elements.forEach(e => { composition[e.symbol] = e.percent })

    try {
      const res = await predict(composition)
      setResult(res)
    } catch (err: any) {
      setError(err?.response?.data?.error || err?.message || 'Prediction failed')
    } finally {
      setLoading(false)
    }
  }, [elements, isValid, loading])

  const formatVal = (v?: number, precision = 2, sciNotation = false) => {
    if (v === undefined || v === null) return '—'
    if (sciNotation) return v.toExponential(2)
    return v.toFixed(precision)
  }

  return (
    <div className="card fade-in-up" id="predictor-panel">
      <div className="panel-header">
        <div className="panel-icon" style={{ background: 'linear-gradient(135deg, rgba(255,215,0,0.16), rgba(255,149,0,0.12))', borderColor: 'rgba(255,215,0,0.24)', color: 'var(--color-accent)' }}>
          <Beaker size={18} />
        </div>
        <div>
          <h2 className="panel-title">Alloy predictor</h2>
          <p className="panel-subtitle">Enter a composition to generate a material estimate and compare baseline values.</p>
        </div>
      </div>

      <div className="section-pill-row">
        <span className="section-pill"><SlidersHorizontal size={12} /> Composition input</span>
        <span className="section-pill"><Sparkles size={12} /> LLM refinement</span>
      </div>

      <div className="mb-md">
        <label className="label">Composition by weight</label>

        <div className="stack-gap" style={{ gap: 10 }}>
          {elements.map((el, idx) => (
            <div key={idx} className="flex items-center gap-md predictor-row" id={`element-row-${idx}`}>
              <div className="symbol-chip">{el.symbol}</div>

              <input
                type="range"
                min={0} max={100} step={0.5}
                value={el.percent}
                onChange={e => updatePercent(idx, parseFloat(e.target.value))}
                style={{ flex: 1 }}
                aria-label={`${el.symbol} weight percent`}
              />

              <input
                type="number"
                className="input input--number"
                min={0} max={100} step={0.5}
                value={el.percent}
                onChange={e => updatePercent(idx, parseFloat(e.target.value) || 0)}
                aria-label={`${el.symbol} percent value`}
              />
              <span className="text-sm text-muted">%</span>

              <button
                className="btn btn--danger btn--sm"
                onClick={() => removeElement(idx)}
                title="Remove element"
                aria-label={`Remove ${el.symbol}`}
                style={{ padding: '6px 10px' }}
              ><Trash2 size={12} /></button>
            </div>
          ))}
        </div>

        {/* Total indicator */}
        <div
          className="flex justify-between items-center mt-md"
          style={{
            padding: '10px 14px',
            borderRadius: 8,
            background: isValid
              ? 'rgba(0,255,159,0.06)'
              : total > 105
              ? 'rgba(255,71,87,0.06)'
              : 'rgba(255,215,0,0.06)',
            border: `1px solid ${isValid ? 'rgba(0,255,159,0.2)' : total > 105 ? 'rgba(255,71,87,0.2)' : 'rgba(255,215,0,0.2)'}`,
          }}
        >
          <span className="text-sm font-mono">
            Total: <strong style={{ color: isValid ? '#00ff9f' : total > 105 ? '#ff4757' : '#ffd700' }}>
              {total.toFixed(1)}%
            </strong>
          </span>
          {Math.abs(total - 100) > 0.5 && (
            <button className="btn btn--outline btn--sm" onClick={normalize}>
              Normalize to 100%
            </button>
          )}
        </div>
      </div>

      <div className="mb-md">
        <label className="label">Add element</label>
        <div className="flex gap-sm" style={{ flexWrap: 'wrap', marginBottom: 10 }}>
          {COMMON_ELEMENTS.filter(s => !elements.find(e => e.symbol === s)).map(sym => (
            <button
              key={sym}
              className="btn btn--outline btn--sm font-mono"
              onClick={() => addElement(sym)}
              style={{ padding: '5px 12px', fontSize: '0.8rem' }}
              id={`add-${sym.toLowerCase()}`}
            >{sym}</button>
          ))}
        </div>
        <div className="flex gap-sm predictor-add-row">
          <input
            className="input"
            style={{ flex: 1 }}
            placeholder="Custom symbol (e.g. V, Hf, Re)"
            value={customSymbol}
            onChange={e => setCustomSymbol(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && addElement(customSymbol)}
            maxLength={4}
          />
          <button
            className="btn btn--outline"
            onClick={() => addElement(customSymbol)}
            disabled={!customSymbol.trim()}
          ><BadgePlus size={14} /> Add</button>
        </div>
      </div>

      <button
        id="predict-btn"
        className="btn btn--primary full-width"
        style={{ justifyContent: 'center' }}
        onClick={handlePredict}
        disabled={!isValid || loading}
      >
        {loading ? (
          <>
            <span style={{
              width: 16, height: 16, border: '2px solid #080c18',
              borderTopColor: 'transparent', borderRadius: '50%',
              animation: 'spin 0.8s linear infinite', display: 'inline-block',
            }} />
            Running prediction
          </>
        ) : 'Predict alloy'}
      </button>

      {error && (
        <div className="mt-md text-sm" style={{ color: '#ff4757', padding: '10px 14px', background: 'rgba(255,71,87,0.08)', borderRadius: 8 }}>
          ⚠️ {error}
        </div>
      )}

      {result && (
        <div className="fade-in-up" style={{ marginTop: 24 }}>
          <div className="divider" />

          <div style={{ marginBottom: 16 }}>
            <h4 style={{ color: 'var(--color-accent)' }}>⚗️ {result.predicted_name}</h4>
            <p className="text-xs text-muted mt-sm font-mono">{result.method}</p>
          </div>

          {/* Comparison table: Baseline vs Refined */}
          {result.baseline_properties && (
            <div className="table-wrapper mb-md">
              <table>
                <thead>
                  <tr>
                    <th>Property</th>
                    <th>📐 Baseline (Rule of Mixtures)</th>
                    <th>🤖 LLM-Refined</th>
                    <th>Unit</th>
                  </tr>
                </thead>
                <tbody>
                  {[
                    { key: 'density',                label: 'Density',            unit: 'g/cm³',   precision: 2, sci: false },
                    { key: 'melting_point',          label: 'Melting Point',      unit: 'K',       precision: 0, sci: false },
                    { key: 'thermal_conductivity',   label: 'Thermal Cond.',      unit: 'W/(m·K)', precision: 1, sci: false },
                    { key: 'electrical_resistivity', label: 'Resistivity',        unit: 'Ω·m',     precision: 0, sci: true  },
                    { key: 'yield_strength',         label: 'Yield Strength',     unit: 'MPa',     precision: 0, sci: false },
                    { key: 'youngs_modulus',         label: "Young's Modulus",    unit: 'GPa',     precision: 0, sci: false },
                  ].map(({ key, label, unit, precision, sci }) => {
                    const baseline = result.baseline_properties?.[key]
                    const refined  = (result as any)[key]
                    const changed  = baseline !== undefined && refined !== undefined &&
                      Math.abs(baseline - refined) / Math.max(Math.abs(baseline), 1) > 0.02
                    return (
                      <tr key={key}>
                        <td style={{ color: 'var(--color-text)', fontFamily: 'var(--font-sans)' }}>{label}</td>
                        <td style={{ opacity: 0.7 }}>{formatVal(baseline, precision, sci)}</td>
                        <td style={{ color: changed ? 'var(--color-accent)' : 'var(--color-success)' }}>
                          {formatVal(refined, precision, sci)}
                          {changed && (
                            <span className="text-xs" style={{ marginLeft: 6, opacity: 0.7 }}>
                              ({baseline !== undefined && refined !== undefined
                                ? ((refined - baseline) / Math.abs(baseline) * 100).toFixed(1) + '%'
                                : ''})
                            </span>
                          )}
                        </td>
                        <td>{unit}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}

          {/* Scientific Explanation */}
          {result.scientific_explanation && (
            <div style={{
              background: 'rgba(0,212,255,0.04)',
              border: '1px solid rgba(0,212,255,0.15)',
              borderRadius: 12,
              padding: '16px 20px',
              marginBottom: 12,
            }}>
              <div className="label" style={{ marginBottom: 8 }}>🔬 Scientific Explanation</div>
              <p className="text-sm" style={{ color: 'var(--color-text-muted)', lineHeight: 1.75 }}>
                {result.scientific_explanation}
              </p>
            </div>
          )}

          {/* Notes */}
          {result.notes && (
            <div style={{
              background: 'rgba(255,215,0,0.05)',
              border: '1px solid rgba(255,215,0,0.2)',
              borderRadius: 10,
              padding: '12px 16px',
            }}>
              <p className="text-xs text-muted"
                dangerouslySetInnerHTML={{
                  __html: result.notes
                    .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
                    .replace(/\n/g, '<br/>')
                }}
              />
            </div>
          )}
        </div>
      )}
    </div>
  )
}
