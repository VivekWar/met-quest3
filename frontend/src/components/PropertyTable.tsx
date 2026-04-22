import React, { useMemo, useState } from 'react'
import { ChevronDown, ChevronUp, ChevronsUpDown, Trophy } from 'lucide-react'
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
  { key: 'density', label: 'Density', unit: 'g/cm³', precision: 2 },
  { key: 'glass_transition_temp', label: 'Tg', unit: '°C', precision: 0, getter: (m) => m.glass_transition_temp !== undefined ? m.glass_transition_temp - 273.15 : undefined },
  { key: 'heat_deflection_temp', label: 'HDT', unit: '°C', precision: 0, getter: (m) => m.heat_deflection_temp !== undefined ? m.heat_deflection_temp - 273.15 : undefined },
  { key: 'processing_temp_max_c', label: 'Proc. Max', unit: '°C', precision: 0 },
  { key: 'yield_strength', label: 'Yield Strength', unit: 'MPa', precision: 0 },
  { key: 'youngs_modulus', label: "Young's Mod.", unit: 'GPa', precision: 1 },
  { key: 'thermal_conductivity', label: 'Thermal Cond.', unit: 'W/(m·K)', precision: 2 },
  { key: 'thermal_expansion', label: 'CTE', unit: '10⁻⁶/K', precision: 1 },
  { key: 'tensile_strength', label: 'Tensile Str.', unit: 'MPa', precision: 0 },
  { key: 'melting_point', label: 'Melt Pt', unit: 'K', precision: 0 },
  { key: 'specific_heat', label: 'Specific Heat', unit: 'J/(kg·K)', precision: 0 },
]

const getCategoryTag = (cat: string) => {
  const map: Record<string, string> = {
    Metal: 'metal',
    Ceramic: 'ceramic',
    Polymer: 'polymer',
    Semiconductor: 'semiconductor',
    Composite: 'composite',
  }

  return map[cat] || 'unknown'
}

const formatVal = (val: number | undefined, precision: number | null): string => {
  if (val === undefined || val === null) return '—'
  if (precision === null) {
    return val.toExponential(2)
  }

  return val.toFixed(precision)
}

const MEDAL = ['🥇', '🥈', '🥉']

const getNumericValue = (material: Material, def: PropertyDef) => {
  if (def.getter) {
    return def.getter(material)
  }

  return (material as Record<string, number | undefined>)[def.key]
}

export const PropertyTable: React.FC<Props> = ({ materials }) => {
  const [sortKey, setSortKey] = useState<string | null>(null)
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc')

  const handleSort = (key: string) => {
    if (sortKey === key) {
      setSortDir(currentSortDirection => (currentSortDirection === 'asc' ? 'desc' : 'asc'))
      return
    }

    setSortKey(key)
    setSortDir('asc')
  }

  const sorted = useMemo(() => {
    if (!sortKey) {
      return materials
    }

    const def = PROPERTY_DEFS.find(property => property.key === sortKey)
    if (!def) {
      return materials
    }

    return [...materials].sort((a, b) => {
      const av = getNumericValue(a, def)
      const bv = getNumericValue(b, def)

      if (av === undefined && bv === undefined) return 0
      if (av === undefined) return 1
      if (bv === undefined) return -1

      return sortDir === 'asc' ? av - bv : bv - av
    })
  }, [materials, sortDir, sortKey])

  const sortedLabel = sortKey ? PROPERTY_DEFS.find(property => property.key === sortKey)?.label : null

  return (
    <div className="card fade-in-up" id="property-table-section" style={{ padding: 0, overflow: 'hidden' }}>
      <div style={{ padding: '20px 24px 14px', display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 16, flexWrap: 'wrap' }}>
        <div>
          <h3 style={{ display: 'flex', alignItems: 'center', gap: 10, margin: 0 }}>
            <Trophy size={18} />
            Property Comparison
          </h3>
          <p className="text-sm text-muted" style={{ margin: '8px 0 0' }}>
            Compare the selected materials across engineering properties.
          </p>
        </div>
        <div className="text-xs text-dim font-mono" style={{ alignSelf: 'center' }}>
          {sortKey ? `Sorted by ${sortedLabel} (${sortDir})` : 'Click a column to sort'}
        </div>
      </div>

      <div className="table-wrapper" style={{ borderRadius: 0, border: 'none', borderTop: '1px solid var(--color-border)' }}>
        <table>
          <thead>
            <tr>
              <th style={{ minWidth: 180 }}>Material</th>
              {PROPERTY_DEFS.map(property => (
                <th key={property.key} style={{ minWidth: 120 }}>
                  <button
                    type="button"
                    onClick={() => handleSort(property.key)}
                    title={`Sort by ${property.label}`}
                    style={{
                      all: 'unset',
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 8,
                      cursor: 'pointer',
                      userSelect: 'none',
                    }}
                  >
                    <span style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-start', gap: 2 }}>
                      <span>{property.label}</span>
                      <span style={{ fontSize: '0.65rem', color: 'var(--color-text-dim)', fontWeight: 400 }}>
                        {property.unit}
                      </span>
                    </span>
                    <span aria-hidden="true" style={{ display: 'inline-flex', color: 'var(--color-text-dim)' }}>
                      {sortKey === property.key ? (sortDir === 'asc' ? <ChevronUp size={14} /> : <ChevronDown size={14} />) : <ChevronsUpDown size={14} />}
                    </span>
                  </button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {sorted.length === 0 && (
              <tr>
                <td colSpan={PROPERTY_DEFS.length + 1} style={{ padding: '28px 16px', textAlign: 'center' }}>
                  No materials available for comparison.
                </td>
              </tr>
            )}

            {sorted.map((material, index) => (
              <tr key={material.id} id={`mat-row-${material.id}`}>
                <td>
                  <div className="flex items-center gap-sm">
                    <span style={{ fontSize: '1rem', flexShrink: 0 }}>{MEDAL[index] ?? '⚙️'}</span>
                    <div>
                      <div style={{ fontWeight: 600, color: 'var(--color-text)', fontSize: '0.875rem' }}>
                        {material.name}
                      </div>
                      <div className="flex gap-sm mt-sm" style={{ flexWrap: 'wrap' }}>
                        <span className="font-mono" style={{ fontSize: '0.7rem', color: 'var(--color-text-dim)' }}>
                          {material.formula}
                        </span>
                        <span className={`tag tag--${getCategoryTag(material.category)}`}>
                          {material.category}
                        </span>
                      </div>
                    </div>
                  </div>
                </td>

                {PROPERTY_DEFS.map(property => {
                  const rawValue = property.getter ? property.getter(material) : (material as Record<string, number | undefined>)[property.key]
                  const displayValue = formatVal(rawValue, property.precision)

                  return (
                    <td key={property.key} className={displayValue === '—' ? 'val-null' : ''}>
                      {displayValue}
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
