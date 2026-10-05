'use client';

import { memo, useMemo, useState, useEffect } from 'react';
import { Clock, Cpu, Gauge, Zap, ArrowDownToLine, ArrowUpFromLine, DollarSign, JapaneseYen, ArrowRight, ArrowDown, Send, MessageSquare, Loader2, Percent, RotateCw, Pin, KeyRound, Globe, ChevronsDownUp, ChevronsUpDown, TestTube2, Sigma, Brain, Type } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { motion, AnimatePresence } from 'motion/react';
import JsonView from '@uiw/react-json-view';
import { githubDarkTheme } from '@uiw/react-json-view/githubDark';
import { githubLightTheme } from '@uiw/react-json-view/githubLight';
import { useTheme } from 'next-themes';
import { type RelayLog, type ChannelAttempt, useLogDetail } from '@/api/endpoints/log';
import { getModelIcon, resolveBrandColor } from '@/lib/model-icons';
import { Badge } from '@/components/ui/badge';
import { cn, formatCount, formatMoney } from '@/lib/utils';
import { formatUnixSeconds } from '@/lib/time';
import { endpointTypeLabelKey } from '@/components/modules/group/utils';
import { resolveLogDisplayFields, formatJsonForCopy } from './display';
import { useLogFieldVisibility } from './ui-store';
import { LogCandidatesPanel } from './LogCandidatesPanel';
import { useSettingStore } from '@/stores/setting';
import { CopyIconButton } from '@/components/common/CopyButton';
import {
    MorphingDialog,
    MorphingDialogTrigger,
    MorphingDialogContainer,
    MorphingDialogContent,
    MorphingDialogClose,
    MorphingDialogTitle,
    MorphingDialogDescription,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';

/** Format count or money result into a compact display string like "1.23 万". */
function fmt({ value, unit }: { value: string; unit: string }) {
    return unit ? `${value} ${unit}` : value;
}

/** Format CNY cost with 2 significant figures. */
function costFmt(cny: number): string {
    if (cny === 0) return "0.00";
    const abs = Math.abs(cny);
    if (abs >= 1) return cny.toFixed(2);
    // Determine decimal places needed for 2 sig figs, then round and format
    const exp = Math.floor(Math.log10(abs));
    const decimals = Math.max(0, 1 - exp);
    const rounded = Number(abs.toFixed(decimals + 1));
    const s = Number(rounded.toPrecision(2));
    const finalDecimals = Math.max(0, 1 - Math.floor(Math.log10(s)));
    return (cny < 0 ? "-" : "") + s.toFixed(finalDecimals);
}

function formatTime(timestamp: number): string {
    return formatUnixSeconds(timestamp, {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
    });
}

function formatDuration(ms: number): string {
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
}

/** Format tokens-per-second for display. */
function formatTPS(tokens: number, timeMs: number): string {
    if (tokens <= 0 || timeMs <= 0) return '- tk/s';
    const seconds = timeMs / 1000;
    const tps = tokens / seconds;
    if (tps >= 100) return `${tps.toFixed(0)} tk/s`;
    if (tps >= 10) return `${tps.toFixed(1)} tk/s`;
    return `${tps.toFixed(2)} tk/s`;
}

/** Format cache hit rate = cacheReadTokens / totalTokens. */
function formatCacheHitRate(cacheRead: number, total: number): string {
    if (cacheRead <= 0 || total <= 0) return '-';
    const rate = (cacheRead / total) * 100;
    if (rate >= 100) return '100%';
    if (rate >= 10) return `${rate.toFixed(1)}%`;
    return `${rate.toFixed(2)}%`;
}

/** Resolve the badge styling and label key for a single attempt status.
 *
 * Skipped (cooldown / disabled / no key) and circuit-break attempts never
 * reached the upstream, so they are rendered with a neutral muted tone instead
 * of destructive red — otherwise an all-cooldown request looks like a wall of
 * red failures in the relay log (issue #95 改动6).
 */
function attemptStatusBadge(status: ChannelAttempt['status']): {
    className: string;
    labelKey: 'success' | 'failed' | 'skipped' | 'circuitBreak';
} {
    switch (status) {
        case 'success':
            return { className: 'bg-primary/15 text-primary', labelKey: 'success' };
        case 'circuit_break':
            return { className: 'bg-amber-500/15 text-amber-600 dark:text-amber-400', labelKey: 'circuitBreak' };
        case 'skipped':
            return { className: 'bg-muted text-muted-foreground', labelKey: 'skipped' };
        case 'failed':
        default:
            return { className: 'bg-destructive/15 text-destructive', labelKey: 'failed' };
    }
}

interface RetryBadgeWithTooltipProps {
    channelName: string;
    brandColor: string;
    attempts: ChannelAttempt[];
    channelNameById?: ReadonlyMap<number, string>;
}

function RetryBadgeWithTooltip({ channelName, brandColor, attempts, channelNameById }: RetryBadgeWithTooltipProps) {
    const t = useTranslations('log.card');
    const { resolvedTheme } = useTheme();
    const badgeColor = resolveBrandColor(brandColor, resolvedTheme === 'dark');

    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <Badge
                    variant="secondary"
                    className="min-w-0 max-w-full text-xs px-1.5 py-0.5 cursor-help font-medium"
                    style={{ backgroundColor: `${badgeColor}15`, color: badgeColor }}
                >
                    <RotateCw className="size-3 mr-1 opacity-80" />
                    <span className="max-w-[12rem] truncate sm:max-w-[18rem]" title={channelName}>{channelName}</span>
                </Badge>
            </TooltipTrigger>
            <TooltipContent className="border bg-card p-2 min-w-[280px] max-w-[calc(100vw-2rem)] rounded-xl flex flex-col gap-1">
                {attempts.map((attempt, idx) => (
                    <div key={idx} className="flex flex-col w-full">
                        <div className="flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-muted/50 transition-colors">
                            {(() => {
                                const badge = attemptStatusBadge(attempt.status);
                                return (
                                    <Badge
                                        className={cn(
                                            "h-5 shrink-0 px-1.5 text-[10px] font-bold uppercase shadow-none border-0",
                                            badge.className
                                        )}
                                    >
                                        {t(badge.labelKey)}
                                    </Badge>
                                );
                            })()}
                            <div className="flex min-w-0 flex-col flex-1">
                                <span className="truncate text-xs font-semibold text-foreground">
                                    {attempt.channel_name?.trim() || channelNameById?.get(attempt.channel_id) || `Channel #${attempt.channel_id}`}
                                </span>
                                <span className="text-[10px] text-muted-foreground">
                                    {attempt.model_name}{attempt.adapter_type ? ` • ${attempt.adapter_type}` : ''} • {formatDuration(attempt.duration)}
                                </span>
                            </div>
                        </div>
                        {
                            idx < attempts.length - 1 && (
                                <div className="flex justify-center py-0.5">
                                    <ArrowDown className="size-3 text-muted-foreground/30" />
                                </div>
                            )
                        }
                    </div>
                ))}
            </TooltipContent>
        </Tooltip >
    );
}

function DeferredJsonContent({ content, fallbackText, collapsed }: { content: string | undefined; fallbackText: string; collapsed: boolean }) {
    const { resolvedTheme } = useTheme();
    const { isOpen } = useMorphingDialog();
    const [shouldRender, setShouldRender] = useState(false);

    const parsed = useMemo(() => {
        if (!content) return { isJson: false, data: null };
        try {
            return { isJson: true, data: JSON.parse(content) };
        } catch {
            return { isJson: false, data: content };
        }
    }, [content]);

    useEffect(() => {
        if (isOpen) {
            const timer = setTimeout(() => setShouldRender(true), 300);
            return () => clearTimeout(timer);
        }
    }, [isOpen]);

    if (!isOpen) {
        if (shouldRender) setShouldRender(false);
        return null;
    }

    if (!content) {
        return (
            <div className="h-full min-h-0 overflow-auto overscroll-contain overflow-x-auto">
                <pre className="p-4 text-xs text-muted-foreground whitespace-pre-wrap wrap-break-word leading-relaxed">
                    {fallbackText}
                </pre>
            </div>
        );
    }

    return (
        <AnimatePresence mode="wait">
            {!shouldRender ? (
                <motion.div
                    key="loading"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    className="flex h-full min-h-0 items-center justify-center overflow-auto overscroll-contain p-4"
                >
                    <Loader2 className="h-5 w-5 text-muted-foreground animate-spin" />
                </motion.div>
            ) : parsed.isJson ? (
                <motion.div
                    key="json"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.2 }}
                    className="h-full min-h-0 overflow-auto overscroll-contain p-4 overflow-x-auto"
                >
                    <JsonView
                        value={parsed.data as object}
                        style={{
                            ...(resolvedTheme === 'dark' ? githubDarkTheme : githubLightTheme),
                            fontSize: '12px',
                            fontFamily: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace',
                            backgroundColor: 'transparent',
                        }}
                        displayDataTypes={false}
                        displayObjectSize={false}
                        collapsed={collapsed}
                        shortenTextAfterLength={collapsed ? 30 : 0}
                    />
                </motion.div>
            ) : (
                <motion.pre
                    key="text"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.2 }}
                    className="h-full min-h-0 overflow-auto overscroll-contain p-4 text-xs text-muted-foreground whitespace-pre-wrap wrap-break-word font-mono leading-relaxed overflow-x-auto"
                >
                    {content}
                </motion.pre>
            )}
        </AnimatePresence>
    );
}

export const LogCard = memo(function LogCard({ log, channelNameById }: { log: RelayLog; channelNameById?: ReadonlyMap<number, string> }) {
    const t = useTranslations('log.card');
    const tCommon = useTranslations('common');
    const tGroup = useTranslations('group');
    const { detail, isLoading: isDetailLoading, fetchDetail, reset: resetDetail } = useLogDetail();
    const hasError = !!log.error;
    const hasMultipleAttempts = log.attempts && log.attempts.length > 1;
    const [leftView, setLeftView] = useState<'group' | 'request'>('group');
    const [requestJsonCollapsed, setRequestJsonCollapsed] = useState(false);
    const [responseJsonCollapsed, setResponseJsonCollapsed] = useState(false);
    const displayFields = useMemo(() => resolveLogDisplayFields(log, detail, channelNameById), [channelNameById, detail, log]);
    const vis = useLogFieldVisibility();
    const chinaMode = useSettingStore((s) => s.chinaMode);
    const { Avatar: ModelAvatar, color: brandColor } = useMemo(
        () => getModelIcon(displayFields.actualModelName),
        [displayFields.actualModelName]
    );
    // 深色模式下黑/深色系品牌色（Grok/Kimi/Replicate/MiniMax/Yi 等）直接用作
    // Badge 文字会「黑字+透明黑底」看不清，按主题解析为可读颜色（issue: 日志深色模式对比度）
    const { resolvedTheme } = useTheme();
    const badgeColor = resolveBrandColor(brandColor, resolvedTheme === 'dark');
    const requestAPIKeyName = displayFields.requestAPIKeyName;
    const clientIP = log.client_ip || '';
    const cacheReadTokens = displayFields.cacheReadTokens;
    const semanticCacheHit = displayFields.semanticCacheHit;
    const effectiveInputTokens = Math.max(0, log.input_tokens - cacheReadTokens);
    const inputLabel = cacheReadTokens > 0 ? t('realInput') : t('input');
    const displayChannelName = displayFields.channelName || '-';
    const displayEndpointType = useMemo(() => {
        const adapter = displayFields.outboundAdapterType;
        if (adapter) {
            const label = t(`adapterLabels.${adapter}`);
            // next-intl 缺 key 时可能回传 key 路径；有专用文案用文案，否则用原始 adapter
            if (label && !label.includes('adapterLabels.')) return label;
            return adapter;
        }
        // 旧日志无 adapter_type 时回退 endpoint/request type，避免整块空白
        const reqTypeKey = displayFields.requestTypeKey;
        if (reqTypeKey) {
            const label = t(`requestTypeLabels.${reqTypeKey}`);
            if (label && !label.includes('requestTypeLabels.')) return label;
        }
        const rawEndpointType = displayFields.endpointType;
        if (!rawEndpointType) return '';
        const labelKey = endpointTypeLabelKey(rawEndpointType);
        return labelKey ? tGroup(labelKey) : rawEndpointType;
    }, [displayFields.endpointType, displayFields.outboundAdapterType, displayFields.requestTypeKey, t, tGroup]);
    const displayActualModelName = displayFields.actualModelName || '-';
    const displayRequestModelName = displayFields.requestModelName || log.request_model_name;

    const requestContent = detail?.request_content;
    const responseContent = detail?.response_content;
    const requestCopyText = useMemo(() => formatJsonForCopy(requestContent), [requestContent]);
    const responseCopyText = useMemo(() => formatJsonForCopy(responseContent), [responseContent]);
    const usageKnown = useMemo(() => {
        if (log.input_tokens > 0 || log.output_tokens > 0 || Number(log.cost) > 0) {
            return true;
        }
        if (log.error) {
            return true;
        }
        if (!responseContent) {
            return false;
        }
        try {
            const parsed = JSON.parse(responseContent) as { usage?: unknown };
            return parsed.usage !== undefined;
        } catch {
            return false;
        }
    }, [log.cost, log.error, log.input_tokens, log.output_tokens, responseContent]);

    const inputTokenDisplay = usageKnown
        ? fmt(formatCount(effectiveInputTokens).formatted)
        : tCommon('unknown');
    const outputTokenDisplay = usageKnown
        ? fmt(formatCount(log.output_tokens).formatted)
        : tCommon('unknown');
    // 总消耗 = 真实输入 + 缓存输入 + 输出 = input_tokens(含缓存) + output_tokens（issue #107）
    const totalTokens = log.input_tokens + log.output_tokens;
    const totalTokenDisplay = usageKnown
        ? fmt(formatCount(totalTokens).formatted)
        : tCommon('unknown');
    const costDisplay = usageKnown
        ? (chinaMode
            ? costFmt(formatMoney(Number(log.cost)).raw)
            : formatMoney(Number(log.cost)).raw.toFixed(2))
        : tCommon('unknown');

    return (
        // Root layout already provides the single global TooltipProvider (shared
        // GlobalTooltipProvider + one TooltipOverlay) used by every other module
        // (channel/group/setting cards). Wrapping EACH retry LogCard in its own
        // provider creates many parallel global tooltip singletons — each with its
        // own layoutId animation + useFloating autoUpdate listeners — which spirals
        // into a layout-animation loop and OOMs the tab on hover. Reuse the global one.
        <MorphingDialog
            onOpen={() => {
                setLeftView('group');
                setRequestJsonCollapsed(false);
                setResponseJsonCollapsed(false);
                fetchDetail(log.id);
            }}
            onClose={resetDetail}
        >
                <MorphingDialogTrigger
                    className={cn(
                        "rounded-xl border bg-card w-full text-left",
                        hasError ? "border-destructive/40" : "border-border",
                    )}
                >
                    <div className={cn("p-2.5 sm:p-4 grid grid-cols-[auto_1fr] gap-2.5 sm:gap-4", hasError ? "items-start" : "items-center")}>
                        <div className="sm:hidden"><ModelAvatar size={36} /></div>
                        <div className="hidden sm:block"><ModelAvatar size={40} /></div>
                        <div className="min-w-0 flex flex-col gap-2">
                            <div className="flex min-w-0 flex-wrap items-center gap-2 text-sm md:flex-nowrap">
                                <span className="min-w-0 max-w-full font-semibold text-card-foreground truncate md:max-w-[32%]" title={displayRequestModelName}>
                                    {displayRequestModelName}
                                </span>
                                {log.is_test && (
                                    <Badge
                                        variant="outline"
                                        className="shrink-0 text-xs px-1.5 py-0 border-blue-400/50 text-blue-500 dark:text-blue-400"
                                        title={t('testLog')}
                                    >
                                        <TestTube2 className="size-3 mr-1" />
                                        {t('testLog')}
                                    </Badge>
                                )}
                                <ArrowRight className="size-3.5 shrink-0 text-muted-foreground/50" />
                                {vis.endpointType && displayEndpointType && (
                                    <Badge
                                        variant="secondary"
                                        className="max-w-full shrink-0 text-xs px-1.5 py-0"
                                        style={{ backgroundColor: `${badgeColor}15`, color: badgeColor }}
                                        title={displayEndpointType}
                                    >
                                        <span className="block max-w-[10rem] truncate">{displayEndpointType}</span>
                                    </Badge>
                                )}
                                {vis.channelName && (
                                    <>
                                        <ArrowRight className="size-3.5 shrink-0 text-muted-foreground/50" />
                                        {hasMultipleAttempts ? (
                                            <RetryBadgeWithTooltip
                                                channelName={displayChannelName}
                                                brandColor={brandColor}
                                                attempts={log.attempts!}
                                                channelNameById={channelNameById}
                                            />
                                        ) : (
                                            <Badge
                                                variant="secondary"
                                                className="max-w-full shrink-0 text-xs px-1.5 py-0"
                                                style={{ backgroundColor: `${badgeColor}15`, color: badgeColor }}
                                                title={displayChannelName}
                                            >
                                                <span className="block max-w-[18rem] truncate">{displayChannelName}</span>
                                            </Badge>
                                        )}
                                    </>
                                )}
                                {vis.actualModel && (
                                    <span className="min-w-0 text-muted-foreground truncate md:flex-1" title={displayActualModelName}>
                                        {displayActualModelName}
                                    </span>
                                )}
                                {log.attempts?.some(a => a.sticky) && (
                                    <Pin className="size-3.5 shrink-0 text-amber-500" />
                                )}
                            </div>
                            <div className="grid grid-cols-2 md:grid-cols-7 gap-x-4 gap-y-1.5 text-xs tabular-nums text-muted-foreground">
                                <div className="flex items-center gap-1.5">
                                    <Clock className="size-3.5 shrink-0" style={{ color: badgeColor }} />
                                    <span>{formatTime(log.time)}</span>
                                </div>
                                {vis.apiKeyName && requestAPIKeyName && (
                                    <div className="flex items-center gap-1.5">
                                        <KeyRound className="size-3.5 shrink-0 text-orange-500" />
                                        <span className="truncate" title={requestAPIKeyName}>
                                            {requestAPIKeyName}
                                        </span>
                                    </div>
                                )}
                                {vis.clientIP && clientIP && (
                                    <div className="flex items-center gap-1.5">
                                        <Globe className="size-3.5 shrink-0 text-sky-500" />
                                        <span className="truncate" title={clientIP}>{clientIP}</span>
                                    </div>
                                )}
                                <div className="flex items-center gap-1.5">
                                    <Zap className="size-3.5 shrink-0 text-amber-500" />
                                    <span>{t('firstToken')} {formatDuration(log.ftut)}</span>
                                </div>
                                <div className="flex items-center gap-1.5">
                                    <Cpu className="size-3.5 shrink-0 text-blue-500" />
                                    <span>{t('totalTime')} {formatDuration(log.use_time)}</span>
                                </div>
                                {vis.tps && (
                                    <div className="flex items-center gap-1.5">
                                        <Gauge className="size-3.5 shrink-0 text-lime-500" />
                                        <span>{t('tps')} {formatTPS(log.output_tokens, log.use_time)}</span>
                                    </div>
                                )}
                                {vis.cacheHitRate && cacheReadTokens > 0 && (
                                    <div className="flex items-center gap-1.5">
                                        <Percent className="size-3.5 shrink-0 text-teal-500" />
                                        <span>{t('cacheHitRate')} {formatCacheHitRate(cacheReadTokens, totalTokens)}</span>
                                    </div>
                                )}
                                <div className="flex items-center gap-1.5">
                                    <ArrowDownToLine className="size-3.5 shrink-0 text-green-500" />
                                    <span>{inputLabel} {inputTokenDisplay}</span>
                                </div>
                                {semanticCacheHit && (
                                    <div className="flex items-center gap-1.5">
                                        <ArrowDownToLine className="size-3.5 shrink-0 text-cyan-500" />
                                        <span>{t('semanticCacheHit')}</span>
                                    </div>
                                )}
                                {cacheReadTokens > 0 && (
                                    <div className="flex items-center gap-1.5">
                                        <ArrowDownToLine className="size-3.5 shrink-0 text-teal-500" />
                                        <span>{t('cacheHit')} {fmt(formatCount(cacheReadTokens).formatted)}</span>
                                    </div>
                                )}
                                <div className="flex items-center gap-1.5">
                                    <ArrowUpFromLine className="size-3.5 shrink-0 text-purple-500" />
                                    <span>{t('output')} {outputTokenDisplay}</span>
                                </div>
                                <div className="flex items-center gap-1.5">
                                    <Sigma className="size-3.5 shrink-0 text-rose-500" />
                                    <span className="font-medium text-rose-600 dark:text-rose-400">{t('totalTokens')} {totalTokenDisplay}</span>
                                </div>
                                {vis.cost && (
                                    <div className="flex items-center gap-1.5">
                                        {chinaMode ? <JapaneseYen className="size-3.5 shrink-0 text-emerald-500" /> : <DollarSign className="size-3.5 shrink-0 text-emerald-500" />}
                                        <span className="font-medium text-emerald-600 dark:text-emerald-400">
                                            {t('cost')} {costDisplay}
                                        </span>
                                        {log.billing_window === 'peak' && (
                                            <Badge
                                                variant="outline"
                                                className="shrink-0 text-xs px-1.5 py-0 border-amber-400/50 text-amber-500 dark:text-amber-400"
                                                title={t('billingWindowHint')}
                                            >
                                                {t('billingWindowPeak')}
                                            </Badge>
                                        )}
                                        {log.billing_window === 'offpeak' && (
                                            <Badge
                                                variant="outline"
                                                className="shrink-0 text-xs px-1.5 py-0 border-sky-400/50 text-sky-500 dark:text-sky-400"
                                                title={t('billingWindowHint')}
                                            >
                                                {t('billingWindowOffPeak')}
                                            </Badge>
                                        )}
                                    </div>
                                )}
                                {vis.reasoningEffort && !!log.reasoning_effort && (
                                    <div className="flex items-center gap-1.5">
                                        <Brain className="size-3.5 shrink-0 text-violet-500" />
                                        <span>{t('reasoningEffort')} {log.reasoning_effort}</span>
                                    </div>
                                )}
                                {vis.reasoningTokens && (log.reasoning_tokens ?? 0) > 0 && (
                                    <div className="flex items-center gap-1.5">
                                        <Brain className="size-3.5 shrink-0 text-indigo-500" />
                                        <span>{t('reasoningTokens')} {fmt(formatCount(log.reasoning_tokens ?? 0).formatted)}{t('reasoningTokensUnit')}</span>
                                    </div>
                                )}
                                {vis.reasoningTokens && (log.reasoning_tokens ?? 0) <= 0 && (log.reasoning_chars ?? 0) > 0 && (
                                    <div className="flex items-center gap-1.5">
                                        <Type className="size-3.5 shrink-0 text-indigo-500" />
                                        <span>{t('reasoningChars')} {fmt(formatCount(log.reasoning_chars ?? 0).formatted)}{t('reasoningCharsUnit')}</span>
                                    </div>
                                )}
                            </div>
                            {hasError && (
                                <div className="p-2.5 rounded-xl bg-destructive/10 border border-destructive/20 overflow-hidden">
                                    <p className="text-xs text-destructive line-clamp-2">{log.error}</p>
                                </div>
                            )}
                        </div>
                    </div>
                </MorphingDialogTrigger>

                <MorphingDialogContainer>
                    <MorphingDialogContent className="relative flex h-[calc(100dvh-2rem)] max-h-[calc(100dvh-2rem)] min-h-0 w-[calc(100vw-2rem)] flex-col overflow-hidden rounded-xl bg-card p-3 text-card-foreground sm:h-[min(840px,90dvh)] sm:max-h-[min(840px,90dvh)] sm:max-w-7xl sm:rounded-3xl sm:p-6 md:w-[95vw]">
                        <MorphingDialogClose className="top-4 right-5 text-muted-foreground hover:text-foreground transition-colors" />
                        <MorphingDialogTitle className="flex min-w-0 flex-wrap items-center gap-2 mb-3 pr-8 text-sm">
                            <ModelAvatar size={28} />
                            <span className="min-w-0 max-w-full truncate font-semibold text-card-foreground" title={displayRequestModelName}>{displayRequestModelName}</span>
                            {log.is_test && (
                                <Badge
                                    variant="outline"
                                    className="shrink-0 text-xs px-1.5 py-0 border-blue-400/50 text-blue-500 dark:text-blue-400"
                                >
                                    <TestTube2 className="size-3 mr-1" />
                                    {t('testLog')}
                                </Badge>
                            )}
                            <ArrowRight className="size-3.5 text-muted-foreground/50" />
                            {vis.endpointType && displayEndpointType && (
                                <Badge
                                    variant="secondary"
                                    className="max-w-full shrink-0 text-xs px-1.5 py-0"
                                    style={{ backgroundColor: `${badgeColor}15`, color: badgeColor }}
                                    title={displayEndpointType}
                                >
                                    <span className="block max-w-[10rem] truncate">{displayEndpointType}</span>
                                </Badge>
                            )}
                            {vis.channelName && (
                                <>
                                    <ArrowRight className="size-3.5 text-muted-foreground/50" />
                                    {hasMultipleAttempts ? (
                                        <RetryBadgeWithTooltip
                                            channelName={displayChannelName}
                                            brandColor={brandColor}
                                            attempts={log.attempts!}
                                            channelNameById={channelNameById}
                                        />
                                    ) : (
                                    <Badge
                                        variant="secondary"
                                        className="max-w-full text-xs px-1.5 py-0"
                                        style={{ backgroundColor: `${badgeColor}15`, color: badgeColor }}
                                        title={displayChannelName}
                                    >
                                        <span className="block max-w-[12rem] truncate sm:max-w-[18rem]">{displayChannelName}</span>
                                    </Badge>
                                    )}
                                </>
                            )}
                            {vis.actualModel && (
                                <span className="min-w-0 flex-1 truncate text-muted-foreground" title={displayActualModelName}>{displayActualModelName}</span>
                            )}
                            {log.attempts?.some(a => a.sticky) && (
                                <Pin className="size-3.5 shrink-0 text-amber-500" />
                            )}
                        </MorphingDialogTitle>

                        <MorphingDialogDescription className="flex min-h-0 min-w-0 flex-1 overflow-hidden">
                            <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-4 overflow-hidden">
                                {hasError && (
                                    <div className="relative flex-initial rounded-2xl border border-destructive/20 bg-destructive/5 px-3 py-2.5">
                                        <div className="absolute right-2 top-2">
                                            <CopyIconButton
                                                text={log.error ?? ''}
                                                className="p-1 rounded-md text-destructive/60 hover:text-destructive hover:bg-destructive/10 transition-colors"
                                                copyIconClassName="size-4"
                                                checkIconClassName="size-4"
                                            />
                                        </div>
                                        <p className="max-h-24 overflow-auto overscroll-contain pr-8 text-xs leading-relaxed whitespace-pre-wrap wrap-break-word text-destructive">
                                            {log.error}
                                        </p>
                                    </div>
                                )}
                                <div className="min-h-0 min-w-0 flex-1 overflow-hidden pb-1">
                                    <div className="grid h-full min-h-0 grid-cols-1 grid-rows-2 gap-4 md:grid-cols-2 md:grid-rows-1">
                                        <div className="flex min-h-0 min-w-0 flex-col rounded-2xl border border-border bg-muted/30 overflow-hidden">
                                            <div className="flex flex-wrap items-center gap-2 px-3 md:px-4 py-2.5 md:py-3 border-b border-border bg-muted/50 shrink-0">
                                                <Send className="size-4 text-green-500" />
                                                <button
                                                    type="button"
                                                    aria-pressed={leftView === 'group'}
                                                    onClick={() => setLeftView('group')}
                                                    className={cn(
                                                        "rounded-md px-1.5 py-0.5 text-sm transition-colors",
                                                        leftView === 'group'
                                                            ? "bg-background font-medium text-foreground shadow-sm"
                                                            : "text-muted-foreground hover:text-foreground"
                                                    )}
                                                >
                                                    {t('groupCandidates')}
                                                </button>
                                                <span className="text-xs text-muted-foreground/50">/</span>
                                                <button
                                                    type="button"
                                                    aria-pressed={leftView === 'request'}
                                                    onClick={() => setLeftView('request')}
                                                    className={cn(
                                                        "rounded-md px-1.5 py-0.5 text-sm transition-colors",
                                                        leftView === 'request'
                                                            ? "bg-background font-medium text-foreground shadow-sm"
                                                            : "text-muted-foreground hover:text-foreground"
                                                    )}
                                                >
                                                    {t('requestBody')}
                                                </button>
                                                <div className="ml-auto flex items-center gap-1">
                                                    {leftView === 'request' && (
                                                        <>
                                                            <Badge variant="secondary" className="text-xs">
                                                                {usageKnown ? `${fmt(formatCount(log.input_tokens).formatted)} ${t('tokens')}` : tCommon('unknown')}
                                                            </Badge>
                                                            {requestContent && (
                                                                <>
                                                                    <button
                                                                        type="button"
                                                                        onClick={() => setRequestJsonCollapsed((v) => !v)}
                                                                        className="rounded-md p-1 text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                                                                        title={requestJsonCollapsed ? t('expandAll') : t('collapseAll')}
                                                                        aria-label={requestJsonCollapsed ? t('expandAll') : t('collapseAll')}
                                                                    >
                                                                        {requestJsonCollapsed ? <ChevronsUpDown className="size-3.5" /> : <ChevronsDownUp className="size-3.5" />}
                                                                    </button>
                                                                    <CopyIconButton text={requestCopyText} className="text-muted-foreground hover:text-foreground" />
                                                                </>
                                                            )}
                                                        </>
                                                    )}
                                                </div>
                                            </div>
                                            <div className="min-h-0 min-w-0 flex-1 overflow-hidden">
                                                {leftView === 'group' ? (
                                                    <LogCandidatesPanel log={detail ?? log} channelNameById={channelNameById} />
                                                ) : isDetailLoading ? (
                                                    <div className="p-4 flex items-center justify-center h-full">
                                                        <Loader2 className="h-5 w-5 text-muted-foreground animate-spin" />
                                                    </div>
                                                ) : (
                                                    <DeferredJsonContent content={requestContent} fallbackText={t('noRequestContent')} collapsed={requestJsonCollapsed} />
                                                )}
                                            </div>
                                        </div>
                                        <div className="flex min-h-0 min-w-0 flex-col rounded-2xl border border-border bg-muted/30 overflow-hidden">
                                            <div className="flex flex-wrap items-center gap-2 px-3 md:px-4 py-2.5 md:py-3 border-b border-border bg-muted/50 shrink-0">
                                                <MessageSquare className="size-4 text-purple-500" />
                                                <span className="text-sm font-medium text-card-foreground">{t('responseContent')}</span>
                                                <div className="ml-auto flex items-center gap-1">
                                                    <Badge variant="secondary" className="text-xs">
                                                        {usageKnown ? `${fmt(formatCount(log.output_tokens).formatted)} ${t('tokens')}` : tCommon('unknown')}
                                                    </Badge>
                                                    {responseContent && (
                                                        <>
                                                            <button
                                                                type="button"
                                                                onClick={() => setResponseJsonCollapsed((v) => !v)}
                                                                className="rounded-md p-1 text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                                                                title={responseJsonCollapsed ? t('expandAll') : t('collapseAll')}
                                                                aria-label={responseJsonCollapsed ? t('expandAll') : t('collapseAll')}
                                                            >
                                                                {responseJsonCollapsed ? <ChevronsUpDown className="size-3.5" /> : <ChevronsDownUp className="size-3.5" />}
                                                            </button>
                                                            <CopyIconButton text={responseCopyText} className="text-muted-foreground hover:text-foreground" />
                                                        </>
                                                    )}
                                                </div>
                                            </div>
                                            <div className="min-h-0 min-w-0 flex-1 overflow-hidden">
                                                {isDetailLoading ? (
                                                    <div className="p-4 flex items-center justify-center h-full">
                                                        <Loader2 className="h-5 w-5 text-muted-foreground animate-spin" />
                                                    </div>
                                                ) : (
                                                    <DeferredJsonContent content={responseContent} fallbackText={t('noResponseContent')} collapsed={responseJsonCollapsed} />
                                                )}
                                            </div>
                                        </div>
                                    </div>
                                </div>
                            </div>
                        </MorphingDialogDescription>

                        <div className="shrink-0 border-t border-border/50 pt-3 text-xs text-muted-foreground">
                            <div className="flex flex-wrap items-center gap-3 md:gap-4">
                            <div className="flex items-center gap-1.5">
                                <Clock className="size-3.5" style={{ color: badgeColor }} />
                                <span className="tabular-nums">{formatTime(log.time)}</span>
                            </div>
                            {vis.apiKeyName && requestAPIKeyName && (
                                <div className="flex min-w-0 items-center gap-1.5">
                                    <KeyRound className="size-3.5 shrink-0 text-orange-500" />
                                    <span className="truncate" title={requestAPIKeyName}>
                                        {requestAPIKeyName}
                                    </span>
                                </div>
                            )}
                            <div className="flex items-center gap-1.5">
                                <Zap className="size-3.5 text-amber-500" />
                                <span>{t('firstTokenTime')}: {formatDuration(log.ftut)}</span>
                            </div>
                            <div className="flex items-center gap-1.5">
                                <Cpu className="size-3.5 text-blue-500" />
                                <span>{t('totalTime')}: {formatDuration(log.use_time)}</span>
                            </div>
                            {vis.tps && (
                                <div className="flex items-center gap-1.5">
                                    <Gauge className="size-3.5 text-lime-500" />
                                    <span>{t('tps')}: {formatTPS(log.output_tokens, log.use_time)}</span>
                                </div>
                            )}
                            {vis.cacheHitRate && cacheReadTokens > 0 && (
                                <div className="flex items-center gap-1.5">
                                    <Percent className="size-3.5 text-teal-500" />
                                    <span>{t('cacheHitRate')}: {formatCacheHitRate(cacheReadTokens, totalTokens)}</span>
                                </div>
                            )}
                            <div className="flex items-center gap-1.5">
                                <ArrowDownToLine className="size-3.5 text-green-500" />
                                <span>{inputLabel}: {inputTokenDisplay}</span>
                            </div>
                            <div className="flex items-center gap-1.5">
                                <ArrowUpFromLine className="size-3.5 text-purple-500" />
                                <span>{t('output')}: {outputTokenDisplay}</span>
                            </div>
                            <div className="flex items-center gap-1.5">
                                <Sigma className="size-3.5 text-rose-500" />
                                <span className="font-medium text-rose-600 dark:text-rose-400">{t('totalTokens')}: {totalTokenDisplay}</span>
                            </div>
                            {cacheReadTokens > 0 && (
                                <div className="flex items-center gap-1.5">
                                    <ArrowDownToLine className="size-3.5 text-teal-500" />
                                    <span>{t('cacheHit')}: {fmt(formatCount(cacheReadTokens).formatted)}</span>
                                </div>
                            )}
                            {semanticCacheHit && (
                                <div className="flex items-center gap-1.5">
                                    <ArrowDownToLine className="size-3.5 text-cyan-500" />
                                    <span>{t('semanticCacheHit')}</span>
                                </div>
                            )}
                            {vis.cost && (
                                <div className="flex items-center gap-1.5">
                                    {chinaMode ? <JapaneseYen className="size-3.5 text-emerald-500" /> : <DollarSign className="size-3.5 text-emerald-500" />}
                                    <span className="font-medium text-emerald-600 dark:text-emerald-400">
                                        {t('cost')}: {costDisplay}
                                    </span>
                                    {log.billing_window === 'peak' && (
                                        <Badge
                                            variant="outline"
                                            className="shrink-0 text-xs px-1.5 py-0 border-amber-400/50 text-amber-500 dark:text-amber-400"
                                            title={t('billingWindowHint')}
                                        >
                                            {t('billingWindowPeak')}
                                        </Badge>
                                    )}
                                    {log.billing_window === 'offpeak' && (
                                        <Badge
                                            variant="outline"
                                            className="shrink-0 text-xs px-1.5 py-0 border-sky-400/50 text-sky-500 dark:text-sky-400"
                                            title={t('billingWindowHint')}
                                        >
                                            {t('billingWindowOffPeak')}
                                        </Badge>
                                    )}
                                </div>
                            )}
                            {vis.reasoningEffort && !!log.reasoning_effort && (
                                <div className="flex items-center gap-1.5">
                                    <Brain className="size-3.5 text-violet-500" />
                                    <span>{t('reasoningEffort')}: {log.reasoning_effort}</span>
                                </div>
                            )}
                            {vis.reasoningTokens && (log.reasoning_tokens ?? 0) > 0 && (
                                <div className="flex items-center gap-1.5">
                                    <Brain className="size-3.5 text-indigo-500" />
                                    <span>{t('reasoningTokens')}: {fmt(formatCount(log.reasoning_tokens ?? 0).formatted)}{t('reasoningTokensUnit')}</span>
                                </div>
                            )}
                            {vis.reasoningTokens && (log.reasoning_tokens ?? 0) <= 0 && (log.reasoning_chars ?? 0) > 0 && (
                                <div className="flex items-center gap-1.5">
                                    <Type className="size-3.5 text-indigo-500" />
                                    <span>{t('reasoningChars')}: {fmt(formatCount(log.reasoning_chars ?? 0).formatted)}{t('reasoningCharsUnit')}</span>
                                </div>
                            )}
                            </div>
                        </div>
                    </MorphingDialogContent>
                </MorphingDialogContainer>
            </MorphingDialog>
    );
});

