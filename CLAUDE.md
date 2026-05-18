# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

ChatAPI lets AI clients (agents, chatbots) call a human operator via an OpenAI Responses-style API. The human operator receives incoming requests in a web console and replies in real time — effectively acting as the "AI" being called.

## Commands

### Backend

```bash
cd backend
uv sync          # install dependencies
uv run main.py   # start the Flask dev server (default: http://0.0.0.0:5000)
```

### Frontend

```bash
cd frontend
npm install
npm run dev      # start Vite dev server (default: http://localhost:5173)
npm run build    # production build (outputs to frontend/dist/)
npm run lint     # ESLint
```

### Environment

Copy and configure before first run:

```bash
cp backend/.env.example backend/.env
```

Required variables: `CHATAPI_USERNAME`, `CHATAPI_PASSWORD`, `CHATAPI_SESSION_SECRET`, `CHATAPI_API_KEY`.

Config is loaded from `<repo_root>/.env` and `<repo_root>/backend/.env` (both are checked; env vars already set in the shell take precedence).

## Architecture

### Request flow

The core mechanic is the **pending turn**: an external AI client POSTs to `/v1/responses` → a `PendingTurn` is registered in `PendingTurnRegistry` (in-memory, thread-safe) → the HTTP request blocks (or streams SSE) while waiting → the human operator sees the request in the web UI and replies via `POST /api/chat/send` → `PendingTurnRegistry.resolve()` unblocks the waiting request → the response is returned to the original caller.

The draft/streaming path uses `POST /api/chat/draft` to send incremental text chunks, which are forwarded as `response.output_text.delta` SSE events before the final `POST /api/chat/send`.

### Backend layers

| Layer | Path | Purpose |
|---|---|---|
| Entry point | `backend/main.py` | Reads TLS settings, calls `create_app()`, runs Flask dev server |
| App factory | `backend/app.py` | Wires all dependencies and registers route blueprints |
| Config | `backend/core/config.py` | Parses `.env` files and env vars into a frozen `Settings` dataclass |
| Auth | `backend/core/auth.py` | Session-based login; `@auth.require_auth` decorator for protected routes |
| Repository | `backend/repositories/conversations.py` | `ConversationStore` — all SQLite access (conversations, messages, config key-value) |
| Pending turns | `backend/services/pending.py` | `PendingTurnRegistry` — in-memory registry of active requests waiting for a human reply |
| SSE streaming | `backend/services/response_stream.py` | Generates OpenAI Responses-format SSE events; detects client disconnect via socket peek |
| Routes | `backend/routes/responses.py` | `/v1/responses`, `/api/chat/send`, `/api/chat/draft`, stream heartbeat config |
| Routes | `backend/routes/conversations.py` | CRUD for conversations; prune endpoint |
| Routes | `backend/routes/auth.py` | Login / logout / session check |

### Database (SQLite)

Three tables: `conversations`, `messages`, `config`. Schema is created on startup via `ConversationStore._init_db()`; `_ensure_column` handles additive migrations. WAL mode is enabled.

### Frontend

React 19 + TypeScript + Vite + Ant Design. Single-page app — all state lives in the `useChatWorkspace` hook (`frontend/src/hooks/`). `App.tsx` renders a resizable sidebar (`ConversationSidebar`) and main chat area (`ChatPane`). On mobile, the sidebar becomes a `Drawer`.

Frontend calls backend at `/api/*` and `/v1/*`. The Vite dev proxy is configured in `frontend/vite.config.ts`.

### Key design details

- `PendingTurnRegistry` uses a single lock and two maps (`request_id → PendingTurn`, `conversation_id → request_id`) — one pending turn per conversation at a time (409 if a second request arrives while one is in flight).
- Client disconnect is detected by peeking the werkzeug socket (`MSG_PEEK`) — if `recv` returns `b""` the connection is gone and the pending turn is discarded with `realtime_status: aborted`.
- The `heartbeat` feature sends periodic filler text deltas while the human is composing, to keep SSE connections alive through proxies with short timeouts. Configured via `POST /api/config/stream-heartbeat` and persisted in the `config` table.
- Response mode can be `assistant_message` (plain text) or `tool_call` (returns a `function_call` output item). The frontend composer switches between these modes.
