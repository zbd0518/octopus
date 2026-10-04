import assert from 'node:assert/strict';
import test, { after } from 'node:test';

const storageDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');
Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
});
const { normalizeOverviewRange, normalizeOverviewCountMode, normalizeOverviewMetricOrder, OVERVIEW_METRIC_KEYS, useHomeViewStore, statsRefreshIntervalMs, STATS_REFRESH_INTERVAL_OPTIONS } = await import('./store.ts');
after(() => {
    if (storageDescriptor) Object.defineProperty(globalThis, 'localStorage', storageDescriptor);
    else Reflect.deleteProperty(globalThis, 'localStorage');
});

test('normalizeOverviewRange defaults to 7d when value is invalid', () => {
    assert.equal(normalizeOverviewRange('unexpected'), '7d');
});

test('normalizeOverviewRange preserves supported values', () => {
    assert.equal(normalizeOverviewRange('7d'), '7d');
    assert.equal(normalizeOverviewRange('30d'), '30d');
    assert.equal(normalizeOverviewRange('90d'), '90d');
});

test('statsRefreshIntervalMs converts interval to milliseconds', () => {
    assert.equal(statsRefreshIntervalMs('5s'), 5000);
    assert.equal(statsRefreshIntervalMs('10s'), 10000);
    assert.equal(statsRefreshIntervalMs('15s'), 15000);
    assert.equal(statsRefreshIntervalMs('30s'), 30000);
    assert.equal(statsRefreshIntervalMs('60s'), 60000);
});

test('statsRefreshIntervalMs returns false for off to disable polling', () => {
    assert.equal(statsRefreshIntervalMs('off'), false);
});

test('STATS_REFRESH_INTERVAL_OPTIONS covers 5s 10s 15s 30s 60s off', () => {
    assert.deepEqual([...STATS_REFRESH_INTERVAL_OPTIONS], ['5s', '10s', '15s', '30s', '60s', 'off']);
});

test('normalizeOverviewCountMode defaults to active when value is invalid', () => {
    assert.equal(normalizeOverviewCountMode('unexpected'), 'active');
    assert.equal(normalizeOverviewCountMode(undefined), 'active');
    assert.equal(normalizeOverviewCountMode(null), 'active');
});

test('normalizeOverviewCountMode preserves active and all', () => {
    assert.equal(normalizeOverviewCountMode('active'), 'active');
    assert.equal(normalizeOverviewCountMode('all'), 'all');
});

test('overview metric order preserves valid order and fills missing metrics', () => {
    const preferred = ['totalCost', 'requestCount'] as const;
    assert.deepEqual(normalizeOverviewMetricOrder([...preferred, 'totalCost', 'removed', null]), [
        ...preferred,
        ...OVERVIEW_METRIC_KEYS.filter((key) => !preferred.includes(key as typeof preferred[number])),
    ]);
});

test('home persistence merge normalizes metric order and preserves explicit hidden metrics', () => {
    const merge = useHomeViewStore.persist.getOptions().merge!;
    const current = useHomeViewStore.getState();
    const merged = merge({ overviewMetricOrder: ['totalCost', 'unknown', 'totalCost'], overviewHiddenMetrics: ['totalTokens'] }, current);
    assert.deepEqual(merged.overviewMetricOrder, normalizeOverviewMetricOrder(['totalCost']));
    assert.deepEqual(merged.overviewHiddenMetrics, ['totalTokens']);
    assert.equal(merged.resetOverviewMetrics, current.resetOverviewMetrics);
});

test('overview metric order restores defaults for invalid persisted values', () => {
    for (const value of [undefined, null, [], 'invalid', { requestCount: true }]) {
        assert.deepEqual(normalizeOverviewMetricOrder(value), OVERVIEW_METRIC_KEYS);
    }
});
