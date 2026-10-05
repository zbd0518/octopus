'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { CalendarClock, ChevronDown, ChevronUp, Loader2, Plus, Trash2 } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from 'radix-ui';
import { Input } from '@/components/ui/input';
import { toast } from '@/components/common/Toast';
import { formatUnixSeconds } from '@/lib/time';
import {
    useDeletePoolScheduledTest,
    usePoolScheduledTestResults,
    usePoolScheduledTests,
    useCreatePoolScheduledTest,
    useUpdatePoolScheduledTest,
    type PoolScheduledTest,
} from '@/api/endpoints/pool';
import type { PoolAccount } from '@/api/endpoints/pool';
import { CRON_PRESETS, validateCronExpr } from './scheduled-test';

const Check = ({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) => (
    <label className="flex cursor-pointer items-center gap-2 text-sm">
        <Checkbox.Root
            aria-label={label}
            checked={checked}
            onCheckedChange={(v) => onChange(v === true)}
            className="grid size-5 shrink-0 place-items-center rounded border border-input bg-background outline-none focus-visible:ring-2 focus-visible:ring-ring data-[state=checked]:border-primary data-[state=checked]:bg-primary data-[state=checked]:text-primary-foreground"
        >
            <span className="text-xs font-bold text-primary-foreground">{checked ? '✓' : ''}</span>
        </Checkbox.Root>
        {label}
    </label>
);

function PlanResults({ poolId, planId }: { poolId: number; planId: number }) {
    const t = useTranslations('pool');
    const { data: results = [] } = usePoolScheduledTestResults(poolId, planId);
    if (!results.length) {
        return <p className="px-1 py-2 text-xs text-muted-foreground">{t('schedTestNoResults')}</p>;
    }
    return (
        <div className="space-y-1 px-1 py-2">
            {results.map((result) => (
                <div key={result.id} className="flex items-start gap-2 text-xs">
                    <Badge variant={result.success ? 'outline' : 'secondary'} className={result.success ? 'text-green-600' : 'text-destructive'}>
                        {result.success ? t('testSuccess', { latency: result.duration_ms }) : t('testFailed', { error: result.detail })}
                    </Badge>
                    <span className="shrink-0 tabular-nums text-muted-foreground">{formatUnixSeconds(Math.floor(new Date(result.created_at).getTime() / 1000))}</span>
                    <span className="tabular-nums text-muted-foreground">{result.duration_ms}ms</span>
                    <span className="min-w-0 break-all text-muted-foreground">#{result.account_id} {result.detail}</span>
                </div>
            ))}
        </div>
    );
}

function PlanRow({ poolId, plan, accounts, onEdit }: {
    poolId: number;
    plan: PoolScheduledTest;
    accounts: PoolAccount[];
    onEdit: (plan: PoolScheduledTest) => void;
}) {
    const t = useTranslations('pool');
    const deletePlan = useDeletePoolScheduledTest(poolId);
    const [showResults, setShowResults] = useState(false);
    const account = plan.account_id ? accounts.find((a) => a.id === plan.account_id) : null;
    const scopeLabel = plan.account_id ? (account?.name || `#${plan.account_id}`) : t('schedTestScopePool');

    return (
        <div className="min-w-0 rounded-lg border border-border/50 bg-card p-3">
            <div className="flex flex-wrap items-center gap-2">
                <code className="rounded bg-muted px-1.5 py-0.5 text-xs font-semibold">{plan.cron_expr}</code>
                <Badge variant="secondary">{scopeLabel}</Badge>
                {plan.auto_recover && <Badge variant="outline" className="text-green-600">{t('schedTestAutoRecover')}</Badge>}
                <Badge variant={plan.enabled ? 'outline' : 'secondary'} className={plan.enabled ? 'text-green-600' : 'text-muted-foreground'}>
                    {t(plan.enabled ? 'enabled' : 'disabled')}
                </Badge>
                <span className="ml-auto text-xs text-muted-foreground">
                    {t('schedTestLastRun')}: {formatUnixSeconds(plan.last_run_at)} · {t('schedTestNextRun')}: {formatUnixSeconds(plan.next_run_at)}
                </span>
            </div>
            <div className="mt-2 flex items-center gap-1.5">
                <Button variant="ghost" size="sm" onClick={() => setShowResults((v) => !v)}>
                    {showResults ? <ChevronUp className="size-4" /> : <ChevronDown className="size-4" />}
                    {t('schedTestResults')}
                </Button>
                <Button variant="ghost" size="sm" onClick={() => onEdit(plan)}>{t('editAccount')}</Button>
                <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive"
                    disabled={deletePlan.isPending}
                    onClick={() => deletePlan.mutate(plan.id, { onSuccess: () => toast.success(t('schedTestDeleted')) })}
                >
                    {deletePlan.isPending ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
                    {t('ui.confirmDelete')}
                </Button>
            </div>
            {showResults && <PlanResults poolId={poolId} planId={plan.id} />}
        </div>
    );
}

type PlanDraft = {
    editingId: number | null;
    accountId: number | null;
    cronExpr: string;
    enabled: boolean;
    autoRecover: boolean;
};

const EMPTY_DRAFT: PlanDraft = { editingId: null, accountId: null, cronExpr: '*/30 * * * *', enabled: true, autoRecover: false };

export function ScheduledTestsCard({ poolId, accounts }: { poolId: number; accounts: PoolAccount[] }) {
    const t = useTranslations('pool');
    const { data: plans = [] } = usePoolScheduledTests(poolId);
    const createPlan = useCreatePoolScheduledTest(poolId);
    const updatePlan = useUpdatePoolScheduledTest(poolId);
    const [formOpen, setFormOpen] = useState(false);
    const [draft, setDraft] = useState<PlanDraft>(EMPTY_DRAFT);
    const busy = createPlan.isPending || updatePlan.isPending;
    const cronValid = validateCronExpr(draft.cronExpr);

    const openCreate = () => { setDraft({ ...EMPTY_DRAFT }); setFormOpen(true); };
    const openEdit = (plan: PoolScheduledTest) => {
        setDraft({
            editingId: plan.id,
            accountId: plan.account_id ?? null,
            cronExpr: plan.cron_expr,
            enabled: plan.enabled,
            autoRecover: plan.auto_recover,
        });
        setFormOpen(true);
    };

    const submit = () => {
        if (!cronValid || busy) return;
        const payload = {
            account_id: draft.accountId,
            cron_expr: draft.cronExpr.trim(),
            enabled: draft.enabled,
            auto_recover: draft.autoRecover,
        };
        const onSuccess = () => {
            setFormOpen(false);
            toast.success(draft.editingId ? t('schedTestUpdated') : t('schedTestCreated'));
        };
        if (draft.editingId) {
            updatePlan.mutate({ testId: draft.editingId, data: payload }, { onSuccess });
        } else {
            createPlan.mutate(payload, { onSuccess });
        }
    };

    return (
        <section aria-label={t('scheduledTests')} className="rounded-xl border border-border/60 bg-card p-4">
            <div className="flex items-center justify-between gap-2">
                <h3 className="flex items-center gap-2 text-sm font-semibold">
                    <CalendarClock className="size-4" />
                    {t('scheduledTests')}
                </h3>
                <Button variant="outline" size="sm" onClick={openCreate}><Plus className="size-4" />{t('schedTestCreate')}</Button>
            </div>
            <p className="mt-1 text-xs text-muted-foreground">{t('scheduledTestsHint')}</p>

            {formOpen && (
                <div className="mt-3 space-y-3 rounded-lg border border-border bg-muted/30 p-3">
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                        <label className="grid gap-1">
                            <span className="text-xs font-medium text-muted-foreground">{t('schedTestScope')}</span>
                            <select
                                value={draft.accountId ?? 0}
                                onChange={(e) => setDraft((d) => ({ ...d, accountId: Number(e.target.value) > 0 ? Number(e.target.value) : null }))}
                                className="h-10 rounded-xl bg-background border border-border text-sm px-3"
                            >
                                <option value={0}>{t('schedTestScopePool')}</option>
                                {accounts.map((account) => (
                                    <option key={account.id} value={account.id}>{account.name || `#${account.id}`}</option>
                                ))}
                            </select>
                        </label>
                        <label className="grid gap-1">
                            <span className="text-xs font-medium text-muted-foreground">{t('schedTestCron')}</span>
                            <Input
                                value={draft.cronExpr}
                                onChange={(e) => setDraft((d) => ({ ...d, cronExpr: e.target.value }))}
                                aria-invalid={!cronValid}
                                placeholder="*/30 * * * *"
                            />
                        </label>
                    </div>
                    <div className="flex flex-wrap gap-1.5">
                        {CRON_PRESETS.map((preset) => (
                            <button
                                key={preset}
                                type="button"
                                onClick={() => setDraft((d) => ({ ...d, cronExpr: preset }))}
                                className={`rounded-full border px-2.5 py-1 text-xs transition-colors ${draft.cronExpr === preset ? 'border-primary bg-primary/10 text-primary' : 'border-border text-muted-foreground hover:bg-muted'}`}
                            >
                                <code>{preset}</code>
                            </button>
                        ))}
                    </div>
                    {!cronValid && <p className="text-xs text-destructive">{t('schedTestInvalidCron')}</p>}
                    <div className="flex flex-wrap items-center gap-4">
                        <Check checked={draft.enabled} onChange={(v) => setDraft((d) => ({ ...d, enabled: v }))} label={t('schedTestEnabled')} />
                        <Check checked={draft.autoRecover} onChange={(v) => setDraft((d) => ({ ...d, autoRecover: v }))} label={t('schedTestAutoRecover')} />
                    </div>
                    <div className="flex gap-2">
                        <Button size="sm" disabled={!cronValid || busy} onClick={submit}>
                            {busy && <Loader2 className="size-4 animate-spin" />}
                            {draft.editingId ? t('save') : t('schedTestCreate')}
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => setFormOpen(false)}>{t('cancel')}</Button>
                    </div>
                </div>
            )}

            <div className="mt-3 space-y-2">
                {plans.length === 0 ? (
                    <p className="rounded-lg border border-dashed border-border/50 px-3 py-4 text-center text-xs text-muted-foreground">{t('schedTestNoPlans')}</p>
                ) : plans.map((plan) => <PlanRow key={plan.id} poolId={poolId} plan={plan} accounts={accounts} onEdit={openEdit} />)}
            </div>
        </section>
    );
}
