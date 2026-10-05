'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { ListFilter, Loader2, Plus, Trash2 } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { toast } from '@/components/common/Toast';
import {
    useCreatePoolUnschedRule,
    useDeletePoolUnschedRule,
    usePoolUnschedRules,
    useUpdatePoolUnschedRule,
    type PoolUnschedRule,
} from '@/api/endpoints/pool';

type RuleDraft = {
    editingId: number | null;
    name: string;
    matchStatusCode: string;
    matchKeyword: string;
    durationMinutes: string;
    sortOrder: string;
    enabled: boolean;
};

const EMPTY_DRAFT: RuleDraft = {
    editingId: null,
    name: '',
    matchStatusCode: '',
    matchKeyword: '',
    durationMinutes: '30',
    sortOrder: '0',
    enabled: true,
};

const INTEGER_RE = /^[+-]?\d+$/;

/** Client-side mirror of the backend rule validation. */
export function validateUnschedRuleDraft(draft: RuleDraft): boolean {
    const code = draft.matchStatusCode.trim();
    const keyword = draft.matchKeyword.trim();
    if (code === '' && keyword === '') return false;
    if (code !== '') {
        if (!INTEGER_RE.test(code)) return false;
        const n = Number(code);
        if (n < 400 || n > 599) return false;
    }
    if (!INTEGER_RE.test(draft.durationMinutes.trim()) || Number(draft.durationMinutes) <= 0) return false;
    if (draft.sortOrder.trim() !== '' && !INTEGER_RE.test(draft.sortOrder.trim())) return false;
    return true;
}

/** Serializes the draft into the API request shape (null = status dimension off). */
export function unschedRuleDraftToPayload(draft: RuleDraft): {
    name: string;
    match_status_code: number | null;
    match_keyword: string;
    duration_minutes: number;
    enabled: boolean;
    sort_order: number;
} {
    const code = draft.matchStatusCode.trim();
    return {
        name: draft.name.trim(),
        match_status_code: code === '' ? null : Number(code),
        match_keyword: draft.matchKeyword.trim(),
        duration_minutes: Number(draft.durationMinutes),
        enabled: draft.enabled,
        sort_order: draft.sortOrder.trim() === '' ? 0 : Number(draft.sortOrder),
    };
}

export function draftFromUnschedRule(rule: PoolUnschedRule): RuleDraft {
    return {
        editingId: rule.id,
        name: rule.name,
        matchStatusCode: rule.match_status_code != null ? String(rule.match_status_code) : '',
        matchKeyword: rule.match_keyword ?? '',
        durationMinutes: String(rule.duration_minutes ?? 30),
        sortOrder: String(rule.sort_order ?? 0),
        enabled: rule.enabled,
    };
}

function RuleRow({ rule, onEdit }: { rule: PoolUnschedRule; onEdit: (rule: PoolUnschedRule) => void }) {
    const t = useTranslations('pool');
    const updateRule = useUpdatePoolUnschedRule();
    const deleteRule = useDeletePoolUnschedRule();
    return (
        <div className="min-w-0 rounded-lg border border-border/50 bg-card p-3">
            <div className="flex flex-wrap items-center gap-2">
                <span className="truncate text-sm font-medium">{rule.name || `#${rule.id}`}</span>
                {rule.match_status_code != null && <Badge variant="secondary">HTTP {rule.match_status_code}</Badge>}
                {rule.match_keyword && <Badge variant="secondary" className="max-w-full break-all">“{rule.match_keyword}”</Badge>}
                <Badge variant="outline">{rule.duration_minutes}min</Badge>
                <span className="ml-auto flex items-center gap-2">
                    <Switch
                        aria-label={t('unschedRuleEnabled')}
                        checked={rule.enabled}
                        onCheckedChange={(v) => updateRule.mutate({
                            id: rule.id,
                            data: {
                                name: rule.name,
                                match_status_code: rule.match_status_code ?? null,
                                match_keyword: rule.match_keyword,
                                duration_minutes: rule.duration_minutes,
                                enabled: v,
                                sort_order: rule.sort_order,
                            },
                        })}
                    />
                </span>
            </div>
            <div className="mt-2 flex items-center gap-1.5">
                <Button variant="ghost" size="sm" onClick={() => onEdit(rule)}>{t('unschedRuleEdit')}</Button>
                <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive"
                    disabled={deleteRule.isPending}
                    onClick={() => deleteRule.mutate(rule.id, { onSuccess: () => toast.success(t('unschedRuleDeleted')) })}
                >
                    {deleteRule.isPending ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
                </Button>
            </div>
        </div>
    );
}

export function PoolUnschedRulesCard() {
    const t = useTranslations('pool');
    const { data: rules = [] } = usePoolUnschedRules();
    const createRule = useCreatePoolUnschedRule();
    const updateRule = useUpdatePoolUnschedRule();
    const [formOpen, setFormOpen] = useState(false);
    const [draft, setDraft] = useState<RuleDraft>(EMPTY_DRAFT);
    const busy = createRule.isPending || updateRule.isPending;
    const valid = validateUnschedRuleDraft(draft);

    const openCreate = () => { setDraft({ ...EMPTY_DRAFT }); setFormOpen(true); };
    const openEdit = (rule: PoolUnschedRule) => { setDraft(draftFromUnschedRule(rule)); setFormOpen(true); };

    const submit = () => {
        if (!valid || busy) return;
        const payload = unschedRuleDraftToPayload(draft);
        const onSuccess = () => {
            setFormOpen(false);
            toast.success(draft.editingId ? t('unschedRuleUpdated') : t('unschedRuleCreated'));
        };
        if (draft.editingId) {
            updateRule.mutate({ id: draft.editingId, data: payload }, { onSuccess });
        } else {
            createRule.mutate(payload, { onSuccess });
        }
    };

    return (
        <section aria-label={t('unschedRules')} className="rounded-xl border border-border/60 bg-card p-4">
            <div className="flex items-center justify-between gap-2">
                <h3 className="flex items-center gap-2 text-sm font-semibold">
                    <ListFilter className="size-4" />
                    {t('unschedRules')}
                </h3>
                <Button variant="outline" size="sm" onClick={openCreate}><Plus className="size-4" />{t('unschedRuleCreate')}</Button>
            </div>
            <p className="mt-1 text-xs text-muted-foreground">{t('unschedRulesHint')}</p>

            {formOpen && (
                <div className="mt-3 space-y-3 rounded-lg border border-border bg-muted/30 p-3">
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                        <label className="grid gap-1">
                            <span className="text-xs font-medium text-muted-foreground">{t('unschedRuleName')}</span>
                            <Input value={draft.name} onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))} />
                        </label>
                        <label className="grid gap-1">
                            <span className="text-xs font-medium text-muted-foreground">{t('unschedRuleStatusCode')}</span>
                            <Input
                                type="number"
                                value={draft.matchStatusCode}
                                onChange={(e) => setDraft((d) => ({ ...d, matchStatusCode: e.target.value }))}
                                placeholder="529"
                            />
                        </label>
                        <label className="grid gap-1 sm:col-span-2">
                            <span className="text-xs font-medium text-muted-foreground">{t('unschedRuleKeyword')}</span>
                            <Input
                                value={draft.matchKeyword}
                                onChange={(e) => setDraft((d) => ({ ...d, matchKeyword: e.target.value }))}
                            />
                        </label>
                        <label className="grid gap-1">
                            <span className="text-xs font-medium text-muted-foreground">{t('unschedRuleDuration')}</span>
                            <Input
                                type="number"
                                min="1"
                                value={draft.durationMinutes}
                                onChange={(e) => setDraft((d) => ({ ...d, durationMinutes: e.target.value }))}
                            />
                        </label>
                        <label className="grid gap-1">
                            <span className="text-xs font-medium text-muted-foreground">{t('unschedRuleSortOrder')}</span>
                            <Input
                                type="number"
                                value={draft.sortOrder}
                                onChange={(e) => setDraft((d) => ({ ...d, sortOrder: e.target.value }))}
                            />
                        </label>
                    </div>
                    {!valid && <p className="text-xs text-destructive">{t('unschedRuleInvalidInput')}</p>}
                    <div className="flex gap-2">
                        <Button size="sm" disabled={!valid || busy} onClick={submit}>
                            {busy && <Loader2 className="size-4 animate-spin" />}
                            {draft.editingId ? t('save') : t('unschedRuleCreate')}
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => setFormOpen(false)}>{t('cancel')}</Button>
                    </div>
                </div>
            )}

            <div className="mt-3 space-y-2">
                {rules.length === 0 ? (
                    <p className="rounded-lg border border-dashed border-border/50 px-3 py-4 text-center text-xs text-muted-foreground">{t('unschedRuleNoRules')}</p>
                ) : rules.map((rule) => <RuleRow key={rule.id} rule={rule} onEdit={openEdit} />)}
            </div>
        </section>
    );
}
