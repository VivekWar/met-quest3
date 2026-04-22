import React from 'react'
import { Clock3, MessageSquareText, Plus } from 'lucide-react'
import { ChatSession } from '../hooks/useChatStorage'
import '../styles/chat-history.css'

interface ChatHistoryProps {
  sessions: ChatSession[]
  activeSessionId: string | null
  onSelectSession: (sessionId: string) => void
  onCreateNewSession: () => void
}

export const ChatHistory: React.FC<ChatHistoryProps> = ({
  sessions,
  activeSessionId,
  onSelectSession,
  onCreateNewSession,
}) => {
  const formatDate = (timestamp: number) => {
    const date = new Date(timestamp)
    const today = new Date()
    const yesterday = new Date(today)
    yesterday.setDate(yesterday.getDate() - 1)

    if (date.toDateString() === today.toDateString()) {
      return date.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })
    } else if (date.toDateString() === yesterday.toDateString()) {
      return 'Yesterday'
    } else {
      return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
    }
  }

  return (
    <div className="chat-history">
      <div className="chat-history-header">
        <div>
          <h2>Sessions</h2>
          <p className="chat-history-subtitle">{sessions.length} total</p>
        </div>
        <button className="btn-new-chat" onClick={onCreateNewSession} title="Start a new chat">
          <Plus size={14} /> New
        </button>
      </div>

      {sessions.length === 0 ? (
        <div className="chat-history-empty">
          <div className="empty-icon">
            <MessageSquareText size={22} />
          </div>
          <p>No sessions yet</p>
          <span>Start a new chat to save your material exploration history.</span>
        </div>
      ) : (
        <>
          <div className="chat-history-list">
            {sessions.map((session) => (
              <button
                type="button"
                key={session.id}
                className={`chat-history-item ${activeSessionId === session.id ? 'active' : ''}`}
                onClick={() => onSelectSession(session.id)}
              >
                <div className="chat-item-content">
                  <div className="chat-item-title">{session.title}</div>
                  <div className="chat-item-meta">
                    <span><MessageSquareText size={12} /> {session.messages.length}</span>
                    <span><Clock3 size={12} /> {formatDate(session.updatedAt)}</span>
                  </div>
                </div>
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
