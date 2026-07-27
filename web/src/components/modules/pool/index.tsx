'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Layers, Plus, Trash2, ChevronLeft, Pencil, FlaskConical, RefreshCw, KeyRound, Upload } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { toast } from '@/components/common/Toast';
import { LoadingState } from '@/components/common/LoadingState';
import { ErrorState } from '@/components/common/ErrorState';
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogFooter,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import {
    usePoolList,
    usePoolAccounts,
    useCreatePool,
    useDeletePool,
    useDeletePoolAccount,
    useTestPoolAccount,
    useFetchPoolQuota,
    useRefreshPoolToken,
    useImportPoolAccounts,
    type AccountPool,
    type PoolAccount,
} from '@/api/endpoints/pool';
import { AccountFormDialog } from './AccountFormDialog';
import { POOL_PLATFORM_OPTIONS } from './type-options';

export function Pool() {
    const [selectedPool, setSelectedPool] = useState<AccountPool | null>(null);

    if (selectedPool) {
        return <PoolDetail pool={selectedPool} onBack={() => setSelectedPool(null)} />;
    }
    return <PoolList onSelect={setSelectedPool} />;
}

function PoolList({ onSelect }: { onSelect: (pool: AccountPool) => void }) {
    const t = useTranslations('pool');
    const { data: pools, isLoading, error } = usePoolList();
    const createPool = useCreatePool();
    const deletePool = useDeletePool();
    const [dialogOpen, setDialogOpen] = useState(false);
    const [form, setForm] = useState({ name: '', description: '', strategy: 'ewma', default_concurrency: 1, cooldown_base_sec: 300 });

    if (isLoading) return <LoadingState />;
    if (error) return <ErrorState message={String(error)} />;

    const handleCreate = () => {
        createPool.mutate(form, {
            onSuccess: () => { setDialogOpen(false); setForm({ name: '', description: '', strategy: 'ewma', default_concurrency: 1, cooldown_base_sec: 300 }); toast.success(t('created')); },
            onError: (e) => toast.error(String(e)),
        });
    };

    return (
        <div className="space-y-4 p-4">
            <div className="flex items-center justify-between">
                <h2 className="text-xl font-semibold flex items-center gap-2">
                    <Layers className="h-5 w-5" />
                    {t('title')}
                </h2>
                <Button onClick={() => setDialogOpen(true)} size="sm">
                    <Plus className="h-4 w-4 mr-1" />
                    {t('create')}
                </Button>
            </div>

            <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3">
                {(pools ?? []).map((pool) => (
                    <div
                        key={pool.id}
                        className="rounded-lg border p-4 cursor-pointer hover:border-primary/50 transition-colors"
                        onClick={() => onSelect(pool)}
                    >
                        <div className="flex items-center justify-between">
                            <span className="font-medium">{pool.name}</span>
                            <div className="flex items-center gap-1">
                                <Badge variant={pool.enabled ? 'default' : 'secondary'}>
                                    {pool.enabled ? t('enabled') : t('disabled')}
                                </Badge>
                                <Button
                                    variant="ghost" size="icon" className="h-7 w-7"
                                    onClick={(e) => { e.stopPropagation(); deletePool.mutate(pool.id, { onSuccess: () => toast.success(t('deleted')), onError: (e) => toast.error(String(e)) }); }}
                                >
                                    <Trash2 className="h-3.5 w-3.5" />
                                </Button>
                            </div>
                        </div>
                        {pool.description && <p className="text-sm text-muted-foreground mt-1">{pool.description}</p>}
                        <div className="flex items-center gap-3 mt-2 text-xs text-muted-foreground">
                            <span>{t('strategy')}: {pool.strategy}</span>
                            <span>{t('concurrency')}: {pool.default_concurrency}</span>
                        </div>
                    </div>
                ))}
            </div>

            <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>{t('create')}</DialogTitle>
                    </DialogHeader>
                    <div className="space-y-4">
                        <div>
                            <Label>{t('name')}</Label>
                            <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
                        </div>
                        <div>
                            <Label>{t('description')}</Label>
                            <Input value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
                        </div>
                        <div>
                            <Label>{t('strategy')}</Label>
                            <Select value={form.strategy} onValueChange={(v) => setForm({ ...form, strategy: v })}>
                                <SelectTrigger><SelectValue /></SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="ewma">EWMA</SelectItem>
                                    <SelectItem value="round_robin">Round Robin</SelectItem>
                                    <SelectItem value="random">Random</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                        <div className="grid grid-cols-2 gap-4">
                            <div>
                                <Label>{t('concurrency')}</Label>
                                <Input type="number" value={form.default_concurrency} onChange={(e) => setForm({ ...form, default_concurrency: Number(e.target.value) })} />
                            </div>
                            <div>
                                <Label>{t('cooldown')}</Label>
                                <Input type="number" value={form.cooldown_base_sec} onChange={(e) => setForm({ ...form, cooldown_base_sec: Number(e.target.value) })} />
                            </div>
                        </div>
                    </div>
                    <DialogFooter>
                        <Button onClick={handleCreate} disabled={!form.name || createPool.isPending}>{t('create')}</Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}

// 平台标签展示。
function PlatformBadge({ platform }: { platform: string }) {
    const opt = POOL_PLATFORM_OPTIONS.find((o) => o.value === platform);
    return <Badge variant="outline" className="text-xs">{opt?.label || platform}</Badge>;
}

// 解析 quota JSON 字符串展示用量。
function QuotaCell({ quota }: { quota: string }) {
    if (!quota) return <span className="text-muted-foreground">-</span>;
    let q: { used?: number; total?: number } | null = null;
    try {
        q = JSON.parse(quota);
    } catch {
        return <span className="text-xs text-muted-foreground">-</span>;
    }
    const used = Number(q?.used) || 0;
    const total = Number(q?.total) || 0;
    if (total <= 0) return <span className="text-xs text-muted-foreground">{used}</span>;
    const pct = Math.min(100, (used / total) * 100);
    const color = pct > 90 ? 'text-destructive' : pct > 70 ? 'text-amber-600' : 'text-muted-foreground';
    return (
        <div className="text-xs">
            <span className={color}>{used} / {total}</span>
            <div className="mt-0.5 h-1 w-16 overflow-hidden rounded bg-muted">
                <div className="h-full bg-primary" style={{ width: `${pct}%` }} />
            </div>
        </div>
    );
}

function PoolDetail({ pool, onBack }: { pool: AccountPool; onBack: () => void }) {
    const t = useTranslations('pool');
    const { data: accounts, isLoading, error } = usePoolAccounts(pool.id);
    const deleteAccount = useDeletePoolAccount(pool.id);
    const testAccount = useTestPoolAccount(pool.id);
    const fetchQuota = useFetchPoolQuota(pool.id);
    const refreshToken = useRefreshPoolToken(pool.id);
    const importAccounts = useImportPoolAccounts();
    const [dialogOpen, setDialogOpen] = useState(false);
    const [editingAccount, setEditingAccount] = useState<PoolAccount | null>(null);
    const [importOpen, setImportOpen] = useState(false);
    const [importText, setImportText] = useState('');
    const [now] = useState(() => Math.floor(Date.now() / 1000));

    if (isLoading) return <LoadingState />;
    if (error) return <ErrorState message={String(error)} />;

    const openCreate = () => { setEditingAccount(null); setDialogOpen(true); };
    const openEdit = (acct: PoolAccount) => { setEditingAccount(acct); setDialogOpen(true); };

    const handleImport = () => {
        importAccounts.mutate(
            { poolId: pool.id, accounts: importText },
            {
                onSuccess: (res) => { setImportOpen(false); setImportText(''); toast.success(t('importSuccess', { count: res.imported })); },
                onError: (e) => toast.error(String(e)),
            },
        );
    };

    return (
        <div className="space-y-4 p-4">
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                    <Button variant="ghost" size="icon" onClick={onBack}><ChevronLeft className="h-4 w-4" /></Button>
                    <h2 className="text-xl font-semibold">{pool.name}</h2>
                    <Badge variant={pool.enabled ? 'default' : 'secondary'}>{pool.enabled ? t('enabled') : t('disabled')}</Badge>
                </div>
                <div className="flex gap-2">
                    <Button variant="outline" size="sm" onClick={() => setImportOpen(true)}>
                        <Upload className="h-4 w-4 mr-1" />
                        {t('importAccounts')}
                    </Button>
                    <Button onClick={openCreate} size="sm">
                        <Plus className="h-4 w-4 mr-1" />
                        {t('addAccount')}
                    </Button>
                </div>
            </div>

            <div className="overflow-x-auto rounded-lg border">
                <table className="w-full text-sm">
                    <thead className="bg-muted/50">
                        <tr>
                            <th className="text-left p-3 font-medium whitespace-nowrap">{t('accountName')}</th>
                            <th className="text-left p-3 font-medium whitespace-nowrap">{t('platform')}</th>
                            <th className="text-left p-3 font-medium whitespace-nowrap">{t('type')}</th>
                            <th className="text-left p-3 font-medium whitespace-nowrap">{t('status')}</th>
                            <th className="text-left p-3 font-medium whitespace-nowrap">{t('models')}</th>
                            <th className="text-left p-3 font-medium whitespace-nowrap">{t('concurrency')}</th>
                            <th className="text-left p-3 font-medium whitespace-nowrap">{t('requests')}</th>
                            <th className="text-left p-3 font-medium whitespace-nowrap">{t('errors')}</th>
                            <th className="text-left p-3 font-medium whitespace-nowrap">{t('quota')}</th>
                            <th className="text-left p-3 font-medium whitespace-nowrap">{t('cooldownStatus')}</th>
                            <th className="text-right p-3 font-medium whitespace-nowrap">{t('actions')}</th>
                        </tr>
                    </thead>
                    <tbody>
                        {(accounts ?? []).map((acct: PoolAccount) => {
                            const inCooldown = acct.rate_limit_reset_at > now || acct.overload_until > now;
                            const tokenExpired = acct.type === 'oauth' && acct.token_expires_at > 0 && acct.token_expires_at < now + 60;
                            return (
                                <tr key={acct.id} className="border-t">
                                    <td className="p-3 whitespace-nowrap">{acct.name || `#${acct.id}`}</td>
                                    <td className="p-3"><PlatformBadge platform={acct.platform} /></td>
                                    <td className="p-3"><Badge variant="secondary" className="text-xs">{t(`typeLabels.${acct.type}`)}</Badge></td>
                                    <td className="p-3">
                                        {tokenExpired ? (
                                            <Badge variant="destructive" className="text-xs">{t('tokenExpired')}</Badge>
                                        ) : inCooldown ? (
                                            <Badge variant="outline" className="text-xs text-amber-600">{t('cooling')}</Badge>
                                        ) : acct.status === 'active' && acct.schedulable ? (
                                            <Badge variant="default" className="text-xs">{t('active')}</Badge>
                                        ) : (
                                            <Badge variant="destructive" className="text-xs">{acct.status}</Badge>
                                        )}
                                    </td>
                                    <td className="p-3 max-w-[160px]">
                                        {acct.models ? (
                                            <div className="flex flex-wrap gap-1">
                                                {acct.models.split(',').slice(0, 3).map((m, i) => (
                                                    <Badge key={i} variant="outline" className="text-xs">{m.trim()}</Badge>
                                                ))}
                                                {acct.models.split(',').length > 3 && <span className="text-xs text-muted-foreground">+{acct.models.split(',').length - 3}</span>}
                                            </div>
                                        ) : <span className="text-xs text-muted-foreground">{t('allModels')}</span>}
                                    </td>
                                    <td className="p-3">{acct.concurrency || pool.default_concurrency}</td>
                                    <td className="p-3">{acct.total_requests}</td>
                                    <td className="p-3">{acct.total_errors}</td>
                                    <td className="p-3"><QuotaCell quota={acct.quota} /></td>
                                    <td className="p-3">{inCooldown ? <Badge variant="outline" className="text-xs text-amber-600">{t('cooling')}</Badge> : <span className="text-muted-foreground">-</span>}</td>
                                    <td className="p-3 text-right whitespace-nowrap">
                                        <div className="flex justify-end gap-1">
                                            <Button variant="ghost" size="icon" className="h-7 w-7" title={t('editAccount')} onClick={() => openEdit(acct)}>
                                                <Pencil className="h-3.5 w-3.5" />
                                            </Button>
                                            <Button
                                                variant="ghost" size="icon" className="h-7 w-7" title={t('testAccount')}
                                                disabled={testAccount.isPending}
                                                onClick={() => {
                                                    const model = acct.models?.split(',')[0]?.trim() || 'gpt-4o-mini';
                                                    testAccount.mutate({ accountId: acct.id, model }, {
                                                        onSuccess: (res) => res.success ? toast.success(t('testSuccess', { latency: res.latency_ms })) : toast.error(t('testFailed', { error: res.error || '' })),
                                                        onError: (e) => toast.error(String(e)),
                                                    });
                                                }}
                                            >
                                                <FlaskConical className="h-3.5 w-3.5" />
                                            </Button>
                                            <Button variant="ghost" size="icon" className="h-7 w-7" title={t('refreshQuota')}
                                                disabled={fetchQuota.isPending}
                                                onClick={() => fetchQuota.mutate(acct.id, { onSuccess: () => toast.success(t('quotaRefreshed')), onError: (e) => toast.error(String(e)) })}
                                            >
                                                <RefreshCw className="h-3.5 w-3.5" />
                                            </Button>
                                            {acct.type === 'oauth' && (
                                                <Button variant="ghost" size="icon" className="h-7 w-7" title={t('refreshToken')}
                                                    disabled={refreshToken.isPending}
                                                    onClick={() => refreshToken.mutate(acct.id, { onSuccess: () => toast.success(t('tokenRefreshed')), onError: (e) => toast.error(String(e)) })}
                                                >
                                                    <KeyRound className="h-3.5 w-3.5" />
                                                </Button>
                                            )}
                                            <Button variant="ghost" size="icon" className="h-7 w-7" title={t('accountDeleted')}
                                                onClick={() => deleteAccount.mutate(acct.id, { onSuccess: () => toast.success(t('accountDeleted')), onError: (e) => toast.error(String(e)) })}
                                            >
                                                <Trash2 className="h-3.5 w-3.5" />
                                            </Button>
                                        </div>
                                    </td>
                                </tr>
                            );
                        })}
                        {(!accounts || accounts.length === 0) && (
                            <tr><td colSpan={11} className="p-6 text-center text-muted-foreground">{t('noAccounts')}</td></tr>
                        )}
                    </tbody>
                </table>
            </div>

            <AccountFormDialog
                poolId={pool.id}
                account={editingAccount}
                open={dialogOpen}
                onOpenChange={setDialogOpen}
            />

            <Dialog open={importOpen} onOpenChange={setImportOpen}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>{t('importAccounts')}</DialogTitle>
                    </DialogHeader>
                    <div className="space-y-2">
                        <p className="text-sm text-muted-foreground">{t('importHint')}</p>
                        <textarea
                            rows={10}
                            className="w-full rounded-md border border-input bg-transparent px-3 py-2 font-mono text-xs"
                            value={importText}
                            onChange={(e) => setImportText(e.target.value)}
                            placeholder={t('importPlaceholder')}
                        />
                    </div>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setImportOpen(false)}>{t('cancel')}</Button>
                        <Button onClick={handleImport} disabled={!importText || importAccounts.isPending}>{t('import')}</Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}
