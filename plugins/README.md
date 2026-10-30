# Octopus plugins

Octopus loads a local `.octopkg` ZIP archive containing `manifest.json`, `plugin.wasm` and optional `ui/` assets. The host uses Extism and wazero, retains `CGO_ENABLED=0`, and grants no filesystem, environment, socket, SQL or host-process access to guest modules.

## Build

TinyGo:

```bash
cd plugins/examples/go-observer
tinygo build -target wasi -o plugin.wasm .
go run ../cmd/octopus-plugin pack -dir . -out go-observer.octopkg
```

Rust:

```bash
cd plugins/examples/rust-observer
cargo build --release --target wasm32-wasip1
cp target/wasm32-wasip1/release/octopus_rust_observer.wasm plugin.wasm
go run ../cmd/octopus-plugin pack -dir . -out rust-observer.octopkg
```

`go run ./plugins/cmd/octopus-plugin validate PACKAGE` validates archive structure, manifest constraints and prints the WASM digest. Install through the Plugins page as an administrator, review its declared permissions and enable it. Upgrade uses the same package ID and a greater semantic version.

## ABI

The guest exports `handle` (or `manifest.entry`). Its Extism input is a JSON object containing `api_version`, `request_id`, `plugin_id`, `event`, optional `attempt_id`, `payload`, and ordinary `config`. Return `api_version`, `decision` (`continue`, `observe`, `handled`, or `allow`), optional `payload`, `tag`, and `error`. Both PDKs expose `continue`, `observe`, failure and host-call helpers.

The guest can import `extism:host/user.host_call(pointer) -> pointer`. Its JSON request has `capability` and `payload`. The host enforces manifest permissions for `kv.*`, `secret.*`, `http.request`, `notify.publish` and `log.write`. Binary request/response bodies are standard JSON base64 strings. No plugin may set upstream URLs, credentials, hop-by-hop headers or Host/Content-Length directly.

## Extension events

- `relay.request.before_route`: edit the internal request DTO. Model-only edits preserve the original wire payload and unknown fields; other edits use the matching outbound protocol encoder to produce the wire payload while retaining the updated internal DTO. The host revalidates model authorization and computes session/cache keys afterward. Passthrough supports model-only edits.
- `relay.route.filter`: return `{"exclude_channel_ids":[]}`. Candidates may only be removed.
- `relay.request.before_upstream`: return whitelisted headers and a base64 body. Credentials and URL are never sent to the plugin.
- `relay.response.before_send`: non-streaming JSON only, before any response byte is written. The body is base64-encoded JSON.
- `relay.completed`, `channel.health.changed`, `pool.account.changed`, `notification.created`: bounded asynchronous observations. Delivery is at least once while the process is running; events are not durable across restarts.
- `http.rpc`: called via `GET` or `POST /api/v1/plugins/:id/rpc/*path`. Declare `rpc:read` or `rpc:write`. Viewer accounts cannot write.
- `plugin.migrate`: upgrade-only call with `{"from":N,"to":N}`; failure retains the old installed version.

## UI contract

The parent fetches `GET /api/v1/plugins/:id/ui-bundle?version=...` with its normal Authorization header and configured API base path. This authenticated, read-permission-protected endpoint returns an envelope containing `{entry, assets: {"relative/path": {content: "base64 bytes", mime: "..."}}}`. Only enabled plugins with a UI are served, with package size/count limits and no filesystem symlinks. There is no directly navigable host-origin HTML endpoint, URL token, or credential passed to the UI.

The parent creates a self-contained `srcdoc` iframe with **only** `sandbox="allow-scripts"` (no `allow-same-origin`). The UI has an opaque origin. A CSP placed before plugin content denies network connections, forms, nested frames, workers, objects and base URLs; sandbox restrictions deny popups and top-level navigation. Local scripts, styles, images, fonts and media are rewritten to data URLs, including relative HTML references and CSS `url()` / quoted `@import` references resolved relative to each file. Inline scripts/styles are supported. Build UI scripts as classic self-contained bundles: ES module imports, runtime relative fetch, and `srcset` are not supported. For dynamically assigned local resource URLs use `window.octopus.asset('relative/path')`. External resources are not supported. The example uses separate local JS and CSS files.

A read-only `window.octopus` object is available before UI scripts execute: `{pluginId, session, parentOrigin, asset}`. `session` is a fresh random nonce for this UI load, not an authentication credential. Send RPC with `parent.postMessage({type: 'octopus:plugin:rpc:v1', pluginId, session, requestId, method: 'GET', path: 'status'}, parentOrigin)`. Only `GET`/`POST` and slash-separated alphanumeric/underscore/hyphen path segments are accepted (no query, traversal, or URL syntax); POST may include `body`. GET forwards the actual wildcard path to the guest (e.g. `/status`) without a body.

The parent requires the exact mounted iframe `event.source`, opaque `event.origin === 'null'`, matching plugin ID, nonce and valid message shape/path. Responses contain `{type, pluginId, session, requestId, result}` or `error`; they are sent only to the captured iframe window (`'*'` is necessary for an opaque recipient), never broadcast, and discarded after unmount/session replacement. The UI must require `event.source === parent`, `event.origin === window.octopus.parentOrigin` and matching type/pluginId/session/requestId when accepting responses. The backend independently repeats authentication, role and manifest permission checks. The nonce is a session binding, not protection against code within the same plugin UI; treat the whole UI package as one principal.

## Deployment and backup

Packages live below `${OCTOPUS_DATA_DIR}/plugins`. Multi-instance deployments must install the same local package on each node; runtime and KV are not synchronized over Redis. Uninstall stops new calls and removes code on restart. Backups contain metadata, configuration, KV and encrypted secrets, not executable packages; restore requires reinstalling the package. Backup files may contain plaintext credentials and require the same protection as a full credential export.
