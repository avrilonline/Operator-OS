# Operator OS Architecture

Operator OS is designed as an ultra-lightweight, Go-native agent framework targeting resource-constrained environments (edge devices, older mobile hardware, cheap SBCs). This document outlines the core structural components and data flow of the system.

## 1. High-Level Overview

At its core, Operator OS is a headless daemon (the "Gateway") combined with a CLI. It connects users (via Chat Apps or CLI) to AI Providers (OpenAI, Anthropic, Gemini, etc.) while maintaining state, memory, and scheduled background tasks.

```text
┌──────────────┐     ┌────────────────────────────────┐     ┌──────────────┐
│              │     │                                │     │              │
│ Chat Clients ├────►│       Operator Gateway         ├────►│ AI Providers │
│ (Telegram,   │◄────┤  (HTTP Server / WebSockets)    │◄────┤ (LLMs, TTS)  │
│ Discord, etc)│     │                                │     │              │
└──────────────┘     └──────────────┬─────────────────┘     └──────────────┘
                                    │
                                    ▼
                     ┌────────────────────────────────┐
                     │                                │
                     │      Local Environment         │
                     │  (Workspace, Files, Skills,    │
                     │   Cron Jobs, Exec Shell)       │
                     │                                │
                     └────────────────────────────────┘
```

## 2. Core Subsystems

### The Agent Loop (`pkg/agent`)
The primary execution cycle. It handles context assembly, tool extraction from the LLM response, and sequential tool execution. The agent utilizes "continuous reasoning"—meaning it can think, parse errors, and automatically loop back for corrections before generating a final response.

### Provider Abstraction (`pkg/providers`)
Operator utilizes a protocol-based abstraction model rather than hardcoding specific APIs. 
- **OpenAI-Compatible**: Routes requests for OpenAI, DeepSeek, Zhipu, Groq, Ollama, and OpenRouter through a unified schema.
- **Anthropic Protocol**: Handles Claude's distinct tool-use syntax and message arrays.
- **Custom Adapters**: Native integration for specific edge-cases like Google's Antigravity (Cloud Code) or GitHub Copilot via gRPC.

### Channels & Routing (`pkg/channels` & `pkg/routing`)
Handles I/O across various networks.
- **Push (Webhooks)**: LINE, WeCom App, Slack (hybrid).
- **Pull (Long-polling / WebSocket)**: Telegram, Discord, OneBot, Feishu.
- **Routing**: Messages are normalized into a unified schema, tagged with standard `SessionKey`s, and pushed onto the internal event bus.

### Tools & Sandboxing (`pkg/tools`)
Tools grant the agent agency over the host.
- **Filesystem**: Read/Write/Edit files.
- **Shell (`exec`)**: Background process management and CLI execution.
- **Security Boundary**: Controlled via `restrict_to_workspace`. When enabled, tools strictly reject path traversal (`../`) and block destructive commands (`rm -rf`, disk wipes).

### Memory & State (`pkg/session` & `workspace/`)
Memory is structural, stored as plain text. 
- **Ephemeral Context**: Current chat session history.
- **Long-term Knowledge**: Stored in `MEMORY.md`. The agent actively edits this file to build continuity across reboots.

### Heartbeat & Cron (`pkg/heartbeat` & `pkg/cron`)
Allows the agent to act autonomously without user triggers.
- **Heartbeat**: Evaluates `HEARTBEAT.md` every X minutes, spawning isolated sub-agents to check emails, weather, or APIs.
- **Cron**: Explicitly scheduled automated jobs.

## 3. Execution Lifecycle

1. **Ingestion**: A message arrives via `pkg/channels`.
2. **Normalization**: The channel adapter maps it to an internal `Message` struct.
3. **Session Assembly**: `pkg/session` fetches the history and prepends standard system prompts (`SOUL.md`, `IDENTITY.md`, `USER.md`).
4. **Inference**: The compiled context is dispatched to `pkg/providers`.
5. **Tool Execution**: If the LLM returns tool calls, `pkg/agent` pauses, executes the tool functions via `pkg/tools`, and appends the result to the context.
6. **Delivery**: The final text response is routed back to the originating channel adapter.

## 4. Sub-agent Orchestration
Operator OS handles asynchronous or heavy tasks by spawning sub-agents. This allows the primary conversational loop to remain unblocked. Sub-agents run with independent context and can communicate back to the user using the native `message` tool once their task completes.
