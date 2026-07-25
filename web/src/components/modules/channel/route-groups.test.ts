import assert from 'node:assert/strict';
import test from 'node:test';
import { collectChannelRouteGroups } from './route-groups.ts';

test('collectChannelRouteGroups returns empty for empty input', () => {
    assert.deepEqual(collectChannelRouteGroups([], 1), []);
    assert.deepEqual(collectChannelRouteGroups(null, 1), []);
    assert.deepEqual(collectChannelRouteGroups(undefined, 1), []);
});

test('collectChannelRouteGroups aggregates models per group and ignores other channels', () => {
    const groups = [
        {
            id: 2,
            name: 'gpt-5.6',
            items: [
                { channel_id: 10, model_name: 'gpt-5.6' },
                { channel_id: 10, model_name: 'gpt-5.6-thinking' },
                { channel_id: 99, model_name: 'other' },
            ],
        },
        {
            id: 1,
            name: 'claude',
            items: [{ channel_id: 10, model_name: 'claude-sonnet' }],
        },
        {
            id: 3,
            name: 'empty-for-me',
            items: [{ channel_id: 99, model_name: 'x' }],
        },
    ];

    const result = collectChannelRouteGroups(groups, 10);
    assert.deepEqual(result, [
        {
            groupId: 1,
            groupName: 'claude',
            models: ['claude-sonnet'],
        },
        {
            groupId: 2,
            groupName: 'gpt-5.6',
            models: ['gpt-5.6', 'gpt-5.6-thinking'],
        },
    ]);
});

test('collectChannelRouteGroups dedupes models and skips groups without id', () => {
    const groups = [
        {
            name: 'no-id',
            items: [{ channel_id: 1, model_name: 'm1' }],
        },
        {
            id: 5,
            name: 'dup',
            items: [
                { channel_id: 1, model_name: 'm1' },
                { channel_id: 1, model_name: ' m1 ' },
                { channel_id: 1, model_name: 'm2' },
                { channel_id: 1, model_name: '' },
            ],
        },
    ];

    const result = collectChannelRouteGroups(groups, 1);
    assert.deepEqual(result, [
        {
            groupId: 5,
            groupName: 'dup',
            models: ['m1', 'm2'],
        },
    ]);
});
