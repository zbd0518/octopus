export interface PluginUIBundle {
    entry: string;
    assets: Record<string, { content: string; mime: string }>;
}

export const PLUGIN_UI_CSP = "default-src 'none'; script-src 'unsafe-inline' data: blob:; style-src 'unsafe-inline' data: blob:; img-src data: blob:; font-src data: blob:; media-src data: blob:; connect-src 'none'; frame-src 'none'; worker-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; navigate-to 'none'";

// Runs exclusively inside the opaque frame. Never parse plugin markup in the host DOM.
function bootstrap(bundle: PluginUIBundle, pluginId: string, session: string, parentOrigin: string, csp: string) {
    const base = 'https://plugin.invalid/';
    const urls = new Map<string, string>();
    const building = new Set<string>();
    const text = (content: string) => new TextDecoder().decode(Uint8Array.from(atob(content), c => c.charCodeAt(0)));
    const rewriteCSS = (css: string, from: string): string => css
        .replace(/url\(\s*(['"]?)(.*?)\1\s*\)/gi, (_match, _quote, value: string) => `url("${resolve(value, from)}")`)
        .replace(/@import\s+(['"])(.*?)\1/gi, (_match, _quote, value: string) => `@import "${resolve(value, from)}"`);
    const resolve = (value: string, from: string): string => {
        if (value.startsWith('data:') || value.startsWith('#')) return value;
        const url = new URL(value, base + from);
        if (url.origin !== new URL(base).origin || url.username || url.password) return 'about:blank';
        const name = decodeURIComponent(url.pathname.slice(1));
        const asset = bundle.assets[name];
        if (!asset || building.has(name)) return 'about:blank';
        if (!urls.has(name)) {
            building.add(name);
            let content: BlobPart = Uint8Array.from(atob(asset.content), c => c.charCodeAt(0));
            if (asset.mime.split(';')[0] === 'text/css') {
                content = rewriteCSS(text(asset.content), name);
            }
            // Data URLs work in opaque frames without inheriting host origin or credentials.
            const bytes = typeof content === 'string' ? new TextEncoder().encode(content) : content as Uint8Array;
            let binary = '';
            for (const byte of bytes) binary += String.fromCharCode(byte);
            urls.set(name, `data:${asset.mime};base64,${btoa(binary)}`);
            building.delete(name);
        }
        return urls.get(name)! + url.hash;
    };
    Object.defineProperty(window, 'octopus', { value: Object.freeze({
        pluginId, session, parentOrigin,
        asset: (value: string) => resolve(value, bundle.entry),
    }), writable: false, configurable: false });
    const entry = bundle.assets[bundle.entry];
    const doc = new DOMParser().parseFromString(text(entry.content), 'text/html');
    doc.querySelectorAll('base, meta[http-equiv], iframe, frame, object, embed').forEach(node => node.remove());
    for (const node of doc.querySelectorAll('*')) {
        for (const attr of ['src', 'href', 'poster']) {
            const value = node.getAttribute(attr);
            if (value !== null) node.setAttribute(attr, resolve(value, bundle.entry));
        }
        // Responsive source lists need their own parser; use the ordinary src fallback.
        node.removeAttribute('srcset');
        const style = node.getAttribute('style');
        if (style) node.setAttribute('style', rewriteCSS(style, bundle.entry));
    }
    for (const style of doc.querySelectorAll('style')) {
        style.textContent = rewriteCSS(style.textContent ?? '', bundle.entry);
    }
    // document.write executes scripts in their original order. The first CSP remains
    // enforced while the document is replaced; plugin meta tags cannot relax it.
    const policy = doc.createElement('meta');
    policy.httpEquiv = 'Content-Security-Policy';
    policy.content = csp;
    doc.head.prepend(policy);
    document.open();
    document.write('<!doctype html>' + doc.documentElement.outerHTML);
    document.close();
}

export function createPluginDocument(bundle: PluginUIBundle, pluginId: string, session: string, parentOrigin: string): string {
    const json = JSON.stringify([bundle, pluginId, session, parentOrigin, PLUGIN_UI_CSP]).replace(/</g, '\\u003c');
    return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="${PLUGIN_UI_CSP}"></head><body><script>(${bootstrap.toString()})(...${json});</script></body></html>`;
}
