'use client';

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { toast } from '@/components/common/Toast';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { useCreatePool, useUpdatePool, type AccountPool } from '@/api/endpoints/pool';

// 号池调度策略选项（与后端 poolscheduler 支持的策略一致）。
export const POOL_STRATEGY_OPTIONS = ['ewma', 'round_robin', 'random', 'least_loaded'] as const;
export type PoolStrategy = (typeof POOL_STRATEGY_OPTIONS)[number];

type PoolFormDialogProps = {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    /** null = 创建，否则编辑该号池 */
    pool: AccountPool | null;
};

type PoolFormState = {
    name: string;
    description: string;
    strategy: PoolStrategy;
    concurrency: string;
    cooldown: string;
    enabled: boolean;
};

const createDefaults: PoolFormState = {
    name: '',
    description: '',
    strategy: 'ewma',
    concurrency: '1',
    cooldown: '300',
    enabled: true,
};

// 严格整数解析：空串 / 非整数字符串一律视为非法。
function parseInteger(raw: string): number | null {
    const s = raw.trim();
    if (!s || !/^-?\d+$/.test(s)) return null;
    const n = Number(s);
    return Number.isSafeInteger(n) ? n : null;
}

// 创建 / 编辑号池共用对话框：标准 Radix Dialog（焦点陷阱 + Esc 关闭），
// 三段式布局（header / 可滚动 body / footer 带 safe-area 底边距）。
// 错误提示依赖全局 MutationCache onError 的 toast，这里只挂 onSuccess。
export function PoolFormDialog({ open, onOpenChange, pool }: PoolFormDialogProps) {
    const t = useTranslations('pool');
    const createPool = useCreatePool();
    const updatePool = useUpdatePool();
    const [form, setForm] = useState<PoolFormState>(createDefaults);
    const [showErrors, setShowErrors] = useState(false);
    const isPending = createPool.isPending || updatePool.isPending;
    const isEdit = pool !== null;

    // 每次打开时按目标池子重置表单。
    useEffect(() => {
        if (!open) return;
        if (pool) {
            setForm({
                name: pool.name,
                description: pool.description || '',
                strategy: (POOL_STRATEGY_OPTIONS as readonly string[]).includes(pool.strategy)
                    ? (pool.strategy as PoolStrategy)
                    : 'ewma',
                concurrency: String(pool.default_concurrency ?? 1),
                cooldown: String(pool.cooldown_base_sec ?? 300),
                enabled: pool.enabled,
            });
        } else {
            setForm(createDefaults);
        }
        setShowErrors(false);
    }, [open, pool]);

    // 提交进行中禁止关闭（Esc / 遮罩点击 / footer 按钮都经过这里）。
    const handleOpenChange = (next: boolean) => {
        if (!next && isPending) return;
        onOpenChange(next);
    };

    const parsedConcurrency = parseInteger(form.concurrency);
    const parsedCooldown = parseInteger(form.cooldown);
    const trimmedName = form.name.trim();
    const nameInvalid = showErrors && trimmedName.length === 0;
    const concurrencyInvalid = showErrors && (parsedConcurrency === null || parsedConcurrency < 1);
    const cooldownInvalid = showErrors && (parsedCooldown === null || parsedCooldown < 0);

    const handleSubmit = () => {
        if (isPending) return;
        setShowErrors(true);
        if (!trimmedName || parsedConcurrency === null || parsedConcurrency < 1 || parsedCooldown === null || parsedCooldown < 0) {
            return;
        }
        const payload = {
            name: trimmedName,
            description: form.description.trim(),
            strategy: form.strategy,
            default_concurrency: parsedConcurrency,
            cooldown_base_sec: parsedCooldown,
        };
        if (pool) {
            updatePool.mutate(
                { id: pool.id, ...payload, enabled: form.enabled },
                {
                    onSuccess: () => {
                        onOpenChange(false);
                        toast.success(t('ui.updated'));
                    },
                },
            );
        } else {
            createPool.mutate(payload, {
                onSuccess: () => {
                    onOpenChange(false);
                    toast.success(t('created'));
                },
            });
        }
    };

    return (
        <Dialog open={open} onOpenChange={handleOpenChange}>
            <DialogContent className="flex max-h-[calc(100dvh-2rem)] flex-col gap-0 overflow-hidden p-0">
                <DialogHeader className="shrink-0 space-y-1.5 border-b px-5 py-4 pr-12">
                    <DialogTitle>{isEdit ? t('ui.editPool') : t('create')}</DialogTitle>
                    <DialogDescription>
                        {isEdit ? pool?.name : t('ui.createDescription')}
                    </DialogDescription>
                </DialogHeader>

                <form className="flex min-h-0 flex-1 flex-col" onSubmit={(event) => { event.preventDefault(); handleSubmit(); }}>
                <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-5 py-4">
                    <div className="space-y-2">
                        <Label htmlFor="pool-form-name">{t('name')}</Label>
                        <Input
                            id="pool-form-name"
                            value={form.name}
                            onChange={(e) => setForm({ ...form, name: e.target.value })}
                            disabled={isPending}
                            aria-invalid={nameInvalid || undefined}
                        />
                        {nameInvalid && <p className="text-xs text-destructive">{t('ui.poolNameRequired')}</p>}
                    </div>

                    <div className="space-y-2">
                        <Label htmlFor="pool-form-description">{t('description')}</Label>
                        <Input
                            id="pool-form-description"
                            value={form.description}
                            onChange={(e) => setForm({ ...form, description: e.target.value })}
                            disabled={isPending}
                        />
                    </div>

                    <div className="space-y-2">
                        <Label htmlFor="pool-form-strategy">{t('strategy')}</Label>
                        <Select
                            value={form.strategy}
                            onValueChange={(v) => setForm({ ...form, strategy: v as PoolStrategy })}
                            disabled={isPending}
                        >
                            <SelectTrigger id="pool-form-strategy" className="w-full">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {POOL_STRATEGY_OPTIONS.map((s) => (
                                    <SelectItem key={s} value={s}>{t(`ui.strategyLabels.${s}`)}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    <div className="grid grid-cols-2 gap-4">
                        <div className="space-y-2">
                            <Label htmlFor="pool-form-concurrency">{t('concurrency')}</Label>
                            <Input
                                id="pool-form-concurrency"
                                type="number"
                                min={1}
                                step={1}
                                inputMode="numeric"
                                value={form.concurrency}
                                onChange={(e) => setForm({ ...form, concurrency: e.target.value })}
                                disabled={isPending}
                                aria-invalid={concurrencyInvalid || undefined}
                            />
                            {concurrencyInvalid && <p className="text-xs text-destructive">{t('ui.poolNumberInvalid')}</p>}
                        </div>
                        <div className="space-y-2">
                            <Label htmlFor="pool-form-cooldown">{t('cooldown')}</Label>
                            <Input
                                id="pool-form-cooldown"
                                type="number"
                                min={0}
                                step={1}
                                inputMode="numeric"
                                value={form.cooldown}
                                onChange={(e) => setForm({ ...form, cooldown: e.target.value })}
                                disabled={isPending}
                                aria-invalid={cooldownInvalid || undefined}
                            />
                            {cooldownInvalid && <p className="text-xs text-destructive">{t('ui.poolNumberInvalid')}</p>}
                        </div>
                    </div>

                    {isEdit && (
                        <div className="flex items-center justify-between rounded-lg border p-3">
                            <Label htmlFor="pool-form-enabled">{t('enabled')}</Label>
                            <Switch
                                id="pool-form-enabled"
                                checked={form.enabled}
                                onCheckedChange={(v) => setForm({ ...form, enabled: v })}
                                disabled={isPending}
                            />
                        </div>
                    )}
                </div>

                <DialogFooter className="shrink-0 border-t px-5 py-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
                    <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={isPending}>
                        {t('ui.close')}
                    </Button>
                    <Button type="submit" disabled={isPending}>
                        {isEdit
                            ? updatePool.isPending ? t('ui.saving') : t('save')
                            : createPool.isPending ? t('ui.creating') : t('create')}
                    </Button>
                </DialogFooter>
                </form>
            </DialogContent>
        </Dialog>
    );
}
