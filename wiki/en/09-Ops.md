# Ops

## 🛠️ Ops

The Ops module focuses on runtime posture and operational diagnostics:

| Tab | What it shows |
|-----|---------------|
| Telemetry | Hero metrics (uptime, total requests, avg latency, error rate, active connections, memory usage), P95 latency, throughput RPS, database health, session & quota activity, semantic cache snapshot, provider health table (sortable columns + mini bar charts) |
| Quota | API key limit posture across RPM, TPM, max-cost, and per-model quota settings, merged with total tokens + success rate + "view key detail" jump |
| Health | Database reachability, cache readiness, task-runtime sanity, recent error count, and failing groups (with jump to Analytics → Route Health) |
| Maintenance | Actionable runtime tuning: Retry, Circuit Breaker, and Response Filter settings consolidated in one tab (moved out of the Settings page) |
| System | Build metadata, database type, public API base URL, proxy, retention intervals, AI route mode, and AI route services |
| Audit | Paginated audit history for management-side write operations |

**Provider Prompt Cache Analytics:**

The Telemetry tab includes provider-side prompt cache monitoring, tracking upstream provider prompt caching effectiveness: cache rate, cache reuse ratio, cache read / write tokens, estimated cost savings per channel, and a 24-hour cache trend chart. This is separate from the semantic cache.

**Audit scope:**

- Covers selected management write routes such as channel / group / model / setting / API key / alert / user mutations, AI route generation, log clearing, price refresh, import, and self-update
- Does not record public `/v1/...` relay traffic

---

| [← Home](../Home.md) | [← Previous](08-Analytics.md) | [Next →](10-Settings.md) |