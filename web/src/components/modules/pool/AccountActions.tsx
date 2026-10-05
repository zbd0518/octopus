'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { DropdownMenu } from 'radix-ui';
import { Ellipsis, FlaskConical, KeyRound, Loader2, Pencil, RefreshCw, RotateCcw, Unplug, Clock, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { toast } from '@/components/common/Toast';
import { useTestPoolAccount, useFetchPoolQuota, useRefreshPoolToken, useRecoverPoolAccount, useRestorePoolAccountProxy, type PoolAccount } from '@/api/endpoints/pool';
import { platformSupportsQuota, type PoolPlatform, type PoolAccountType } from './type-options';
import { modelNames } from './display';

export function AccountActions({ account, onEdit, onDelete, onPause }: {
  account: PoolAccount;
  onEdit: (account: PoolAccount) => void;
  onDelete: (account: PoolAccount) => void;
  onPause: (account: PoolAccount) => void;
}) {
  const t = useTranslations('pool');
  const test = useTestPoolAccount(account.pool_id);
  const quota = useFetchPoolQuota(account.pool_id);
  const refresh = useRefreshPoolToken(account.pool_id);
  const recover = useRecoverPoolAccount(account.pool_id);
  const restoreProxy = useRestorePoolAccountProxy(account.pool_id);
  const [menuOpen, setMenuOpen] = useState(false);
  const busy = test.isPending || quota.isPending || refresh.isPending || recover.isPending || restoreProxy.isPending;
  const menuItem = 'flex cursor-pointer items-center gap-2 rounded-md px-3 py-2.5 text-sm outline-none data-[highlighted]:bg-accent data-[disabled]:pointer-events-none data-[disabled]:opacity-50';

  return <div className="flex shrink-0 items-center justify-end gap-1">
    <Button variant="ghost" size="icon" className="size-9" aria-label={t('editAccount')} title={t('editAccount')} disabled={busy} onClick={() => onEdit(account)}><Pencil className="size-4" /></Button>
    <Button variant="ghost" size="icon" className="size-9" aria-label={t('testAccount')} title={t('testAccount')} disabled={busy} onClick={() => test.mutate({ accountId: account.id, model: modelNames(account.models)[0] || 'gpt-4o-mini' }, { onSuccess: (res) => res.success ? toast.success(t('testSuccess', { latency: res.latency_ms })) : toast.error(t('testFailed', { error: res.error || '' })) })}>
      {test.isPending ? <Loader2 className="size-4 animate-spin" /> : <FlaskConical className="size-4" />}
    </Button>
    <DropdownMenu.Root open={menuOpen} onOpenChange={setMenuOpen}>
      <DropdownMenu.Trigger asChild><Button variant="ghost" size="icon" className="size-9" aria-label={t('ui.moreActions')} title={t('ui.moreActions')} disabled={busy}>{busy ? <Loader2 className="size-4 animate-spin" /> : <Ellipsis className="size-4" />}</Button></DropdownMenu.Trigger>
      <DropdownMenu.Portal><DropdownMenu.Content align="end" sideOffset={6} className="z-50 min-w-48 rounded-lg border bg-popover p-1 text-popover-foreground shadow-lg">
        {platformSupportsQuota(account.platform as PoolPlatform, account.type as PoolAccountType) && <DropdownMenu.Item className={menuItem} onSelect={() => quota.mutate(account.id, { onSuccess: () => toast.success(t('quotaRefreshed')) })}><RefreshCw className="size-4" />{t('refreshQuota')}</DropdownMenu.Item>}
        {account.type === 'oauth' && <DropdownMenu.Item className={menuItem} onSelect={() => refresh.mutate(account.id, { onSuccess: () => toast.success(t('tokenRefreshed')) })}><KeyRound className="size-4" />{t('refreshToken')}</DropdownMenu.Item>}
        <DropdownMenu.Item className={menuItem} onSelect={() => recover.mutate(account.id, { onSuccess: () => toast.success(t('recoverSuccess')) })}><RotateCcw className="size-4" />{t('recoverAccount')}</DropdownMenu.Item>
        {account.proxy_fallback_origin_id ? (
          <DropdownMenu.Item className={menuItem} onSelect={() => restoreProxy.mutate(account.id, { onSuccess: () => toast.success(t('proxyFallbackRestored')) })}><Unplug className="size-4" />{t('restoreProxy')}</DropdownMenu.Item>
        ) : null}
        <DropdownMenu.Item className={menuItem} onSelect={() => onPause(account)}><Clock className="size-4" />{t('tempUnschedAction')}</DropdownMenu.Item>
        <DropdownMenu.Separator className="my-1 h-px bg-border" />
        <DropdownMenu.Item className={`${menuItem} text-destructive`} onSelect={() => onDelete(account)}><Trash2 className="size-4" />{t('ui.deleteAccount')}</DropdownMenu.Item>
      </DropdownMenu.Content></DropdownMenu.Portal>
    </DropdownMenu.Root>
  </div>;
}
