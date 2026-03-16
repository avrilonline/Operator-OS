# Configuration Reference

Operator OS is configured through a JSON file at `~/.operator/config.json` and optional environment variables in `.env`.

## Config File Location

By default, the config is loaded from `~/.operator/config.json`. Copy the example to get started:

```bash
mkdir -p ~/.operator
cp config/config.example.json ~/.operator/config.json
```

---

## agents

Agent defaults and behavior settings.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `agents.defaults.workspace` | string | `~/.operator/workspace` | Directory for agent file operations |
| `agents.defaults.restrict_to_workspace` | bool | `true` | Confine file access to workspace directory |
| `agents.defaults.model_name` | string | `"gpt4"` | Default model name from `model_list` |
| `agents.defaults.max_tokens` | int | `8192` | Maximum tokens per response |
| `agents.defaults.temperature` | float | `0.7` | Sampling temperature (0.0–2.0) |
| `agents.defaults.max_tool_iterations` | int | `20` | Maximum tool call iterations per turn |

---

## model_list

Array of model configurations. Each entry defines an LLM provider connection.

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `model_name` | string | Yes | Friendly name to reference this model |
| `model` | string | Yes | Provider-prefixed model ID (e.g., `openai/gpt-5.2`) |
| `api_key` | string | Yes* | API key for the provider (*not required for Ollama) |
| `api_base` | string | No | Custom API base URL (overrides provider default) |
| `auth_method` | string | No | Authentication method (e.g., `oauth` for Antigravity) |

### Provider Prefixes

| Provider | Prefix | Example |
|----------|--------|---------|
| Anthropic | `anthropic/` | `anthropic/claude-sonnet-4.6` |
| OpenAI | `openai/` | `openai/gpt-5.2` |
| Google Gemini | `gemini/` | `gemini/gemini-3.1-pro` |
| Groq | `groq/` | `groq/llama3-8b-8192` |
| DeepSeek | `deepseek/` | `deepseek/deepseek-chat` |
| Ollama (Local) | `ollama/` | `ollama/llama3` |
| Antigravity | `antigravity/` | `antigravity/gemini-2.0-flash` |

### Load Balancing

Define multiple entries with the same `model_name` but different `api_key` / `api_base` values. Requests are distributed across matching entries automatically.

```json
{
  "model_list": [
    { "model_name": "gpt4", "model": "openai/gpt-5.2", "api_key": "sk-key1", "api_base": "https://api1.example.com/v1" },
    { "model_name": "gpt4", "model": "openai/gpt-5.2", "api_key": "sk-key2", "api_base": "https://api2.example.com/v1" }
  ]
}
```

---

## channels

Messaging channel configurations. Each channel has `enabled` (bool) and `allow_from` (string array of user IDs) fields.

### Common Fields

| Key | Type | Description |
|-----|------|-------------|
| `enabled` | bool | Enable/disable this channel |
| `allow_from` | string[] | Allowlist of user/chat IDs (empty = allow all) |
| `reasoning_channel_id` | string | Channel/thread to post reasoning traces |

### telegram

| Key | Type | Description |
|-----|------|-------------|
| `token` | string | Bot token from @BotFather |
| `base_url` | string | Custom Telegram API base URL |
| `proxy` | string | HTTP proxy for API requests |

### discord

| Key | Type | Description |
|-----|------|-------------|
| `token` | string | Discord bot token |
| `proxy` | string | HTTP proxy |
| `group_trigger.mention_only` | bool | Only respond when @mentioned in groups |

### slack

| Key | Type | Description |
|-----|------|-------------|
| `bot_token` | string | Bot user OAuth token (`xoxb-...`) |
| `app_token` | string | App-level token for Socket Mode (`xapp-...`) |

### whatsapp

| Key | Type | Description |
|-----|------|-------------|
| `bridge_url` | string | WebSocket URL for WhatsApp bridge |
| `use_native` | bool | Use native whatsmeow (requires `whatsapp_native` build tag) |
| `session_store_path` | string | Path for session persistence |

### line

| Key | Type | Description |
|-----|------|-------------|
| `channel_secret` | string | LINE channel secret |
| `channel_access_token` | string | LINE channel access token |
| `webhook_path` | string | Webhook endpoint path (default: `/webhook/line`) |

### dingtalk

| Key | Type | Description |
|-----|------|-------------|
| `client_id` | string | DingTalk app client ID |
| `client_secret` | string | DingTalk app client secret |

### feishu

| Key | Type | Description |
|-----|------|-------------|
| `app_id` | string | Feishu/Lark app ID |
| `app_secret` | string | Feishu/Lark app secret |
| `encrypt_key` | string | Event encryption key |
| `verification_token` | string | Event verification token |

### qq

| Key | Type | Description |
|-----|------|-------------|
| `app_id` | string | QQ bot app ID |
| `app_secret` | string | QQ bot app secret |

### onebot

| Key | Type | Description |
|-----|------|-------------|
| `ws_url` | string | WebSocket URL (e.g., `ws://127.0.0.1:3001`) |
| `access_token` | string | OneBot access token |
| `reconnect_interval` | int | Reconnect interval in seconds |
| `group_trigger_prefix` | string[] | Prefixes that trigger the bot in groups |

### wecom / wecom_app / wecom_aibot

| Key | Type | Applies To | Description |
|-----|------|------------|-------------|
| `token` | string | all | Verification token |
| `encoding_aes_key` | string | all | 43-character AES key |
| `webhook_url` | string | wecom | Outgoing webhook URL |
| `webhook_path` | string | all | Incoming webhook endpoint |
| `corp_id` | string | wecom_app | Enterprise corp ID |
| `corp_secret` | string | wecom_app | App secret |
| `agent_id` | int | wecom_app | App agent ID |
| `reply_timeout` | int | wecom, wecom_app | Reply timeout in seconds |
| `max_steps` | int | wecom_aibot | Max conversation steps |
| `welcome_message` | string | wecom_aibot | Initial greeting |

### maixcam

| Key | Type | Description |
|-----|------|-------------|
| `host` | string | Bind host (default: `0.0.0.0`) |
| `port` | int | Bind port (default: `18790`) |

---

## tools

### tools.web

Web search providers.

| Key | Type | Description |
|-----|------|-------------|
| `brave.enabled` | bool | Enable Brave Search |
| `brave.api_key` | string | Brave Search API key |
| `brave.max_results` | int | Max results per search |
| `duckduckgo.enabled` | bool | Enable DuckDuckGo (no API key needed) |
| `duckduckgo.max_results` | int | Max results per search |
| `perplexity.enabled` | bool | Enable Perplexity |
| `perplexity.api_key` | string | Perplexity API key |
| `perplexity.max_results` | int | Max results per search |
| `proxy` | string | HTTP proxy for all web searches |

### tools.cron

| Key | Type | Description |
|-----|------|-------------|
| `exec_timeout_minutes` | int | Max execution time for cron jobs |

### tools.exec

| Key | Type | Description |
|-----|------|-------------|
| `enable_deny_patterns` | bool | Enable command deny list |
| `custom_deny_patterns` | string[] | Regex patterns for denied commands |

### tools.mcp

Model Context Protocol server configurations.

| Key | Type | Description |
|-----|------|-------------|
| `enabled` | bool | Enable MCP globally |
| `servers.<name>.enabled` | bool | Enable this specific server |
| `servers.<name>.type` | string | Connection type: `http` or `stdio` |
| `servers.<name>.url` | string | Server URL (for `http` type) |
| `servers.<name>.command` | string | Command to run (for `stdio` type) |
| `servers.<name>.args` | string[] | Command arguments |
| `servers.<name>.env` | object | Environment variables for the server |
| `servers.<name>.headers` | object | HTTP headers (for `http` type) |

### tools.skills

| Key | Type | Description |
|-----|------|-------------|
| `registries.<name>.enabled` | bool | Enable this skill registry |
| `registries.<name>.base_url` | string | Registry API base URL |
| `registries.<name>.search_path` | string | Search endpoint path |
| `registries.<name>.skills_path` | string | Skills listing endpoint |
| `registries.<name>.download_path` | string | Download endpoint path |

---

## heartbeat

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enabled` | bool | `true` | Enable heartbeat health checks |
| `interval` | int | `30` | Heartbeat interval in seconds |

## devices

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enabled` | bool | `false` | Enable device monitoring |
| `monitor_usb` | bool | `true` | Monitor USB device events |

## gateway

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `host` | string | `"127.0.0.1"` | Gateway bind address |
| `port` | int | `18790` | Gateway bind port |

---

## Environment Variables

Set these in a `.env` file in the project root or export them in your shell. Environment variables override config file values for API keys.

| Variable | Description |
|----------|-------------|
| `OPENROUTER_API_KEY` | OpenRouter API key |
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `OPENAI_API_KEY` | OpenAI API key |
| `GEMINI_API_KEY` | Google Gemini API key |
| `CEREBRAS_API_KEY` | Cerebras API key |
| `ZHIPU_API_KEY` | Zhipu API key |
| `TELEGRAM_BOT_TOKEN` | Telegram bot token |
| `DISCORD_BOT_TOKEN` | Discord bot token |
| `LINE_CHANNEL_SECRET` | LINE channel secret |
| `LINE_CHANNEL_ACCESS_TOKEN` | LINE channel access token |
| `BRAVE_SEARCH_API_KEY` | Brave Search API key |
| `TZ` | Timezone (default: `UTC`) |
