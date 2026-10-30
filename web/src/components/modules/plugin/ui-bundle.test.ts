import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';
import { createPluginDocument, PLUGIN_UI_CSP } from './ui-bundle.ts';

// Import the real client with only its application-specific i18n dependency stubbed.
const source = readFileSync(new URL('../../../api/client.ts', import.meta.url), 'utf8');
const compiled = ts.transpile(source
    .replaceAll("from './types'", `from '${new URL('../../../api/types.ts', import.meta.url).href}'`)
    .replace("import { resolveRuntimeI18nMessage } from '@/lib/i18n-runtime';", 'const resolveRuntimeI18nMessage = (_key, _args, fallback) => fallback;'), { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 });

test('client leaves multipart boundary to fetch and preserves JSON headers for bodyless writes and API prefix', async () => {
    const oldFetch = globalThis.fetch;
    const oldBase = process.env.NEXT_PUBLIC_API_BASE_URL;
    const oldWindow = Object.getOwnPropertyDescriptor(globalThis, 'window');
    Object.defineProperty(globalThis, 'window', { configurable: true, value: {} });
    process.env.NEXT_PUBLIC_API_BASE_URL = '/octopus';
    const requests: { url: string; options: RequestInit }[] = [];
    globalThis.fetch = async (url, options) => {
        requests.push({ url: String(url), options: options! });
        return Response.json({ data: { ok: true } });
    };
    try {
        const { apiClient, setAuthStoreGetter } = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString('base64')}`);
        setAuthStoreGetter(() => ({ token: 'test-parent-token', logout() {} }));
        const form = new FormData();
        form.append('file', new Blob(['package']), 'sample.octopkg');
        await apiClient.upload('/api/v1/plugins/install', form);
        assert.equal(requests[0].url, '/octopus/api/v1/plugins/install');
        assert.equal(new Headers(requests[0].options.headers).has('Content-Type'), false);
        assert.equal(new Headers(requests[0].options.headers).get('Authorization'), 'Bearer test-parent-token');
        assert.equal(requests[0].options.body, form);
        assert.match(new Request('https://host.test', requests[0].options).headers.get('Content-Type')!, /^multipart\/form-data; boundary=/);
        for (const method of ['POST', 'PUT', 'PATCH']) {
            await apiClient.request('/api/v1/test', { method });
            const req = requests.at(-1)!;
            assert.equal(new Headers(req.options.headers).get('Content-Type'), 'application/json');
            assert.equal(req.options.body, undefined);
        }
        await apiClient.get('/api/v1/plugins/sample/ui-bundle', { version: '1.0.0' });
        assert.equal(requests.at(-1)!.url, '/octopus/api/v1/plugins/sample/ui-bundle?version=1.0.0');
        assert.equal(new Headers(requests.at(-1)!.options.headers).get('Authorization'), 'Bearer test-parent-token');
    } finally {
        globalThis.fetch = oldFetch;
        if (oldWindow) Object.defineProperty(globalThis, 'window', oldWindow);
        else Reflect.deleteProperty(globalThis, 'window');
        if (oldBase === undefined) delete process.env.NEXT_PUBLIC_API_BASE_URL;
        else process.env.NEXT_PUBLIC_API_BASE_URL = oldBase;
    }
});

test('plugin srcdoc puts restrictive CSP before untrusted encoded payload', () => {
    const document = createPluginDocument({ entry: 'index.html', assets: { 'index.html': { content: btoa('<script>alert(1)</script>'), mime: 'text/html' } } }, '</script><script>hostAttack()</script>', 'nonce', 'https://host.test');
    assert.ok(document.indexOf('Content-Security-Policy') < document.indexOf('<script>'));
    assert.equal(document.includes('<script>hostAttack()'), false);
    assert.ok(document.includes('\\u003c/script>'));
    for (const rule of ["connect-src 'none'", "frame-src 'none'", "form-action 'none'", "base-uri 'none'", "object-src 'none'", "navigate-to 'none'"]) assert.ok(PLUGIN_UI_CSP.includes(rule));
    const view = readFileSync(new URL('./index.tsx', import.meta.url), 'utf8');
    assert.match(view, /sandbox="allow-scripts"/);
    assert.doesNotMatch(view, /allow-same-origin|[?&]token=/);
    assert.match(view, /srcDoc=\{document\}/);
});
