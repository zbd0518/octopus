# Admin Roles

### 👥 Admin Roles

The management API and embedded Web UI use three built-in roles:

- `admin`: full access, including user management
- `editor`: operational write access for channels, groups, settings, API keys, logs, alerts, and AI routing
- `viewer`: read-only access to operational data

Role checks are enforced on the server side, using the currently stored role rather than trusting only the JWT claim.

Octopus also supports passwordless login and registration via WebAuthn/Passkey with configurable RP settings (RP ID, RP name, and allowed origins), configured from the Settings page's WebAuthn / Passkey card.

---

| [← Home](../Home.md) | [← Previous](02-Configuration.md) | [Next →](04-Channels.md) |