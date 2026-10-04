'use client';

import { useEffect, useId, useState, type FormEvent } from 'react';
import { XIcon } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Hint } from '@/components/ui/hint';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import {
    Dialog,
    DialogClose,
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
import { ProxySelector } from '@/components/modules/proxy-pool/ProxySelector';
import type { ProxyMode } from '@/api/endpoints/proxy-pool';
import { toast } from '@/components/common/Toast';
import { apiClient } from '@/api/client';
import { getCurrentTimeZone } from '@/lib/time';
import {
    useCreatePoolAccount,
    useUpdatePoolAccount,
    type PoolAccount,
    type PoolAccountRequest,
    type PoolAccountExtra,
} from '@/api/endpoints/pool';
import {
    POOL_PLATFORM_OPTIONS,
    POOL_TYPE_OPTIONS_BY_PLATFORM,
    DEFAULT_BASE_URL_BY_PLATFORM,
    platformSupportsOAuth,
    type PoolPlatform,
    type PoolAccountType,
} from './type-options';
import {
    MAX_HEADER_ROWS,
    MODEL_PREVIEW_LIMIT,
    SELECT_NONE_SENTINEL,
    applyHeaderOverridesToExtra,
    dateTimeLocalToUnixSeconds,
    fromSelectSentinel,
    parseIntegerDraft,
    parseJsonObject,
    splitModels,
    toSelectSentinel,
    unixSecondsToDateTimeLocal,
    validateHeaderRows,
    type HeaderRow,
    type HeaderRowsValidation,
} from './account-form';

// 不豆名单（与后端 relay.applyHeaderOverrides 对齐）。静态查表 → Record。
const HEADER_OVERRIDE_BLOCKED: Record<string, true> = {
    authorization: true,
    'x-api-key': true,
    cookie: true,
    host: true,
    'content-length': true,
    'chatgpt-account-id': true,
    'x-claude-code-session-id': true,
    'x-client-request-id': true,
    'x-grok-conv-id': true,
};

// 同上 header override 资格条件（与 sub2api IsHeaderOverrideEligible 对齐）。
function isHeaderOverrideEligible(platform: string, acctType: string): boolean {
    return (
        ((platform === 'anthropic' || platform === 'openai') && acctType === 'apikey') ||
        (platform === 'grok' && (acctType === 'apikey' || acctType === 'oauth'))
    );
}

type AccountFormDialogProps = {
    poolId: number;
    account?: PoolAccount | null;
    open: boolean;
    onOpenChange: (open: boolean) => void;
};

const emptyForm: PoolAccountRequest = {
    name: '',
    platform: 'anthropic',
    type: 'apikey',
    models: '',
    credentials: '',
    base_url: '',
    priority: 0,
    concurrency: 0,
    weight: 0,
    load_factor: 0,
    auto_pause_on_expired: false,
    expires_at: 0,
    proxy_config_id: null,
    notes: '',
    extra: '',
};

type NumberDrafts = { priority: string; concurrency: string; weight: string; load_factor: string };

const emptyNumberDrafts: NumberDrafts = { priority: '0', concurrency: '0', weight: '0', load_factor: '0' };

function headerRowsFromExtra(extra: PoolAccountExtra): HeaderRow[] {
    return Object.entries(extra.header_overrides ?? {}).map(([key, value]) => ({ key, value: String(value ?? '') }));
}

export function AccountFormDialog({ poolId, account, open, onOpenChange }: AccountFormDialogProps) {
    const t = useTranslations('pool');
    const createAccount = useCreatePoolAccount(poolId);
    const updateAccount = useUpdatePoolAccount(poolId);
    const [proxyValue, setProxyValue] = useState<{ proxy_mode: ProxyMode; proxy_config_id: number | null }>({ proxy_mode: 'direct', proxy_config_id: null });
    const [form, setForm] = useState<PoolAccountRequest>(emptyForm);
    // 数字字段以草稿字符串编辑，提交前统一做整数校验（invalidNumber 反馈），空串回落 0=继承。
    const [numDraft, setNumDraft] = useState<NumberDrafts>(emptyNumberDrafts);
    const [expiryDraft, setExpiryDraft] = useState('');
    // 请求头草稿：逐字编辑不丢行，提交时才序列化进 extra。
    const [headerRows, setHeaderRows] = useState<HeaderRow[]>([]);
    const [headerEnabled, setHeaderEnabled] = useState(false);
    const [authorizing, setAuthorizing] = useState(false);
    const uid = useId();
    const fid = (suffix: string) => `${uid}-${suffix}`;

    useEffect(() => {
        if (!open) return;
        setAuthorizing(false);
        if (account) {
            setForm({
                name: account.name,
                platform: account.platform || 'custom',
                type: account.type || 'apikey',
                models: account.models || '',
                credentials: '',
                base_url: account.base_url || '',
                priority: account.priority,
                concurrency: account.concurrency,
                weight: account.weight ?? 0,
                load_factor: account.load_factor ?? 0,
                auto_pause_on_expired: account.auto_pause_on_expired ?? false,
                expires_at: account.expires_at ?? 0,
                proxy_config_id: account.proxy_config_id ?? null,
                notes: account.notes || '',
                extra: account.extra || '',
            });
            setProxyValue({ proxy_mode: account.proxy_config_id ? 'pool' : 'direct', proxy_config_id: account.proxy_config_id ?? null });
            setNumDraft({
                priority: String(account.priority ?? 0),
                concurrency: String(account.concurrency ?? 0),
                weight: String(account.weight ?? 0),
                load_factor: String(account.load_factor ?? 0),
            });
            setExpiryDraft(unixSecondsToDateTimeLocal(account.expires_at ?? 0, getCurrentTimeZone()));
            const parsed = parseJsonObject(account.extra);
            if (parsed.ok) {
                const extra = parsed.value as PoolAccountExtra;
                setHeaderRows(headerRowsFromExtra(extra));
                setHeaderEnabled(extra.header_overrides_enabled ?? false);
            } else {
                // extra 非法：不能凭空猜出请求头行，置空并交给 invalidExtra 提示 + 禁保存兜底。
                setHeaderRows([]);
                setHeaderEnabled(false);
            }
        } else {
            setForm({ ...emptyForm, base_url: DEFAULT_BASE_URL_BY_PLATFORM.anthropic });
            setProxyValue({ proxy_mode: 'direct', proxy_config_id: null });
            setNumDraft(emptyNumberDrafts);
            setExpiryDraft('');
            setHeaderRows([]);
            setHeaderEnabled(false);
        }
    }, [open, account]);

    // 平台切换时更新默认 base_url 与可用类型。
    const handlePlatformChange = (nextPlatform: string) => {
        const types = POOL_TYPE_OPTIONS_BY_PLATFORM[nextPlatform as PoolPlatform] || ['apikey'];
        setForm((f) => ({
            ...f,
            platform: nextPlatform,
            type: types.includes(f.type as PoolAccountType) ? f.type : types[0],
            base_url: !f.base_url?.trim() || f.base_url === DEFAULT_BASE_URL_BY_PLATFORM[f.platform as PoolPlatform]
                ? DEFAULT_BASE_URL_BY_PLATFORM[nextPlatform as PoolPlatform] || ''
                : f.base_url,
        }));
    };

    const handleTypeChange = (type: string) => {
        setForm((f) => ({ ...f, type }));
    };

    const handleOAuthLogin = async () => {
        const platform = form.platform as PoolPlatform;
        if (!platformSupportsOAuth(platform) || authorizing || isPending) return;
        setAuthorizing(true);
        try {
            // 先通过管理 API 发起 initiate（带 JWT），拿到 auth_url 后再跳转授权页。
            // 授权回调走既有流程创建账号；本弹窗手动保存仍要求凭据非空。
            const data = await apiClient.get<{ auth_url: string }>(
                '/api/v1/pool/oauth/initiate',
                { platform, pool_id: poolId },
            );
            if (!data?.auth_url) {
                throw new Error(t('oauthInitiateFailed'));
            }
            window.location.href = data.auth_url;
        } catch (e) {
            toast.error(String(e));
            setAuthorizing(false);
        }
    };

    const handleExpiryChange = (value: string) => {
        setExpiryDraft(value);
        const seconds = dateTimeLocalToUnixSeconds(value, timeZone);
        if (seconds !== null) {
            setForm((f) => ({ ...f, expires_at: seconds }));
        }
    };

    const handleSubmit = (e: FormEvent<HTMLFormElement>) => {
        e.preventDefault();
        if (!canSubmit) return;
        const expiresAt = autoPauseEnabled
            ? (dateTimeLocalToUnixSeconds(expiryDraft, timeZone) ?? 0)
            : form.expires_at ?? 0;
        const finalExtra = applyHeaderOverridesToExtra(
            parsedExtra.ok ? parsedExtra.value : {},
            overrideEligible,
            headerRows,
            headerEnabled,
        );
        const payload: PoolAccountRequest = {
            ...form,
            priority: priorityDraft.value,
            concurrency: concurrencyDraft.value,
            weight: weightDraft.value,
            load_factor: loadFactorDraft.value,
            expires_at: expiresAt,
            extra: JSON.stringify(finalExtra),
            proxy_config_id: proxyValue.proxy_mode === 'pool' ? proxyValue.proxy_config_id : null,
        };
        const onSuccess = () => {
            onOpenChange(false);
            toast.success(account ? t('accountUpdated') : t('accountCreated'));
        };
        if (account) {
            updateAccount.mutate(
                { accountId: account.id, data: payload },
                { onSuccess },
            );
        } else {
            createAccount.mutate(payload, { onSuccess });
        }
    };

    // pending 期间禁止关闭（ESC / 遮罩 / 关闭按钮 / 取消全部被拦下）。
    const handleOpenChange = (next: boolean) => {
        if (!next && isPending) return;
        onOpenChange(next);
    };

    // extra 非法时不允许结构化改写（改了就会把原值静默丢掉），只能修好 extra 再保存。
    const updateExtra = (patch: Partial<PoolAccountExtra>) => {
        if (!parsedExtra.ok) return;
        const next: Record<string, unknown> = { ...parsedExtra.value, ...patch };
        // 删除空字段让东西干净
        Object.keys(next).forEach((k) => {
            const v = next[k];
            if (v === '' || v === undefined) delete next[k];
        });
        setForm((f) => ({ ...f, extra: JSON.stringify(next) }));
    };

    const platform = form.platform as PoolPlatform;
    const acctType = form.type as PoolAccountType;
    const typeOptions = POOL_TYPE_OPTIONS_BY_PLATFORM[platform] || ['apikey'];
    const isOAuth = acctType === 'oauth';
    const isPending = createAccount.isPending || updateAccount.isPending;
    const isEdit = !!account;
    const timeZone = getCurrentTimeZone();

    const priorityDraft = parseIntegerDraft(numDraft.priority);
    const concurrencyDraft = parseIntegerDraft(numDraft.concurrency);
    const weightDraft = parseIntegerDraft(numDraft.weight);
    const loadFactorDraft = parseIntegerDraft(numDraft.load_factor);
    const numbersValid = priorityDraft.valid && concurrencyDraft.valid && weightDraft.valid && loadFactorDraft.valid;

    const parsedExtra = parseJsonObject(form.extra);
    const currentExtra: PoolAccountExtra = parsedExtra.ok ? (parsedExtra.value as PoolAccountExtra) : {};
    const extraInvalid = !parsedExtra.ok;
    const overrideEligible = isHeaderOverrideEligible(platform, acctType);
    const headerValidation: HeaderRowsValidation = validateHeaderRows(headerRows);
    const headersValid = overrideEligible && headerEnabled ? headerValidation.valid : true;

    const expirySeconds = dateTimeLocalToUnixSeconds(expiryDraft, timeZone);
    const autoPauseEnabled = form.auto_pause_on_expired ?? false;
    const expiryInvalidVisible = autoPauseEnabled && expiryDraft.trim() !== '' && expirySeconds === null;

    // 创建时凭据必填（含 OAuth 手动保存；OAuth 授权按钮走既有 initiate，不经本表单创建）。
    const credentialsMissing = !isEdit && (form.credentials ?? '').trim() === '';
    const canSubmit = !isPending && !credentialsMissing && !extraInvalid && numbersValid && headersValid && !expiryInvalidVisible;

    const modelList = splitModels(form.models ?? '');

    if (!open) return null;

    return (
        <Dialog open={open} onOpenChange={handleOpenChange}>
            <DialogContent showCloseButton={false} className="flex max-h-[calc(100dvh-2rem)] w-full flex-col gap-0 overflow-hidden rounded-xl p-0 sm:max-w-3xl">
                <DialogHeader className="relative shrink-0 space-y-1.5 border-b px-5 py-4 text-left sm:px-6">
                    <DialogTitle>{account ? t('editAccount') : t('addAccount')}</DialogTitle>
                    <DialogDescription>{t('ui.accountDescription')}</DialogDescription>
                    <DialogClose asChild>
                        <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            className="absolute top-3 right-3 size-8"
                            disabled={isPending}
                            aria-label={t('ui.close')}
                        >
                            <XIcon className="size-4" />
                        </Button>
                    </DialogClose>
                </DialogHeader>

                <form onSubmit={handleSubmit} className="flex min-h-0 flex-1 flex-col">
                    <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-5 py-4 sm:px-6">
                        {account?.error_message && (
                            <div className="min-w-0 break-all rounded-lg border border-destructive/30 bg-destructive/10 p-2 text-xs text-destructive">
                                {account.error_message}
                            </div>
                        )}

                        {/* 基本配置 */}
                        <section className="space-y-3">
                            <h4 className="text-sm font-semibold">{t('ui.basicSettings')}</h4>
                            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                                <div className="min-w-0 sm:col-span-2">
                                    <Label>{t('platform')}</Label>
                                    <div role="group" className="mt-2 flex flex-wrap gap-2">
                                        {POOL_PLATFORM_OPTIONS.map((opt) => {
                                            const selected = form.platform === opt.value;
                                            return (
                                                <Button
                                                    key={opt.value}
                                                    type="button"
                                                    variant={selected ? 'default' : 'outline'}
                                                    size="sm"
                                                    aria-pressed={selected}
                                                    onClick={() => handlePlatformChange(opt.value)}
                                                >
                                                    {t(`platformLabels.${opt.value}`)}
                                                </Button>
                                            );
                                        })}
                                    </div>
                                </div>

                                <div className="min-w-0">
                                    <Label htmlFor={fid('type')}>{t('type')}</Label>
                                    <Select value={form.type} onValueChange={handleTypeChange}>
                                        <SelectTrigger id={fid('type')} className="mt-1 w-full"><SelectValue /></SelectTrigger>
                                        <SelectContent>
                                            {typeOptions.map((tp) => (
                                                <SelectItem key={tp} value={tp}>{t(`typeLabels.${tp}`)}</SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                </div>

                                <div className="min-w-0">
                                    <Label htmlFor={fid('name')}>{t('accountName')}</Label>
                                    <Input
                                        id={fid('name')}
                                        className="mt-1"
                                        value={form.name}
                                        onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                                        placeholder={t('ui.namePlaceholder')}
                                    />
                                </div>

                                {/* 凭据区：按 type 条件渲染 */}
                                <div className="min-w-0 sm:col-span-2">
                                    {isOAuth ? (
                                        <div className="space-y-2 rounded-lg border border-primary/30 bg-primary/5 p-3">
                                            <div className="flex items-center justify-between gap-2">
                                                <Label htmlFor={fid('credentials')} className="text-sm">
                                                    {t('oauthLogin')}
                                                    <Hint text={t('oauthLoginHint')} />
                                                </Label>
                                                <Button
                                                    type="button"
                                                    size="sm"
                                                    onClick={handleOAuthLogin}
                                                    disabled={authorizing || isPending}
                                                >
                                                    {authorizing ? t('ui.authorizing') : t('oauthLoginBtn')}
                                                </Button>
                                            </div>
                                            <div>
                                                <Label htmlFor={fid('credentials')} className="text-xs">{t('oauthManualPaste')}</Label>
                                                <textarea
                                                    id={fid('credentials')}
                                                    className="mt-1 w-full rounded-md border border-input bg-transparent px-3 py-2 font-mono text-xs"
                                                    rows={3}
                                                    value={form.credentials}
                                                    onChange={(e) => setForm((f) => ({ ...f, credentials: e.target.value }))}
                                                    placeholder='{"access_token":"...","refresh_token":"...","account_id":"..."}'
                                                />
                                            </div>
                                        </div>
                                    ) : acctType === 'apikey' ? (
                                        <div>
                                            <Label htmlFor={fid('credentials')}>
                                                {t('apiKey')}
                                                <Hint text={`${t('credentialsHint')} {"type":"apikey","api_key":"sk-..."}`} />
                                            </Label>
                                            <Input
                                                id={fid('credentials')}
                                                className="mt-1 font-mono"
                                                type="password"
                                                value={form.credentials}
                                                onChange={(e) => setForm((f) => ({ ...f, credentials: e.target.value }))}
                                                placeholder="sk-..."
                                            />
                                        </div>
                                    ) : acctType === 'cookie' ? (
                                        <div>
                                            <Label htmlFor={fid('credentials')}>{t('cookie')}</Label>
                                            <textarea
                                                id={fid('credentials')}
                                                className="mt-1 w-full rounded-md border border-input bg-transparent px-3 py-2 font-mono text-xs"
                                                rows={2}
                                                value={form.credentials}
                                                onChange={(e) => setForm((f) => ({ ...f, credentials: e.target.value }))}
                                                placeholder="sessionKey=... (volcengine: Cookie|||csrf-token)"
                                            />
                                        </div>
                                    ) : acctType === 'upstream' ? (
                                        <div>
                                            <Label htmlFor={fid('credentials')}>{t('apiKey')}</Label>
                                            <Input
                                                id={fid('credentials')}
                                                className="mt-1 font-mono"
                                                type="password"
                                                value={form.credentials}
                                                onChange={(e) => setForm((f) => ({ ...f, credentials: e.target.value }))}
                                                placeholder="sk-..."
                                            />
                                        </div>
                                    ) : (
                                        // 未知类型（如 setup-token）兜底凭据编辑，避免存量账号凭据不可改。
                                        <div>
                                            <Label htmlFor={fid('credentials')}>{t('credentials')}</Label>
                                            <textarea
                                                id={fid('credentials')}
                                                className="mt-1 w-full rounded-md border border-input bg-transparent px-3 py-2 font-mono text-xs"
                                                rows={2}
                                                value={form.credentials}
                                                onChange={(e) => setForm((f) => ({ ...f, credentials: e.target.value }))}
                                            />
                                        </div>
                                    )}
                                    {isEdit ? (
                                        !form.credentials && (
                                            <p className="mt-1 text-xs text-muted-foreground">{t('ui.credentialsKeep')}</p>
                                        )
                                    ) : (
                                        <p className="mt-1 text-xs text-muted-foreground">{t('ui.credentialsRequired')}</p>
                                    )}
                                </div>

                                <div className="min-w-0">
                                    <Label htmlFor={fid('base-url')}>{t('baseUrl')}</Label>
                                    <Input
                                        id={fid('base-url')}
                                        className="mt-1"
                                        value={form.base_url}
                                        onChange={(e) => setForm((f) => ({ ...f, base_url: e.target.value }))}
                                        placeholder={DEFAULT_BASE_URL_BY_PLATFORM[platform] || 'https://...'}
                                    />
                                </div>

                                <div className="min-w-0">
                                    <Label>{t('proxyConfig')}</Label>
                                    <div className="mt-1">
                                        <ProxySelector
                                            value={proxyValue}
                                            onChange={(v) => setProxyValue({ proxy_mode: v.proxy_mode, proxy_config_id: v.proxy_config_id ?? null })}
                                        />
                                    </div>
                                </div>

                                <div className="min-w-0 sm:col-span-2">
                                    <Label htmlFor={fid('models')}>{t('models')}</Label>
                                    <Input
                                        id={fid('models')}
                                        className="mt-1"
                                        value={form.models}
                                        onChange={(e) => setForm((f) => ({ ...f, models: e.target.value }))}
                                        placeholder={t('modelsPlaceholder')}
                                    />
                                    {modelList.length > 0 && (
                                        <div className="mt-2 flex min-w-0 flex-wrap gap-1">
                                            {modelList.slice(0, MODEL_PREVIEW_LIMIT).map((m, i) => (
                                                <Badge key={`${m}-${i}`} variant="secondary" className="max-w-full break-all whitespace-normal">{m}</Badge>
                                            ))}
                                            {modelList.length > MODEL_PREVIEW_LIMIT && (
                                                <Badge variant="outline" className="whitespace-normal">
                                                    {t('ui.modelPreviewMore', { count: modelList.length - MODEL_PREVIEW_LIMIT })}
                                                </Badge>
                                            )}
                                        </div>
                                    )}
                                </div>

                                <div className="min-w-0 sm:col-span-2">
                                    <Label htmlFor={fid('notes')}>{t('notes')}</Label>
                                    <Input
                                        id={fid('notes')}
                                        className="mt-1"
                                        value={form.notes}
                                        onChange={(e) => setForm((f) => ({ ...f, notes: e.target.value }))}
                                    />
                                </div>
                            </div>
                        </section>

                        {/* 调度与有效期 */}
                        <section className="space-y-3">
                            <h4 className="text-sm font-semibold">{t('ui.schedulingSettings')}</h4>
                            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                                <div className="min-w-0">
                                    <Label htmlFor={fid('priority')}>{t('priority')}</Label>
                                    <Input
                                        id={fid('priority')}
                                        type="number"
                                        inputMode="numeric"
                                        className="mt-1"
                                        value={numDraft.priority}
                                        aria-invalid={!priorityDraft.valid}
                                        onChange={(e) => setNumDraft((d) => ({ ...d, priority: e.target.value }))}
                                    />
                                    {!priorityDraft.valid && (
                                        <p className="mt-1 text-xs text-destructive">{t('ui.invalidNumber')}</p>
                                    )}
                                </div>
                                <div className="min-w-0">
                                    <Label htmlFor={fid('concurrency')}>{t('concurrency')} (0={t('inheritPool')})</Label>
                                    <Input
                                        id={fid('concurrency')}
                                        type="number"
                                        inputMode="numeric"
                                        className="mt-1"
                                        value={numDraft.concurrency}
                                        aria-invalid={!concurrencyDraft.valid}
                                        onChange={(e) => setNumDraft((d) => ({ ...d, concurrency: e.target.value }))}
                                    />
                                    {!concurrencyDraft.valid && (
                                        <p className="mt-1 text-xs text-destructive">{t('ui.invalidNumber')}</p>
                                    )}
                                </div>
                                <div className="min-w-0">
                                    <Label htmlFor={fid('weight')}>
                                        {t('weight')}
                                        <Hint text={t('weightHint')} />
                                    </Label>
                                    <Input
                                        id={fid('weight')}
                                        type="number"
                                        inputMode="numeric"
                                        className="mt-1"
                                        value={numDraft.weight}
                                        aria-invalid={!weightDraft.valid}
                                        onChange={(e) => setNumDraft((d) => ({ ...d, weight: e.target.value }))}
                                    />
                                    {!weightDraft.valid && (
                                        <p className="mt-1 text-xs text-destructive">{t('ui.invalidNumber')}</p>
                                    )}
                                </div>
                                <div className="min-w-0">
                                    <Label htmlFor={fid('load-factor')}>
                                        {t('loadFactor')} (0={t('inheritPool')})
                                        <Hint text={t('loadFactorHint')} />
                                    </Label>
                                    <Input
                                        id={fid('load-factor')}
                                        type="number"
                                        inputMode="numeric"
                                        className="mt-1"
                                        value={numDraft.load_factor}
                                        aria-invalid={!loadFactorDraft.valid}
                                        onChange={(e) => setNumDraft((d) => ({ ...d, load_factor: e.target.value }))}
                                    />
                                    {!loadFactorDraft.valid && (
                                        <p className="mt-1 text-xs text-destructive">{t('ui.invalidNumber')}</p>
                                    )}
                                </div>

                                <div className="min-w-0 rounded-lg border border-border/60 p-3 sm:col-span-2">
                                    <div className="flex items-center justify-between gap-2">
                                        <Label htmlFor={fid('auto-pause')}>{t('autoPauseOnExpired')}</Label>
                                        <input
                                            id={fid('auto-pause')}
                                            type="checkbox"
                                            className="h-4 w-4"
                                            checked={form.auto_pause_on_expired ?? false}
                                            onChange={(e) => setForm((f) => ({ ...f, auto_pause_on_expired: e.target.checked }))}
                                        />
                                    </div>
                                    {form.auto_pause_on_expired && (
                                        <div className="mt-2">
                                            <Label htmlFor={fid('expires-at')} className="text-xs">{t('expiresAt')}</Label>
                                            <Input
                                                id={fid('expires-at')}
                                                type="datetime-local"
                                                className="mt-1"
                                                value={expiryDraft}
                                                aria-invalid={expiryInvalidVisible}
                                                onChange={(e) => handleExpiryChange(e.target.value)}
                                            />
                                            {expiryInvalidVisible && (
                                                <p className="mt-1 text-xs text-destructive">{t('ui.invalidExpiry')}</p>
                                            )}
                                            <p className="mt-1 text-xs text-muted-foreground">{t('ui.timeZone', { zone: timeZone })}</p>
                                        </div>
                                    )}
                                </div>
                            </div>
                        </section>

                        {/* 平台附加与请求头 */}
                        <section className="space-y-3">
                            <h4 className="text-sm font-semibold">{t('ui.advancedSettings')}</h4>
                            {extraInvalid && (
                                <p className="min-w-0 break-all text-xs text-destructive">{t('ui.invalidExtra')}</p>
                            )}
                            <details open={extraInvalid || undefined} className="rounded-lg border border-border/60 p-3">
                                <summary className="cursor-pointer text-xs text-muted-foreground">{t('extras.section')} (JSON)</summary>
                                <Label htmlFor={fid('extra-json')} className="sr-only">{t('extras.section')} (JSON)</Label>
                                <textarea
                                    id={fid('extra-json')}
                                    className="mt-3 w-full rounded-md border border-input bg-background p-3 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
                                    rows={4}
                                    value={form.extra ?? ''}
                                    aria-invalid={extraInvalid}
                                    onChange={(event) => {
                                        const raw = event.target.value;
                                        setForm((current) => ({ ...current, extra: raw }));
                                        const parsed = parseJsonObject(raw);
                                        if (parsed.ok) {
                                            const extra = parsed.value as PoolAccountExtra;
                                            setHeaderRows(headerRowsFromExtra(extra));
                                            setHeaderEnabled(extra.header_overrides_enabled ?? false);
                                        }
                                    }}
                                />
                            </details>
                            <ExtrasEditor
                                platform={platform}
                                acctType={acctType}
                                extra={currentExtra}
                                onExtraChange={updateExtra}
                                disabled={extraInvalid}
                                headerRows={headerRows}
                                onHeaderRowsChange={setHeaderRows}
                                headerEnabled={headerEnabled}
                                onHeaderEnabledChange={setHeaderEnabled}
                                validation={headerValidation}
                                prefix={uid}
                            />
                        </section>
                    </div>

                    <DialogFooter className="flex-row justify-end gap-2 border-t px-5 py-3 pb-[calc(0.75rem+env(safe-area-inset-bottom,0px))] sm:px-6">
                        <Button type="button" variant="outline" onClick={() => handleOpenChange(false)} disabled={isPending}>
                            {t('cancel')}
                        </Button>
                        <Button type="submit" disabled={isPending || !canSubmit}>
                            {isPending ? t('ui.saving') : account ? t('save') : t('addAccount')}
                        </Button>
                    </DialogFooter>
                </form>
            </DialogContent>
        </Dialog>
    );
}

// ExtrasEditor 平台附加字段与自定义请求头编辑器。按 platform 显隐 gemini/openai 区。
// 请求头以草稿行（HeaderRow[]）编辑：清空 key 不丢行，提交时才由父级序列化进 extra。
function ExtrasEditor({ platform, acctType, extra, onExtraChange, disabled, headerRows, onHeaderRowsChange, headerEnabled, onHeaderEnabledChange, validation, prefix }: {
    platform: PoolPlatform;
    acctType: PoolAccountType;
    extra: PoolAccountExtra;
    onExtraChange: (patch: Partial<PoolAccountExtra>) => void;
    /** extra 原始 JSON 非法时禁用结构化编辑，避免静默丢掉原值 */
    disabled: boolean;
    headerRows: HeaderRow[];
    onHeaderRowsChange: (rows: HeaderRow[]) => void;
    headerEnabled: boolean;
    onHeaderEnabledChange: (enabled: boolean) => void;
    validation: HeaderRowsValidation;
    prefix: string;
}) {
    const t = useTranslations('pool');

    const addHeaderRow = () => {
        if (headerRows.length >= MAX_HEADER_ROWS) return;
        onHeaderRowsChange([...headerRows, { key: '', value: '' }]);
    };
    const setHeaderRow = (idx: number, patch: Partial<HeaderRow>) => {
        const next = [...headerRows];
        next[idx] = { ...next[idx], ...patch };
        onHeaderRowsChange(next);
    };
    const removeHeaderRow = (idx: number) => {
        onHeaderRowsChange(headerRows.filter((_, i) => i !== idx));
    };

    return (
        <div className="space-y-3 rounded-lg border border-border/60 p-3">
            <Label>{t('extras.section')}</Label>

            {platform === 'gemini' && (
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <div className="min-w-0">
                        <Label htmlFor={`${prefix}-extras-project`} className="text-xs">{t('extras.projectId')}</Label>
                        <Input
                            id={`${prefix}-extras-project`}
                            className="mt-1"
                            value={extra.project_id ?? ''}
                            disabled={disabled}
                            onChange={(e) => onExtraChange({ project_id: e.target.value })}
                        />
                    </div>
                    <div className="min-w-0">
                        <Label htmlFor={`${prefix}-extras-tier`} className="text-xs">{t('extras.tierId')}</Label>
                        <Input
                            id={`${prefix}-extras-tier`}
                            className="mt-1"
                            value={extra.tier_id ?? ''}
                            disabled={disabled}
                            onChange={(e) => onExtraChange({ tier_id: e.target.value })}
                        />
                    </div>
                    <div className="min-w-0">
                        <Label htmlFor={`${prefix}-extras-oauth-type`} className="text-xs">{t('extras.oauthType')}</Label>
                        <Select
                            value={toSelectSentinel(extra.oauth_type ?? '')}
                            onValueChange={(v) => onExtraChange({ oauth_type: fromSelectSentinel(v) })}
                            disabled={disabled}
                        >
                            <SelectTrigger id={`${prefix}-extras-oauth-type`} className="mt-1 w-full"><SelectValue /></SelectTrigger>
                            <SelectContent>
                                <SelectItem value={SELECT_NONE_SENTINEL}>{t('extras.oauthTypeNone')}</SelectItem>
                                <SelectItem value="code_assist">code_assist</SelectItem>
                            </SelectContent>
                        </Select>
                    </div>
                </div>
            )}

            {platform === 'openai' && (
                <div className="min-w-0">
                    <Label htmlFor={`${prefix}-extras-auth-mode`} className="text-xs">{t('extras.authMode')}</Label>
                    <Select
                        value={toSelectSentinel(extra.auth_mode ?? '')}
                        onValueChange={(v) => onExtraChange({ auth_mode: fromSelectSentinel(v) })}
                        disabled={disabled}
                    >
                        <SelectTrigger id={`${prefix}-extras-auth-mode`} className="mt-1 w-full"><SelectValue /></SelectTrigger>
                        <SelectContent>
                            <SelectItem value={SELECT_NONE_SENTINEL}>{t('extras.authModeDefault')}</SelectItem>
                            <SelectItem value="personalAccessToken">personalAccessToken</SelectItem>
                        </SelectContent>
                    </Select>
                </div>
            )}

            <div className="min-w-0">
                <Label htmlFor={`${prefix}-extras-tls`} className="text-xs">{t('extras.tlsFingerprint')} ({t('extras.reserved')})</Label>
                <Input
                    id={`${prefix}-extras-tls`}
                    className="mt-1"
                    value={extra.tls_fingerprint_profile ?? ''}
                    disabled={disabled}
                    onChange={(e) => onExtraChange({ tls_fingerprint_profile: e.target.value })}
                    placeholder="chrome_120"
                />
            </div>

            {isHeaderOverrideEligible(platform, acctType) && (
                <div className="border-t border-border/60 pt-3">
                    <div className="flex items-center justify-between gap-2">
                        <Label htmlFor={`${prefix}-header-enabled`} className="text-xs">{t('headerOverride.section')}</Label>
                        <input
                            id={`${prefix}-header-enabled`}
                            type="checkbox"
                            className="h-4 w-4"
                            disabled={disabled}
                            checked={headerEnabled}
                            onChange={(e) => onHeaderEnabledChange(e.target.checked)}
                        />
                    </div>
                    {headerEnabled && (
                        <div className="mt-2 space-y-2">
                            {headerRows.map((row, idx) => {
                                const normalizedKey = row.key.trim().toLowerCase();
                                const blocked = HEADER_OVERRIDE_BLOCKED[normalizedKey]
                                    || normalizedKey.startsWith('x-codex-');
                                const flagged = validation.invalidKeys.includes(idx)
                                    || validation.invalidValues.includes(idx)
                                    || validation.duplicates.includes(idx);
                                return (
                                    <div key={idx} className="flex gap-2">
                                        <Input
                                            placeholder={t('headerOverride.row.key')}
                                            aria-label={t('headerOverride.row.key')}
                                            value={row.key}
                                            disabled={disabled}
                                            aria-invalid={flagged}
                                            onChange={(e) => setHeaderRow(idx, { key: e.target.value })}
                                            className={blocked ? 'border-destructive/50' : ''}
                                        />
                                        <Input
                                            placeholder={t('headerOverride.row.value')}
                                            aria-label={t('headerOverride.row.value')}
                                            value={row.value}
                                            disabled={disabled}
                                            aria-invalid={validation.invalidValues.includes(idx)}
                                            onChange={(e) => setHeaderRow(idx, { value: e.target.value })}
                                        />
                                        <Button type="button" variant="outline" size="sm" disabled={disabled} onClick={() => removeHeaderRow(idx)}>
                                            {t('headerOverride.remove')}
                                        </Button>
                                    </div>
                                );
                            })}
                            <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                disabled={disabled || headerRows.length >= MAX_HEADER_ROWS}
                                onClick={addHeaderRow}
                            >
                                {t('headerOverride.add')}
                            </Button>
                            {(validation.invalidKeys.length > 0 || validation.invalidValues.length > 0) && (
                                <p className="text-xs text-destructive">{t('ui.invalidHeader')}</p>
                            )}
                            {validation.duplicates.length > 0 && (
                                <p className="text-xs text-destructive">{t('ui.duplicateHeader')}</p>
                            )}
                            <p className="text-xs text-muted-foreground">{t('headerOverride.blockedWarning')}</p>
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}
