'use client';

import type { ReactNode } from 'react';
import { useMemo, useState } from 'react';
import { ArrowDownUp, KeyRound, Layers3, Radio, Waves, TrendingUp } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useAnalyticsUtilization, type AnalyticsRange, type AnalyticsProviderBreakdownItem, type AnalyticsModelBreakdownItem, type AnalyticsAPIKeyBreakdownItem } from '@/api/endpoints/analytics';
import { useStatsDaily, useStatsHourly } from '@/api/endpoints/stats';
import { formatCount, formatMoney, cn } from '@/lib/utils';
import { ObservatorySection, QueryState, StatusBadge, formatPercent } from './shared';
import { useAnalyticsCacheTtl } from './cache-context';
import { useBreakdownSort, type AnalyticsSortKey } from './use-sort';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart';
import { Bar, BarChart, CartesianGrid, XAxis, YAxis, Area, AreaChart } from 'recharts';

type BreakdownItem = AnalyticsProviderBreakdownItem | AnalyticsModelBreakdownItem | AnalyticsAPIKeyBreakdownItem;

const SORT_OPTIONS: AnalyticsSortKey[] = ['requests', 'success_rate', 'cost'];

function BreakdownCard({
    title,
    icon: Icon,
    items,
    getName,
    getMeta,
    noBillingHint,
}: {
    title: string;
    icon: typeof Radio;
    items: BreakdownItem[];
    getName: (item: BreakdownItem) => string;
    getMeta?: (item: BreakdownItem) => ReactNode;
    noBillingHint?: string;
}) {
    const t = useTranslations('analytics');
    const { sorted, sortKey, setSortKey, sortOrder, toggleOrder } = useBreakdownSort(items);
    const allCostZero = items.length > 0 && items.every((item) => item.total_cost === 0);
    const sortLabelKeys: Record<AnalyticsSortKey, string> = {
        requests: 'sort.byRequests',
        success_rate: 'sort.bySuccessRate',
        cost: 'sort.byCost',
    };
    return (
        <article className="rounded-lg border border-border/30 bg-card p-4 shadow-sm ">
            <div className="mb-4 flex items-center justify-between gap-2">
                <div className="flex items-center gap-3">
                    <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 text-primary shadow-sm">
                        <Icon className="h-4 w-4" />
                    </div>
                    <h4 className="text-sm font-semibold">{title}</h4>
                </div>
                <div className="flex items-center gap-1">
                    <Select value={sortKey} onValueChange={(v) => setSortKey(v as AnalyticsSortKey)}>
                        <SelectTrigger className="h-6 w-auto rounded-md border-border/50 bg-background px-1.5 text-[11px]">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {SORT_OPTIONS.map((opt) => (
                                <SelectItem key={opt} value={opt} className="text-xs">
                                    {t(sortLabelKeys[opt])}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                    <button
                        type="button"
                        onClick={toggleOrder}
                        title={sortOrder === 'desc' ? t('sort.descending') : t('sort.ascending')}
                        className="flex h-6 w-6 items-center justify-center rounded-md border border-border/50 bg-background text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                    >
                        <ArrowDownUp className={`size-3 ${sortOrder === 'asc' ? 'rotate-180' : ''}`} />
                    </button>
                </div>
            </div>

            <div className="max-h-80 space-y-3 overflow-y-auto pr-1">
                {sorted.map((item) => (
                    <div key={`${title}-${getName(item)}`} className="rounded-lg border border-border/25 bg-card px-3 py-3 shadow-sm">
                        <div className="flex items-start justify-between gap-3">
                            <div className="min-w-0">
                                <div className="truncate text-sm font-medium">{getName(item)}</div>
                                <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                                    <span>{formatCount(item.request_count).formatted.value}{formatCount(item.request_count).formatted.unit}</span>
                                    <span>{formatPercent(item.success_rate).formatted.value}%</span>
                                    {getMeta ? getMeta(item) : null}
                                </div>
                            </div>
                            <div className="shrink-0 text-right">
                                <div className="text-sm font-semibold">
                                    {formatMoney(item.total_cost).formatted.value}
                                    <span className="ml-0.5 text-xs text-muted-foreground">{formatMoney(item.total_cost).formatted.unit}</span>
                                </div>
                                <div className="mt-1 text-xs text-muted-foreground">
                                    {formatCount(item.total_tokens).formatted.value}
                                    <span className="ml-0.5">{formatCount(item.total_tokens).formatted.unit}</span>
                                </div>
                            </div>
                        </div>
                    </div>
                ))}
            </div>

            {allCostZero && noBillingHint ? (
                <p className="mt-3 rounded-lg border border-amber-500/15 bg-amber-500/6 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
                    {noBillingHint}
                </p>
            ) : null}
        </article>
    );
}

export function Utilization({ range }: { range: AnalyticsRange }) {
    const t = useTranslations('analytics');
    const cacheTtl = useAnalyticsCacheTtl();
    const { data, isLoading, error } = useAnalyticsUtilization(range, cacheTtl);
    const { data: dailyStats } = useStatsDaily();
    const is1d = range === '1d';
    const isToday = range === 'today';
    // 1d/today 用小时桶：1d 走滚动 24 小时窗口（?window=24h，后端昨天剩余小时 +
    // 今天已过小时，与缓存页口径一致）；today 走缺省今日口径（今日 0 点起）。
    const isHourly = is1d || isToday;
    const { data: hourlyStats } = useStatsHourly(is1d ? { window: '24h' } : undefined);

    const analyticsT = useTranslations('analytics');
    const rangeLabel = analyticsT(`range.${range}`);
    // 严格参考缓存页的标题规则：
    // - 1天 → "24 小时趋势"
    // - 其他 → "7天 趋势"
    // 说明行固定为“按时间桶查看趋势变化”
    const trendTitle = is1d ? '24 小时趋势' : `${rangeLabel} 趋势`;
    const trendDesc = '按时间桶查看趋势变化';

    const isEmpty =
        !data ||
        (
            data.provider_breakdown.length === 0 &&
            data.model_breakdown.length === 0 &&
            data.apikey_breakdown.length === 0
        );

    // 选择数据源：1d/today 用小时桶（1d 24 小时 / today 今日），其他用天桶
    const rawSource = isHourly ? (hourlyStats ?? []) : (dailyStats ?? []);
    const hasSourceData = rawSource.length > 0;

    // 统计类型：调用次数 / Token用量 / 成本
    const [metric, setMetric] = useState<'requests' | 'tokens' | 'cost'>('requests');

    // 图表类型：柱状图 / 面积图（右上角切换，参考 new-api 风格）
    const [chartType, setChartType] = useState<'bar' | 'area'>('bar');

    const metricInfo = {
        requests: { label: '调用次数', key: 'value' },
        tokens: { label: 'Token 用量', key: 'value' },
        cost: { label: '成本', key: 'value' },
    }[metric];

    const trendChartData = useMemo(() => {
        if (rawSource.length === 0) return [];
        // 1d/today 取最近 24 点（小时桶），其它按 range 切片。
        // ytd / all 不再硬编码 30 天，要求显示更多历史数据。
        let sliced: any[] = rawSource;
        if (isHourly) {
            sliced = rawSource.slice(-24);
        } else if (range === '7d') {
            sliced = rawSource.slice(-7);
        } else if (range === '30d') {
            sliced = rawSource.slice(-30);
        } else if (range === '90d') {
            sliced = rawSource.slice(-90);
        } else if (range === 'ytd') {
            // 年初至今：从当前年 01-01 开始的所有天数据。
            // daily 的 date 是 YYYYMMDD 字符串，可以字面序比较。
            const now = new Date();
            const year = now.getFullYear();
            const ytdStart = `${year}0101`;
            const startIdx = rawSource.findIndex((d: any) => String(d.date || '') >= ytdStart);
            if (startIdx >= 0) {
                sliced = rawSource.slice(startIdx);
            } else {
                // 没有找到当年数据，退回最近 366 天（与后端 all 窗口保持一致）
                sliced = rawSource.slice(-366);
            }
        } else if (range === 'all') {
            // 全部：为了图表可读性与缓存页保持一致，最多展示最近 366 天。
            // 如果数据少于 366 天就全部展示。
            sliced = rawSource.slice(-366);
        }

        return sliced.map((d: any) => {
            const reqRaw = d.request_count?.raw ?? 0;
            const inputRaw = d.input_token?.raw ?? 0;
            const outputRaw = d.output_token?.raw ?? 0;
            const costRaw = d.total_cost?.raw ?? 0;

            let value = 0;
            if (metric === 'requests') value = reqRaw;
            else if (metric === 'tokens') value = inputRaw + outputRaw;
            else value = costRaw;

            let timeLabel = '';
            if (isHourly) {
                const h = d.hour ?? 0;
                const hourLabel = `${String(h).padStart(2, '0')}:00`;
                // 滚动 24h 窗口跨两天：昨天的小时桶加 月/日 前缀（与缓存页趋势标签一致）。
                // 序列末尾恒为今天当前小时，date 小于末尾 date 的即为昨天，避免依赖浏览器本地时区。
                const lastDate = String(sliced[sliced.length - 1]?.date ?? '');
                const pointDate = String(d.date ?? '');
                if (/^\d{8}$/.test(pointDate) && /^\d{8}$/.test(lastDate) && pointDate < lastDate) {
                    timeLabel = `${pointDate.slice(4, 6)}/${pointDate.slice(6, 8)} ${hourLabel}`;
                } else {
                    timeLabel = hourLabel;
                }
            } else {
                timeLabel = d.date ?? '';
                if (/^\d{8}$/.test(timeLabel)) {
                    timeLabel = `${timeLabel.slice(4, 6)}/${timeLabel.slice(6, 8)}`;
                }
            }

            return {
                time: timeLabel,
                value,
            };
        });
    }, [rawSource, isHourly, range, metric]);

    const hasTrend = trendChartData.some((p) => (p.value ?? 0) > 0);

    return (
        <ObservatorySection
            title={t('cards.utilization.title')}
            description={t('utilization.description')}
            icon={Waves}
        >
            <QueryState
                loading={isLoading}
                error={error}
                empty={isEmpty}
                emptyLabel={isLoading ? t('states.loading') : t('utilization.empty')}
            >
                                <div className="space-y-4">
                    {/* 用量趋势 - 参考缓存页 + new-api 风格（置于统计分解卡片之上） */}
                    {hasSourceData && (
                        <article className="min-w-0 rounded-xl border border-border/60 bg-card p-4">
                            <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
                                <div>
                                    <h4 className="text-sm font-semibold">{trendTitle}</h4>
                                    <p className="mt-0.5 text-sm leading-6 text-muted-foreground">{trendDesc}</p>
                                </div>
                                {/* 图表类型切换：柱状图 / 面积图 - 右上角，参考 new-api */}
                                <div className="inline-flex rounded-lg border border-border/50 bg-muted/30 p-0.5 text-xs self-start">
                                    <button
                                        type="button"
                                        onClick={() => setChartType('bar')}
                                        className={cn(
                                            'rounded-md px-3 py-1 transition-colors',
                                            chartType === 'bar' ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'
                                        )}
                                    >
                                        柱状图
                                    </button>
                                    <button
                                        type="button"
                                        onClick={() => setChartType('area')}
                                        className={cn(
                                            'rounded-md px-3 py-1 transition-colors',
                                            chartType === 'area' ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'
                                        )}
                                    >
                                        面积图
                                    </button>
                                </div>
                            </div>

                            {/* 统计类型切换：调用次数 / Token 用量 / 成本 */}
                            <div className="mb-2 inline-flex rounded-lg border border-border/50 bg-muted/30 p-0.5 text-xs">
                                {[ 
                                    { key: 'requests' as const, label: '调用次数' },
                                    { key: 'tokens' as const, label: 'Token 用量' },
                                    { key: 'cost' as const, label: '成本' },
                                ].map((m) => (
                                    <button
                                        key={m.key}
                                        type="button"
                                        onClick={() => setMetric(m.key)}
                                        className={cn(
                                            'rounded-md px-3 py-1 transition-colors',
                                            metric === m.key ? 'bg-card text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'
                                        )}
                                    >
                                        {m.label}
                                    </button>
                                ))}
                            </div>

                            {!hasTrend ? (
                                <div className="rounded-lg border border-dashed border-border/40 px-4 py-6 text-center text-sm text-muted-foreground">
                                    {is1d ? '近24小时暂无用量数据。' : isToday ? '今日暂无用量数据。' : '所选时间范围内暂无用量数据。'}
                                </div>
                            ) : (
                                <ChartContainer
                                    config={{
                                        value: { label: metricInfo.label, color: 'var(--chart-1)' },
                                    }}
                                    className="h-[16rem] w-full"
                                >
                                    {chartType === 'bar' ? (
                                        <BarChart data={trendChartData} margin={{ top: 16, right: 8, bottom: 0, left: 0 }}>
                                            <CartesianGrid strokeDasharray="3 3" vertical={false} />
                                            <XAxis
                                                dataKey="time"
                                                tickLine={false}
                                                axisLine={false}
                                                tick={{ fontSize: 11 }}
                                            />
                                            <YAxis
                                                tickLine={false}
                                                axisLine={false}
                                                tick={{ fontSize: 11 }}
                                                tickFormatter={(v: number) => {
                                                    const f = formatCount(v);
                                                    return f.formatted.value + f.formatted.unit;
                                                }}
                                            />
                                            <ChartTooltip
                                                cursor={{ fill: 'hsl(var(--foreground) / 0.06)' }}
                                                content={
                                                    <ChartTooltipContent
                                                        indicator="dot"
                                                        formatter={(value) => (
                                                            <div className="flex items-center justify-between gap-4">
                                                                <span className="text-muted-foreground">{metricInfo.label}</span>
                                                                <span className="font-mono font-medium tabular-nums">
                                                                    {metric === 'cost'
                                                                        ? formatMoney(value as number).formatted.value + formatMoney(value as number).formatted.unit
                                                                        : formatCount(value as number).formatted.value + formatCount(value as number).formatted.unit}
                                                                </span>
                                                            </div>
                                                        )}
                                                    />
                                                }
                                            />
                                            <Bar
                                                dataKey="value"
                                                fill="var(--color-value)"
                                                radius={[4, 4, 0, 0]}
                                                maxBarSize={48}
                                            />
                                        </BarChart>
                                    ) : (
                                        <AreaChart data={trendChartData} margin={{ top: 16, right: 8, bottom: 0, left: 0 }}>
                                            <CartesianGrid strokeDasharray="3 3" vertical={false} />
                                            <XAxis
                                                dataKey="time"
                                                tickLine={false}
                                                axisLine={false}
                                                tick={{ fontSize: 11 }}
                                            />
                                            <YAxis
                                                tickLine={false}
                                                axisLine={false}
                                                tick={{ fontSize: 11 }}
                                                tickFormatter={(v: number) => {
                                                    const f = formatCount(v);
                                                    return f.formatted.value + f.formatted.unit;
                                                }}
                                            />
                                            <ChartTooltip
                                                cursor={{ fill: 'hsl(var(--foreground) / 0.06)' }}
                                                content={
                                                    <ChartTooltipContent
                                                        indicator="dot"
                                                        formatter={(value) => (
                                                            <div className="flex items-center justify-between gap-4">
                                                                <span className="text-muted-foreground">{metricInfo.label}</span>
                                                                <span className="font-mono font-medium tabular-nums">
                                                                    {metric === 'cost'
                                                                        ? formatMoney(value as number).formatted.value + formatMoney(value as number).formatted.unit
                                                                        : formatCount(value as number).formatted.value + formatCount(value as number).formatted.unit}
                                                                </span>
                                                            </div>
                                                        )}
                                                    />
                                                }
                                            />
                                            <Area
                                                type="natural"
                                                dataKey="value"
                                                stroke="var(--color-value)"
                                                fill="var(--color-value)"
                                                fillOpacity={0.25}
                                            />
                                        </AreaChart>
                                    )}
                                </ChartContainer>
                            )}
                        </article>
                    )}

                    <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
                        <BreakdownCard
                            title={t('utilization.providers')}
                            icon={Radio}
                            items={data?.provider_breakdown ?? []}
                            getName={(item) => (item as AnalyticsProviderBreakdownItem).channel_name}
                            getMeta={(item) => {
                                const provider = item as AnalyticsProviderBreakdownItem;
                                return (
                                    <StatusBadge
                                        label={provider.enabled ? t('utilization.enabled') : t('utilization.disabled')}
                                        tone={provider.enabled ? 'success' : 'neutral'}
                                    />
                                );
                            }}
                            noBillingHint={t('utilization.noBilling')}
                        />
                        <BreakdownCard
                            title={t('utilization.models')}
                            icon={Layers3}
                            items={data?.model_breakdown ?? []}
                            getName={(item) => (item as AnalyticsModelBreakdownItem).model_name}
                            noBillingHint={t('utilization.noBilling')}
                        />
                        <BreakdownCard
                            title={t('utilization.apikeys')}
                            icon={KeyRound}
                            items={data?.apikey_breakdown ?? []}
                            getName={(item) => (item as AnalyticsAPIKeyBreakdownItem).name}
                            noBillingHint={t('utilization.noBilling')}
                        />
                    </div>
                </div>
                
            </QueryState>
        </ObservatorySection>
    );
}
