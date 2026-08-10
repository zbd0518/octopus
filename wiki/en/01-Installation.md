# Installation

## 🚀 Quick Start

### 🐳 Docker

Run directly:

```bash
docker run -d --name octopus \
  --restart unless-stopped \
  -p 8080:8080 \
  -v octopus-data:/app/data \
  -e OCTOPUS_AUTH_JWT_SECRET="replace-with-a-long-random-secret" \
  lingyuins/octopus:latest
```

Recommended on Windows Docker Desktop:

```powershell
docker run -d --name octopus `
  --restart unless-stopped `
  -p 8080:8080 `
  -v octopus-data:/app/data `
  -e OCTOPUS_AUTH_JWT_SECRET="replace-with-a-long-random-secret" `
  lingyuins/octopus:latest
```

Or use docker compose:

```yaml
services:
  octopus:
    image: lingyuins/octopus:latest
    container_name: octopus
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - octopus-data:/app/data
    environment:
      OCTOPUS_AUTH_JWT_SECRET: "replace-with-a-long-random-secret"

volumes:
  octopus-data:
```

Then run:

```bash
docker compose up -d
```

Note: The official image runs as the non-root user `octopus` with UID/GID `1000`. The examples above use a Docker named volume for `/app/data` to avoid most host-permission issues, especially on Linux bind mounts and Windows Docker Desktop. If you intentionally bind-mount a host directory to `/app/data` (for example `./data:/app/data`), make sure that directory is writable by UID/GID `1000`, otherwise startup will fail with `permission denied` when creating `config.json` or `data.db`.

The official Docker image rebuilds the frontend during image build and embeds the latest exported UI into the Go binary, so the container includes the matching management UI for that release.

> **🕐 Timezone:** The image defaults to `Asia/Shanghai`. When running with `docker run` (not compose), pass `-e TZ=Asia/Shanghai` or your target IANA timezone (e.g. `-e TZ=America/Los_Angeles`). The server's log timestamps, statistics day boundaries, and frontend time display all depend on the container's timezone setting.

If you are upgrading from an older web build and still see stale frontend errors in the browser, clear the site data / service worker cache once after upgrading so the latest embedded assets are loaded.

> **🔗 Behind a reverse proxy:** By default Octopus does not trust any proxy, so `c.ClientIP()` returns the direct TCP address. Behind Docker bridge networking or a reverse proxy this is the gateway (e.g. `172.17.0.1`), not the real client — relay logs, login rate-limiting, and API-key IP allowlists all see the gateway IP. Set `OCTOPUS_SERVER_TRUSTED_PROXIES` to the proxy's CIDR (e.g. `172.17.0.0/16` for the default Docker bridge) to resolve the real client IP from `X-Forwarded-For`.


### 📦 Download from Release

Download the binary for your platform from [Releases](https://github.com/lingyuins/octopus/releases), then run:

```bash
./octopus start
```

### 🛠️ Build from Source

**Requirements:**
- Go 1.24.4
- Node.js 20+
- pnpm

```bash
# Clone the repository
git clone https://github.com/lingyuins/octopus.git
cd octopus
# Optional: bootstrap the initial admin via environment variables
export OCTOPUS_INITIAL_ADMIN_USERNAME="admin"
export OCTOPUS_INITIAL_ADMIN_PASSWORD="change-this-password-long"
# Optional but recommended: set a persistent JWT secret
export OCTOPUS_AUTH_JWT_SECRET="replace-with-a-long-random-secret"
# Start the backend service directly (API-only mode works even before frontend assets are built)
go run main.go start
```

If `static/out/` already contains built frontend assets, the Go binary serves the management UI directly. Otherwise, Octopus still starts normally and exposes the API endpoints, but the management UI is unavailable until you build the frontend and place the exported assets under `static/out/` before running `go build` / `go run`.

**Build frontend assets for the embedded management UI**

```bash
cd web && pnpm install && NEXT_PUBLIC_APP_VERSION="$(git describe --tags --always 2>/dev/null || printf 'dev')" pnpm build && cd ..
# Move frontend assets to the embed directory expected by the Go binary
mkdir -p static/out
mv web/out/* static/out/
# If Next.js exports an empty _not-found directory, add a placeholder before building Go
printf 'placeholder for go:embed\n' > static/out/_not-found/.keep
# Start the backend service with embedded UI assets available in the repository
go run main.go start
```

**Development Mode**

```bash
cd web && pnpm install && NEXT_PUBLIC_API_BASE_URL="http://127.0.0.1:8080" NEXT_PUBLIC_APP_VERSION="$(git describe --tags --always 2>/dev/null || printf 'dev')" pnpm dev
## Open a new terminal, optionally set initial admin credentials for automatic bootstrap
export OCTOPUS_INITIAL_ADMIN_USERNAME="admin"
export OCTOPUS_INITIAL_ADMIN_PASSWORD="change-this-password-long"
## Optional but recommended: set a persistent JWT secret
export OCTOPUS_AUTH_JWT_SECRET="replace-with-a-long-random-secret"
## Start the backend service
go run main.go start
## Access the frontend at
http://localhost:3000
```

### 🔐 Initial Admin Setup

On first launch, you can initialize the admin account in either of these ways:

- Provide `OCTOPUS_INITIAL_ADMIN_USERNAME` and `OCTOPUS_INITIAL_ADMIN_PASSWORD` to bootstrap automatically at startup
- Or open the Web UI on first visit and create the initial admin account through the guided setup wizard

> ⚠️ **Security Notice**: The initial admin password must be at least 12 characters long.
>
> ⚠️ **Security Notice**: If `OCTOPUS_AUTH_JWT_SECRET` or `auth.jwt_secret` is not configured, Octopus will generate an in-memory JWT secret at startup. Existing login tokens will become invalid after a restart.

---

| [← Home](../Home.md) | | [Next →](02-Configuration.md) |