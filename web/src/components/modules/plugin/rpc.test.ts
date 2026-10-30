import assert from 'node:assert/strict';
import test from 'node:test';
import { dispatchPluginRPC, isPluginRPCMessage, pluginRPCPath } from './rpc.ts';

const valid = { type: 'octopus:plugin:rpc:v1', pluginId: 'sample', session: 'nonce', requestId: '1', method: 'GET', path: 'status' };

test('plugin RPC validates plugin, session, verb, request ID and path', () => {
    assert.equal(isPluginRPCMessage(valid, 'sample', 'nonce'), true);
    for (const change of [{ pluginId: 'other' }, { session: 'old' }, { method: 'DELETE' }, { requestId: '' }, { path: '../status' }, { path: 'status?token=x' }]) {
        assert.equal(isPluginRPCMessage({ ...valid, ...change }, 'sample', 'nonce'), false);
    }
    assert.equal(isPluginRPCMessage(null, 'sample', 'nonce'), false);
    assert.equal(pluginRPCPath('/status/detail'), 'status/detail');
    for (const path of ['', '//status', '../status', 'status?x=y', 'https://x', '%2e%2e/status', 'a\\b', 'x'.repeat(257)]) assert.throws(() => pluginRPCPath(path));
});

test('plugin RPC binds exact source and opaque origin and replies only to that window', async () => {
    const sent: unknown[] = [];
    const target = { postMessage: (...args: unknown[]) => sent.push(args) } as unknown as Window;
    const calls: unknown[] = [];
    const call = async (...args: unknown[]) => { calls.push(args); return { ok: true }; };
    for (const event of [
        { source: null, origin: 'null', data: valid },
        { source: {} as Window, origin: 'null', data: valid },
        { source: target, origin: 'https://host.test', data: valid },
        { source: target, origin: 'null', data: { ...valid, session: 'old' } },
    ]) await dispatchPluginRPC(event, target, 'sample', 'nonce', call, () => true);
    assert.equal(calls.length, 0);
    await dispatchPluginRPC({ source: target, origin: 'null', data: valid }, target, 'sample', 'nonce', call, () => true);
    assert.deepEqual(calls, [['sample', 'GET', 'status', undefined]]);
    assert.deepEqual(sent, [[{ type: valid.type, pluginId: 'sample', session: 'nonce', requestId: '1', result: { ok: true } }, '*']]);
});

test('plugin RPC drops responses and requests from a disposed session', async () => {
    let active = true;
    let finish: (value: unknown) => void = () => {};
    const sent: unknown[] = [];
    const target = { postMessage: (value: unknown) => sent.push(value) } as unknown as Window;
    const pending = dispatchPluginRPC({ source: target, origin: 'null', data: valid }, target, 'sample', 'nonce', () => new Promise(resolve => { finish = resolve; }), () => active);
    active = false;
    finish({ secret: true });
    await pending;
    await dispatchPluginRPC({ source: target, origin: 'null', data: valid }, target, 'sample', 'nonce', async () => assert.fail('disposed request dispatched'), () => false);
    assert.deepEqual(sent, []);
});
