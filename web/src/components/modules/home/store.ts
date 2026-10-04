'use client';

import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';

export type RankSortMode = 'cost' | 'count' | 'tokens' | 'key-usage';
export type ChartMetricType = 'cost' | 'count' | 'tokens' | 'success-rate';
export type ChartPeriod = '1' | '7' | '30';
export type OverviewRange = '7d' | '30d' | '90d' | 'all';
export type OverviewCountMode = 'active' | 'all';
export type StatsRefreshInterval = '5s' | '10s' | '15s' | '30s' | '60s' | 'off';

 const RANK_SORT_MODES: readonly RankSortMode[] = ['cost', 'count', 'tokens', 'key-usage'];
 const CHART_METRIC_TYPES: readonly ChartMetricType[] = ['cost', 'count', 'tokens', 'success-rate'];
 const CHART_PERIODS: readonly ChartPeriod[] = ['1', '7', '30'];
 const OVERVIEW_RANGES: readonly OverviewRange[] = ['7d', '30d', '90d', 'all'];
const STATS_REFRESH_INTERVALS: readonly StatsRefreshInterval[] = ['5s', '10s', '15s', '30s', '60s', 'off'];

/** 首页统计自动刷新间隔可选项（用于 UI 渲染）。 */
export const STATS_REFRESH_INTERVAL_OPTIONS: readonly StatsRefreshInterval[] = STATS_REFRESH_INTERVALS;

/** 将刷新间隔转换为毫秒数；off 表示关闭自动刷新（返回 false 让 refetchInterval 停止轮询）。 */
export function statsRefreshIntervalMs(value: StatsRefreshInterval): number | false {
    if (value === 'off') return false;
    const seconds = Number(value.slice(0, -1));
    return Number.isFinite(seconds) ? seconds * 1000 : false;
}

function normalizeRankSortMode(value: string | null | undefined): RankSortMode {
    return RANK_SORT_MODES.includes(value as RankSortMode) ? (value as RankSortMode) : 'cost';
}

function normalizeChartMetricType(value: string | null | undefined): ChartMetricType {
    return CHART_METRIC_TYPES.includes(value as ChartMetricType) ? (value as ChartMetricType) : 'cost';
}

function normalizeChartMetrics(value: unknown, fallback: ChartMetricType[] = ['cost']): ChartMetricType[] {
    const seen = new Set<ChartMetricType>();
    const result: ChartMetricType[] = [];
    if (Array.isArray(value)) {
        for (const item of value) {
            const key = item as ChartMetricType;
            if (
                typeof item === 'string' &&
                (CHART_METRIC_TYPES as readonly string[]).includes(item) &&
                !seen.has(key)
            ) {
                seen.add(key);
                result.push(key);
            }
        }
    }
    return result.length > 0 ? result : [...fallback];
}

function normalizeChartPeriod(value: string | null | undefined): ChartPeriod {
    return CHART_PERIODS.includes(value as ChartPeriod) ? (value as ChartPeriod) : '1';
}

export function normalizeOverviewRange(value: string | null | undefined): OverviewRange {
    return OVERVIEW_RANGES.includes(value as OverviewRange) ? (value as OverviewRange) : '7d';
}

const OVERVIEW_COUNT_MODES: readonly OverviewCountMode[] = ['active', 'all'];

export function normalizeOverviewCountMode(value: string | null | undefined): OverviewCountMode {
    return OVERVIEW_COUNT_MODES.includes(value as OverviewCountMode) ? (value as OverviewCountMode) : 'active';
}

function normalizeStatsRefreshInterval(value: string | null | undefined): StatsRefreshInterval {
    return STATS_REFRESH_INTERVALS.includes(value as StatsRefreshInterval)
        ? (value as StatsRefreshInterval)
        : '30s';
}

// 首页 analytics 概览的 8 个指标卡，用户可显隐与排序。
export type OverviewMetricKey =
    | 'requestCount'
    | 'successRate'
    | 'totalTokens'
    | 'totalCost'
    | 'providerCount'
    | 'apiKeyCount'
    | 'modelCount'
    | 'fallbackRate';

export const OVERVIEW_METRIC_KEYS: readonly OverviewMetricKey[] = [
    'requestCount',
    'successRate',
    'totalTokens',
    'totalCost',
    'providerCount',
    'apiKeyCount',
    'modelCount',
    'fallbackRate',
];

export function normalizeOverviewMetricOrder(value: unknown): OverviewMetricKey[] {
    const seen = new Set<OverviewMetricKey>();
    const result: OverviewMetricKey[] = [];
    if (Array.isArray(value)) {
        for (const item of value) {
            const key = item as OverviewMetricKey;
            if (
                typeof item === 'string' &&
                (OVERVIEW_METRIC_KEYS as readonly string[]).includes(item) &&
                !seen.has(key)
            ) {
                seen.add(key);
                result.push(key);
            }
        }
    }
    // 补全缺失或新增的指标，保证顺序数组始终覆盖全集。
    for (const key of OVERVIEW_METRIC_KEYS) {
        if (!seen.has(key)) result.push(key);
    }
    return result;
}

function normalizeOverviewHiddenMetrics(value: unknown): OverviewMetricKey[] {
    if (!Array.isArray(value)) return [];
    const result: OverviewMetricKey[] = [];
    for (const item of value) {
        const key = item as OverviewMetricKey;
        if (
            typeof item === 'string' &&
            (OVERVIEW_METRIC_KEYS as readonly string[]).includes(item) &&
            !result.includes(key)
        ) {
            result.push(key);
        }
    }
    return result;
}

interface HomeViewState {
    rankSortMode: RankSortMode;
    chartMetricType: ChartMetricType;
    chartMetrics: ChartMetricType[];
    chartPeriod: ChartPeriod;
    overviewRange: OverviewRange;
    overviewMetricOrder: OverviewMetricKey[];
    overviewHiddenMetrics: OverviewMetricKey[];
    overviewCountMode: OverviewCountMode;
    statsRefreshInterval: StatsRefreshInterval;
    setRankSortMode: (value: RankSortMode) => void;
    setChartMetricType: (value: ChartMetricType) => void;
    toggleChartMetric: (value: ChartMetricType) => void;
    setChartPeriod: (value: ChartPeriod) => void;
    setOverviewRange: (value: OverviewRange) => void;
    setOverviewCountMode: (value: OverviewCountMode) => void;
    setOverviewMetricHidden: (key: OverviewMetricKey, hidden: boolean) => void;
    moveOverviewMetric: (key: OverviewMetricKey, direction: 'up' | 'down') => void;
    resetOverviewMetrics: () => void;
    setStatsRefreshInterval: (value: StatsRefreshInterval) => void;
}

export const useHomeViewStore = create<HomeViewState>()(
    persist(
        (set) => ({
            rankSortMode: 'cost',
            chartMetricType: 'cost',
            chartMetrics: ['cost'],
            chartPeriod: '1',
            overviewRange: '7d',
            overviewMetricOrder: [...OVERVIEW_METRIC_KEYS],
            overviewHiddenMetrics: [],
            overviewCountMode: 'active',
            statsRefreshInterval: '30s',
            setRankSortMode: (value) => set({ rankSortMode: normalizeRankSortMode(value) }),
            setChartMetricType: (value) => set({
                chartMetricType: normalizeChartMetricType(value),
                chartMetrics: [normalizeChartMetricType(value)],
            }),
            toggleChartMetric: (value) => set({
                chartMetricType: value,
                chartMetrics: [value],
            }),
            setChartPeriod: (value) => set({ chartPeriod: normalizeChartPeriod(value) }),
            setOverviewRange: (value) => set({ overviewRange: normalizeOverviewRange(value) }),
            setOverviewCountMode: (value) => set({ overviewCountMode: normalizeOverviewCountMode(value) }),
            setStatsRefreshInterval: (value) =>
                set({ statsRefreshInterval: normalizeStatsRefreshInterval(value) }),
            setOverviewMetricHidden: (key, hidden) => set((state) => {
                const hiddenNow = state.overviewHiddenMetrics;
                const alreadyHidden = hiddenNow.includes(key);
                if (hidden) {
                    if (alreadyHidden) return {};
                    // 至少保留一个可见指标，避免概览区被清空。
                    const visibleCount = state.overviewMetricOrder.length - hiddenNow.length;
                    if (visibleCount <= 1) return {};
                    return { overviewHiddenMetrics: [...hiddenNow, key] };
                }
                if (!alreadyHidden) return {};
                return { overviewHiddenMetrics: hiddenNow.filter((k) => k !== key) };
            }),
            moveOverviewMetric: (key, direction) => set((state) => {
                const order = [...state.overviewMetricOrder];
                const idx = order.indexOf(key);
                if (idx < 0) return {};
                const target = direction === 'up' ? idx - 1 : idx + 1;
                if (target < 0 || target >= order.length) return {};
                [order[idx], order[target]] = [order[target], order[idx]];
                return { overviewMetricOrder: order };
            }),
            resetOverviewMetrics: () =>
                set({ overviewMetricOrder: [...OVERVIEW_METRIC_KEYS], overviewHiddenMetrics: [] }),
        }),
        {
            name: 'home-view-options-storage-v3',
            storage: createJSONStorage(() => localStorage),
            partialize: (state) => ({
                rankSortMode: state.rankSortMode,
                chartMetricType: state.chartMetricType,
                chartMetrics: state.chartMetrics,
                chartPeriod: state.chartPeriod,
                overviewRange: state.overviewRange,
                overviewMetricOrder: state.overviewMetricOrder,
                overviewHiddenMetrics: state.overviewHiddenMetrics,
                overviewCountMode: state.overviewCountMode,
                statsRefreshInterval: state.statsRefreshInterval,
            }),
            merge: (persistedState, currentState) => {
                const typed = (persistedState as Partial<HomeViewState> | null) ?? null;
                const legacyMetric = normalizeChartMetricType(typed?.chartMetricType);
                const metrics = normalizeChartMetrics(typed?.chartMetrics, [legacyMetric]);
                return {
                    ...currentState,
                    ...typed,
                    rankSortMode: normalizeRankSortMode(typed?.rankSortMode),
                    chartMetricType: metrics[0] ?? legacyMetric,
                    chartMetrics: metrics,
                    chartPeriod: normalizeChartPeriod(typed?.chartPeriod),
                    overviewRange: normalizeOverviewRange(typed?.overviewRange),
                    overviewMetricOrder: normalizeOverviewMetricOrder(typed?.overviewMetricOrder),
                    overviewHiddenMetrics: normalizeOverviewHiddenMetrics(typed?.overviewHiddenMetrics),
                    overviewCountMode: normalizeOverviewCountMode(typed?.overviewCountMode),
                    statsRefreshInterval: normalizeStatsRefreshInterval(typed?.statsRefreshInterval),
                };
            },
        }
    )
);

/**
 * 读取首页统计自动刷新间隔，返回 TanStack Query 的 refetchInterval 可直接消费的值。
 * off → false（关闭轮询）；其余 → 毫秒数。
 */
export function useHomeStatsRefreshMs(): number | false {
    return useHomeViewStore((state) => statsRefreshIntervalMs(state.statsRefreshInterval));
}
