import { useState, useCallback, useEffect } from 'react'

export interface Constraint {
  id: string
  key: string
  operator: 'min' | 'max' | 'equals' | 'contains'
  value: string | number
  label: string
}

export interface ChatMessage {
  id: string
  type: 'user' | 'assistant'
  originalQuery?: string
  constraints?: Constraint[]
  query?: string
  response?: any
  timestamp: number
  tokens?: number
}

export interface ChatSession {
  id: string
  title: string
  messages: ChatMessage[]
  createdAt: number
  updatedAt: number
}

const STORAGE_KEY = 'met-quest-chats'
const ACTIVE_SESSION_KEY = 'met-quest-active-session'

export const useChatStorage = () => {
  const [sessions, setSessions] = useState<ChatSession[]>([])
  const [activeSessionId, setActiveSessionId] = useState<string | null>(null)
  const [isLoaded, setIsLoaded] = useState(false)

  // Load from localStorage on mount
  useEffect(() => {
    const stored = localStorage.getItem(STORAGE_KEY)
    const activeSess = localStorage.getItem(ACTIVE_SESSION_KEY)
    
    if (stored) {
      try {
        setSessions(JSON.parse(stored))
      } catch (e) {
        console.error('Failed to parse chat sessions:', e)
      }
    }
    
    if (activeSess) {
      setActiveSessionId(activeSess)
    }
    
    setIsLoaded(true)
  }, [])

  // Save sessions to localStorage
  useEffect(() => {
    if (isLoaded) {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(sessions))
    }
  }, [sessions, isLoaded])

  // Save active session to localStorage
  useEffect(() => {
    if (isLoaded && activeSessionId) {
      localStorage.setItem(ACTIVE_SESSION_KEY, activeSessionId)
    }
  }, [activeSessionId, isLoaded])

  const createSession = useCallback((title?: string) => {
    const id = Date.now().toString()
    const session: ChatSession = {
      id,
      title: title || `Chat ${new Date().toLocaleDateString()}`,
      messages: [],
      createdAt: Date.now(),
      updatedAt: Date.now(),
    }
    setSessions(prev => [session, ...prev])
    setActiveSessionId(id)
    return id
  }, [])

  const addMessage = useCallback((sessionId: string, message: ChatMessage) => {
    setSessions(prev =>
      prev.map(session =>
        session.id === sessionId
          ? {
              ...session,
              messages: [...session.messages, message],
              updatedAt: Date.now(),
            }
          : session
      )
    )
  }, [])

  const updateMessage = useCallback((sessionId: string, messageId: string, updates: Partial<ChatMessage>) => {
    setSessions(prev =>
      prev.map(session =>
        session.id === sessionId
          ? {
              ...session,
              messages: session.messages.map(msg =>
                msg.id === messageId ? { ...msg, ...updates } : msg
              ),
              updatedAt: Date.now(),
            }
          : session
      )
    )
  }, [])

  const deleteSession = useCallback((sessionId: string) => {
    setSessions(prev => prev.filter(s => s.id !== sessionId))
    if (activeSessionId === sessionId) {
      const remaining = sessions.filter(s => s.id !== sessionId)
      setActiveSessionId(remaining.length > 0 ? remaining[0].id : null)
    }
  }, [activeSessionId, sessions])

  const clearAllSessions = useCallback(() => {
    setSessions([])
    setActiveSessionId(null)
    localStorage.removeItem(STORAGE_KEY)
    localStorage.removeItem(ACTIVE_SESSION_KEY)
  }, [])

  const getActiveSession = useCallback(() => {
    return sessions.find(s => s.id === activeSessionId)
  }, [sessions, activeSessionId])

  const renameSession = useCallback((sessionId: string, newTitle: string) => {
    setSessions(prev =>
      prev.map(session =>
        session.id === sessionId
          ? { ...session, title: newTitle, updatedAt: Date.now() }
          : session
      )
    )
  }, [])

  return {
    sessions,
    activeSessionId,
    isLoaded,
    createSession,
    addMessage,
    updateMessage,
    deleteSession,
    clearAllSessions,
    getActiveSession,
    renameSession,
    setActiveSessionId,
  }
}
