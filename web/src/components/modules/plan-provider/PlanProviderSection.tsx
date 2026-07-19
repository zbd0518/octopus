'use client';

import { useState, useCallback } from 'react';
import { useTranslations } from 'next-intl';
import { Plus, RefreshCw, Trash2, ExternalLink, Key, Loader2 } from 'lucide-react';
import {
    useBalanceProviders,
    useTokenPlanProviders,
    useBalanceCategories,
    useTokenPlanCategories,
    useAddPlanProvider,
    useRefreshPlanProvider,
    useDeletePlanProvider,
    type PlanProvider,
    type PlanProviderCategoryInfo,
} from '@/api/endpoints/plan-provider';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
    DialogTrigger,
} from '@/components/ui/dialog';
import {
    Tooltip,
    TooltipContent,
    TooltipTrigger,
} from '@/components/animate-ui/components/animate/tooltip';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';

// --- Balance Section ---

export function BalanceSection() {
    const t = useTranslations('hub');
    const { data: providers = [], isLoading, error } = useBalanceProviders();
    const { data: categories = [] } = useBalanceCategories();

    return (
        <PlanProviderSection
            type="balance"
            title={t('plan.balance') || '额度'}
            providers={providers}
            categories={categories}
            isLoading={isLoading}
            error={error}
        />
    );
}

// --- TokenPlan Section ---

export function TokenPlanSection() {
    const t = useTranslations('hub');
    const { data: providers = [], isLoading, error } = useTokenPlanProviders();
    const { data: categories = [] } = useTokenPlanCategories();

    return (
        <PlanProviderSection
            type="tokenplan"
            title={t('plan.tokenPlan') || 'TokenPlan'}
            providers={providers}
            categories={categories}
            isLoading={isLoading}
            error={error}
        />
    );
}

// --- Shared Section Component ---

interface PlanProviderSectionProps {
    type: 'balance' | 'tokenplan';
    title: string;
    providers: PlanProvider[];
    categories: PlanProviderCategoryInfo[];
    isLoading: boolean;
    error: unknown;
}

function PlanProviderSection({ type, title, providers, categories, isLoading, error }: PlanProviderSectionProps) {
    const t = useTranslations('hub');
    const addMutation = useAddPlanProvider();
    const refreshMutation = useRefreshPlanProvider();
    const deleteMutation = useDeletePlanProvider();
    const [addOpen, setAddOpen] = useState(false);
    const [selectedCategory, setSelectedCategory] = useState('');
    const [apiKey, setApiKey] = useState('');
    const [forwardApiKey, setForwardApiKey] = useState('');
    const [customName, setCustomName] = useState('');
    const [mimoAuthMode, setMimoAuthMode] = useState<'passToken' | 'serviceToken'>('serviceToken');

    const isConsoleTokenPlan = selectedCategory === 'stepfun_plan' || selectedCategory === 'sensenova_plan' || selectedCategory === 'mimo_plan' || selectedCategory === 'bailian_plan';
    const isMiMoPlan = selectedCategory === 'mimo_plan';
    const isCodexPlan = selectedCategory === 'codex';
    const supportsForwardApiKey = isConsoleTokenPlan && !isMiMoPlan;

    const handleAdd = useCallback(async () => {
        if (!selectedCategory || !apiKey.trim()) return;
        try {
            await addMutation.mutateAsync({
                category: selectedCategory,
                api_key: apiKey.trim(),
                forward_api_key: supportsForwardApiKey && forwardApiKey.trim() ? forwardApiKey.trim() : undefined,
                name: customName.trim() || undefined,
            });
            toast.success('已添加');
            setAddOpen(false);
            setSelectedCategory('');
            setApiKey('');
            setForwardApiKey('');
            setCustomName('');
            setMimoAuthMode('serviceToken');
        } catch (e: unknown) {
            const msg = e instanceof Error ? e.message : '添加失败';
            toast.error(msg);
        }
    }, [selectedCategory, apiKey, forwardApiKey, supportsForwardApiKey, customName, addMutation]);

    const handleRefresh = useCallback(async (id: number) => {
        try {
            await refreshMutation.mutateAsync(id);
            toast.success('已刷新');
        } catch (e: unknown) {
            const msg = e instanceof Error ? e.message : '刷新失败';
            toast.error(msg);
        }
    }, [refreshMutation]);

    const handleDelete = useCallback(async (id: number) => {
        try {
            await deleteMutation.mutateAsync(id);
            toast.success('已删除');
        } catch (e: unknown) {
            const msg = e instanceof Error ? e.message : '删除失败';
            toast.error(msg);
        }
    }, [deleteMutation]);

    const selectedInfo = categories.find(c => c.category === selectedCategory);

    return (
        <div className="space-y-4">
            {/* Header */}
            <div className="flex items-center justify-between">
                <h2 className="text-lg font-semibold">{title}</h2>
                <Dialog open={addOpen} onOpenChange={(open) => { setAddOpen(open); if (!open) { setMimoAuthMode('serviceToken'); setApiKey(''); } }}>
                    <DialogTrigger asChild>
                        <Button size="sm" className="rounded-xl gap-1.5">
                            <Plus className="size-4" />
                            <span className="hidden sm:inline">{t('plan.addProvider') || '添加'}</span>
                        </Button>
                    </DialogTrigger>
                    <DialogContent className="sm:max-w-md">
                        <DialogHeader>
                            <DialogTitle>{t('plan.addProviderTitle') || '添加额度监控'}</DialogTitle>
                            <DialogDescription>
                                {t('plan.addProviderDesc') || '选择厂商并输入 API Key，系统将自动查询额度并创建渠道。'}
                            </DialogDescription>
                        </DialogHeader>
                        <div className="space-y-4 py-2">
                            <div className="space-y-2">
                                <label className="text-sm font-medium">{t('plan.provider') || '厂商'}</label>
                                <Select value={selectedCategory} onValueChange={setSelectedCategory}>
                                    <SelectTrigger>
                                        <SelectValue placeholder={t('plan.selectProvider') || '选择厂商'} />
                                    </SelectTrigger>
                                    <SelectContent>
                                        {categories.map((cat) => (
                                            <SelectItem key={cat.category} value={cat.category}>
                                                {cat.name}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                                {selectedInfo && (
                                    <p className="text-xs text-muted-foreground">
                                        {selectedInfo.description}
                                        {selectedInfo.help_url && (
                                            <a
                                                href={selectedInfo.help_url}
                                                target="_blank"
                                                rel="noopener noreferrer"
                                                className="ml-2 text-primary hover:underline inline-flex items-center gap-0.5"
                                            >
                                                <ExternalLink className="size-3" />
                                                {t('plan.getKey') || '获取 Key'}
                                            </a>
                                        )}
                                    </p>
                                )}
                            </div>

                            <div className="space-y-2">
                                <label className="text-sm font-medium">
                                    {isMiMoPlan
                                        ? (t('plan.cookieLabel') || 'Cookie')
                                        : isCodexPlan
                                            ? (t('plan.codexOAuthLabel') || 'OAuth JSON')
                                            : isConsoleTokenPlan
                                                ? (t('plan.consoleTokenLabel') || '控制台 Token')
                                                : (t('plan.apiKeyLabel') || 'API Key')}
                                </label>
                                {isMiMoPlan && (
                                    <div className="space-y-1">
                                        <Select value={mimoAuthMode} onValueChange={(v: string) => { setMimoAuthMode(v as 'passToken' | 'serviceToken'); setApiKey(''); }}>
                                            <SelectTrigger className="h-9 text-sm">
                                                <SelectValue />
                                            </SelectTrigger>
                                            <SelectContent>
                                                <SelectItem value="serviceToken">serviceToken — 1 天有效</SelectItem>
                                                <SelectItem value="passToken">passToken — 30 天，自动刷新</SelectItem>
                                            </SelectContent>
                                        </Select>
                                    </div>
                                )}
                                <Input
                                    type="password"
                                    placeholder={isMiMoPlan
                                        ? (mimoAuthMode === 'passToken'
                                            ? (t('plan.mimoPassTokenPlaceholder') || '粘贴 account.xiaomi.com 的完整 Cookie（需包含 passToken=、userId=、cUserId= 字段）')
                                            : (t('plan.mimoServiceTokenPlaceholder') || '粘贴 platform.xiaomimimo.com 的完整 Cookie（需包含 api-platform_serviceToken=、userId=、api-platform_slh=、api-platform_ph= 字段）'))
                                        : isCodexPlan
                                            ? (t('plan.codexOAuthPlaceholder') || '粘贴 OAuth JSON 凭据（含 access_token 和 account_id）')
                                            : isConsoleTokenPlan
                                                ? (selectedInfo?.category === 'sensenova_plan'
                                                    ? (t('plan.sensenovaTokenPlaceholder') || '粘贴控制台 Bearer Token 值')
                                                    : (t('plan.oasisTokenPlaceholder') || '粘贴控制台 Cookie 中的 Oasis-Token 值'))
                                                : (t('plan.apiKeyPlaceholder') || '请输入 API Key')}
                                    value={apiKey}
                                    onChange={(e) => setApiKey(e.target.value)}
                                />
                                {isMiMoPlan && mimoAuthMode === 'passToken' && (
                                    <p className="text-[11px] leading-tight text-red-500">
                                        {t('plan.mimoPassTokenHint') || '⚠️ 安全风险极高：passToken 是小米账号长期会话凭证，可能可以换取小米云、小米社区、MiMo 等任何接入小米账号体系的服务的 Token（未验证）。填入后系统自动通过 SSO 刷新 serviceToken，无需手动更新。'}
                                    </p>
                                )}
                                {isMiMoPlan && mimoAuthMode === 'serviceToken' && (
                                    <p className="text-[11px] leading-tight text-amber-500">
                                        {t('plan.mimoServiceTokenHint') || '登录 platform.xiaomimimo.com → F12 → Application → Cookies，复制 api-platform 域下所有 Cookie。有效期约 1 天，过期后需手动更新。'}
                                    </p>
                                )}
                                {isConsoleTokenPlan && !isMiMoPlan && (
                                    <p className="text-[11px] leading-tight text-amber-500">
                                        {selectedInfo?.category === 'sensenova_plan'
                                            ? (t('plan.sensenovaTokenHint') || '需登录 platform.sensenova.cn 控制台，从请求头复制 Bearer Token 值。有效期约 3 小时，过期后需重新获取。')
                                            : (t('plan.oasisTokenHint') || '需登录 platform.stepfun.com 控制台，从浏览器 Cookie 复制 Oasis-Token 值（格式：access...refresh）。该 Token 有效期约 30 分钟，过期后需重新获取。')}
                                    </p>
                                )}
                                {isCodexPlan && (
                                    <p className="text-[11px] leading-tight text-amber-500">
                                        {t('plan.codexOAuthHint') || '从 ChatGPT 订阅账号获取 OAuth JSON 凭据（含 access_token 和 account_id）。系统将自动创建 Codex 类型转发渠道（接入点 chatgpt.com/backend-api/codex/responses）。access_token 有效期较短，过期后需重新获取。'}
                                    </p>
                                )}
                            </div>
                            {supportsForwardApiKey && (
                                <div className="space-y-2">
                                    <label className="text-sm font-medium">
                                        {t('plan.forwardApiKeyLabel') || 'API Key（可选）'}
                                    </label>
                                    <Input
                                        type="password"
                                        placeholder={t('plan.forwardApiKeyPlaceholder') || 'sk- 开头的 API Key，用于转发'}
                                        value={forwardApiKey}
                                        onChange={(e) => setForwardApiKey(e.target.value)}
                                    />
                                    <p className="text-xs text-muted-foreground">
                                        {t('plan.forwardApiKeyHint') || '填写后将自动创建或复用转发渠道，模型相同的合并为同一渠道。留空则仅监控套餐额度。'}
                                    </p>
                                </div>
                            )}

                            <div className="space-y-2">
                                <label className="text-sm font-medium">{t('plan.customName') || '自定义名称（可选）'}</label>
                                <Input
                                    placeholder={t('plan.customNamePlaceholder') || '留空则使用默认名称'}
                                    value={customName}
                                    onChange={(e) => setCustomName(e.target.value)}
                                />
                            </div>

                            <Button
                                className="w-full rounded-xl"
                                onClick={handleAdd}
                                disabled={!selectedCategory || !apiKey.trim() || addMutation.isPending}
                            >
                                {addMutation.isPending ? (
                                    <>
                                        <Loader2 className="size-4 animate-spin mr-2" />
                                        {t('plan.querying') || '查询中...'}
                                    </>
                                ) : (
                                    t('plan.add') || '添加并查询'
                                )}
                            </Button>
                        </div>
                    </DialogContent>
                </Dialog>
            </div>

            {/* Content */}
            {isLoading ? (
                <div className="rounded-xl border border-border bg-card p-6 text-sm text-muted-foreground text-center">
                    {t('plan.loading') || '正在加载...'}
                </div>
            ) : error ? (
                <div className="text-center py-8 text-muted-foreground">
                    {t('plan.loadError') || '加载失败'}
                </div>
            ) : providers.length === 0 ? (
                <div className="text-center py-8 text-muted-foreground border border-dashed rounded-xl">
                    <Key className="size-8 mx-auto mb-2 opacity-30" />
                    <p>{t('plan.empty') || '暂无监控项，点击上方按钮添加'}</p>
                </div>
            ) : (
                <div className="space-y-3">
                    {providers.map((provider) => (
                        <ProviderCard
                            key={provider.id}
                            provider={provider}
                            type={type}
                            onRefresh={handleRefresh}
                            onDelete={handleDelete}
                            isRefreshing={refreshMutation.isPending}
                            isDeleting={deleteMutation.isPending}
                        />
                    ))}
                </div>
            )}
        </div>
    );
}

// --- Provider Card ---

function ProviderCard({
    provider,
    type,
    onRefresh,
    onDelete,
    isRefreshing,
    isDeleting,
}: {
    provider: PlanProvider;
    type: 'balance' | 'tokenplan';
    onRefresh: (id: number) => void;
    onDelete: (id: number) => void;
    isRefreshing: boolean;
    isDeleting: boolean;
}) {
    const t = useTranslations('hub');

    // Find category info for display
    const isBalance = provider.provider_type === 'balance';

    const formatBalance = (val: number) => {
        if (val === 0) return '0';
        if (Math.abs(val) < 0.01) return val.toFixed(6);
        return val.toLocaleString(undefined, { maximumFractionDigits: 2 });
    };

    const formatTime = (val: string | null) => {
        if (!val) return '';
        try {
            const d = new Date(val);
            return d.toLocaleString('zh-CN', {
                month: '2-digit',
                day: '2-digit',
                hour: '2-digit',
                minute: '2-digit',
            });
        } catch {
            return val;
        }
    };

    return (
        <div className={cn(
            'rounded-xl border border-border bg-card p-4 transition-colors',
            !provider.channel_enabled && 'opacity-60'
        )}>
            {/* Top Row */}
            <div className="flex items-start justify-between gap-3 mb-3">
                <div className="min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                        <h3 className="font-semibold text-sm truncate">{provider.name}</h3>
                        <Badge variant="outline" className="text-xs shrink-0">
                            {isBalance ? '余额' : '套餐'}
                        </Badge>
                    </div>
                    <div className="flex items-center gap-2 mt-1">
                        <p className="text-xs text-muted-foreground truncate">
                            {provider.channel_name || (provider.channel_id > 0 ? `渠道 #${provider.channel_id}` : (t('plan.monitorOnly') || '仅监控'))}
                        </p>
                        {provider.last_refresh && (
                            <span className="text-xs text-muted-foreground shrink-0">
                                {formatTime(provider.last_refresh)}
                            </span>
                        )}
                    </div>
                </div>

                <div className="flex items-center gap-1 shrink-0">
                    <Tooltip>
                        <TooltipTrigger asChild>
                            <Button
                                size="icon"
                                variant="ghost"
                                className="size-8 rounded-lg"
                                onClick={() => onRefresh(provider.id)}
                                disabled={isRefreshing || isDeleting}
                            >
                                <RefreshCw className={cn('size-3.5', isRefreshing && 'animate-spin')} />
                            </Button>
                        </TooltipTrigger>
                        <TooltipContent>{t('plan.refresh') || '刷新'}</TooltipContent>
                    </Tooltip>

                    <Tooltip>
                        <TooltipTrigger asChild>
                            <Button
                                size="icon"
                                variant="ghost"
                                className="size-8 rounded-lg text-destructive hover:text-destructive"
                                onClick={() => onDelete(provider.id)}
                                disabled={isRefreshing || isDeleting}
                            >
                                <Trash2 className="size-3.5" />
                            </Button>
                        </TooltipTrigger>
                        <TooltipContent>{t('plan.delete') || '删除'}</TooltipContent>
                    </Tooltip>
                </div>
            </div>

            {/* Balance / TokenPlan Info */}
            {isBalance ? (
                <div className="grid grid-cols-2 gap-3">
                    <div className="rounded-lg bg-muted/50 p-2.5">
                        <p className="text-xs text-muted-foreground mb-1">
                            {t('plan.balanceAvailable') || '可用余额'}
                        </p>
                        <p className="text-lg font-bold text-primary tabular-nums">
                            {formatBalance(provider.balance)}
                        </p>
                    </div>
                    <div className="rounded-lg bg-muted/50 p-2.5">
                        <p className="text-xs text-muted-foreground mb-1">
                            {t('plan.balanceUsed') || '已用额度'}
                        </p>
                        <p className="text-lg font-bold tabular-nums text-muted-foreground">
                            {formatBalance(provider.balance_used)}
                        </p>
                    </div>
                </div>
            ) : (
                <div className="space-y-2">
                    {/* 主配额 */}
                    <div className="grid grid-cols-2 gap-3">
                        <div className="rounded-lg bg-muted/50 p-2.5">
                            <p className="text-xs text-muted-foreground mb-1">
                                {t('plan.quotaTotal') || '总配额'}
                            </p>
                            <p className="text-lg font-bold tabular-nums">
                                {formatBalance(provider.quota_total)}
                            </p>
                        </div>
                        <div className="rounded-lg bg-muted/50 p-2.5">
                            <p className="text-xs text-muted-foreground mb-1">
                                {t('plan.quotaUsed') || '已使用'}
                            </p>
                            <p className="text-lg font-bold tabular-nums text-orange-500">
                                {formatBalance(provider.quota_used)}
                            </p>
                        </div>
                    </div>
                    {/* 进度条 */}
                    {provider.quota_total > 0 && (
                        <div className="space-y-1">
                            <div className="h-2 rounded-full bg-muted overflow-hidden">
                                <div
                                    className="h-full rounded-full bg-primary transition-all"
                                    style={{
                                        width: `${Math.min(100, (provider.quota_used / provider.quota_total) * 100)}%`
                                    }}
                                />
                            </div>
                            <div className="flex items-center justify-between text-xs text-muted-foreground">
                                <span>
                                    {((provider.quota_used / provider.quota_total) * 100).toFixed(1)}%
                                </span>
                                {provider.quota_reset_at && (
                                    <span>
                                        {t('plan.resetAt') || '重置'} {formatTime(provider.quota_reset_at)}
                                    </span>
                                )}
                            </div>
                        </div>
                    )}
                    {/* 周配额（如果有） */}
                    {provider.weekly_total > 0 && (
                        <div className="rounded-lg bg-muted/50 p-2.5">
                            <div className="flex items-center justify-between">
                                <p className="text-xs text-muted-foreground">
                                    {t('plan.weeklyQuota') || '周/日配额'}
                                </p>
                                {provider.weekly_reset_at && (
                                    <p className="text-xs text-muted-foreground">
                                        {t('plan.resetAt') || '重置'} {formatTime(provider.weekly_reset_at)}
                                    </p>
                                )}
                            </div>
                            <div className="flex items-baseline gap-2 mt-1">
                                <span className="font-semibold text-sm tabular-nums">
                                    {formatBalance(provider.weekly_total - provider.weekly_used)}
                                </span>
                                <span className="text-xs text-muted-foreground">
                                    / {formatBalance(provider.weekly_total)}
                                </span>
                            </div>
                        </div>
                    )}
                </div>
            )}

            {/* Models */}
            {provider.models && (
                <div className="mt-2 flex items-center gap-1 flex-wrap">
                    {provider.models.split(',').slice(0, 4).map((m) => (
                        <Badge key={m} variant="secondary" className="text-xs">
                            {m.trim()}
                        </Badge>
                    ))}
                    {provider.models.split(',').length > 4 && (
                        <Badge variant="secondary" className="text-xs">
                            +{provider.models.split(',').length - 4}
                        </Badge>
                    )}
                </div>
            )}
        </div>
    );
}
