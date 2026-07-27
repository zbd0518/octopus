'use client';

import { useState, useEffect } from 'react';
import { useTranslations } from 'next-intl';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
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
import {
    useCreatePoolAccount,
    useUpdatePoolAccount,
    type PoolAccount,
    type PoolAccountRequest,
} from '@/api/endpoints/pool';
import {
    POOL_PLATFORM_OPTIONS,
    POOL_TYPE_OPTIONS_BY_PLATFORM,
    DEFAULT_BASE_URL_BY_PLATFORM,
    platformSupportsOAuth,
    type PoolPlatform,
    type PoolAccountType,
} from './type-options';

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
    proxy_config_id: null,
    notes: '',
};

export function AccountFormDialog({ poolId, account, open, onOpenChange }: AccountFormDialogProps) {
    const t = useTranslations('pool');
    const createAccount = useCreatePoolAccount(poolId);
    const updateAccount = useUpdatePoolAccount(poolId);
    const [proxyValue, setProxyValue] = useState<{ proxy_mode: ProxyMode; proxy_config_id: number | null }>({ proxy_mode: 'direct', proxy_config_id: null });
    const [form, setForm] = useState<PoolAccountRequest>(emptyForm);

    useEffect(() => {
        if (open) {
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
                    proxy_config_id: account.proxy_config_id ?? null,
                    notes: account.notes || '',
                });
                setProxyValue({ proxy_mode: account.proxy_config_id ? 'pool' : 'direct', proxy_config_id: account.proxy_config_id ?? null });
            } else {
                setForm({ ...emptyForm, base_url: DEFAULT_BASE_URL_BY_PLATFORM.anthropic });
                setProxyValue({ proxy_mode: 'direct', proxy_config_id: null });
            }
        }
    }, [open, account]);

    // 平台切换时更新默认 base_url 与可用类型。
    const handlePlatformChange = (platform: string) => {
        const types = POOL_TYPE_OPTIONS_BY_PLATFORM[platform as PoolPlatform] || ['apikey'];
        const newType = types.includes(form.type as PoolAccountType) ? form.type : types[0];
        setForm({
            ...form,
            platform,
            type: newType,
            base_url: DEFAULT_BASE_URL_BY_PLATFORM[platform as PoolPlatform] || '',
        });
    };

    const handleTypeChange = (type: string) => {
        setForm({ ...form, type });
    };

    const handleOAuthLogin = () => {
        const platform = form.platform as PoolPlatform;
        if (!platformSupportsOAuth(platform)) return;
        // 跳转 OAuth initiate 端点，回调后前端刷新列表。
        window.location.href = `/api/v1/pool/oauth/initiate?platform=${platform}&pool_id=${poolId}`;
    };

    const handleSubmit = () => {
        const payload: PoolAccountRequest = {
            ...form,
            proxy_config_id: proxyValue.proxy_mode === 'pool' ? proxyValue.proxy_config_id : null,
        };
        const onSuccess = () => {
            onOpenChange(false);
            toast.success(account ? t('accountUpdated') : t('accountCreated'));
        };
        const onError = (e: unknown) => toast.error(String(e));

        if (account) {
            updateAccount.mutate(
                { accountId: account.id, data: payload },
                { onSuccess, onError },
            );
        } else {
            createAccount.mutate(payload, { onSuccess, onError });
        }
    };

    const platform = form.platform as PoolPlatform;
    const acctType = form.type as PoolAccountType;
    const typeOptions = POOL_TYPE_OPTIONS_BY_PLATFORM[platform] || ['apikey'];
    const isOAuth = acctType === 'oauth';
    const isPending = createAccount.isPending || updateAccount.isPending;

    if (!open) return null;

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={() => onOpenChange(false)}>
            <div className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-lg bg-card p-6 shadow-lg" onClick={(e) => e.stopPropagation()}>
                <h3 className="mb-4 text-lg font-semibold">{account ? t('editAccount') : t('addAccount')}</h3>
                <div className="space-y-4">
                    {/* 平台分段选择器 */}
                    <div>
                        <Label>{t('platform')}</Label>
                        <div className="mt-2 flex flex-wrap gap-2">
                            {POOL_PLATFORM_OPTIONS.map((opt) => (
                                <Button
                                    key={opt.value}
                                    type="button"
                                    variant={form.platform === opt.value ? 'default' : 'outline'}
                                    size="sm"
                                    onClick={() => handlePlatformChange(opt.value)}
                                >
                                    {opt.label}
                                </Button>
                            ))}
                        </div>
                    </div>

                    {/* 类型选择 */}
                    <div>
                        <Label>{t('type')}</Label>
                        <Select value={form.type} onValueChange={handleTypeChange}>
                            <SelectTrigger className="mt-1"><SelectValue /></SelectTrigger>
                            <SelectContent>
                                {typeOptions.map((tp) => (
                                    <SelectItem key={tp} value={tp}>{t(`typeLabels.${tp}`)}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    {/* 账号名 */}
                    <div>
                        <Label>{t('accountName')}</Label>
                        <Input
                            className="mt-1"
                            value={form.name}
                            onChange={(e) => setForm({ ...form, name: e.target.value })}
                            placeholder="claude-01"
                        />
                    </div>

                    {/* 凭据区：按 type 条件渲染 */}
                    {isOAuth ? (
                        <div className="space-y-2 rounded-lg border border-primary/30 bg-primary/5 p-3">
                            <div className="flex items-center justify-between">
                                <Label className="text-sm">{t('oauthLogin')}</Label>
                                <Button type="button" size="sm" onClick={handleOAuthLogin}>
                                    {t('oauthLoginBtn')}
                                </Button>
                            </div>
                            <p className="text-xs text-muted-foreground">{t('oauthLoginHint')}</p>
                            <div>
                                <Label className="text-xs">{t('oauthManualPaste')}</Label>
                                <textarea
                                    className="mt-1 w-full rounded-md border border-input bg-transparent px-3 py-2 font-mono text-xs"
                                    rows={3}
                                    value={form.credentials}
                                    onChange={(e) => setForm({ ...form, credentials: e.target.value })}
                                    placeholder='{"access_token":"...","refresh_token":"...","account_id":"..."}'
                                />
                            </div>
                        </div>
                    ) : acctType === 'apikey' ? (
                        <div>
                            <Label>{t('apiKey')}</Label>
                            <Input
                                className="mt-1 font-mono"
                                type="password"
                                value={form.credentials}
                                onChange={(e) => setForm({ ...form, credentials: e.target.value })}
                                placeholder="sk-..."
                            />
                            <p className="mt-1 text-xs text-muted-foreground">
                                {t('credentialsHint')} {"{\"type\":\"apikey\",\"api_key\":\"sk-...\"}"}
                            </p>
                        </div>
                    ) : acctType === 'cookie' ? (
                        <div>
                            <Label>{t('cookie')}</Label>
                            <textarea
                                className="mt-1 w-full rounded-md border border-input bg-transparent px-3 py-2 font-mono text-xs"
                                rows={2}
                                value={form.credentials}
                                onChange={(e) => setForm({ ...form, credentials: e.target.value })}
                                placeholder="sessionKey=... (volcengine: Cookie|||csrf-token)"
                            />
                        </div>
                    ) : acctType === 'upstream' ? (
                        <div>
                            <Label>{t('apiKey')}</Label>
                            <Input
                                className="mt-1 font-mono"
                                type="password"
                                value={form.credentials}
                                onChange={(e) => setForm({ ...form, credentials: e.target.value })}
                                placeholder="sk-..."
                            />
                            <div className="mt-2">
                                <Label>{t('baseUrl')}</Label>
                                <Input
                                    className="mt-1"
                                    value={form.base_url}
                                    onChange={(e) => setForm({ ...form, base_url: e.target.value })}
                                    placeholder="https://your-upstream.com"
                                />
                            </div>
                        </div>
                    ) : null}

                    {/* base_url（upstream 已在上面渲染，其余在此显示） */}
                    {acctType !== 'upstream' && (
                        <div>
                            <Label>{t('baseUrl')}</Label>
                            <Input
                                className="mt-1"
                                value={form.base_url}
                                onChange={(e) => setForm({ ...form, base_url: e.target.value })}
                                placeholder={DEFAULT_BASE_URL_BY_PLATFORM[platform] || 'https://...'}
                            />
                        </div>
                    )}

                    {/* 模型绑定 */}
                    <div>
                        <Label>{t('models')}</Label>
                        <Input
                            className="mt-1"
                            value={form.models}
                            onChange={(e) => setForm({ ...form, models: e.target.value })}
                            placeholder={t('modelsPlaceholder')}
                        />
                        {form.models && (
                            <div className="mt-2 flex flex-wrap gap-1">
                                {form.models.split(',').filter(Boolean).map((m, i) => (
                                    <Badge key={i} variant="secondary" className="text-xs">{m.trim()}</Badge>
                                ))}
                            </div>
                        )}
                    </div>

                    {/* 代理 */}
                    <div>
                        <Label>{t('proxyConfig')}</Label>
                        <div className="mt-1">
                            <ProxySelector
                                value={proxyValue}
                                onChange={(v) => setProxyValue({ proxy_mode: v.proxy_mode, proxy_config_id: v.proxy_config_id ?? null })}
                            />
                        </div>
                    </div>

                    {/* priority / concurrency / notes */}
                    <div className="grid grid-cols-2 gap-4">
                        <div>
                            <Label>{t('priority')}</Label>
                            <Input
                                type="number"
                                className="mt-1"
                                value={form.priority}
                                onChange={(e) => setForm({ ...form, priority: Number(e.target.value) })}
                            />
                        </div>
                        <div>
                            <Label>{t('concurrency')} (0={t('inheritPool')})</Label>
                            <Input
                                type="number"
                                className="mt-1"
                                value={form.concurrency}
                                onChange={(e) => setForm({ ...form, concurrency: Number(e.target.value) })}
                            />
                        </div>
                    </div>
                    <div>
                        <Label>{t('notes')}</Label>
                        <Input
                            className="mt-1"
                            value={form.notes}
                            onChange={(e) => setForm({ ...form, notes: e.target.value })}
                        />
                    </div>

                    {account?.error_message && (
                        <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-2 text-xs text-destructive">
                            {account.error_message}
                        </div>
                    )}
                </div>

                <div className="mt-6 flex justify-end gap-2">
                    <Button variant="outline" onClick={() => onOpenChange(false)}>{t('cancel')}</Button>
                    <Button onClick={handleSubmit} disabled={isPending || (!account && !form.credentials && !isOAuth)}>
                        {account ? t('save') : t('addAccount')}
                    </Button>
                </div>
            </div>
        </div>
    );
}
