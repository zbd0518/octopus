'use client';

import { useTranslations } from 'next-intl';
import type { PoolAccount } from '@/api/endpoints/pool';
import { Badge } from '@/components/ui/badge';
import { formatUnixSeconds } from '@/lib/time';
import { accountState, modelNames, parseQuota } from './display';
import { POOL_PLATFORM_OPTIONS } from './type-options';

export function PlatformBadge({ platform }: { platform: string }) {
  const t = useTranslations('pool');
  const known = POOL_PLATFORM_OPTIONS.some((option) => option.value === platform);
  return <Badge variant="outline" className="max-w-full truncate">{known ? t(`platformLabels.${platform}`) : platform}</Badge>;
}

export function AccountStatus({ account, now }: { account: PoolAccount; now: number }) {
  const t = useTranslations('pool');
  const state = accountState(account, now);
  if (state === 'active') return <Badge className="border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400">{t('active')}</Badge>;
  if (state === 'tokenExpired') return <Badge variant="destructive">{t('tokenExpired')}</Badge>;
  if (state === 'expired') return <Badge variant="secondary">{t('expiredBadge')}</Badge>;
  if (state === 'temporary') return <Badge variant="outline" className="text-amber-700 dark:text-amber-400" title={account.temp_unsched_reason}>{t('tempUnschedBadge', { minutes: Math.max(1, Math.ceil((account.temp_unsched_until - now) / 60)) })}</Badge>;
  if (state === 'cooling') return <Badge variant="outline" className="text-amber-700 dark:text-amber-400" title={formatUnixSeconds(Math.max(account.rate_limit_reset_at, account.overload_until))}>{t('cooling')}</Badge>;
  const known = ['disabled', 'error', 'expired', 'invalid', 'unavailable'].includes(account.status);
  return <Badge variant="secondary" title={account.error_message}>{t(known ? `ui.statusLabels.${account.status}` : 'ui.unavailable')}</Badge>;
}

export function ModelBadges({ models }: { models: string }) {
  const t = useTranslations('pool');
  const names = modelNames(models);
  if (!names.length) return <span className="text-xs text-muted-foreground">{t('allModels')}</span>;
  return <div className="flex min-w-0 flex-wrap gap-1" title={names.join(', ')}>
    {names.slice(0, 3).map((name) => <Badge key={name} variant="outline" className="max-w-36 truncate text-xs">{name}</Badge>)}
    {names.length > 3 && <span className="text-xs text-muted-foreground">+{names.length - 3}</span>}
  </div>;
}

export function QuotaCell({ quota }: { quota: string }) {
  const t = useTranslations('pool');
  const data = parseQuota(quota);
  if (!data) return <span className="text-muted-foreground">—</span>;
  const warning = data.percent >= 90;
  return <div className="min-w-24 space-y-1 text-xs tabular-nums">
    <span className={warning ? 'text-destructive' : 'text-muted-foreground'}>{data.used.toLocaleString()}{data.total > 0 ? ` / ${data.total.toLocaleString()}` : ''}</span>
    {data.total > 0 && <div role="progressbar" aria-label={t('quota')} aria-valuemin={0} aria-valuemax={100} aria-valuenow={data.percent} className="h-1.5 overflow-hidden rounded-full bg-muted">
      <div className={`h-full rounded-full ${warning ? 'bg-destructive' : 'bg-primary'}`} style={{ width: `${data.percent}%` }} />
    </div>}
  </div>;
}
