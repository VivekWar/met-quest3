import React, { useState, useRef, useEffect } from 'react'
import { ArrowUp, Bot, Check, Copy, Maximize2, Minimize2, Sparkles, User, X } from 'lucide-react'
import { ChatMessage } from '../hooks/useChatStorage'
import '../styles/chat.css'

interface ChatPanelProps {
  messages: ChatMessage[]
  onSendMessage: (query: string) => Promise<void> | void
  loading?: boolean
}

export const ChatPanel: React.FC<ChatPanelProps> = ({
  messages,
  onSendMessage,
  loading = false,
}) => {
  const [query, setQuery] = useState('')
  const [isSending, setIsSending] = useState(false)
  const [expandedReports, setExpandedReports] = useState<Record<string, boolean>>({})
  const [copiedMessageId, setCopiedMessageId] = useState<string | null>(null)
  const [showTour, setShowTour] = useState(false)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const TOUR_KEY = 'met-quest-tour-dismissed'

  const starterPrompts = [
    'Need a lightweight material for an aircraft bracket with high fatigue resistance.',
    'Suggest a corrosion-resistant material for marine bolts.',
    'Which material is best for a 3D-printable enclosure near 90C?',
  ]

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  useEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = '0px'
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`
  }, [query])

  useEffect(() => {
    if (typeof window === 'undefined') return
    const dismissed = localStorage.getItem(TOUR_KEY) === 'true'
    setShowTour(!dismissed)
  }, [])

  const dismissTour = () => {
    setShowTour(false)
    localStorage.setItem(TOUR_KEY, 'true')
  }

  const getAssistantCopyText = (msg: ChatMessage): string => {
    if (!msg.response) return ''
    const topThree = (msg.response.recommendations || []).slice(0, 3)
    const lines: string[] = []
    if (topThree.length > 0) {
      lines.push('Top 3 recommendations:')
      topThree.forEach((item: any, idx: number) => {
        lines.push(`${idx + 1}. ${item.name}${item.category ? ` (${item.category})` : ''}`)
      })
      lines.push('')
    }
    lines.push(msg.response.report || '')
    return lines.join('\n')
  }

  const copyMessage = async (msg: ChatMessage) => {
    const text = msg.type === 'user' ? (msg.originalQuery || msg.query || '') : getAssistantCopyText(msg)
    if (!text) return
    try {
      await navigator.clipboard.writeText(text)
      setCopiedMessageId(msg.id)
      window.setTimeout(() => {
        setCopiedMessageId((current) => (current === msg.id ? null : current))
      }, 1500)
    } catch {
      // no-op for unsupported clipboard permissions
    }
  }

  const renderInlineText = (text: string) => {
    return text.split(/(\*\*.*?\*\*)/g).map((part, idx) => {
      if (part.startsWith('**') && part.endsWith('**')) {
        return <strong key={idx}>{part.slice(2, -2)}</strong>
      }
      return <React.Fragment key={idx}>{part}</React.Fragment>
    })
  }

  const renderReport = (report: string) => {
    return report.split('\n').map((rawLine, idx) => {
      const line = rawLine.trim()
      if (!line) {
        return <div key={idx} className="report-space" />
      }

      const heading = line.match(/^#{1,6}\s+(.+)$/)
      if (heading) {
        return <h3 key={idx} className="report-heading">{renderInlineText(heading[1])}</h3>
      }

      const bullet = line.match(/^[-*]\s+(.+)$/)
      if (bullet) {
        return (
          <div key={idx} className="report-bullet">
            <span aria-hidden="true" />
            <p>{renderInlineText(bullet[1])}</p>
          </div>
        )
      }

      const numbered = line.match(/^(\d+)\.\s+(.+)$/)
      if (numbered) {
        return (
          <div key={idx} className="report-numbered">
            <span>{numbered[1]}</span>
            <p>{renderInlineText(numbered[2])}</p>
          </div>
        )
      }

      return <p key={idx} className="report-paragraph">{renderInlineText(line)}</p>
    })
  }

  const send = async () => {
    const text = query.trim()
    if (!text || loading || isSending) {
      return
    }

    setIsSending(true)
    try {
      await onSendMessage(text)
      setQuery('')
    } finally {
      setIsSending(false)
    }
  }

  return (
    <div className="chat-panel chat-panel--full">
      <div className="chat-messages">
        {messages.length === 0 ? (
          <div className="chat-empty-state chat-empty-state--landing">
            <div className="chat-empty-icon chat-empty-icon--spark">
              <Sparkles size={28} />
            </div>
            <h2>Find the right material faster.</h2>
            <p>Describe your use-case, constraints, and process. I will return ranked options with reasoning.</p>
            {showTour && (
              <div className="tour-card">
                <button className="tour-close" onClick={dismissTour} aria-label="Dismiss quick tour">
                  <X size={14} />
                </button>
                <h3>Quick start</h3>
                <ol>
                  <li>Describe your problem and manufacturing process.</li>
                  <li>Review top candidates and compare shortlist cards.</li>
                  <li>Copy or expand analysis when sharing with your team.</li>
                </ol>
              </div>
            )}
            <div className="starter-grid">
              {starterPrompts.map((prompt) => (
                <button
                  key={prompt}
                  className="starter-chip"
                  onClick={() => setQuery(prompt)}
                >
                  {prompt}
                </button>
              ))}
            </div>
          </div>
        ) : (
          messages.map((msg) => (
            <div key={msg.id} className={`chat-message-row chat-message-row-${msg.type}`}>
              <div className="chat-avatar" aria-hidden="true">
                {msg.type === 'user' ? <User size={14} /> : <Bot size={14} />}
              </div>

              <div className={`chat-message chat-message-${msg.type}`}>
              <div className="chat-message-header">
                <span className="chat-message-role">{msg.type === 'user' ? 'You' : 'Material Assistant'}</span>
                <div className="chat-header-right">
                  <button
                    className="message-action"
                    onClick={() => {
                      void copyMessage(msg)
                    }}
                    title="Copy message"
                    aria-label="Copy message"
                  >
                    {copiedMessageId === msg.id ? <Check size={13} /> : <Copy size={13} />}
                    {copiedMessageId === msg.id ? 'Copied' : 'Copy'}
                  </button>
                  <span className="chat-message-time">
                    {new Date(msg.timestamp).toLocaleTimeString()}
                  </span>
                </div>
              </div>

              {msg.type === 'user' ? (
                <div className="chat-message-query">{msg.originalQuery}</div>
              ) : (
                <>
                  {msg.response && (
                    <>
                      <div className="chat-message-response">
                        {msg.response.recommendations && msg.response.recommendations.length > 0 && (
                          <div className="recommendation-summary recommendation-summary--compact">
                            <strong>Top shortlist</strong>
                            <div className="top3-grid">
                              {msg.response.recommendations.slice(0, 3).map((item: any, idx: number) => (
                                <div key={`${msg.id}-${item.id ?? item.name ?? idx}`} className={`top3-card ${idx === 0 ? 'top3-card-best' : ''}`}>
                                  <div className="top3-rank">#{idx + 1}</div>
                                  <div className="top3-name">{item.name}</div>
                                  <div className="chip-row">
                                    <span className="preview-chip">{item.category}</span>
                                    {item.subcategory && (
                                      <span className="preview-chip preview-chip--muted">{item.subcategory}</span>
                                    )}
                                  </div>
                                </div>
                              ))}
                            </div>
                          </div>
                        )}

                        <div className={`report-excerpt ${expandedReports[msg.id] ? 'is-expanded' : ''}`}>
                          {renderReport(msg.response.report)}
                        </div>
                        {msg.response.report && msg.response.report.length > 380 && (
                          <button
                            className="message-action message-action-secondary"
                            onClick={() =>
                              setExpandedReports((current) => ({
                                ...current,
                                [msg.id]: !current[msg.id],
                              }))
                            }
                          >
                            {expandedReports[msg.id] ? <Minimize2 size={13} /> : <Maximize2 size={13} />}
                            {expandedReports[msg.id] ? 'Collapse report' : 'Expand report'}
                          </button>
                        )}
                      </div>
                      {msg.tokens && (
                        <div className="chat-message-tokens">
                          Tokens used: {msg.tokens}
                        </div>
                      )}
                    </>
                  )}
                </>
              )}
              </div>
            </div>
          ))
        )}
        {loading && (
          <div className="chat-message-row chat-message-row-assistant">
            <div className="chat-avatar" aria-hidden="true">
              <Bot size={14} />
            </div>
            <div className="chat-message chat-message-assistant">
              <div className="chat-message-header">
                <span className="chat-message-role">Material Assistant</span>
                <span className="chat-message-time">Analyzing...</span>
              </div>
              <div className="assistant-thinking">
                <span />
                <span />
                <span />
              </div>
            </div>
          </div>
        )}
        <div ref={messagesEndRef} />
      </div>

      <div className="chat-composer-shell">
        <div className="chat-composer">
          <textarea
            ref={textareaRef}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Message Material Assistant..."
            onKeyDown={(event) => {
              if (event.key === 'Enter' && !event.shiftKey) {
                event.preventDefault()
                void send()
              }
            }}
          />

          <button
            type="button"
            className="chat-send-button"
            onClick={() => {
              void send()
            }}
            disabled={!query.trim() || loading || isSending}
            title="Send"
          >
            <ArrowUp size={16} />
          </button>
        </div>
      </div>
    </div>
  )
}
