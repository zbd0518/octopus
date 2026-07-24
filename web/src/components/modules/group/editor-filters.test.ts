import assert from 'node:assert/strict';
import test from 'node:test';
import {
    buildChannelEnabledMap,
    countDisabledMembers,
    filterMatchedForAutoAdd,
    filterModelChannelsForPicker,
    syncMembersChannelEnabled,
} from './editor-filters.ts';

type LLMChannel = {
    name: string;
    enabled: boolean;
    channel_id: number;
    channel_name: string;
};

function mc(partial: Partial<LLMChannel> & Pick<LLMChannel, 'name' | 'channel_id' | 'channel_name'>): LLMChannel {
    return {
        enabled: true,
        ...partial,
    };
}

test('buildChannelEnabledMap keeps last enabled flag per channel', () => {
    const map = buildChannelEnabledMap([
        mc({ name: 'gpt-4', channel_id: 1, channel_name: 'A', enabled: true }),
        mc({ name: 'gpt-4o', channel_id: 1, channel_name: 'A', enabled: false }),
        mc({ name: 'claude', channel_id: 2, channel_name: 'B', enabled: true }),
    ]);
    assert.equal(map.get(1), false);
    assert.equal(map.get(2), true);
});

test('filterModelChannelsForPicker hides disabled by default', () => {
    const list = [
        mc({ name: 'gpt-4', channel_id: 1, channel_name: 'A', enabled: true }),
        mc({ name: 'gpt-4', channel_id: 2, channel_name: 'B', enabled: false }),
    ];
    const filtered = filterModelChannelsForPicker(list, {
        showDisabled: false,
        channelGroupId: null,
        groupIdByChannelId: new Map(),
    });
    assert.deepEqual(filtered.map((x) => x.channel_id), [1]);
});

test('filterModelChannelsForPicker can show disabled and filter by channel group', () => {
    const list = [
        mc({ name: 'm1', channel_id: 1, channel_name: 'A', enabled: false }),
        mc({ name: 'm2', channel_id: 2, channel_name: 'B', enabled: true }),
        mc({ name: 'm3', channel_id: 3, channel_name: 'C', enabled: true }),
    ];
    const groupIdByChannelId = new Map([
        [1, 10],
        [2, 10],
        [3, 20],
    ]);
    const filtered = filterModelChannelsForPicker(list, {
        showDisabled: true,
        channelGroupId: 10,
        groupIdByChannelId,
    });
    assert.deepEqual(filtered.map((x) => x.channel_id).sort(), [1, 2]);
});

test('filterMatchedForAutoAdd skips disabled unless showDisabled', () => {
    const matched = [
        mc({ name: 'a', channel_id: 1, channel_name: 'A', enabled: true }),
        mc({ name: 'b', channel_id: 2, channel_name: 'B', enabled: false }),
    ];
    assert.equal(filterMatchedForAutoAdd(matched, false).length, 1);
    assert.equal(filterMatchedForAutoAdd(matched, true).length, 2);
});

test('syncMembersChannelEnabled refreshes enabled from latest channel list', () => {
    const members = [
        { id: '1:gpt-4', channel_id: 1, name: 'gpt-4', channel_name: 'Old', enabled: true, weight: 1 },
        { id: '2:claude', channel_id: 2, name: 'claude', channel_name: 'B', enabled: true, weight: 1 },
    ];
    const next = syncMembersChannelEnabled(members, [
        mc({ name: 'gpt-4', channel_id: 1, channel_name: 'A', enabled: false }),
        mc({ name: 'claude', channel_id: 2, channel_name: 'B', enabled: true }),
    ]);
    assert.equal(next[0].enabled, false);
    assert.equal(next[0].channel_name, 'A');
    assert.equal(next[1].enabled, true);
    assert.equal(countDisabledMembers(next), 1);
});

test('syncMembersChannelEnabled refreshes upstream_metrics for same model row', () => {
    const oldMetrics = { latency_ms: 100, avg_tps: 10, success_rate: 0.95 };
    const newMetrics = { latency_ms: 200, avg_tps: 5, success_rate: 0.5 };
    const members = [
        {
            id: '1:gpt-4',
            channel_id: 1,
            name: 'gpt-4',
            channel_name: 'A',
            enabled: true,
            weight: 1,
            upstream_metrics: oldMetrics,
        },
    ];
    const next = syncMembersChannelEnabled(members, [
        {
            name: 'gpt-4',
            channel_id: 1,
            channel_name: 'A',
            enabled: true,
            upstream_metrics: newMetrics,
        },
    ]);
    assert.notEqual(next, members);
    assert.equal(next[0].upstream_metrics, newMetrics);
    assert.equal(next[0].upstream_metrics?.success_rate, 0.5);
});

test('syncMembersChannelEnabled returns same reference when unchanged', () => {
    const metrics = { latency_ms: 100, avg_tps: 10, success_rate: 0.95 };
    const members = [
        {
            id: '1:gpt-4',
            channel_id: 1,
            name: 'gpt-4',
            channel_name: 'A',
            enabled: true,
            weight: 1,
            upstream_metrics: metrics,
        },
    ];
    const next = syncMembersChannelEnabled(members, [
        {
            name: 'gpt-4',
            channel_id: 1,
            channel_name: 'A',
            enabled: true,
            upstream_metrics: metrics,
        },
    ]);
    assert.equal(next, members);
});
