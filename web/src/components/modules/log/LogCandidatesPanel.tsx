'use client';

import { useQuery } from '@tanstack/react-query';
import { useTranslations } from 'next-intl';
import { ChevronDown, Loader2, Pin } from 'lucide-react';
import { apiClient } from '@/api/client';
import type { Group } from '@/api/endpoints/group';
import type { RelayLog } from '@/api/endpoints/log';
import { Badge } from '@/components/ui/badge';
import { useMorphingDialog } from '@/components/ui/morphing-dialog';
import { getModelIcon } from '@/lib/model-icons';
import { cn } from '@/lib/utils';
import { buildLogCandidates, recordedCooldownSeconds } from './candidates';

export function LogCandidatesPanel({ log, channelNameById }: {
    log: RelayLog;
    channelNameById?: ReadonlyMap<number, string>;
}) {
    const t = useTranslations('log.card');
    const { isOpen } = useMorphingDialog();
    const groups = useQuery({
        queryKey: ['groups', 'list'],
        queryFn: () => apiClient.get<Group[]>('/api/v1/group/list'),
        enabled: isOpen,
        staleTime: 30_000,
        retry: false,
        meta: { skipGlobalErrorHandler: true },
    });
    // Logs do not persist group membership; only exact-name matches can safely supplement recorded attempts.
    const group = groups.data?.find((item) => item.name === log.request_model_name);
    const candidates = buildLogCandidates(log, group?.items);
    const forwardedCount = (log.attempts ?? []).filter((attempt) => attempt.status === 'success' || attempt.status === 'failed').length;

    return (
        <div className="h-full min-h-0 overflow-auto overscroll-contain">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border/60 px-4 py-2 text-[11px] text-muted-foreground">
                <span>{t('candidateHistoryHint')}</span>
                {log.attempts?.length ? <span className="tabular-nums">{t('forwardedAttempts', { count: forwardedCount })}</span> : null}
            </div>
            {groups.isError && (
                <div className="flex items-center justify-between gap-2 px-4 py-2 text-xs text-muted-foreground">
                    <span>{t('candidateLoadFailed')}</span>
                    <button type="button" onClick={() => groups.refetch()} className="shrink-0 text-primary hover:underline">{t('retryLoad')}</button>
                </div>
            )}
            {candidates.map((candidate) => {
                const { Avatar } = getModelIcon(candidate.model_name.replace(/^[a-z]{2}:/i, ''));
                const lastAttempt = candidate.attempts.at(-1);
                const cooldown = recordedCooldownSeconds(lastAttempt);
                const isFinal = candidate.channel_id === log.channel && candidate.model_name === log.actual_model_name;
                const status = lastAttempt?.status;
                const label = cooldown !== undefined ? t('recordedCooldown', { seconds: cooldown })
                    : status === 'circuit_break' ? t('circuitBreak')
                    : status ? t(status)
                    : isFinal && !log.error ? t('success') : t('notRecorded');
                const channelName = candidate.channel_name.trim() || channelNameById?.get(candidate.channel_id) || t('channelFallback', { id: candidate.channel_id });
                const row = (
                        <div className={cn('flex min-w-0 items-center gap-3 px-4 py-3', isFinal && 'bg-primary/5')}>
                            <div className="shrink-0"><Avatar size={26} /></div>
                            <div className="min-w-0 flex-1">
                                <p className="truncate text-sm font-medium text-foreground" title={candidate.model_name}>{candidate.model_name}</p>
                                <p className="truncate text-xs text-muted-foreground" title={channelName}>{channelName}{lastAttempt?.channel_key_id ? ` · ${t('channelKey', { id: lastAttempt.channel_key_id })}` : ''}</p>
                            </div>
                            {candidate.attempts.some((attempt) => attempt.sticky) && <Pin className="size-3 shrink-0 text-amber-500" aria-label={t('stickyAttempt')} />}
                            <Badge variant="outline" className={cn('shrink-0 rounded-full px-1.5 py-0 text-[10px] font-normal',
                                cooldown !== undefined || status === 'circuit_break' ? 'border-orange-500/30 bg-orange-500/10 text-orange-700 dark:text-orange-300'
                                    : status === 'failed' ? 'border-destructive/30 bg-destructive/10 text-destructive'
                                        : status === 'success' || (isFinal && !log.error) ? 'border-primary/20 bg-primary/10 text-primary'
                                            : 'border-border text-muted-foreground',
                            )}>{label}</Badge>
                            {candidate.attempts.length > 0 && <ChevronDown className="size-3 shrink-0 text-muted-foreground transition-transform group-open:rotate-180" />}
                        </div>
                );
                return (
                    <div key={JSON.stringify([candidate.channel_id, candidate.model_name])} className="border-b border-border/70 last:border-b-0">
                        {candidate.attempts.length > 0 ? (
                            <details className="group text-xs">
                                <summary
                                    className="list-none cursor-pointer hover:bg-muted/50 [&::-webkit-details-marker]:hidden"
                                    aria-label={`${candidate.model_name} — ${channelName}`}
                                >
                                    {row}
                                </summary>
                                <div className="mx-4 mb-3 space-y-2 rounded-lg bg-muted/50 p-2.5">
                                    {candidate.attempts.map((attempt, index) => (
                                        <div key={index} className="space-y-1">
                                            <div className="flex flex-wrap gap-2 text-muted-foreground">
                                                <span>#{attempt.attempt_num}</span>
                                                <span>{t(attempt.status === 'circuit_break' ? 'circuitBreak' : attempt.status)}</span>
                                                {attempt.adapter_type && <span>{attempt.adapter_type}</span>}
                                                <span>{attempt.duration < 1000 ? `${attempt.duration}ms` : `${(attempt.duration / 1000).toFixed(2)}s`}</span>
                                            </div>
                                            {attempt.msg && <p className="whitespace-pre-wrap wrap-break-word text-foreground/80">{attempt.msg}</p>}
                                        </div>
                                    ))}
                                </div>
                            </details>
                        ) : row}
                    </div>
                );
            })}
            {groups.isLoading && <div className="flex items-center justify-center gap-2 p-4 text-xs text-muted-foreground"><Loader2 className="size-4 animate-spin" />{t('loadingCandidates')}</div>}
            {!candidates.length && !groups.isLoading && <p className="p-4 text-xs text-muted-foreground">{t('noCandidates')}</p>}
        </div>
    );
}
