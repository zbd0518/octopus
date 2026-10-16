import assert from 'node:assert/strict';
import test from 'node:test';

import { decodeBulkBase64Keys, looksLikeApiKey, looksLikeBase64Payload, mergeBulkKeys, parseBulkKeys, splitBase64Payloads } from './bulk-keys.ts';

/** btoa 只在浏览器全局存在，测试环境用 Buffer 等价编码。 */
function encodeBase64(value: string): string {
    return Buffer.from(value, 'utf8').toString('base64');
}

/** 在每个字符处断行，模拟 base64 被折断粘贴（每段都非法）。 */
function wrapBase64(payload: string): string {
    const parts = payload.match(/.{1,8}/g) ?? [];
    return parts.join('\n');
}

test('looksLikeApiKey rejects short or spaced values', () => {
    assert.equal(looksLikeApiKey('sk-12345678'), true);
    assert.equal(looksLikeApiKey('short'), false);
    assert.equal(looksLikeApiKey('has space value'), false);
});

test('parseBulkKeys supports one key per line', () => {
    const parsed = parseBulkKeys('sk-aaa\nsk-bbb\n\n sk-ccc ');
    assert.deepEqual(parsed, [
        { channel_key: 'sk-aaa' },
        { channel_key: 'sk-bbb' },
        { channel_key: 'sk-ccc' },
    ]);
});

test('parseBulkKeys supports key|remark and key\\tremark', () => {
    assert.deepEqual(parseBulkKeys('sk-aaa|pool-1\nsk-bbb\tbackup'), [
        { channel_key: 'sk-aaa', remark: 'pool-1' },
        { channel_key: 'sk-bbb', remark: 'backup' },
    ]);
});

test('parseBulkKeys treats key,remark when right side is not a key', () => {
    assert.deepEqual(parseBulkKeys('sk-aaaaaaaa,主账号'), [
        { channel_key: 'sk-aaaaaaaa', remark: '主账号' },
    ]);
});

test('parseBulkKeys splits single-line comma/semicolon key lists', () => {
    assert.deepEqual(parseBulkKeys('sk-aaaaaaa,sk-bbbbbbb;sk-ccccccc'), [
        { channel_key: 'sk-aaaaaaa' },
        { channel_key: 'sk-bbbbbbb' },
        { channel_key: 'sk-ccccccc' },
    ]);
});

test('mergeBulkKeys fills empty placeholder and dedupes', () => {
    const result = mergeBulkKeys(
        [{ enabled: true, channel_key: '', priority: 0, remark: '' }],
        [
            { channel_key: 'sk-aaa', remark: 'a' },
            { channel_key: 'sk-bbb' },
            { channel_key: 'sk-aaa' },
        ],
    );

    assert.equal(result.added, 2);
    assert.equal(result.skippedDuplicate, 1);
    assert.deepEqual(result.keys, [
        { enabled: true, channel_key: 'sk-aaa', priority: 0, remark: 'a' },
        { enabled: true, channel_key: 'sk-bbb', priority: 0, remark: '' },
    ]);
});

test('mergeBulkKeys keeps existing rows and skips duplicates against them', () => {
    const result = mergeBulkKeys(
        [
            { id: 1, enabled: true, channel_key: 'sk-exist', priority: 2, remark: 'old' },
            { enabled: false, channel_key: 'sk-draft', priority: 0, remark: '' },
        ],
        [
            { channel_key: 'sk-exist' },
            { channel_key: 'sk-new', remark: 'n' },
        ],
    );

    assert.equal(result.added, 1);
    assert.equal(result.skippedDuplicate, 1);
    assert.equal(result.keys.length, 3);
    assert.equal(result.keys[2].channel_key, 'sk-new');
    assert.equal(result.keys[2].remark, 'n');
    assert.equal(result.keys[0].id, 1);
});

test('looksLikeBase64Payload accepts padded payloads and rejects plaintext', () => {
    assert.equal(looksLikeBase64Payload('c2stYWFhYWFhYQ=='), true);
    assert.equal(looksLikeBase64Payload('data:text/plain;base64,c2stYWFhYWFhYQ=='), true);
    assert.equal(looksLikeBase64Payload('sk-aaa'), false);
    assert.equal(looksLikeBase64Payload('has space payload'), false);
});

test('splitBase64Payloads keeps one payload per line', () => {
    assert.deepEqual(splitBase64Payloads('c2stYWFhYQ==\n\nc2stYmJiYg=='), [
        'c2stYWFhYQ==',
        'c2stYmJiYg==',
    ]);
});

test('splitBase64Payloads keeps a single whitespace-free payload intact', () => {
    // base64 换行折断后的多 Key 明文，编码回来就是一个不含空白的串，不能按空白拆。
    const encoded = encodeBase64('sk-aaaa\nsk-bbbb');
    assert.deepEqual(splitBase64Payloads(encoded), [encoded]);
});

test('decodeBulkBase64Keys decodes one key per line', () => {
    const result = decodeBulkBase64Keys([encodeBase64('sk-aaaaaaaa'), encodeBase64('sk-bbbbbbbb')].join('\n'));
    assert.deepEqual(result.decoded, [
        { source: encodeBase64('sk-aaaaaaaa'), channel_key: 'sk-aaaaaaaa', remark: undefined },
        { source: encodeBase64('sk-bbbbbbbb'), channel_key: 'sk-bbbbbbbb', remark: undefined },
    ]);
    assert.deepEqual(result.failed, []);
});

test('decodeBulkBase64Keys returns the whole multi-line plaintext as separate keys', () => {
    const encoded = encodeBase64('sk-aaaaaa|pool-1\nsk-bbbbbb');
    const result = decodeBulkBase64Keys(encoded);
    assert.deepEqual(result.decoded, [
        { source: encoded, channel_key: 'sk-aaaaaa', remark: 'pool-1' },
        { source: encoded, channel_key: 'sk-bbbbbb', remark: undefined },
    ]);
});

test('decodeBulkBase64Keys strips Chinese characters and punctuation from decoded text', () => {
    const encoded = encodeBase64('sk-aaaaaaaa|主账号\n这是一行使用说明\nsk-bbbbbbbb（备用）');
    const result = decodeBulkBase64Keys(encoded);
    assert.deepEqual(result.decoded, [
        { source: encoded, channel_key: 'sk-aaaaaaaa', remark: undefined },
        { source: encoded, channel_key: 'sk-bbbbbbbb', remark: undefined },
    ]);
    assert.deepEqual(result.failed, []);
});

test('decodeBulkBase64Keys marks Chinese-only plaintext as not a key', () => {
    const encoded = encodeBase64('这是一段说明文字');
    const result = decodeBulkBase64Keys(encoded);
    assert.deepEqual(result.decoded, []);
    assert.equal(result.failed.length, 1);
    assert.equal(result.failed[0].reason, 'not-a-key');
});

test('decodeBulkBase64Keys reports undecodable segments without dropping the rest', () => {
    const good = encodeBase64('sk-aaaaaaaa');
    const result = decodeBulkBase64Keys(`${good}\n!!not-base64!!`);
    assert.deepEqual(result.decoded, [{ source: good, channel_key: 'sk-aaaaaaaa', remark: undefined }]);
    assert.equal(result.failed.length, 1);
    assert.equal(result.failed[0].source, '!!not-base64!!');
    assert.equal(result.failed[0].reason, 'invalid-base64');
});

test('decodeBulkBase64Keys falls back to joining a payload wrapped across lines', () => {
    const encoded = encodeBase64(`sk-${'0'.repeat(60)}`);
    const result = decodeBulkBase64Keys(wrapBase64(encoded));
    assert.deepEqual(result.decoded, [{ source: encoded, channel_key: `sk-${'0'.repeat(60)}`, remark: undefined }]);
    assert.deepEqual(result.failed, []);
});
