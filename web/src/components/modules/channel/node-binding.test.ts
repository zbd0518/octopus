import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { createChannelConnectionDraft, newChannelKeys, withNodeTransport, withNodeUpdate } from './node-binding.ts';

const http = {
    node_id: 0,
    base_urls: [{ url: 'https://example.com', delay: 0 }],
    keys: [{ channel_key: 'http-draft-key', enabled: true }],
    pool_id: 3,
    proxy_mode: 'pool',
    proxy_config_id: 7,
    proxy: true,
    channel_proxy: 'http://proxy.example.com',
};

test('node discovery/test/create payload strips HTTP credentials and mutually exclusive transports', () => {
    const draft = { ...http, node_id: 42, model: 'model-a' };
    const payload = withNodeTransport(draft);
    assert.equal(payload.node_id, 42);
    assert.deepEqual(payload.base_urls, []);
    assert.deepEqual(payload.keys, []);
    assert.equal(payload.pool_id, 0);
    assert.equal(payload.proxy_mode, 'direct');
    assert.equal(payload.proxy_config_id, null);
    assert.equal(payload.proxy, false);
    assert.equal(payload.channel_proxy, '');
    assert.equal(payload.model, 'model-a');
    assert.equal(draft.keys[0].channel_key, 'http-draft-key');
    assert.equal(draft.proxy_mode, 'pool');
});

test('ordinary HTTP payload retains exact keys and no implicit transport normalization', () => {
    assert.equal(withNodeTransport(http), http);
    const patch = { keys_to_update: [{ id: 2, remark: 'renamed' }], name: 'HTTP' };
    assert.deepEqual(withNodeUpdate(patch, 0, 0), patch);
    assert.deepEqual(withNodeUpdate({ name: 'name only' }, 0, 0), { name: 'name only' });
});

test('node update never sends placeholder key diffs and preserves unrelated fields', () => {
    const patch = { name: 'node', base_urls: http.base_urls, keys_to_delete: [1], keys_to_add: http.keys, keys_to_update: [{ id: 2 }] };
    const payload = withNodeUpdate(patch, 42, 0);
    assert.equal(payload.node_id, 42);
    assert.equal(payload.name, 'node');
    for (const key of ['base_urls', 'keys_to_delete', 'keys_to_add', 'keys_to_update']) assert.ok(!(key in payload));
    assert.deepEqual(patch.keys_to_delete, [1]);
    assert.ok(!('node_id' in withNodeUpdate({ name: 'same binding' }, 42, 42)));
    assert.equal(withNodeUpdate({ name: 'convert to HTTP' }, 0, 42).node_id, 0);
});

test('persisted node channel starts with blank HTTP drafts and converts with newly added credentials', () => {
    const internal = {
        node_id: 42,
        base_urls: [{ url: 'https://node.octopus.invalid', delay: 0 }],
        keys: [{ id: 9, channel_id: 1, enabled: true, channel_key: 'octopus-node-local-credential', status_code: 0, last_use_time_stamp: 0, total_cost: 0, priority: 0, remark: '' }],
    };
    const draft = createChannelConnectionDraft(internal);
    assert.equal(draft.base_urls[0].url, '');
    assert.equal(draft.keys[0].channel_key, '');
    assert.equal(draft.keys[0].id, undefined);
    draft.base_urls[0].url = 'https://upstream.example.com';
    draft.keys[0].channel_key = ' new-http-key ';
    const payload = withNodeUpdate({ base_urls: draft.base_urls, keys_to_add: newChannelKeys(draft.keys) }, 0, internal.node_id);
    assert.equal(payload.node_id, 0);
    assert.equal(payload.base_urls[0].url, 'https://upstream.example.com');
    assert.deepEqual(payload.keys_to_add, [{ enabled: true, channel_key: 'new-http-key', priority: 0, remark: '', supported_models: '' }]);
    assert.ok(!('keys_to_update' in payload));
    assert.equal(internal.keys[0].channel_key, 'octopus-node-local-credential');
    assert.equal(internal.base_urls[0].url, 'https://node.octopus.invalid');
});

test('persisted HTTP channel draft retains real keys and IDs instead of creating replacement credentials', () => {
    const channel = {
        node_id: 0,
        base_urls: [{ url: 'https://upstream.example.com', delay: 0 }],
        keys: [{ id: 9, channel_id: 1, enabled: true, channel_key: 'existing-http-key', status_code: 200, last_use_time_stamp: 1, total_cost: 2, priority: 3, remark: 'keep', supported_models: 'model-a' }],
    };
    const draft = createChannelConnectionDraft(channel);
    assert.deepEqual(draft.base_urls, channel.base_urls);
    assert.equal(draft.keys[0].id, 9);
    assert.equal(draft.keys[0].channel_key, 'existing-http-key');
    assert.equal(draft.keys[0].priority, 3);
    assert.equal(draft.keys[0].supported_models, 'model-a');
    assert.deepEqual(newChannelKeys(draft.keys), []);
});

test('real create/edit/discovery/test call sites apply transport guards and server write capability', () => {
    const form = readFileSync(new URL('./Form.tsx', import.meta.url), 'utf8');
    const create = readFileSync(new URL('./Create.tsx', import.meta.url), 'utf8');
    const edit = readFileSync(new URL('./CardContent.tsx', import.meta.url), 'utf8');
    assert.match(form, /canBindNode = nodeList\?\.can_write === true/);
    assert.match(form, /disabled=\{!canBindNode\}/);
    assert.match(form, /buildTestPayload = \(\) => withNodeTransport\(\{\s*node_id: formData.node_id/);
    assert.match(form, /const payload = withNodeTransport\(\{\s*node_id: formData.node_id/);
    assert.match(form, /if \(!isNode && enabledKeys.length > 1\)/);
    assert.match(form, /!isNode && <ProxySelector/);
    assert.match(form, /formData.pool_id === 0 && !isNode &&/);
    assert.match(create, /withNodeTransport\(\{\s*node_id: formData.node_id/);
    assert.match(edit, /\.\.\.createChannelConnectionDraft\(channel\)/);
    assert.match(edit, /const keys_to_add = newChannelKeys\(nextKeys\)/);
    assert.match(edit, /withNodeUpdate\(req, formData.node_id, channel.node_id \?\? 0\)/);
    assert.match(form, /type="url"[\s\S]*?required=\{idx === 0\}/);
    assert.match(form, /value=\{k.channel_key\}[\s\S]*?required=\{idx === 0\}/);
});
