# Groups & Model Discovery

### 📁 Group Management

Groups aggregate multiple channels into a unified external model name.

**Core Concepts:**

- **Group name** is the model name exposed by the program
- When calling the API, set the `model` parameter to the group name
- **First Token Timeout**: unit in seconds, only effective for streaming responses, `0` means no limit
- **Session Keep Time**: unit in seconds, keeps using the same channel for the same API key + model within the configured session window, `0` means disabled
- **Condition (JSON)**: optional AND rules currently evaluated in the main LLM relay path; the built-in request context currently includes `model`, `api_key_id`, and `hour`
- **Endpoint Provider**: provider-aware request rewriting that adapts requests for upstream compatibility per endpoint type. Chat providers (`openai`, `deepseek`, `mimo`, `siliconflow`, `newapi`) strip incompatible reasoning fields; music providers (`newapi`, `minimax`) rewrite the request body and path; video provider (`agnes`) rewrites the upstream path; audio speech provider (`mimo`) converts the request format and path
- **Outbound Format**: controls cross-format adapter fallback. `""` (auto), `chat`, and `responses` set the Chat/Responses attempt order; `chat_only` and `responses_only` disable the fallback entirely — useful for upstreams (e.g. public-welfare relays) that reject the other format with 400/404
- **Key Cooldown**: rate-limit cooldown is tracked per `(keyID, model)` (issue #94), so a single model's 429 no longer blocks the same key's other models. Per-channel retry count can be set to `0` (try once, then move to the next channel); the max-total-attempts quota counts only real upstream forwards (cooldown/circuit-breaker skips do not consume it)

**Load Balancing Modes:**

| Mode | Description |
|------|-------------|
| 🔄 **Round Robin** | Cycles through channels sequentially for each request |
| 🎲 **Random** | Randomly selects an available channel for each request |
| 🛡️ **Failover** | Prioritizes high-priority channels, switches to lower priority only on failure |
| ⚖️ **Weighted** | Orders candidates by weight from high to low, then tries them in that order |
| 🤖 **Auto** | Explores under-sampled candidates first, then prefers the candidate with the best success rate inside the configured window |

**Auto Strategy Defaults:**

- **Minimum samples**: `10`
- **Time window**: `300` seconds
- **Sliding window size**: `100` records per channel-model pair
- **Latency weight**: `30`
- Before a candidate reaches the minimum sample count, Octopus prioritizes exploration
- After candidates are explored, Octopus sorts by success rate, then uses sample count, weight, priority, and latency tuning as tie-breakers
- Auto-strategy windows are restored from the database at startup and saved periodically plus on graceful shutdown

**AI Routing Behavior:**

- Clicking **AI Route** on the route page sends all models to AI and generates the full routing table in batch
- Existing groups with the same name only receive missing route items; existing groups are not cleared or replaced
- Clicking **AI Fill Current Group** in the edit dialog sends all models to AI and appends only the matched route items to that group
- The setting previously named AI route target group now acts as the default target group for the single-group compatibility flow only
- AI route tasks are persistent with heartbeat, progress tracking, batch management, and interruption recovery

**CC Switch Integration:**

The group toolbar includes a CC Switch deep link generator that creates provider import links for 5 target apps: Claude Code, Codex, Gemini, OpenCode, and OpenClaw. For Claude Code, it supports mapping Haiku / Sonnet / Opus models to specific route groups.

> 💡 **Example**: Create a group named `gpt-4o`, add multiple providers' GPT-4o channels to it, then access all channels via a unified `model: gpt-4o`.

---

### 🔍 Model Discovery & Capabilities

Octopus exposes multiple levels of model visibility:

#### `/v1/models` — Flat Compatible Model List

Returns all model names that have at least one enabled channel. Compatible with OpenAI SDKs.

This is the broadest view — if a model appears here, Octopus has a channel that *declares* it.

#### `/v1/models?endpoint=<type>` — Endpoint-Filtered List

Narrows the list to models whose **declared endpoint type** matches the filter:

- `?endpoint=chat` — conversation models (chat / responses / messages / deepseek / mimo)
- `?endpoint=embeddings` — embedding models
- `?endpoint=image_generation` — image models
- `?endpoint=music_generation` — music models
- … and so on for `audio_speech`, `audio_transcription`, `video_generation`, `search`, `rerank`, `moderations`

When `endpoint` is omitted or set to `*`, all models are returned.

> Boundaries between some endpoints are not absolute. Models from the **conversation family** (`chat`, `responses`, `messages`, `deepseek`, `mimo`) are visible to one another through the `endpoint` filter because Octopus can bridge these formats transparently.

#### `GET /api/v1/model/capabilities` — Declared Capability Table (Management API)

A management-only endpoint that returns the **aggregated capability view** of every routable model:

```json
{
  "code": 200,
  "message": "success",
  "data": [
    {
      "name": "gpt-4o",
      "endpoints": ["chat"],
      "conversation": true,
      "available": true
    },
    {
      "name": "music-2.6",
      "endpoints": ["music_generation"],
      "conversation": false,
      "available": true
    }
  ]
}
```

| Field | Meaning |
|-------|---------|
| `name` | Model name as exposed to clients |
| `endpoints` | Endpoint types the model declares (deduplicated, sorted) |
| `conversation` | Whether the model belongs to the conversation family |
| `available` | Whether the model has at least one enabled channel |

This is the **declared** capability — what your `Group` configuration says. The actual routable capability may be narrower; see `*` group behaviour below.

#### `*` Group Semantics

A group with endpoint type `*` (EndpointTypeAll) is a **universal pass**: it can be selected by any endpoint type, including `chat`, `embeddings`, `image_generation`, etc.

However, **universal selection does not mean every item in the group actually supports the endpoint**. For non-conversation endpoints (image / video / music / audio / search / rerank / moderation), the relay layer now filters `*` group items before the balancer:

- Only items whose channel type or model name hint at support for the requested endpoint are kept.
- If no items survive filtering, the request returns `404 model not found` instead of blindly trying incompatible channels.
- Conversation endpoints (`chat`, `responses`, `messages`, `deepseek`, `mimo`) are **not** affected by this filtering.

> **Tip:** When you see a model in `/v1/models` or `/api/v1/model/capabilities` but it still returns `model not found` for a specific endpoint, check whether the `*` group's items actually support that endpoint — the relay narrowing may have filtered them all out.

---

| [← Home](../Home.md) | [← Previous](04-Channels.md) | [Next →](06-Model-Market.md) |