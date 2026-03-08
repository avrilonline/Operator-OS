# Operator OS — Chat UI Workstream

**Created:** 2026-03-08
**Status:** Planning
**Branch:** `feat/chat-ui` (based on `operatoros-production-readiness`)
**Location:** `/var/www/prototypes/os-go` → `os-go.operator.onl`
**Target:** Production-ready chat interface for Operator OS platform

---

## Overview

Evolve the existing chat interface into a full production platform client. The current `web/index.html` (1568 lines) already provides a functional foundation — dark/light OKLCH theming, chat bubbles with markdown rendering, WebSocket transport, and a monitor panel. This workstream migrates it from the legacy Pico protocol to the production API, adds authentication, and surfaces all platform features (billing, integrations, admin).

### What Already Exists (`web/index.html`)
- ✅ Chat message UI (user/agent/system bubbles, animations)
- ✅ Markdown rendering (marked.js + DOMPurify)
- ✅ Code blocks with syntax styling
- ✅ Dark/light theme with full OKLCH token system
- ✅ WebSocket transport (currently `/pico/ws` with hardcoded token)
- ✅ Input composer with send button
- ✅ Monitor panel (connection status, health, browser iframe)
- ✅ Responsive layout with pill navigation
- ✅ DM Sans + JetBrains Mono typography
- ✅ Phosphor Icons

### What Needs to Change
- ❌ Hardcoded Pico token → JWT auth (login/register flows)
- ❌ `/pico/ws` protocol → production `/api/v1/ws` with JWT handshake
- ❌ Single-file monolith → modular structure (can stay vanilla JS or migrate to React)
- ❌ No session management → multi-session with history
- ❌ No agent selection → agent CRUD and switching
- ❌ No platform features → billing, integrations, admin panels
- ❌ No error handling → proper error states, reconnect UI, empty states

**Stack decision:** Start by refactoring the existing vanilla JS into modules. Migrate to React + TypeScript + Vite only if complexity demands it (likely at Phase 3–4).
**Styling:** Keep existing OKLCH system — it's already well-designed.
**Real-time:** Migrate WebSocket from Pico protocol to production API.
**Auth:** JWT (login/register/verify flows already built in backend).
**Deployment:** Caddy at `os-go.operator.onl` (existing)

---

## Architecture

```
┌─────────────────────────────────────────────────┐
│                   Chat UI (SPA)                 │
│                                                 │
│  ┌──────────┐ ┌──────────┐ ┌────────────────┐  │
│  │  Auth     │ │  Chat    │ │  Dashboard     │  │
│  │  Module   │ │  Module  │ │  Module        │  │
│  └──────────┘ └──────────┘ └────────────────┘  │
│  ┌──────────┐ ┌──────────┐ ┌────────────────┐  │
│  │  Agents  │ │  Billing │ │  Integrations  │  │
│  │  Module  │ │  Module  │ │  Module        │  │
│  └──────────┘ └──────────┘ └────────────────┘  │
│  ┌──────────┐ ┌──────────┐                      │
│  │  Admin   │ │  Settings│                      │
│  │  Module  │ │  Module  │                      │
│  └──────────┘ └──────────┘                      │
└───────────────────┬─────────────────────────────┘
                    │ HTTPS + WSS
┌───────────────────▼─────────────────────────────┐
│              Operator OS Gateway                │
│         (Go API — already built)                │
│                                                 │
│  60+ REST endpoints across 15 API groups        │
│  JWT auth · Stripe billing · OAuth integrations │
└─────────────────────────────────────────────────┘
```

---

## Phase Overview

| # | Phase | Description | Tasks | Target |
|---|---|---|---|---|
| 1 | Foundation | Project scaffold, auth, routing, API client | C1–C5 | Week 1–2 |
| 2 | Chat Core | Real-time messaging, markdown, streaming | C6–C10 | Week 3–4 |
| 3 | Agent & Session Management | Multi-agent, sessions, history | C11–C14 | Week 5–6 |
| 4 | Platform Features | Billing, integrations, usage dashboard | C15–C19 | Week 7–8 |
| 5 | Admin & Settings | Admin panel, user management, security audit | C20–C23 | Week 9–10 |
| 6 | Polish & Launch | Mobile responsive, a11y, performance, deploy | C24–C28 | Week 11–12 |

---

## Phase 1: Foundation

| ID | Task | Priority | Status | Description |
|---|---|---|---|---|
| C1 | Modularize existing UI | P0 | ⬜ TODO | Extract `web/index.html` into modular structure: `web/{index.html, css/theme.css, js/api.js, js/chat.js, js/auth.js, js/ws.js, js/ui.js}`. Keep vanilla JS for now. Preserve all existing styling and functionality. No regressions. |
| C2 | API client layer | P0 | ⬜ TODO | `web/js/api.js` — fetch wrapper for all backend endpoints. Auto-attach JWT from localStorage. Token refresh interceptor (`POST /auth/refresh`). Error normalization. Base URL detection from `location.origin`. |
| C3 | Auth flows | P0 | ⬜ TODO | Login and register screens (replace the current direct-connect). JWT storage in localStorage. Auth state management. Redirect to login when 401. Email verification flow. Calls: `POST /auth/register`, `POST /auth/login`, `POST /auth/verify-email`, `POST /auth/resend-verification`, `POST /auth/refresh`. |
| C4 | App shell & navigation | P0 | ⬜ TODO | Extend the existing pill nav (Chat/Monitor) with new panels: Agents, Settings. Add top bar with user info + logout. Maintain the current responsive layout. Hash-based routing (`#chat`, `#agents`, `#settings`, `#admin`). |
| C5 | Theme system | P1 | ✅ EXISTS | Already implemented — full OKLCH dark/light tokens, system preference detection, localStorage persistence. Only needs: theme toggle button in the new top bar. |

---

## Phase 2: Chat Core

| ID | Task | Priority | Status | Description |
|---|---|---|---|---|
| C6 | WebSocket migration | P0 | ⬜ TODO | Backend: add `/api/v1/ws` endpoint with JWT auth on handshake (token as query param or first message). Frontend: migrate from `/pico/ws?token=HARDCODED` to `/api/v1/ws?token=JWT`. Keep existing reconnect logic, update protocol to match production message format. |
| C7 | Message thread UI | P1 | ✅ EXISTS | Already implemented — user/agent/system bubbles, auto-scroll, animations. Needs: scroll-to-bottom button, timestamps on messages, loading skeleton for history fetch. |
| C8 | Markdown & code rendering | P1 | ✅ EXISTS | Already implemented — marked.js + DOMPurify, code blocks, blockquotes, lists. Needs: copy button on code blocks, language label, syntax highlighting (Prism.js or Highlight.js). |
| C9 | Streaming responses | P0 | ⬜ TODO | Adapt existing WebSocket message handler for token-by-token streaming from the new API. Typing indicator animation. Cancel generation button. Partial markdown rendering during stream (re-render on each chunk). |
| C10 | Input composer upgrades | P1 | ⬜ TODO | Existing send button works. Add: multi-line with auto-resize, file/image upload (drag-and-drop + paste), Shift+Enter for newline, model selector dropdown (fetched from agent config). |

---

## Phase 3: Agent & Session Management

| ID | Task | Priority | Status | Description |
|---|---|---|---|---|
| C11 | Agent CRUD | P0 | ⬜ TODO | List agents, create/edit/delete. Agent card showing name, model, system prompt preview, status, integration scopes. Set default agent. Calls: `GET/POST /api/v1/agents`, `GET/PUT/DELETE /api/v1/agents/{id}`, `POST /api/v1/agents/{id}/default`. |
| C12 | Multi-session UI | P0 | ⬜ TODO | Session sidebar: list active sessions, create new, rename, delete. Session = conversation thread tied to an agent. Switch between sessions without losing state. Session metadata (created, message count, last active). |
| C13 | Conversation history | P1 | ⬜ TODO | Search across sessions. Filter by agent, date range. Export conversation as markdown/JSON. Pin important conversations. Archive old sessions. |
| C14 | Agent integration scopes | P1 | ⬜ TODO | Per-agent integration permission editor. Visual scope selector showing available integrations, tools, and OAuth scopes. Calls: `AllowedIntegrations` field on agent create/update. |

---

## Phase 4: Platform Features

| ID | Task | Priority | Status | Description |
|---|---|---|---|---|
| C15 | Billing & plans | P0 | ⬜ TODO | Plan comparison page (Free/Starter/Pro/Enterprise). Current plan badge. Upgrade/downgrade with proration preview. Stripe Checkout redirect. Billing portal link. Calls: `GET /api/v1/billing/plans`, `POST /billing/checkout`, `POST /billing/portal`, `GET /billing/subscription`, `POST /billing/change-plan`, `POST /billing/preview-change`. |
| C16 | Usage dashboard | P0 | ⬜ TODO | Token usage charts (daily, by model). Current period summary. Usage vs plan limits with progress bars. Overage warnings. Calls: `GET /billing/usage`, `GET /billing/usage/daily`, `GET /billing/usage/models`, `GET /billing/usage/limits`, `GET /billing/overage`. |
| C17 | Integration marketplace | P1 | ⬜ TODO | Browse available integrations by category. Connect/disconnect OAuth integrations (Google, Shopify). API key integrations. Status indicators (active/failed/revoked). Token health display. Calls: `GET /integrations`, `GET /integrations/categories`, `POST /manage/integrations/connect`, `POST /manage/integrations/disconnect`, `GET /manage/integrations/status`. |
| C18 | OAuth connect flow | P1 | ⬜ TODO | In-app OAuth popup/redirect for Google, Shopify. Callback handling. Scope consent display. Reconnect for expired/revoked tokens. Calls: `POST /oauth/authorize`, `GET /oauth/callback`. |
| C19 | Rate limit display | P2 | ⬜ TODO | Show current rate limit status from `X-RateLimit-*` response headers. Visual indicator when approaching limits. Calls: `GET /api/v1/rate-limit/status`. |

---

## Phase 5: Admin & Settings

| ID | Task | Priority | Status | Description |
|---|---|---|---|---|
| C20 | Admin panel | P1 | ⬜ TODO | User list with search/filter. Suspend/activate/delete users. Role management (user/admin). Platform stats dashboard. Requires admin role. Calls: `GET/PUT/DELETE /admin/users/*`, `POST /admin/users/{id}/suspend`, `POST /admin/users/{id}/activate`, `POST /admin/users/{id}/role`, `GET /admin/stats`. |
| C21 | Audit log viewer | P1 | ⬜ TODO | Filterable event log (by user, action, time range). Action categories (auth, agent, billing, admin). Expandable detail rows. CSV export. Calls: `GET /admin/audit`, `GET /admin/audit/count`. |
| C22 | Security audit dashboard | P2 | ⬜ TODO | Run security audit from UI. Risk score visualization (gauge). Check results grouped by category with pass/fail/warning. Remediation guidance. CWE/OWASP references. Calls: `GET /admin/security-audit`. |
| C23 | User settings | P1 | ⬜ TODO | Profile (email, password change). Theme preference. Notification settings. GDPR: data export request, account deletion request. API key management. Calls: `POST /gdpr/export`, `POST /gdpr/erase`, `GET /gdpr/requests`. |

---

## Phase 6: Polish & Launch

| ID | Task | Priority | Status | Description |
|---|---|---|---|---|
| C24 | Mobile responsive | P1 | ✅ PARTIAL | Existing layout is already mobile-friendly with pill nav. Needs: slide-over panels for settings/agents, touch-friendly composer, iOS Safari safe area handling, test on real devices. |
| C25 | Accessibility | P1 | ⬜ TODO | WCAG 2.1 AA compliance. Keyboard navigation throughout. Screen reader landmarks and ARIA labels. Focus management on route changes. Reduced motion support. Color contrast validation against OKLCH palette. |
| C26 | Performance | P1 | ⬜ TODO | Code splitting per route. Lazy load heavy components (markdown renderer, charts). Service worker for offline shell. Bundle analysis < 200KB initial JS. Lighthouse score > 90. Virtual scrolling for long message lists. |
| C27 | Error handling & empty states | P1 | ⬜ TODO | Global error boundary with recovery. Toast notifications for API errors. Offline detection banner. Empty states for all list views (no agents, no sessions, no integrations). Loading skeletons. |
| C28 | Production deployment | P0 | ⬜ TODO | Already served from `/var/www/prototypes/os-go/web` via `os-go.operator.onl`. Caddy proxy to gateway port already configured. Needs: Gzip/Brotli headers, asset cache headers, CSP headers. If migrated to Vite: add build step + CI. |

---

## Backend Requirements (New Endpoints Needed)

The chat UI requires a few backend additions not yet in the platform:

| ID | Endpoint | Purpose |
|---|---|---|
| B-WS | `GET /api/v1/ws` | WebSocket upgrade for real-time chat. JWT auth on handshake. Message send/receive + streaming tokens. |
| B-SESSIONS | `GET/POST/DELETE /api/v1/sessions` | Session CRUD — list user's chat sessions, create new, delete. |
| B-MESSAGES | `GET /api/v1/sessions/{id}/messages` | Paginated message history for a session. |
| B-SEND | `POST /api/v1/sessions/{id}/messages` | Send a message (triggers agent processing). |
| B-STREAM | `GET /api/v1/sessions/{id}/stream` | SSE fallback for streaming responses if WebSocket isn't available. |
| B-PROFILE | `GET/PUT /api/v1/user/profile` | Get/update current user profile. |
| B-PASSWORD | `POST /api/v1/user/password` | Change password (requires current password). |

---

## Design Principles

1. **API-first** — Every UI feature maps to an existing backend endpoint. No frontend hacks.
2. **Progressive disclosure** — Chat is front and center. Platform features (billing, integrations, admin) are one click away but never in the way.
3. **Real-time by default** — WebSocket for chat, polling fallback for dashboards. No manual refresh.
4. **Mobile-native feel** — Not a desktop app squeezed onto a phone. Touch targets, gestures, native-like transitions.
5. **Type-safe end-to-end** — OpenAPI spec → generated TypeScript types → zero runtime type mismatches.

---

## Deployment

| Environment | Domain | Branch | Path |
|---|---|---|---|
| Dev | `os-go.operator.onl` | `feat/chat-ui` | `/var/www/prototypes/os-go/web` |
| Production | `os-go.operator.onl` | `main` (after merge) | Same |

---

## Changelog

| Date | Change |
|---|---|
| 2026-03-08 | Initial plan created. 28 tasks across 6 phases + 7 backend requirements. |
| 2026-03-08 | Updated: plan now builds on existing `web/index.html` (1568-line chat UI). Marked C5/C7/C8/C24 as existing. C1 changed from scaffold to modularize. Branch: `feat/chat-ui`. Deployment: `os-go.operator.onl`. |
