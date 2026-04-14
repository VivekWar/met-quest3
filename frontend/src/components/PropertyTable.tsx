import React, { useState } from 'react'
import { Material } from '../api/client'

interface Props {
  materials: Material[]
}

type PropertyDef = {
  key: string
  label: string
  unit: string
  precision: number | null
  getter?: (m: Material) => number | undefined
}

const PROPERTY_DEFS: PropertyDef[] = [
  { key: 'density',                label: 'Density',         unit: 'g/cm³',      precision: 2 },
  { key: 'glass_transition_temp',  label: 'Tg',              unit: '°C',          precision: 0, getter: (m) => m.glass_transition_temp !== undefined ? m.glass_transition_temp - 273.15 : undefined },
  { key: 'heat_deflection_temp',   label: 'HDT',             unit: '°C',          precision: 0, getter: (m) => m.heat_deflection_temp !== undefined ? m.heat_deflection_temp - 273.15 : undefined },
  { key: 'processing_temp_max_c',  label: 'Proc. Max',       unit: '°C',          precision: 0 },
  { key: 'yield_strength',         label: 'Yield Strength',  unit: 'MPa',         precision: 0 },
  { key: 'youngs_modulus',         label: "Young's Mod.",   unit: 'GPa',         precision: 1 },
  { key: 'thermal_conductivity',   label: 'Thermal Cond.',   unit: 'W/(m·K)',     precision: 2 },
  { key: 'thermal_expansion',      label: 'CTE',             unit: '10⁻⁶/K',      precision: 1 },
  { key: 'tensile_strength',       label: 'Tensile Str.',    unit: 'MPa',         precision: 0 },
  { key: 'melting_point',          label: 'Melt Pt',         unit: 'K',           precision: 0 },
  { key: 'specific_heat',          label: 'Specific Heat',   unit: 'J/(kg·K)',    precision: 0 },
]

const getCategoryTag = (cat: string) => {
  const map: Record<string, string> = {
    'Metal': 'metal', 'Ceramic': 'ceramic', 'Polymer': 'polymer',
    'Semiconductor': 'semiconductor', 'Composite': 'composite',
  }
  return map[cat] || 'unknown'
}

const formatVal = (val: number | undefined, precision: number | null): string => {
  if (val === undefined || val === null) return '—'
  if (precision === null) {
    // Scientific notation for resistivity
    return val.toExponential(2)
  }
  return val.toFixed(precision)
}

const MEDAL = ['🥇', '🥈', '🥉']

export const PropertyTable: React.FC<Props> = ({ materials }) => {
  const [sortKey, setSortKey] = useState<string | null>(null)
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc')

  const handleSort = (key: string) => {
    if (sortKey === key) {
      setSortDir(d => d === 'asc' ? 'desc' : 'asc')
    } else {
      setSortKey(key)
      setSortDir('asc')
    }
  }

  const sorted = [...materials].sort((a, b) => {
    if (!sortKey) return 0
    const def = PROPERTY_DEFS.find(p => p.key === sortKey)
    const av = def?.getter ? (def.getter(a) ?? Infinity) : ((a as any)[sortKey] ?? Infinity)
    const bv = def?.getter ? (def.getter(b) ?? Infinity) : ((b as any)[sortKey] ?? Infinity)
    return sortDir === 'asc' ? av - bv : bv - av
  })

  return (
    <div className="card fade-in-up" id="property-table-section" style={{ padding: 0, overflow: 'hidden' }}>
      <div style={{ padding: '20px 24px 12px' }}>
        <h3 style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <span>📊</span> Property Comparison
          <span className="text-xs text-dim font-mono" style={{ marginLeft: 'auto', fontWeight: 400 }}>
            Click column headers to sort
          </span>
        </h3>
      </div>

      <div className="table-wrapper" style={{ borderRadius: 0, border: 'none', borderTop: '1px solid var(--color-border)' }}>
        <table>
          <thead>
            <tr>
              <th style={{ minWidth: 180 }}>Material</th>
              {PROPERTY_DEFS.map(p => (
                <th
                  key={p.key}
                  onClick={() => handleSort(p.key)}
                  style={{ cursor: 'pointer', userSelect: 'none', minWidth: 110 }}
                  title={`Sort by ${p.label}`}
                >
                  {p.label}
                  <span style={{ opacity: 0.5, marginLeft: 4 }}>
                    {sortKey === p.key ? (sortDir === 'asc' ? '↑' : '↓') : '⇅'}
                  </span>
                  <br />
                  <span style={{ fontSize: '0.65rem', color: 'var(--color-text-dim)', fontWeight: 400 }}>
                    {p.unit}
                  </span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {sorted.map((mat, idx) => (
              <tr key={mat.id} id={`mat-row-${mat.id}`}>
                <td>
                  <div className="flex items-center gap-sm">
                    <span style={{ fontSize: '1rem', flexShrink: 0 }}>{MEDAL[idx] ?? '⚙️'}</span>
                    <div>
                      <div style={{ fontWeight: 600, color: 'var(--color-text)', fontSize: '0.875rem' }}>
                        {mat.name}
                      </div>
                      <div className="flex gap-sm mt-sm" style={{ flexWrap: 'wrap' }}>
                        <span
                          className="font-mono"
                          style={{ fontSize: '0.7rem', color: 'var(--color-text-dim)' }}
                        >
                          {mat.formula}
                        </span>
                        <span className={`tag tag--${getCategoryTag(mat.category)}`}>
                          {mat.category}
                        </span>
                      </div>
                    </div>
                  </div>
                </td>
                {PROPERTY_DEFS.map(p => {
                  const raw = p.getter ? p.getter(mat) : (mat as any)[p.key]
                  const display = formatVal(raw, p.precision)
                  return (
                    <td key={p.key} className={display === '—' ? 'val-null' : ''}>
                      {display}
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
