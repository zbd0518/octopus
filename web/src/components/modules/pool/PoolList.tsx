'use client';

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Layers, Pencil, Plus, RefreshCw, Search, Trash2 } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { toast } from '@/components/common/Toast';
import { LoadingState } from '@/components/common/LoadingState';
import { ErrorState } from '@/components/common/ErrorState';
import {
    AlertDialog,
    AlertDialogAction,
    AlertDialogCancel,
    AlertDialogContent,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogHeader,
    AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { useDeletePool, usePoolList, type AccountPool } from '@/api/endpoints/pool';
import { cn } from '@/lib/utils';
import { POOL_STRATEGY_OPTIONS, PoolFormDialog, type PoolStrategy } from './PoolFormDialog';

// 策略中文标签；未知策略回退显示原始值，避免 next-intl 缺 key 时报错。
function useStrategyLabel() {
    const t = useTranslations('pool');
    return (strategy: string) => {
        if (!(POOL_STRATEGY_OPTIONS as readonly string[]).includes(strategy)) return strategy;
        return t(`ui.strategyLabels.${strategy as PoolStrategy}`);
    };
}

export function PoolList({ onSelect }: { onSelect: (pool: AccountPool) => void }) {
    const t = useTranslations('pool');
    const strategyText = useStrategyLabel();
    const { data: pools, isLoading, isRefetching, error, refetch } = usePoolList();
    const deletePool = useDeletePool();
    const [search, setSearch] = useState('');
    const [formOpen, setFormOpen] = useState(false);
    const [editingPool, setEditingPool] = useState<AccountPool | null>(null);
    const [deleteTarget, setDeleteTarget] = useState<AccountPool | null>(null);

    // OAuth 授权回跳：URL 带 pool_id 时自动选中对应池子（后端 302 到 /pool?oauth=...&pool_id=N）。
    // 等待池子列表加载完成后处理一次并清理参数。
    useEffect(() => {
        if (typeof window === 'undefined') return;
        const params = new URLSearchParams(window.location.search);
        const poolIdStr = params.get('pool_id');
        if (!poolIdStr) return;
        if (!pools) return; // 列表未加载，等数据到达后再处理
        const url = new URL(window.location.href);
        url.searchParams.delete('pool_id');
        window.history.replaceState({}, '', url.toString());
        const target = pools.find((p) => p.id === Number(poolIdStr));
        if (target) onSelect(target);
    }, [pools, onSelect]);

    const allPools = pools ?? [];
    // 搜索：按名称 / 描述 / 策略（原始值 + 中文标签）。
    const keyword = search.trim().toLowerCase();
    const visiblePools = keyword
        ? allPools.filter((pool) =>
              [pool.name, pool.description, pool.strategy, strategyText(pool.strategy)]
                  .filter((v): v is string => Boolean(v))
                  .some((v) => v.toLowerCase().includes(keyword)))
        : allPools;

    const openCreate = () => { setEditingPool(null); setFormOpen(true); };
    const openEdit = (pool: AccountPool) => { setEditingPool(pool); setFormOpen(true); };

    const handleConfirmDelete = () => {
        if (!deleteTarget) return;
        // 失败提示由全局 MutationCache onError 统一 toast，这里不重复挂 onError。
        deletePool.mutate(deleteTarget.id, {
            onSuccess: () => {
                toast.success(t('deleted'));
                setDeleteTarget(null);
            },
        });
    };

    if (isLoading) {
        return (
            <section className="flex h-full min-h-0 flex-col overflow-y-auto overscroll-contain rounded-t-xl px-3 py-4 md:px-4 md:py-6">
                <LoadingState />
            </section>
        );
    }
    if (error) {
        return (
            <section className="flex h-full min-h-0 flex-col overflow-y-auto overscroll-contain rounded-t-xl px-3 py-4 md:px-4 md:py-6">
                <ErrorState message={String(error)} onRetry={() => refetch()} />
            </section>
        );
    }

    return (
        <section className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto overscroll-contain rounded-t-xl px-3 py-4 md:px-4 md:py-6">
            {/* 头部：标题 + 号池计数 + 描述 + 刷新 / 创建 */}
            <header className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div className="min-w-0 space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                        <h2 className="flex items-center gap-2 text-xl font-semibold">
                            <Layers className="size-5" />
                            {t('title')}
                        </h2>
                        <Badge variant="secondary" className="tabular-nums">
                            {t('ui.pools')} · {allPools.length}
                        </Badge>
                    </div>
                    <p className="text-sm text-muted-foreground">{t('ui.description')}</p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                    <Button
                        variant="outline"
                        size="sm"
                        onClick={() => refetch()}
                        disabled={isRefetching}
                        aria-label={t('ui.refresh')}
                    >
                        <RefreshCw className={cn('size-4', isRefetching && 'animate-spin')} />
                        <span className="hidden sm:inline">{isRefetching ? t('ui.refreshing') : t('ui.refresh')}</span>
                    </Button>
                    <Button size="sm" onClick={openCreate}>
                        <Plus className="size-4" />
                        {t('create')}
                    </Button>
                </div>
            </header>

            {/* 搜索框 */}
            <div className="relative max-w-md">
                <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    placeholder={t('ui.searchPools')}
                    aria-label={t('ui.searchPools')}
                    className="pl-9"
                />
            </div>

            {allPools.length === 0 ? (
                // 空态引导
                <div className="flex flex-1 items-center justify-center">
                    <div className="flex max-w-sm flex-col items-center gap-3 rounded-xl border border-dashed p-8 text-center">
                        <div className="grid size-12 place-items-center rounded-lg border bg-card">
                            <Layers className="size-6 text-muted-foreground" />
                        </div>
                        <div className="space-y-1">
                            <p className="font-medium">{t('ui.noPools')}</p>
                            <p className="text-sm text-muted-foreground">{t('ui.noPoolsDescription')}</p>
                        </div>
                        <Button size="sm" onClick={openCreate}>
                            <Plus className="size-4" />
                            {t('create')}
                        </Button>
                    </div>
                </div>
            ) : visiblePools.length === 0 ? (
                // 搜索无结果
                <div className="flex flex-1 items-center justify-center">
                    <div className="flex flex-col items-center gap-3 p-8 text-center">
                        <div className="space-y-1">
                            <p className="font-medium">{t('ui.noResults')}</p>
                            <p className="text-sm text-muted-foreground">{t('ui.noResultsDescription')}</p>
                        </div>
                        <Button variant="outline" size="sm" onClick={() => setSearch('')}>
                            {t('ui.resetFilters')}
                        </Button>
                    </div>
                </div>
            ) : (
                <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                    {visiblePools.map((pool) => (
                        <PoolCard
                            key={pool.id}
                            pool={pool}
                            onSelect={onSelect}
                            onEdit={openEdit}
                            onDelete={setDeleteTarget}
                        />
                    ))}
                </div>
            )}

            <PoolFormDialog open={formOpen} onOpenChange={setFormOpen} pool={editingPool} />

            {/* 删除二次确认 */}
            <AlertDialog
                open={deleteTarget !== null}
                onOpenChange={(open) => {
                    if (deletePool.isPending) return; // 删除进行中禁止关闭
                    if (!open) setDeleteTarget(null);
                }}
            >
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>{t('ui.deletePoolTitle')}</AlertDialogTitle>
                        <AlertDialogDescription>
                            {deleteTarget ? t('ui.deletePoolDescription', { name: deleteTarget.name }) : ''}
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel disabled={deletePool.isPending}>{t('cancel')}</AlertDialogCancel>
                        <AlertDialogAction
                            variant="destructive"
                            disabled={deletePool.isPending}
                            onClick={(e) => {
                                e.preventDefault(); // 成功后再关闭，pending 期间弹窗保持
                                handleConfirmDelete();
                            }}
                        >
                            {deletePool.isPending ? t('ui.deleting') : t('ui.confirmDelete')}
                        </AlertDialogAction>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </section>
    );
}

// 单个号池卡片：主体是整卡 button（可键盘操作进入详情），编辑/删除是独立按钮避免嵌套交互。
function PoolCard({ pool, onSelect, onEdit, onDelete }: {
    pool: AccountPool;
    onSelect: (pool: AccountPool) => void;
    onEdit: (pool: AccountPool) => void;
    onDelete: (pool: AccountPool) => void;
}) {
    const t = useTranslations('pool');
    const strategyText = useStrategyLabel();
    return (
        <div className="flex flex-col gap-3 rounded-xl border bg-card p-4 transition-colors hover:border-primary/50">
            <button
                type="button"
                onClick={() => onSelect(pool)}
                className="flex min-w-0 flex-1 flex-col items-start gap-2 rounded-lg text-left outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
            >
                <span className="flex w-full items-center gap-2">
                    <span className="min-w-0 flex-1 truncate text-sm font-medium" title={pool.name}>{pool.name}</span>
                    <Badge variant={pool.enabled ? 'default' : 'secondary'} className="shrink-0">
                        {pool.enabled ? t('enabled') : t('disabled')}
                    </Badge>
                </span>
                {pool.description && (
                    <span className="line-clamp-2 w-full text-xs text-muted-foreground">{pool.description}</span>
                )}
                <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                    <span>{t('strategy')}: {strategyText(pool.strategy)}</span>
                    <span>{t('concurrency')}: {pool.default_concurrency}</span>
                    <span>{t('cooldown')}: {pool.cooldown_base_sec}s</span>
                </span>
            </button>
            <div className="flex items-center justify-between gap-2 border-t pt-3">
                <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">{t('ui.openPool')}</span>
                <div className="flex shrink-0 items-center gap-1">
                    <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={t('ui.editPool')}
                        title={t('ui.editPool')}
                        onClick={() => onEdit(pool)}
                    >
                        <Pencil className="size-3.5" />
                    </Button>
                    <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={t('ui.deletePool')}
                        title={t('ui.deletePool')}
                        className="text-destructive hover:text-destructive"
                        onClick={() => onDelete(pool)}
                    >
                        <Trash2 className="size-3.5" />
                    </Button>
                </div>
            </div>
        </div>
    );
}
