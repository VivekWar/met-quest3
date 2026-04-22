import React from 'react'
import { ArrowRight, Bot, BrainCircuit, Database, MessagesSquare, Orbit, ShieldCheck } from 'lucide-react'

interface HomePageProps {
  onStartChat: () => void
}

const showcaseCards = [
  {
    title: 'Thermal constraints',
    copy: 'Ask about heat, cycle time, enclosure limits, or service temperature in one sentence.',
    image:
      'https://images.unsplash.com/photo-1485827404703-89b55fcc595e?auto=format&fit=crop&w=1200&q=80',
  },
  {
    title: 'Manufacturing reality',
    copy: 'Keep desktop FDM, machining, or material feasibility grounded in process limits.',
    image:
      'https://images.unsplash.com/photo-1565043589221-1a6fd9ae45c7?auto=format&fit=crop&w=1200&q=80',
  },
  {
    title: 'Follow-up chat',
    copy: 'Move from recommendation to explanation without resetting the original context.',
    image:
      'https://images.unsplash.com/photo-1581092921461-eab62e97a780?auto=format&fit=crop&w=1200&q=80',
  },
]

export const HomePage: React.FC<HomePageProps> = ({ onStartChat }) => {
  return (
    <div className="home-page">
      <header className="home-nav">
        <div className="home-brand">
          <div className="home-brand-mark">
            <Orbit size={18} />
          </div>
          <div>
            <div className="home-brand-title">Met-Quest</div>
            <div className="home-brand-subtitle">Material decisions with engineering context</div>
          </div>
        </div>

        <button type="button" className="home-cta home-cta-secondary" onClick={onStartChat}>
          Open chat
        </button>
      </header>

      <main className="home-main">
        <section className="home-hero">
          <div className="home-hero-copy">
            <div className="home-kicker">Engineering assistant</div>
            <h1>Pick materials with context, not guesswork.</h1>
            <p>
              Ask once, get a grounded first answer, then keep the conversation going without losing the original constraints.
            </p>

            <div className="home-hero-actions">
              <button type="button" className="home-cta" onClick={onStartChat}>
                Start in chat <ArrowRight size={16} />
              </button>
            </div>

            <div className="home-signal-row">
              <div className="home-signal">
                <BrainCircuit size={16} />
                First-pass recommendation
              </div>
              <div className="home-signal">
                <MessagesSquare size={16} />
                Context-aware follow-ups
              </div>
              <div className="home-signal">
                <ShieldCheck size={16} />
                Process-aware reasoning
              </div>
            </div>
          </div>

          <div className="home-hero-visual" aria-hidden="true">
            <div className="home-visual-backdrop" />
            <img
              src="https://images.unsplash.com/photo-1517048676732-d65bc937f952?auto=format&fit=crop&w=1600&q=80"
              alt=""
            />
            <div className="home-visual-panel home-visual-panel-primary">
              <span>Ask</span>
              service temperature, printer limits, fatigue, stiffness, corrosion
            </div>
            <div className="home-visual-panel home-visual-panel-secondary">
              <Bot size={16} />
              grounded answer, then normal conversation
            </div>
          </div>
        </section>

        <section className="home-showcase">
          {showcaseCards.map((card) => (
            <article key={card.title} className="home-showcase-item">
              <img src={card.image} alt="" />
              <div className="home-showcase-copy">
                <h2>{card.title}</h2>
                <p>{card.copy}</p>
              </div>
            </article>
          ))}
        </section>

        <section className="home-info-band">
          <div className="home-info-text">
            <div className="home-kicker">How it works</div>
            <h2>One good first answer. Better follow-ups after that.</h2>
            <p>
              The first turn runs the full material recommendation flow. Later turns stay inside the same context, so you can ask why one option lost, what changed the trade-off, or what to validate next.
            </p>
          </div>

          <div className="home-info-points">
            <div className="home-point">
              <Database size={18} />
              Catalog-backed recommendation
            </div>
            <div className="home-point">
              <ShieldCheck size={18} />
              Constraints stay attached to the thread
            </div>
            <div className="home-point">
              <MessagesSquare size={18} />
              Follow-ups stay conversational
            </div>
          </div>
        </section>
      </main>
    </div>
  )
}
