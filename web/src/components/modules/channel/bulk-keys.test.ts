import assert from 'node:assert/strict';
import test from 'node:test';

import { looksLikeApiKey, mergeBulkKeys, parseBulkKeys } from './bulk-keys.ts';

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
