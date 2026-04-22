import React, { useState, useCallback, useEffect } from 'react'
import { Circle, Menu, Plus, Sparkles, X } from 'lucide-react'
import './styles/index.css'
import './styles/chat.css'
import './styles/chat-history.css'
import { ChatPanel } from './components/ChatPanel'
import { ChatHistory } from './components/ChatHistory'
import { HomePage } from './components/HomePage'
import { chatFollowup, pingStatus, recommend } from './api/client'
import { useChatStorage, ChatMessage } from './hooks/useChatStorage'

type ApiStatus = 'checking' | 'online' | 'offline'

const CHAT_ROUTE = '/chat'

const getPathname = () => window.location.pathname

const navigateTo = (path: string) => {
  if (window.location.pathname !== path) {
    window.history.pushState({}, '', path)
    window.dispatchEvent(new PopStateEvent('popstate'))
  }
}

const ChatWorkspace: React.FC = () => {
  const [loading, setLoading] = useState(false)
  const [isSidebarOpen, setIsSidebarOpen] = useState(false)
  const [isMobileViewport, setIsMobileViewport] = useState(false)
  const [apiStatus, setApiStatus] = useState<ApiStatus>('checking')

  const chatStorage = useChatStorage()

  useEffect(() => {
    let mounted = true

    const checkApiHealth = async () => {
      const ok = await pingStatus()
      if (mounted) {
        setApiStatus(ok ? 'online' : 'offline')
      }
    }

    checkApiHealth()
    const timer = window.setInterval(checkApiHealth, 45000)
    return () => {
      mounted = false
      window.clearInterval(timer)
    }
  }, [])

  useEffect(() => {
    if (chatStorage.isLoaded && chatStorage.sessions.length === 0) {
      chatStorage.createSession()
    }
  }, [chatStorage.isLoaded, chatStorage.sessions.length, chatStorage])

  useEffect(() => {
    const syncSidebarForViewport = () => {
      const isMobile = window.innerWidth <= 980
      setIsMobileViewport(isMobile)
      setIsSidebarOpen(!isMobile)
    }

    syncSidebarForViewport()
    window.addEventListener('resize', syncSidebarForViewport)
    return () => window.removeEventListener('resize', syncSidebarForViewport)
  }, [])

  const activeSession = chatStorage.getActiveSession()

  const handleSendMessage = useCallback(async (query: string) => {
    const text = query.trim()
    if (!text || loading || !activeSession) {
      return
    }

    if (activeSession.messages.length === 0) {
      const title = text.length > 52 ? `${text.slice(0, 52)}...` : text
      chatStorage.renameSession(activeSession.id, title)
    }

    const userMsg: ChatMessage = {
      id: `${Date.now()}-user`,
      type: 'user',
      originalQuery: text,
      query: text,
      timestamp: Date.now(),
    }
    chatStorage.addMessage(activeSession.id, userMsg)

    setLoading(true)
    try {
      const assistantMessages = activeSession.messages.filter((msg) => msg.type === 'assistant')
      const shouldRunFullRecommendation = assistantMessages.length === 0

      if (shouldRunFullRecommendation) {
        const res = await recommend(text, 'Overall (Top 1000)')
        const assistantMsg: ChatMessage = {
          id: `${Date.now()}-assistant`,
          type: 'assistant',
          response: res,
          timestamp: Date.now(),
          tokens: res.tokens_used,
        }
        chatStorage.addMessage(activeSession.id, assistantMsg)
      } else {
        const history = activeSession.messages.slice(-10).map((msg) => ({
          role: msg.type,
          content: msg.type === 'user'
            ? (msg.originalQuery || msg.query || '')
            : (msg.response?.report || ''),
        }))
        const firstAssistant = assistantMessages[0]
        const topRecommendations = (firstAssistant?.response?.recommendations || [])
          .slice(0, 3)
          .map((item: any) => item?.name)
          .filter(Boolean)

        const follow = await chatFollowup({
          message: text,
          history,
          initial_report: firstAssistant?.response?.report || '',
          top_recommendations: topRecommendations,
        })

        const assistantMsg: ChatMessage = {
          id: `${Date.now()}-assistant`,
          type: 'assistant',
          response: {
            recommendations: [],
            report: follow.reply,
            tokens_used: follow.tokens_used || 0,
          },
          timestamp: Date.now(),
          tokens: follow.tokens_used || 0,
        }
        chatStorage.addMessage(activeSession.id, assistantMsg)
      }
    } catch {
      const assistantMsg: ChatMessage = {
        id: `${Date.now()}-assistant-error`,
        type: 'assistant',
        response: {
          recommendations: [],
          report: 'I could not reach the follow-up chat endpoint. Please try again in a moment.',
          tokens_used: 0,
        },
        timestamp: Date.now(),
      }
      chatStorage.addMessage(activeSession.id, assistantMsg)
    } finally {
      setLoading(false)
    }
  }, [activeSession, chatStorage, loading])

  const handleSelectSession = useCallback((sessionId: string) => {
    chatStorage.setActiveSessionId(sessionId)
    if (window.innerWidth <= 980) {
      setIsSidebarOpen(false)
    }
  }, [chatStorage])

  const handleCreateSession = useCallback(() => {
    chatStorage.createSession()
    if (window.innerWidth <= 980) {
      setIsSidebarOpen(false)
    }
  }, [chatStorage])

  return (
    <div className={`app-shell ${!isMobileViewport && !isSidebarOpen ? 'sidebar-collapsed' : ''}`}>
      <aside className={`app-sidebar ${isSidebarOpen ? 'is-open' : ''}`}>
        <ChatHistory
          sessions={chatStorage.sessions}
          activeSessionId={chatStorage.activeSessionId}
          onSelectSession={handleSelectSession}
          onCreateNewSession={handleCreateSession}
        />
      </aside>

      <main className="app-main">
        <nav className="top-nav">
          <div className="nav-brand">
            <button
              className="icon-button sidebar-toggle"
              onClick={() => setIsSidebarOpen((current) => !current)}
              aria-label={isSidebarOpen ? 'Close session history' : 'Open session history'}
              title={isSidebarOpen ? 'Close history' : 'Open history'}
            >
              {isSidebarOpen ? <X size={16} /> : <Menu size={16} />}
            </button>
            <button type="button" className="home-link-brand" onClick={() => navigateTo('/')}>
              <div className="brand-mark">
                <Sparkles size={18} />
              </div>
              <div>
                <div className="brand-title">Met-Quest Material Assistant</div>
                <div className="brand-subtitle">Tell your use-case, constraints, and manufacturing process.</div>
              </div>
            </button>
          </div>

          <div className="top-nav-actions">
            <button type="button" className="btn-new-chat btn-ghost" onClick={() => navigateTo('/')}>
              Home
            </button>
            <button className="btn-new-chat" onClick={handleCreateSession} title="Start a new chat">
              <Plus size={14} /> New Chat
            </button>
          </div>
        </nav>

        <section className="app-content">
          <div className={`nav-status nav-status-${apiStatus}`}>
            <Circle size={10} />
            API {apiStatus === 'checking' ? 'Checking' : apiStatus === 'online' ? 'Ready' : 'Unavailable'}
          </div>
          {activeSession && (
            <div className="chat-column chat-column--full">
              <ChatPanel
                messages={activeSession.messages}
                onSendMessage={handleSendMessage}
                loading={loading}
              />
            </div>
          )}
        </section>
      </main>

      {isMobileViewport && isSidebarOpen && (
        <button
          className="sidebar-backdrop"
          onClick={() => setIsSidebarOpen(false)}
          aria-label="Close session history panel"
        />
      )}
    </div>
  )
}

const App: React.FC = () => {
  const [pathname, setPathname] = useState(getPathname())

  useEffect(() => {
    const handlePopState = () => setPathname(getPathname())
    window.addEventListener('popstate', handlePopState)
    return () => window.removeEventListener('popstate', handlePopState)
  }, [])

  if (pathname === CHAT_ROUTE) {
    return <ChatWorkspace />
  }

  return <HomePage onStartChat={() => navigateTo(CHAT_ROUTE)} />
}

export default App
