# Analytics

## 📈 Analytics

The Analytics module is a read-oriented operations view with six tabs. The default tab is **Channel × Model** so the most-watched data shows first:

| Tab | What it shows |
|-----|---------------|
| Channel × Model | Channel×model usage matrix, usage-distribution share chart (top-N by model / channel×model, cost/count/tokens metrics) |
| Usage Breakdown | Provider, model, and API key breakdowns for the selected time range (renamed from "Utilization" with a no-billing hint when cost data is empty) |
| Route Health | Health score, enabled / disabled item counts, and recent failure pressure for each group |
| Latency | Request latency metrics (Avg, P50, P95, P99), first-token-user-time (FTUT) metrics, and latency distribution histogram |
| Evaluation | Group readiness, AI route progress, group test progress, and semantic-cache effectiveness |
| Cache | Semantic cache effectiveness and provider-side prompt-cache analytics (cache rate, reuse ratio, estimated cost savings per provider) |

**Time ranges:** `1d`, `7d`, `30d`, `90d`, `ytd`, and `all`

The overview metrics API still exists as `/api/v1/analytics/overview`, but the primary UI entry point for those summary cards is now the Home page. Home also carries an independent `7d / 30d / 90d` overview-range switch, plus a daily hero summary, trend chart, GitHub-style activity heatmap, and ranking panel.

The Evaluation tab is intentionally lightweight: it acts as an entry point into group testing, AI routing, and semantic-cache tuning instead of duplicating those full workflows.

**Share Snapshot:**

The Analytics page includes a Share button that generates a visual PNG snapshot of the current analytics state, which can be downloaded or copied to the clipboard. The snapshot includes key stats (requests, tokens, cost, providers, cache hit rate) and a timestamp.

---

| [← Home](../Home.md) | [← Previous](07-Relay-Endpoints.md) | [Next →](09-Ops.md) |