'use client';

import { Timer } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useAnalyticsLatencyDistribution, type AnalyticsRange } from '@/api/endpoints/analytics';
import { ObservatorySection, QueryState } from './shared';
import { useAnalyticsCacheTtl } from './cache-context';

function LatencyMetricCard({ label, value, unit }: { label: string; value: number; unit: string }) {
    return (
        <div className="flex flex-col items-center justify-center rounded-lg border border-border/30 bg-card p-3 shadow-sm">
            <div className="text-xs text-muted-foreground">{label}</div>
            <div className="mt-1.5 text-lg font-bold tabular-nums">
                {value}
                <span className="ml-1 text-xs font-normal text-muted-foreground">{unit}</span>
            </div>
        </div>
    );
}

export function LatencyDistribution({ range }: { range: AnalyticsRange }) {
    const t = useTranslations('analytics');
    const cacheTtl = useAnalyticsCacheTtl();
    const { data, isLoading, error } = useAnalyticsLatencyDistribution(range, cacheTtl);

    const maxBucketCount = data ? Math.max(...data.buckets.map((b) => b.count), 1) : 1;

    return (
        <ObservatorySection
            title={t('latency.title')}
            description={t('latency.totalRequests', { count: data?.total_requests ?? 0 })}
            icon={Timer}
        >
            <QueryState
                loading={isLoading}
                error={error}
                empty={!data}
                emptyLabel={isLoading ? t('states.loading') : t('states.empty')}
            >
                <div className="grid gap-4 md:grid-cols-2">
                    {/* 延迟指标 */}
                    <div className="space-y-4">
                        <div>
                            <h4 className="mb-2 text-sm font-medium text-muted-foreground">{t('latency.useTime')}</h4>
                            <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
                                <LatencyMetricCard label={t('latency.avg')} value={data?.avg_ms ?? 0} unit="ms" />
                                <LatencyMetricCard label={t('latency.p50')} value={data?.p50_ms ?? 0} unit="ms" />
                                <LatencyMetricCard label={t('latency.p95')} value={data?.p95_ms ?? 0} unit="ms" />
                                <LatencyMetricCard label={t('latency.p99')} value={data?.p99_ms ?? 0} unit="ms" />
                            </div>
                        </div>
                        <div>
                            <h4 className="mb-2 text-sm font-medium text-muted-foreground">{t('latency.ftut')}</h4>
                            <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
                                <LatencyMetricCard label={t('latency.avg')} value={data?.ftut_avg_ms ?? 0} unit="ms" />
                                <LatencyMetricCard label={t('latency.p50')} value={data?.ftut_p50_ms ?? 0} unit="ms" />
                                <LatencyMetricCard label={t('latency.p95')} value={data?.ftut_p95_ms ?? 0} unit="ms" />
                                <LatencyMetricCard label={t('latency.p99')} value={data?.ftut_p99_ms ?? 0} unit="ms" />
                            </div>
                        </div>
                    </div>

                    {/* 延迟分布直方图 */}
                    <div className="rounded-lg border border-border/30 bg-card p-4 shadow-sm">
                        <h4 className="mb-3 text-sm font-medium text-muted-foreground">{t('latency.histogram')}</h4>
                        <div className="space-y-2">
                            {(data?.buckets ?? []).map((bucket) => {
                                const percentage = (bucket.count / maxBucketCount) * 100;
                                return (
                                    <div key={bucket.label} className="flex items-center gap-3">
                                        <div className="w-20 shrink-0 text-xs text-muted-foreground">{bucket.label}</div>
                                        <div className="flex-1">
                                            <div className="h-5 overflow-hidden rounded bg-muted/50">
                                                <div
                                                    className="h-full rounded bg-primary transition-all"
                                                    style={{ width: `${percentage}%` }}
                                                />
                                            </div>
                                        </div>
                                        <div className="w-12 shrink-0 text-right text-xs tabular-nums text-muted-foreground">
                                            {bucket.count.toLocaleString()}
                                        </div>
                                    </div>
                                );
                            })}
                        </div>
                    </div>
                </div>
            </QueryState>
        </ObservatorySection>
    );
}
