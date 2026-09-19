'use client';

import { useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { ShieldCheck } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Hint } from '@/components/ui/hint';
import { Switch } from '@/components/ui/switch';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';

/** 检测类别标识，与后端 model.PrivacyCategory 保持一致 */
const CATEGORIES = [
    'api_key',
    'password',
    'phone',
    'email',
    'id_card',
    'bank_card',
    'entropy',
    'custom',
] as const;

type CategoryId = (typeof CATEGORIES)[number];
type PrivacyAction = 'block' | 'filter';

interface PrivacyRule {
    type: 'keyword' | 'regex';
    pattern: string;
}

interface PrivacyCategoryConfig {
    enabled?: boolean;
    action?: PrivacyAction;
}

interface PrivacyConfig {
    categories?: Partial<Record<CategoryId, PrivacyCategoryConfig>>;
    rules?: PrivacyRule[];
}

const DEFAULT_CONFIG: PrivacyConfig = { categories: {}, rules: [] };
const DEFAULT_CONFIG_TEXT = JSON.stringify(DEFAULT_CONFIG);

export function SettingPrivacyProtection() {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();

    const [enabled, setEnabled] = useState(false);
    const [config, setConfig] = useState<PrivacyConfig>(DEFAULT_CONFIG);
    const [newRulePattern, setNewRulePattern] = useState('');
    const [newRuleType, setNewRuleType] = useState<'keyword' | 'regex'>('keyword');

    const intendedValuesRef = useRef<Record<string, string>>({
        [SettingKey.PrivacyProtectionEnabled]: 'false',
        [SettingKey.PrivacyProtectionConfig]: DEFAULT_CONFIG_TEXT,
    });
    const loadedKeysRef = useRef<Set<string>>(new Set());
    const hasLocalIntentRef = useRef<Record<string, boolean>>({});
    const inFlightValuesRef = useRef<Record<string, string | undefined>>({});

    useEffect(() => {
        if (!settings) return;

        const nextValues: Record<string, string> = {
            [SettingKey.PrivacyProtectionEnabled]:
                settings.find((item) => item.key === SettingKey.PrivacyProtectionEnabled)?.value || 'false',
            [SettingKey.PrivacyProtectionConfig]:
                settings.find((item) => item.key === SettingKey.PrivacyProtectionConfig)?.value || DEFAULT_CONFIG_TEXT,
        };

        const shouldApplyServerValue = (key: string, nextValue: string) => {
            if (!loadedKeysRef.current.has(key)) {
                loadedKeysRef.current.add(key);
                return true;
            }
            if (hasLocalIntentRef.current[key] && intendedValuesRef.current[key] !== nextValue) {
                return false;
            }
            hasLocalIntentRef.current[key] = false;
            return true;
        };

        queueMicrotask(() => {
            if (shouldApplyServerValue(SettingKey.PrivacyProtectionEnabled, nextValues[SettingKey.PrivacyProtectionEnabled])) {
                intendedValuesRef.current[SettingKey.PrivacyProtectionEnabled] = nextValues[SettingKey.PrivacyProtectionEnabled];
                setEnabled(nextValues[SettingKey.PrivacyProtectionEnabled] === 'true');
            }
            if (shouldApplyServerValue(SettingKey.PrivacyProtectionConfig, nextValues[SettingKey.PrivacyProtectionConfig])) {
                intendedValuesRef.current[SettingKey.PrivacyProtectionConfig] = nextValues[SettingKey.PrivacyProtectionConfig];
                try {
                    const parsed = JSON.parse(nextValues[SettingKey.PrivacyProtectionConfig]);
                    setConfig({
                        categories: parsed?.categories ?? {},
                        rules: Array.isArray(parsed?.rules) ? parsed.rules : [],
                    });
                } catch {
                    setConfig(DEFAULT_CONFIG);
                }
            }
        });
    }, [settings]);

    const flushSettingSave = (key: string) => {
        if (inFlightValuesRef.current[key] !== undefined || !hasLocalIntentRef.current[key]) return;

        const value = intendedValuesRef.current[key];
        inFlightValuesRef.current[key] = value;

        setSetting.mutate(
            { key, value },
            {
                onSuccess: () => { toast.success(t('saved')); },
                onError: () => {
                    if (intendedValuesRef.current[key] === value) {
                        hasLocalIntentRef.current[key] = false;
                    }
                },
                onSettled: () => {
                    delete inFlightValuesRef.current[key];
                    if (hasLocalIntentRef.current[key] && intendedValuesRef.current[key] !== value) {
                        flushSettingSave(key);
                    }
                },
            },
        );
    };

    const saveTextSetting = (key: string, value: string) => {
        if (value === intendedValuesRef.current[key]) return;
        intendedValuesRef.current[key] = value;
        hasLocalIntentRef.current[key] = true;
        flushSettingSave(key);
    };

    const saveBooleanSetting = (checked: boolean) => {
        const value = checked ? 'true' : 'false';
        setEnabled(checked);
        if (value === intendedValuesRef.current[SettingKey.PrivacyProtectionEnabled]) return;
        intendedValuesRef.current[SettingKey.PrivacyProtectionEnabled] = value;
        hasLocalIntentRef.current[SettingKey.PrivacyProtectionEnabled] = true;
        flushSettingSave(SettingKey.PrivacyProtectionEnabled);
    };

    const saveConfig = (next: PrivacyConfig) => {
        setConfig(next);
        saveTextSetting(SettingKey.PrivacyProtectionConfig, JSON.stringify(next));
    };

    /** 类别开关，未配置时默认开启 */
    const isCategoryEnabled = (id: CategoryId) => config.categories?.[id]?.enabled !== false;

    /** 类别动作，未配置时默认自动过滤 */
    const categoryAction = (id: CategoryId): PrivacyAction => config.categories?.[id]?.action ?? 'filter';

    const toggleCategory = (id: CategoryId, checked: boolean) => {
        saveConfig({
            ...config,
            categories: { ...config.categories, [id]: { ...config.categories?.[id], enabled: checked } },
        });
    };

    const setCategoryAction = (id: CategoryId, action: PrivacyAction) => {
        saveConfig({
            ...config,
            categories: { ...config.categories, [id]: { ...config.categories?.[id], action } },
        });
    };

    const handleAddRule = () => {
        const pattern = newRulePattern.trim();
        if (!pattern) return;
        const rules = config.rules ?? [];
        if (rules.some((r) => r.pattern === pattern && r.type === newRuleType)) {
            toast.error(t('privacyProtection.rules.duplicate'));
            return;
        }
        // 正则规则前端先校验，避免保存后才被后端拒绝
        if (newRuleType === 'regex') {
            try {
                new RegExp(pattern);
            } catch {
                toast.error(t('privacyProtection.rules.invalidRegex'));
                return;
            }
        }
        saveConfig({ ...config, rules: [...rules, { type: newRuleType, pattern }] });
        setNewRulePattern('');
    };

    const handleRemoveRule = (index: number) => {
        saveConfig({ ...config, rules: (config.rules ?? []).filter((_, i) => i !== index) });
    };

    const handleRuleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            handleAddRule();
        }
    };

    const rules = config.rules ?? [];

    return (
        <div className="min-w-0 space-y-5 rounded-xl border border-border/35 bg-card p-6 text-card-foreground">
            <div className="space-y-1">
                <h2 className="flex items-center gap-2 text-lg font-bold text-card-foreground">
                    <ShieldCheck className="h-5 w-5" />
                    {t('privacyProtection.title')}
                </h2>
                <p className="text-sm text-muted-foreground">{t('privacyProtection.description')}</p>
            </div>

            {/* 总开关 */}
            <div className="flex min-w-0 flex-col gap-3 rounded-lg border border-border/30 bg-card p-4 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0 flex items-center gap-3">
                    <ShieldCheck className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('privacyProtection.enabled.label')}</span>
                </div>
                <Switch checked={enabled} onCheckedChange={saveBooleanSetting} />
            </div>

            {/* 检测项与策略 */}
            <div className="space-y-3">
                <div className="flex items-center gap-2 px-1">
                    <span className="text-sm font-medium">{t('privacyProtection.categories.label')}</span>
                    <Hint text={t('privacyProtection.categories.hint')} />
                </div>

                {CATEGORIES.map((id) => (
                    <div
                        key={id}
                        className="flex min-w-0 flex-col gap-3 rounded-lg border border-border/30 bg-card p-4 sm:flex-row sm:items-center sm:justify-between"
                    >
                        <div className="min-w-0 space-y-0.5">
                            <span className="text-sm font-medium">{t(`privacyProtection.categories.${id}.label`)}</span>
                            <p className="text-xs text-muted-foreground">{t(`privacyProtection.categories.${id}.description`)}</p>
                        </div>
                        <div className="flex shrink-0 items-center gap-3">
                            <Switch
                                checked={isCategoryEnabled(id)}
                                onCheckedChange={(checked) => toggleCategory(id, checked)}
                            />
                            <Select
                                value={categoryAction(id)}
                                onValueChange={(value) => setCategoryAction(id, value as PrivacyAction)}
                            >
                                <SelectTrigger className="w-32 rounded-lg">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent className="rounded-lg">
                                    <SelectItem value="block">{t('privacyProtection.actions.block')}</SelectItem>
                                    <SelectItem value="filter">{t('privacyProtection.actions.filter')}</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>
                ))}
            </div>

            {/* 自定义敏感词 / 正则 */}
            <div className="space-y-3 rounded-lg border border-border/30 bg-card p-4">
                <div className="flex items-center gap-3">
                    <span className="text-sm font-medium">{t('privacyProtection.rules.label')}</span>
                    <Badge variant="secondary" className="text-xs">{rules.length}</Badge>
                </div>

                <div className="flex flex-col gap-2 sm:flex-row">
                    <Select value={newRuleType} onValueChange={(v) => setNewRuleType(v as 'keyword' | 'regex')}>
                        <SelectTrigger className="w-full rounded-xl sm:w-32">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent className="rounded-lg">
                            <SelectItem value="keyword">{t('privacyProtection.rules.typeKeyword')}</SelectItem>
                            <SelectItem value="regex">{t('privacyProtection.rules.typeRegex')}</SelectItem>
                        </SelectContent>
                    </Select>
                    <Input
                        value={newRulePattern}
                        onChange={(e) => setNewRulePattern(e.target.value)}
                        onKeyDown={handleRuleKeyDown}
                        placeholder={t('privacyProtection.rules.placeholder')}
                        className="flex-1 rounded-xl"
                    />
                    <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="shrink-0 rounded-xl"
                        onClick={handleAddRule}
                        disabled={!newRulePattern.trim()}
                    >
                        {t('privacyProtection.rules.add')}
                    </Button>
                </div>

                {rules.length > 0 && (
                    <div className="flex flex-wrap gap-2">
                        {rules.map((rule, index) => (
                            <Badge
                                key={`${rule.type}-${rule.pattern}-${index}`}
                                variant="outline"
                                className="cursor-pointer gap-1.5 rounded-lg px-2.5 py-1 text-xs transition-colors hover:bg-destructive/10 hover:text-destructive hover:border-destructive/30"
                                onClick={() => handleRemoveRule(index)}
                                title={t('privacyProtection.rules.removeHint')}
                            >
                                <span className="text-muted-foreground">
                                    {rule.type === 'regex'
                                        ? t('privacyProtection.rules.typeRegex')
                                        : t('privacyProtection.rules.typeKeyword')}
                                </span>
                                {rule.pattern}
                                <span className="text-muted-foreground">×</span>
                            </Badge>
                        ))}
                    </div>
                )}

                {rules.length === 0 && (
                    <p className="text-xs text-muted-foreground">{t('privacyProtection.rules.empty')}</p>
                )}
            </div>
        </div>
    );
}
