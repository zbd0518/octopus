# Model Market & Pricing

### 💎 Model Market & Pricing

The `Model` route is a model market view with a dual-tab interface: **Market** (pricing and coverage) and **Capabilities** (endpoint support declarations).

**Market tab data merged on each card:**

- Custom or synced pricing from the LLM price catalog
- Channel coverage and enabled key counts from channel-model relationships
- Average latency and success / failure counts from recorded model stats

**Summary metrics:**

| Metric | Meaning |
|--------|---------|
| Models | Number of currently visible model cards |
| Coverage | Total channel-to-model coverage count in the current result set |
| Unique Channels | Distinct channels represented by the visible cards |
| Average Latency | Weighted average latency derived from model request stats |

**Capabilities tab:**

The Capabilities panel shows per-model endpoint support declarations, conversation flag, availability status, and auto-endpoint detection indicators. Models can be searched and filtered by name with status badges (Active, Down, Non-conversation).

**Data Sources:**

- The system periodically syncs model pricing data from [models.dev](https://github.com/sst/models.dev)
- When creating or syncing channels, if a model is not yet in the local catalog, Octopus automatically creates a local model-price record so the price can still be maintained manually
- Manual creation of models that exist in models.dev is also supported for custom pricing

**Price Priority:**

| Priority | Source | Description |
|:--------:|--------|-------------|
| 🥇 High | This Page | Prices set by user in the model market page |
| 🥈 Low | models.dev | Auto-synced default prices |

> 💡 **Tip**: To override a model's default price, simply set a custom price for it in the model market page.

**Operational actions preserved on the page:**

- Create a custom model price record
- Edit input / output / cache prices for an existing model
- Delete a custom model entry
- Refresh upstream pricing from the page header
- Keep the scheduled price refresh policy in the Settings `LLM Price` card

---

| [← Home](../Home.md) | [← Previous](05-Groups.md) | [Next →](07-Relay-Endpoints.md) |