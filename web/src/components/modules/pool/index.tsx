'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Layers, Plus, Trash2, ChevronLeft } from 'lucide-react';
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
    useCreatePoolAccount,
    useDeletePoolAccount,
    type AccountPool,
    type PoolAccount,
} from '@/api/endpoints/pool';

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

function PoolDetail({ pool, onBack }: { pool: AccountPool; onBack: () => void }) {
    const t = useTranslations('pool');
    const { data: accounts, isLoading, error } = usePoolAccounts(pool.id);
    const createAccount = useCreatePoolAccount(pool.id);
    const deleteAccount = useDeletePoolAccount(pool.id);
    const [dialogOpen, setDialogOpen] = useState(false);
    const [form, setForm] = useState({ name: '', credentials: '', base_url: '', priority: 0, concurrency: 0, proxy_config_id: null as number | null });
    const [now] = useState(() => Math.floor(Date.now() / 1000));

    if (isLoading) return <LoadingState />;
    if (error) return <ErrorState message={String(error)} />;

    const handleCreate = () => {
        createAccount.mutate(form, {
            onSuccess: () => { setDialogOpen(false); setForm({ name: '', credentials: '', base_url: '', priority: 0, concurrency: 0, proxy_config_id: null }); toast.success(t('accountCreated')); },
            onError: (e) => toast.error(String(e)),
        });
    };


    return (
        <div className="space-y-4 p-4">
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                    <Button variant="ghost" size="icon" onClick={onBack}><ChevronLeft className="h-4 w-4" /></Button>
                    <h2 className="text-xl font-semibold">{pool.name}</h2>
                    <Badge variant={pool.enabled ? 'default' : 'secondary'}>{pool.enabled ? t('enabled') : t('disabled')}</Badge>
                </div>
                <Button onClick={() => setDialogOpen(true)} size="sm">
                    <Plus className="h-4 w-4 mr-1" />
                    {t('addAccount')}
                </Button>
            </div>

            <div className="rounded-lg border overflow-hidden">
                <table className="w-full text-sm">
                    <thead className="bg-muted/50">
                        <tr>
                            <th className="text-left p-3 font-medium">{t('accountName')}</th>
                            <th className="text-left p-3 font-medium">{t('status')}</th>
                            <th className="text-left p-3 font-medium">{t('concurrency')}</th>
                            <th className="text-left p-3 font-medium">{t('requests')}</th>
                            <th className="text-left p-3 font-medium">{t('errors')}</th>
                            <th className="text-left p-3 font-medium">{t('cooldownStatus')}</th>
                            <th className="text-right p-3 font-medium"></th>
                        </tr>
                    </thead>
                    <tbody>
                        {(accounts ?? []).map((acct: PoolAccount) => {
                            const inCooldown = acct.rate_limit_reset_at > now || acct.overload_until > now;
                            return (
                                <tr key={acct.id} className="border-t">
                                    <td className="p-3">{acct.name || `#${acct.id}`}</td>
                                    <td className="p-3">
                                        <Badge variant={acct.status === 'active' && acct.schedulable ? 'default' : 'destructive'}>
                                            {acct.status}{!acct.schedulable ? ' (off)' : ''}
                                        </Badge>
                                    </td>
                                    <td className="p-3">{acct.concurrency || pool.default_concurrency}</td>
                                    <td className="p-3">{acct.total_requests}</td>
                                    <td className="p-3">{acct.total_errors}</td>
                                    <td className="p-3">
                                        {inCooldown ? <Badge variant="outline" className="text-amber-600">{t('cooling')}</Badge> : <span className="text-muted-foreground">-</span>}
                                    </td>
                                    <td className="p-3 text-right">
                                        <Button variant="ghost" size="icon" className="h-7 w-7"
                                            onClick={() => deleteAccount.mutate(acct.id, { onSuccess: () => toast.success(t('accountDeleted')), onError: (e) => toast.error(String(e)) })}>
                                            <Trash2 className="h-3.5 w-3.5" />
                                        </Button>
                                    </td>
                                </tr>
                            );
                        })}
                        {(!accounts || accounts.length === 0) && (
                            <tr><td colSpan={7} className="p-6 text-center text-muted-foreground">{t('noAccounts')}</td></tr>
                        )}
                    </tbody>
                </table>
            </div>

            <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>{t('addAccount')}</DialogTitle>
                    </DialogHeader>
                    <div className="space-y-4">
                        <div>
                            <Label>{t('accountName')}</Label>
                            <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="claude-01" />
                        </div>
                        <div>
                            <Label>{t('credentials')}</Label>
                            <Input value={form.credentials} onChange={(e) => setForm({ ...form, credentials: e.target.value })} placeholder='{"type":"bearer","token":"sk-ant-..."}' />
                        </div>
                        <div>
                            <Label>{t('baseUrl')}</Label>
                            <Input value={form.base_url} onChange={(e) => setForm({ ...form, base_url: e.target.value })} placeholder="https://api.anthropic.com" />
                        </div>
                        <div className="grid grid-cols-2 gap-4">
                            <div>
                                <Label>{t('priority')}</Label>
                                <Input type="number" value={form.priority} onChange={(e) => setForm({ ...form, priority: Number(e.target.value) })} />
                            </div>
                            <div>
                                <Label>{t('concurrency')} (0={t('inheritPool')})</Label>
                                <Input type="number" value={form.concurrency} onChange={(e) => setForm({ ...form, concurrency: Number(e.target.value) })} />
                            </div>
                        </div>
                        <div>
                            <Label>{t('proxyConfig')}</Label>
                            <Input type="number" value={form.proxy_config_id ?? ''} onChange={(e) => setForm({ ...form, proxy_config_id: e.target.value ? Number(e.target.value) : null })} placeholder={t('proxyConfigPlaceholder')} />
                        </div>
                    </div>
                    <DialogFooter>
                        <Button onClick={handleCreate} disabled={createAccount.isPending}>{t('addAccount')}</Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}
