# Tech Titans Submission Update - MET-QUEST '26

## Project Status

The Smart Alloy Selector is now updated with chat storage, constraint refinement, humanized physics responses, LaTeX-ready notation, and production-oriented validation.

## What Was Added

- Chat history persistence using `localStorage`.
- Session-based chat browsing with rename, delete, and clear-all actions.
- Constraint refinement UI with a `+` button to attach extra requirements to the same conversation.
- Re-query flow that sends the current chat constraints back to the dispatcher.
- More humanized recommendation summaries with clearer engineering language.
- Improved LaTeX / math notation support in the response report.
- Stronger feasibility guardrails for impossible desktop-FDM scenarios.

## Deployment Readiness

- Backend build succeeds.
- Frontend build succeeds.
- Dispatcher validation suite exists for 10 core scenarios.
- Production docs have been updated in the README and deployment guide.

## Notes for Judges

- The system is designed to behave like a virtual materials scientist.
- Recommendations are now easier to read, easier to iterate on, and easier to compare across follow-up chats.
- Constraints can be added progressively instead of rewriting the full query every time.

## Files of Interest

- [README.md](README.md)
- [DEPLOYMENT.md](DEPLOYMENT.md)
- [frontend/src/App.tsx](frontend/src/App.tsx)
- [frontend/src/components/ChatPanel.tsx](frontend/src/components/ChatPanel.tsx)
- [frontend/src/components/ChatHistory.tsx](frontend/src/components/ChatHistory.tsx)
- [frontend/src/hooks/useChatStorage.ts](frontend/src/hooks/useChatStorage.ts)
- [backend/handlers/recommend.go](backend/handlers/recommend.go)
- [backend/models/material.go](backend/models/material.go)
- [backend/services/llm.go](backend/services/llm.go)

## Status

Ready for final submission packaging.
