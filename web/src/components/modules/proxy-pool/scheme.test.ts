import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
    detectProxyScheme,
    buildSchemeTemplate,
    schemeLabel,
    PROXY_SCHEMES,
} from './scheme.ts';

test('detectProxyScheme recognizes each supported scheme', () => {
    const cases: Array<[string, string | null]> = [
        ['http://127.0.0.1:7890', 'http'],
        ['https://proxy.example.com:8443', 'https'],
        ['socks5://127.0.0.1:1080', 'socks5'],
        ['ss://YWVzLTI1Ni1nY206cGFzcw@example.com:8388', 'ss'],
        ['vmess://eyJhZGQiOiJleGFtcGxlLmNvbSJ9', 'vmess'],
        ['vless://uuid@example.com:443?security=tls', 'vless'],
        ['trojan://pass@example.com:443', 'trojan'],
        // legacy alias → socks5
        ['socks://127.0.0.1:1080', 'socks5'],
        // case-insensitive scheme
        ['SOCKS5://127.0.0.1:1080', 'socks5'],
        ['HTTP://127.0.0.1:7890', 'http'],
        // unsupported / malformed
        ['ftp://host:21', null],
        ['quic://host:443', null],
        ['', null],
        ['  ', null],
        ['example.com:8080', null],
    ];
    for (const [input, expected] of cases) {
        assert.equal(detectProxyScheme(input), expected, `detectProxyScheme(${JSON.stringify(input)})`);
    }
});

test('detectProxyScheme requires the :// separator', () => {
    assert.equal(detectProxyScheme('http:127.0.0.1'), null);
    assert.equal(detectProxyScheme('socks5 127.0.0.1'), null);
});

test('PROXY_SCHEMES covers the full supported set', () => {
    assert.deepEqual(PROXY_SCHEMES, ['http', 'https', 'socks5', 'ss', 'vmess', 'vless', 'trojan']);
});

test('buildSchemeTemplate returns a usable example per scheme', () => {
    for (const scheme of PROXY_SCHEMES) {
        const template = buildSchemeTemplate(scheme);
        assert.ok(template.startsWith(`${scheme}://`), `${scheme} template starts with scheme`);
        assert.equal(detectProxyScheme(template), scheme, `${scheme} template re-detects`);
    }
});

test('schemeLabel returns a display name', () => {
    assert.equal(schemeLabel('http'), 'HTTP');
    assert.equal(schemeLabel('socks5'), 'SOCKS5');
    assert.equal(schemeLabel('ss'), 'SS');
    assert.equal(schemeLabel('vmess'), 'VMess');
});
