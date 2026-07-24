import assert from 'node:assert/strict';
import test from 'node:test';
import {
    buildChannelHasEnabledKeyMap,
    channelHasNoEnabledKey,
    countMemberRisks,
    hasEnabledChannelKey,
    healthRiskLevel,
    memberRiskFlags,
} from './editor-member-status.ts';

test('hasEnabledChannelKey requires enabled non-empty key', () => {
    assert.equal(hasEnabledChannelKey(undefined), false);
    assert.equal(hasEnabledChannelKey([]), false);
    assert.equal(hasEnabledChannelKey([{ enabled: false, channel_key: 'sk-x' }]), false);
    assert.equal(hasEnabledChannelKey([{ enabled: true, channel_key: '   ' }]), false);
    assert.equal(hasEnabledChannelKey([{ enabled: true, channel_key: 'sk-x' }]), true);
});

test('buildChannelHasEnabledKeyMap maps by channel id', () => {
    const map = buildChannelHasEnabledKeyMap([
        { id: 1, keys: [{ enabled: true, channel_key: 'sk-a' }] },
        { id: 2, keys: [{ enabled: false, channel_key: 'sk-b' }] },
        { id: 3, keys: null },
    ]);
    assert.equal(map.get(1), true);
    assert.equal(map.get(2), false);
    assert.equal(map.get(3), false);
});

test('healthRiskLevel matches StatusBars thresholds', () => {
    assert.equal(healthRiskLevel(undefined), 'none');
    assert.equal(healthRiskLevel(0), 'none');
    assert.equal(healthRiskLevel(0.95), 'none');
    assert.equal(healthRiskLevel(0.9), 'none');
    assert.equal(healthRiskLevel(0.8), 'moderate');
    assert.equal(healthRiskLevel(0.7), 'moderate');
    assert.equal(healthRiskLevel(0.69), 'low');
    assert.equal(healthRiskLevel(0.1), 'low');
    assert.equal(healthRiskLevel(Number.NaN), 'none');
    assert.equal(healthRiskLevel(Number.POSITIVE_INFINITY), 'none');
    assert.equal(healthRiskLevel(1.5), 'none');
});

test('channelHasNoEnabledKey ignores unknown channels', () => {
    const map = buildChannelHasEnabledKeyMap([{ id: 1, keys: [] }]);
    assert.equal(channelHasNoEnabledKey(1, map), true);
    assert.equal(channelHasNoEnabledKey(99, map), false);
});

test('memberRiskFlags and countMemberRisks aggregate three signals', () => {
    const map = buildChannelHasEnabledKeyMap([
        { id: 1, keys: [] },
        { id: 2, keys: [{ enabled: true, channel_key: 'sk' }] },
        { id: 3, keys: [{ enabled: true, channel_key: 'sk' }] },
    ]);
    const members = [
        { enabled: false, channel_id: 1, upstream_metrics: { success_rate: 0.95 } },
        { enabled: true, channel_id: 2, upstream_metrics: { success_rate: 0.5 } },
        { enabled: true, channel_id: 3, upstream_metrics: { success_rate: 0.75 } },
        { enabled: true, channel_id: 99, upstream_metrics: null },
    ];

    assert.deepEqual(memberRiskFlags(members[0], map), {
        channelDisabled: true,
        noEnabledKey: true,
        healthRisk: 'none',
    });
    assert.deepEqual(memberRiskFlags(members[1], map), {
        channelDisabled: false,
        noEnabledKey: false,
        healthRisk: 'low',
    });
    assert.deepEqual(memberRiskFlags(members[2], map), {
        channelDisabled: false,
        noEnabledKey: false,
        healthRisk: 'moderate',
    });
    assert.deepEqual(memberRiskFlags(members[3], map), {
        channelDisabled: false,
        noEnabledKey: false,
        healthRisk: 'none',
    });

    assert.deepEqual(countMemberRisks(members, map), {
        disabledCount: 1,
        noKeyCount: 1,
        healthRiskCount: 2,
    });
});
