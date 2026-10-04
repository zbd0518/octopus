'use client';

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Checkbox } from 'radix-ui';
import { ArrowLeft, Check, Minus, Plus, Search, Upload, Download, RefreshCw, Loader2, Users, Activity, AlertTriangle, X, Trash2 } from 'lucide-react';
import { useNavStore } from '@/components/modules/navbar';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { LoadingState } from '@/components/common/LoadingState';
import { ErrorState } from '@/components/common/ErrorState';
import { toast } from '@/components/common/Toast';
import { usePoolAccounts, useBatchPoolAccounts, useClearPoolAccounts, useExportPoolAccounts, type AccountPool, type PoolAccount } from '@/api/endpoints/pool';
import { AccountFormDialog } from './AccountFormDialog';
import { AccountActions } from './AccountActions';
import { AccountStatus, ModelBadges, PlatformBadge, QuotaCell } from './AccountDisplay';
import { PoolOperationDialogs } from './PoolOperationDialogs';
import { accountState, selectedAccountIds } from './display';
import { POOL_PLATFORM_OPTIONS } from './type-options';

function AccountCheckbox({ label, checked, onChange, disabled }: { label: string; checked: boolean | 'indeterminate'; onChange: () => void; disabled?: boolean }) {
  return <Checkbox.Root aria-label={label} checked={checked} onCheckedChange={onChange} disabled={disabled} className="grid size-5 shrink-0 place-items-center rounded border border-input bg-background outline-none focus-visible:ring-2 focus-visible:ring-ring data-[state=checked]:border-primary data-[state=checked]:bg-primary data-[state=checked]:text-primary-foreground data-[state=indeterminate]:bg-primary data-[state=indeterminate]:text-primary-foreground disabled:opacity-50">
    <Checkbox.Indicator>{checked === 'indeterminate' ? <Minus className="size-3.5" /> : <Check className="size-3.5" />}</Checkbox.Indicator>
  </Checkbox.Root>;
}

export function PoolDetail({ pool, onBack }: { pool: AccountPool; onBack: () => void }) {
  const t = useTranslations('pool');
  const { data: accounts = [], isLoading, error, refetch, isFetching } = usePoolAccounts(pool.id);
  const batch = useBatchPoolAccounts(pool.id);
  const exportAccounts = useExportPoolAccounts(pool.id);
  const clearAccounts = useClearPoolAccounts(pool.id);
  const activeItem = useNavStore((state) => state.activeItem);
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000));
  const [search, setSearch] = useState('');
  const [platform, setPlatform] = useState('all');
  const [status, setStatus] = useState('all');
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<PoolAccount | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<PoolAccount | null>(null);
  const [pauseTarget, setPauseTarget] = useState<PoolAccount | null>(null);
  const [importOpen, setImportOpen] = useState(false);
  const [exportOpen, setExportOpen] = useState(false);
  const batchBusy = batch.refresh.isPending || batch.clearError.isPending || batch.test.isPending || batch.deleteSelected.isPending || clearAccounts.isPending;

  useEffect(() => {
    if (activeItem !== 'pool') return;
    const tick = () => setNow(Math.floor(Date.now() / 1000));
    tick();
    const timer = window.setInterval(tick, 30_000);
    return () => window.clearInterval(timer);
  }, [activeItem]);

  const query = search.trim().toLowerCase();
  const visible = accounts.filter((account) => {
    const state = accountState(account, now);
    const text = `${account.name} ${account.platform} ${account.models} ${account.notes}`.toLowerCase();
    return (!query || text.includes(query)) && (platform === 'all' || platform === account.platform) && (status === 'all' || (status === 'active' ? state === 'active' : state !== 'active'));
  });
  const selected = selectedAccountIds(selectedIds, accounts);
  const visibleSelected = visible.filter((account) => selectedIds.has(account.id)).length;
  const allChecked = visible.length > 0 && visibleSelected === visible.length;
  const ready = accounts.filter((account) => accountState(account, now) === 'active').length;
  const toggleAll = () => setSelectedIds((previous) => {
    const next = new Set(previous);
    for (const account of visible) { if (allChecked) next.delete(account.id); else next.add(account.id); }
    return next;
  });
  const toggle = (id: number) => setSelectedIds((previous) => {
    const next = new Set(previous);
    if (next.has(id)) next.delete(id); else next.add(id);
    return next;
  });
  const openEdit = (account: PoolAccount) => { setEditing(account); setFormOpen(true); };
  const openCreate = () => { setEditing(null); setFormOpen(true); };
  const clearFilters = () => { setSearch(''); setPlatform('all'); setStatus('all'); };
  const handleClearAll = () => {
    if (accounts.length === 0) return;
    if (!window.confirm(t('clearAccountsConfirm', { count: accounts.length }))) return;
    clearAccounts.mutate(undefined, {
      onSuccess: (res) => { setSelectedIds(new Set()); toast.success(t('clearAccountsResult', { count: res.deleted })); },
      onError: (e) => toast.error(String(e)),
    });
  };
  const handleExport = () => exportAccounts.mutate(undefined, {
    onSuccess: (data) => {
      const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }));
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = `pool-${pool.id}-accounts-${Date.now()}.json`;
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
      setExportOpen(false);
      toast.success(t('exportSuccess', { count: data.length }));
    },
  });

  return <section aria-label={pool.name} className="h-full min-h-0 overflow-y-auto overscroll-contain rounded-t-xl px-3 py-4 pb-6 md:px-5 md:py-5">
    <div className="space-y-5">
      <header className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
        <div className="flex min-w-0 items-start gap-3">
          <Button variant="outline" size="icon" className="size-10 shrink-0 rounded-lg" aria-label={t('ui.back')} onClick={onBack}><ArrowLeft className="size-4" /></Button>
          <div className="min-w-0 space-y-1.5"><div className="flex min-w-0 flex-wrap items-center gap-2"><h2 className="truncate text-xl font-semibold" title={pool.name}>{pool.name}</h2><Badge variant={pool.enabled ? 'outline' : 'secondary'}>{t(pool.enabled ? 'enabled' : 'disabled')}</Badge></div>
            <p className="text-sm text-muted-foreground break-words">{pool.description || t('ui.description')}</p>
            <p className="text-xs text-muted-foreground">{t('strategy')}: {t.has(`ui.strategyLabels.${pool.strategy}`) ? t(`ui.strategyLabels.${pool.strategy}`) : pool.strategy} · {t('concurrency')}: {pool.default_concurrency}</p>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="icon" className="size-9" aria-label={t('ui.refresh')} title={t('ui.refresh')} disabled={isFetching} onClick={() => void refetch()}><RefreshCw className={`size-4 ${isFetching ? 'animate-spin' : ''}`} /></Button>
          <Button variant="outline" size="sm" onClick={() => setImportOpen(true)}><Upload className="size-4" />{t('importAccounts')}</Button>
          <Button variant="outline" size="sm" onClick={() => setExportOpen(true)} disabled={exportAccounts.isPending || !accounts.length}><Download className="size-4" />{t('exportAccounts')}</Button>
          <Button variant="outline" size="sm" onClick={handleClearAll} disabled={clearAccounts.isPending || accounts.length === 0}><Trash2 className="size-4" />{t('clearAccounts')}</Button>
          <Button size="sm" onClick={openCreate}><Plus className="size-4" />{t('addAccount')}</Button>
        </div>
      </header>
      <div className="grid grid-cols-3 gap-2 md:gap-3">
        {[{ label: t('ui.accounts'), value: accounts.length, icon: Users }, { label: t('ui.ready'), value: ready, icon: Activity }, { label: t('ui.attention'), value: accounts.length - ready, icon: AlertTriangle }].map(({ label, value, icon: Icon }) => <div key={label} className="min-w-0 rounded-xl border border-border/60 bg-card p-3 md:p-4"><div className="flex items-center gap-2 text-xs text-muted-foreground"><Icon className="hidden size-4 sm:block" /><span className="truncate">{label}</span></div><p className="mt-2 text-xl font-semibold tabular-nums">{value.toLocaleString()}</p></div>)}
      </div>
      <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap">
        <div className="relative min-w-0 flex-1"><Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" /><Input aria-label={t('ui.searchAccounts')} placeholder={t('ui.searchAccounts')} value={search} onChange={(event) => setSearch(event.target.value)} className="h-10 pl-9" /></div>
        <div className="grid grid-cols-2 gap-2 sm:w-auto"><Select value={platform} onValueChange={setPlatform}><SelectTrigger aria-label={t('platform')} className="h-10 w-full sm:w-40"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="all">{t('ui.allPlatforms')}</SelectItem>{POOL_PLATFORM_OPTIONS.map((option) => <SelectItem key={option.value} value={option.value}>{t(`platformLabels.${option.value}`)}</SelectItem>)}</SelectContent></Select>
          <Select value={status} onValueChange={setStatus}><SelectTrigger aria-label={t('status')} className="h-10 w-full sm:w-36"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="all">{t('ui.allStatuses')}</SelectItem><SelectItem value="active">{t('ui.ready')}</SelectItem><SelectItem value="attention">{t('ui.attention')}</SelectItem></SelectContent></Select></div>
      </div>
      {selected.length > 0 && <div className="flex flex-wrap items-center gap-2 rounded-xl border border-primary/20 bg-primary/5 p-3" role="region" aria-label={t('ui.selectionDescription')}>
        <span className="mr-auto text-sm font-medium">{t('selectedCount', { count: selected.length })}</span>
        <Button variant="outline" size="sm" disabled={batchBusy || !selected.some((id) => accounts.find((account) => account.id === id)?.type === 'oauth')} onClick={() => batch.refresh.mutate(selected.filter((id) => accounts.find((account) => account.id === id)?.type === 'oauth'), { onSuccess: (res) => toast.success(t('batchRefreshResult', { ok: res.ok, failed: res.failed.length })) })}>{batch.refresh.isPending && <Loader2 className="size-4 animate-spin" />}{t('batchRefresh')}</Button>
        <Button variant="outline" size="sm" disabled={batchBusy} onClick={() => batch.clearError.mutate(selected, { onSuccess: (res) => toast.success(t('batchClearResult', { ok: res.ok, failed: res.failed.length })) })}>{batch.clearError.isPending && <Loader2 className="size-4 animate-spin" />}{t('batchClearError')}</Button>
        <Button variant="outline" size="sm" disabled={batchBusy} onClick={() => batch.test.mutate({ accountIds: selected, model: '' }, { onSuccess: (res) => toast.success(t('batchTestResult', { ok: res.filter((item) => item.success).length, total: res.length })) })}>{batch.test.isPending && <Loader2 className="size-4 animate-spin" />}{t('batchTest')}</Button>
        <Button variant="outline" size="sm" disabled={batchBusy} onClick={() => { if (!window.confirm(t('batchDeleteConfirm', { count: selected.length }))) return; batch.deleteSelected.mutate(selected, { onSuccess: (res) => { setSelectedIds(new Set()); toast.success(t('batchDeleteResult', { count: res.deleted })); }, onError: (e) => toast.error(String(e)) }); }}>{batch.deleteSelected.isPending && <Loader2 className="size-4 animate-spin" />}{t('batchDelete')}</Button>
        <Button variant="ghost" size="icon" className="size-9" aria-label={t('ui.clearSelection')} disabled={batchBusy} onClick={() => setSelectedIds(new Set())}><X className="size-4" /></Button>
      </div>}
      {isLoading ? <LoadingState /> : error ? <ErrorState onRetry={() => void refetch()} /> : !visible.length ? <div className="flex min-h-64 flex-col items-center justify-center gap-3 rounded-xl border border-dashed p-6 text-center"><Users className="size-8 text-muted-foreground" /><h3 className="font-medium">{accounts.length ? t('ui.noResults') : t('noAccounts')}</h3><p className="text-sm text-muted-foreground">{accounts.length ? t('ui.noResultsDescription') : t('ui.accountDescription')}</p><Button variant="outline" onClick={accounts.length ? clearFilters : openCreate}>{accounts.length ? t('ui.resetFilters') : t('addAccount')}</Button></div> : <>
        <div className="flex items-center gap-2 text-sm text-muted-foreground lg:hidden"><AccountCheckbox label={t('ui.selectAll')} checked={allChecked ? true : visibleSelected ? 'indeterminate' : false} onChange={toggleAll} disabled={batchBusy} />{t('ui.selectAll')}<span className="ml-auto tabular-nums">{visible.length} / {accounts.length}</span></div>
        <div className="grid gap-3 sm:grid-cols-2 lg:hidden">{visible.map((account) => <article key={account.id} className="min-w-0 space-y-3 rounded-xl border border-border/60 bg-card p-4">
          <div className="flex items-start gap-3"><AccountCheckbox label={t('ui.selectAccount', { name: account.name || `#${account.id}` })} checked={selectedIds.has(account.id)} onChange={() => toggle(account.id)} disabled={batchBusy} /><div className="min-w-0 flex-1"><h3 className="truncate font-medium" title={account.name}>{account.name || `#${account.id}`}</h3><div className="mt-2 flex flex-wrap gap-1.5"><PlatformBadge platform={account.platform} /><AccountStatus account={account} now={now} /><Badge variant="secondary">{t.has(`typeLabels.${account.type}`) ? t(`typeLabels.${account.type}`) : account.type}</Badge></div></div></div>
          <ModelBadges models={account.models} />
          <dl className="grid grid-cols-2 gap-3 text-xs"><div><dt className="text-muted-foreground">{t('requests')}</dt><dd className="mt-1 tabular-nums">{account.total_requests.toLocaleString()}</dd></div><div><dt className="text-muted-foreground">{t('errors')}</dt><dd className="mt-1 tabular-nums">{account.total_errors.toLocaleString()}</dd></div><div><dt className="text-muted-foreground">{t('concurrency')}</dt><dd className="mt-1">{account.concurrency || pool.default_concurrency}</dd></div><div><dt className="mb-1 text-muted-foreground">{t('quota')}</dt><dd><QuotaCell quota={account.quota} /></dd></div></dl>
          {account.error_message && <details className="rounded-lg bg-destructive/5 p-2 text-xs"><summary className="cursor-pointer text-destructive">{t('errors')}</summary><p className="mt-2 max-h-24 overflow-y-auto break-words text-muted-foreground">{account.error_message}</p></details>}
          <div className="border-t pt-2"><AccountActions account={account} onEdit={openEdit} onDelete={setDeleteTarget} onPause={setPauseTarget} /></div>
        </article>)}</div>
        <div className="hidden max-h-[65dvh] overflow-auto rounded-xl border border-border/60 lg:block"><table className="w-full min-w-[1050px] text-left text-sm"><thead className="sticky top-0 z-10 bg-card text-xs text-muted-foreground"><tr><th className="w-12 p-3"><AccountCheckbox label={t('ui.selectAll')} checked={allChecked ? true : visibleSelected ? 'indeterminate' : false} onChange={toggleAll} disabled={batchBusy} /></th>{['accountName', 'platform', 'status', 'models', 'concurrency', 'requests', 'errors', 'quota', 'actions'].map((key) => <th key={key} scope="col" className="whitespace-nowrap p-3 font-medium">{t(key)}</th>)}</tr></thead><tbody>{visible.map((account) => <tr key={account.id} className="border-t border-border/50 hover:bg-muted/30"><td className="p-3"><AccountCheckbox label={t('ui.selectAccount', { name: account.name || `#${account.id}` })} checked={selectedIds.has(account.id)} onChange={() => toggle(account.id)} disabled={batchBusy} /></td><td className="max-w-56 p-3"><button className="block max-w-full truncate text-left font-medium hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" onClick={() => openEdit(account)}>{account.name || `#${account.id}`}</button><span className="text-xs text-muted-foreground">{t.has(`typeLabels.${account.type}`) ? t(`typeLabels.${account.type}`) : account.type}</span>{account.error_message && <details className="mt-1 text-xs"><summary className="cursor-pointer text-destructive">{t('errors')}</summary><p className="max-h-24 overflow-y-auto break-words text-muted-foreground">{account.error_message}</p></details>}</td><td className="p-3"><PlatformBadge platform={account.platform} /></td><td className="whitespace-nowrap p-3"><AccountStatus account={account} now={now} /></td><td className="min-w-48 max-w-64 p-3"><ModelBadges models={account.models} /></td><td className="p-3 tabular-nums">{account.concurrency || pool.default_concurrency}<span className="block whitespace-nowrap text-xs text-muted-foreground">{t('weightCol')}: {account.weight ?? 0} · {t('loadFactorCol')}: {account.load_factor > 0 ? account.load_factor : account.concurrency || pool.default_concurrency}</span></td><td className="p-3 tabular-nums">{account.total_requests.toLocaleString()}</td><td className="p-3 tabular-nums">{account.total_errors.toLocaleString()}</td><td className="p-3"><QuotaCell quota={account.quota} /></td><td className="p-3"><AccountActions account={account} onEdit={openEdit} onDelete={setDeleteTarget} onPause={setPauseTarget} /></td></tr>)}</tbody></table></div>
      </>}
    </div>
    <AccountFormDialog poolId={pool.id} account={editing} open={formOpen} onOpenChange={setFormOpen} />
    <PoolOperationDialogs poolId={pool.id} deleteTarget={deleteTarget} onDeleteTargetChange={setDeleteTarget} pauseTarget={pauseTarget} onPauseTargetChange={setPauseTarget} importOpen={importOpen} onImportOpenChange={setImportOpen} exportOpen={exportOpen} onExportOpenChange={setExportOpen} onExport={handleExport} exporting={exportAccounts.isPending} />
  </section>;
}
