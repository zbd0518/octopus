'use client';

import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Wand2, Save, Loader2, Plus, X, Search, CheckCircle2, AlertCircle, Download, Check, ClipboardCopy, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Hint } from '@/components/ui/hint';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Switch } from '@/components/ui/switch';
import { SettingKey, useSettingList } from '@/api/endpoints/setting';
import {
    useModelChannelList,
    useModelNormalizeProgress,
    useSaveModelNormalizeRules,
    useStartModelNormalize,
    type LLMChannel,
} from '@/api/endpoints/model';
import { toast } from '@/components/common/Toast';
import {
    normalizeModelNameWithRules,
    normalizeToBase,
    dotDashKey,
    PROTECTED_MODEL_TOKENS,
    setNormalizeRules,
    type ExplicitMapping,
    type NormalizeRulesInput,
} from '@/components/modules/model/normalize';
import { writeClipboardText } from '@/lib/clipboard';
import { cn } from '@/lib/utils';
import { useNavStore } from '@/components/modules/navbar';

/**
 * 归一化规则配置卡片。
 *
 * 模型名归一化用于「按模型聚合/去重」视图：把同一基础模型在不同渠道下的
 * 命名变体（如 @cf/moonshotai/kimi-k2.5、dmxapi-kimi-k2.5-cc）归并为基础名
 * （kimi-k2.5）。这里维护两类规则——路由前缀（剥离开头）与功能后缀（剥离结尾），
 * 持久化到 DB Setting，运行时由 useNormalizeRulesSync 注入 normalize 模块。
 *
 * 下方的「覆盖分析」用渠道模型名数据（GET /api/v1/model/channel）跑一遍归一化，
 * 展示已被规则归并的簇（规则有效）与疑似漏归并的候选（补规则的线索）。
 */

// 不可归并的模型族标记——出现在待剥离片段中时拒绝归并，避免 gpt-4o-mini 被吃进 gpt-4o。
const MODEL_FAMILY_TOKENS = [...PROTECTED_MODEL_TOKENS, 'micro', 'big'];

function parseStringArray(raw: string | undefined): string[] {
    if (!raw) return [];
    try {
        const parsed = JSON.parse(raw);
        return Array.isArray(parsed) ? parsed.filter((x): x is string => typeof x === 'string') : [];
    } catch {
        return [];
    }
}

function parseExplicitMappings(raw: string | undefined): ExplicitMapping[] {
    if (!raw) return [];
    try {
        const parsed = JSON.parse(raw);
        if (!Array.isArray(parsed)) return [];
        return parsed
            .filter((x): x is ExplicitMapping =>
                x && typeof x === 'object'
                && typeof x.variant === 'string' && x.variant.trim() !== ''
                && typeof x.canonical === 'string' && x.canonical.trim() !== '')
            .map((x) => ({ variant: x.variant, canonical: x.canonical }));
    } catch {
        return [];
    }
}

function downloadBlob(blob: Blob, filename: string) {
    const url = URL.createObjectURL(blob);
    try {
        const a = document.createElement('a');
        a.href = url;
        a.download = filename;
        document.body.appendChild(a);
        a.click();
        a.remove();
    } finally {
        URL.revokeObjectURL(url);
    }
}

// 导入规则文件的解析结果：分类计数 + 原始规则数据，供预览后合并。
interface ImportPreview {
    routerPrefixes: string[];
    functionalSuffixes: string[];
    explicitMappings: ExplicitMapping[];
    prefixCount: number;
    suffixCount: number;
    mappingCount: number;
    coverage: CoverageSummary;
}

interface CoverageSummary {
    inputCount: number;
    canonicalCount: number;
    mergedVariantCount: number;
    highRiskCount: number;
    validationIssueCount: number;
    unresolvedCount: number;
}

/** 解析 AI 回传的完整规则替换候选，字段必须与三字段协议完全一致。 */
function parseRulesFile(text: string): ImportPreview {
    const data = JSON.parse(text) as unknown;
    if (!data || typeof data !== 'object' || Array.isArray(data)) {
        throw new Error('rules file must be a JSON object');
    }
    const keys = Object.keys(data).sort();
    if (keys.join('|') !== 'explicit_mappings|functional_suffixes|router_prefixes') {
        throw new Error('rules file must contain exactly router_prefixes, functional_suffixes, and explicit_mappings');
    }
    const asStrings = (v: unknown): string[] =>
        Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [];
    const asMappings = (v: unknown): ExplicitMapping[] =>
        Array.isArray(v)
            ? v.filter((x): x is ExplicitMapping =>
                x && typeof x === 'object'
                && typeof x.variant === 'string' && x.variant.trim() !== ''
                && typeof x.canonical === 'string' && x.canonical.trim() !== '')
                .map((x) => ({ variant: x.variant, canonical: x.canonical }))
            : [];
    const source = data as Record<string, unknown>;
    if (!Array.isArray(source.router_prefixes)
        || !Array.isArray(source.functional_suffixes)
        || !Array.isArray(source.explicit_mappings)) {
        throw new Error('rules file fields must be arrays');
    }
    const routerPrefixes = asStrings(source.router_prefixes);
    const functionalSuffixes = asStrings(source.functional_suffixes);
    const explicitMappings = asMappings(source.explicit_mappings);
    if (routerPrefixes.length !== source.router_prefixes.length
        || functionalSuffixes.length !== source.functional_suffixes.length
        || explicitMappings.length !== source.explicit_mappings.length) {
        throw new Error('rules file contains invalid rule entries');
    }
    return {
        routerPrefixes,
        functionalSuffixes,
        explicitMappings,
        prefixCount: routerPrefixes.length,
        suffixCount: functionalSuffixes.length,
        mappingCount: explicitMappings.length,
        coverage: emptyCoverageSummary(),
    };
}

function emptyCoverageSummary(): CoverageSummary {
    return {
        inputCount: 0,
        canonicalCount: 0,
        mergedVariantCount: 0,
        highRiskCount: 0,
        validationIssueCount: 0,
        unresolvedCount: 0,
    };
}

// 去重保序工具。
function dedupe<T>(arr: T[]): T[] {
    const seen = new Set<T>();
    const out: T[] = [];
    for (const x of arr) {
        if (!seen.has(x)) {
            seen.add(x);
            out.push(x);
        }
    }
    return out;
}

/** 规范化规则条目：路由前缀补 `-`、功能后缀补 `-` 前缀，保证与归一匹配逻辑一致。 */
function normalizeRouterPrefix(value: string): string {
    const v = value.trim();
    if (!v) return '';
    return v.endsWith('-') ? v : `${v}-`;
}
function normalizeFunctionalSuffix(value: string): string {
    const v = value.trim();
    if (!v) return '';
    return v.startsWith('-') ? v : `-${v}`;
}

interface MergedCluster {
    canonical: string;
    originals: string[];
}
interface SuspectedCluster {
    base: string;
    variants: { original: string; diff: string }[];
}

function semanticTokenSignature(name: string): string {
    let normalized = name.toLowerCase().replace(/[._:@/()[\]]/g, '-');
    while (normalized.includes('--')) normalized = normalized.replaceAll('--', '-');
    const padded = `-${normalized.replace(/^-+|-+$/g, '')}-`;
    const tokens = PROTECTED_MODEL_TOKENS.filter((token) => (
        padded.includes(`-${token}-`)
        && !(token === 'reasoning' && padded.includes('-non-reasoning-'))
    ));
    return tokens.sort().join('|');
}

function hasSemanticDifference(names: string[]): boolean {
    if (names.length < 2) return false;
    const first = semanticTokenSignature(names[0]);
    return names.slice(1).some((name) => semanticTokenSignature(name) !== first);
}

function hasPrefixBoundary(value: string): boolean {
    return /[-/.:_()[\]]$/.test(value);
}

function hasProtectedSuffixToken(value: string): boolean {
    let normalized = value.toLowerCase().replace(/^[-]+/, '').replace(/[._:@/()[\]]/g, '-');
    while (normalized.includes('--')) normalized = normalized.replaceAll('--', '-');
    const padded = `-${normalized.replace(/^-+|-+$/g, '')}-`;
    return PROTECTED_MODEL_TOKENS.some((token) => padded.includes(`-${token}-`));
}

function countRuleValidationIssues(rules: NormalizeRulesInput): number {
    let count = 0;
    const rulesWithoutExplicit = { ...rules, explicitMappings: [] };
    for (const prefix of rules.routerPrefixes ?? []) {
        const value = prefix.trim();
        if (!value || value.includes('/') || !hasPrefixBoundary(value)) count += 1;
    }
    for (const suffix of rules.functionalSuffixes ?? []) {
        const value = suffix.trim();
        if (!value || value.includes('/') || /[\r\n\t]/.test(value) || hasProtectedSuffixToken(value)) count += 1;
    }

    const mappingsByKey = new Map<string, string>();
    for (const mapping of rules.explicitMappings ?? []) {
        const variant = mapping.variant.trim();
        const canonical = mapping.canonical.trim().toLowerCase();
        if (!variant || !canonical) {
            count += 1;
            continue;
        }
        if (/[\/\[\]\s]/.test(canonical)
            || normalizeModelNameWithRules(canonical, rulesWithoutExplicit) !== canonical) {
            count += 1;
        }
        const key = dotDashKey(normalizeModelNameWithRules(variant, rulesWithoutExplicit));
        const previous = mappingsByKey.get(key);
        if (previous !== undefined && previous !== canonical) count += 1;
        else mappingsByKey.set(key, canonical);
    }
    return count;
}

// 在剥离掉共同基础名后，剩余的「变体片段」是否由分隔符切出的完整 token 组成。
// 拒绝字母粘连（如 gpt-4o vs gpt-4ox）以及含模型族标记（如 mini）的疑似归并。
function isCleanVariantFragment(fragment: string): boolean {
    const f = fragment.toLowerCase().replace(/^[.\-/]+|[.\-/]+$/g, '');
    if (!f) return false;
    if (MODEL_FAMILY_TOKENS.includes(f)) return false;
    // 剩余片段若仍含分隔符，按 token 逐个校验，任一命中模型族标记即拒绝。
    const tokens = f.split(/[.\-/]+/).filter(Boolean);
    if (tokens.length === 0) return false;
    return !tokens.some((tok) => MODEL_FAMILY_TOKENS.includes(tok));
}

/**
 * 对渠道模型名跑归一化，产出两类分析结果：
 *  - merged：归一后同名、原名不同的簇（当前规则已成功归并）。
 *  - suspected：原名互为前缀/后缀关系、归一后却不同（疑似漏归并，补规则线索）。
 *    用 isCleanVariantFragment 过滤掉 mini/nano 等模型族误报。
 */
function analyzeCoverage(channels: LLMChannel[], rules: NormalizeRulesInput): {
    merged: MergedCluster[];
    suspected: SuspectedCluster[];
    coverage: CoverageSummary;
} {
    const seen = new Map<string, Set<string>>();
    const originals: string[] = [];
    const originalSet = new Set<string>();
    let unresolvedCount = 0;
    for (const ch of channels) {
        const name = ch.name?.trim();
        if (!name) continue;
        const canonical = normalizeForAnalysis(name, rules);
        if (!canonical) unresolvedCount += 1;
        if (!seen.has(canonical)) seen.set(canonical, new Set());
        seen.get(canonical)!.add(name);
        if (!originalSet.has(name)) {
            originalSet.add(name);
            originals.push(name);
        }
    }

    const merged: MergedCluster[] = [];
    let mergedVariantCount = 0;
    let highRiskCount = 0;
    for (const [canonical, set] of seen) {
        if (set.size > 1) {
            const variants = Array.from(set).sort();
            merged.push({ canonical, originals: variants });
            mergedVariantCount += variants.length - 1;
            if (hasSemanticDifference(variants)) highRiskCount += 1;
        }
    }
    merged.sort((a, b) => b.originals.length - a.originals.length || a.canonical.localeCompare(b.canonical));

    // 疑似漏归并：归一后不同的名之间，存在 A 是 B 的前缀（按分隔符边界）且剥离片段干净。
    const lowerOriginals = originals.map((o) => o.toLowerCase());
    const prefixIndex = new Map<string, string[]>();
    for (const original of originals) {
        const lower = original.toLowerCase();
        for (let i = 0; i < lower.length; i += 1) {
            if (!/[.\-/]/.test(lower[i])) continue;
            const prefix = lower.slice(0, i);
            if (!prefix) continue;
            const values = prefixIndex.get(prefix);
            if (values) values.push(original);
            else prefixIndex.set(prefix, [original]);
        }
    }
    const suspectedMap = new Map<string, SuspectedCluster>();
    for (let i = 0; i < lowerOriginals.length; i += 1) {
        const a = lowerOriginals[i];
        const candidates = prefixIndex.get(a) ?? [];
        for (const variant of candidates) {
            if (variant.toLowerCase() === a) continue;
            const fragment = variant.slice(a.length);
            if (!isCleanVariantFragment(fragment)) continue;
            const base = originals[i];
            if (!suspectedMap.has(base)) suspectedMap.set(base, { base, variants: [] });
            const arr = suspectedMap.get(base)!.variants;
            if (!arr.some((v) => v.original === variant)) {
                arr.push({ original: variant, diff: fragment });
            }
        }
    }
    const suspected = Array.from(suspectedMap.values()).sort((a, b) => b.variants.length - a.variants.length || a.base.localeCompare(b.base));
    return {
        merged,
        suspected,
        coverage: {
            inputCount: originals.length,
            canonicalCount: seen.size - (seen.has('') ? 1 : 0),
            mergedVariantCount,
            highRiskCount,
            validationIssueCount: countRuleValidationIssues(rules),
            unresolvedCount,
        },
    };
}

// 分析用的归一化：复用运行时规则，但保留原始大小写做簇内展示（归一仅用于分簇键）。
function normalizeForAnalysis(name: string, rules: NormalizeRulesInput): string {
    return normalizeModelNameWithRules(name, rules);
}

function RuleList({
    items,
    onAdd,
    onRemove,
    onClearAll,
    addPlaceholder,
    emptyHint,
}: {
    items: string[];
    onAdd: (value: string) => void;
    onRemove: (index: number) => void;
    onClearAll: () => void;
    addPlaceholder: string;
    emptyHint: string;
}) {
    const t = useTranslations('setting');
    const [draft, setDraft] = useState('');

    const handleAdd = () => {
        const v = draft.trim();
        if (!v) return;
        onAdd(v);
        setDraft('');
    };

    const handleClearAll = () => {
        if (items.length === 0) return;
        if (!window.confirm(t('normalize.clearAllConfirm'))) return;
        onClearAll();
    };

    return (
        <div className="space-y-2">
            <div className="flex gap-2">
                <Input
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                            e.preventDefault();
                            handleAdd();
                        }
                    }}
                    placeholder={addPlaceholder}
                    className="flex-1 rounded-xl"
                />
                <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="shrink-0 rounded-xl"
                    onClick={handleAdd}
                    disabled={!draft.trim()}
                >
                    <Plus className="mr-1 size-3.5" />
                    {t('normalize.add')}
                </Button>
            </div>

            {items.length > 0 && (
                <div className="flex items-center justify-end">
                    <button
                        type="button"
                        onClick={handleClearAll}
                        className="flex items-center gap-1 rounded-md px-1.5 py-0.5 text-xs text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
                    >
                        <Trash2 className="size-3" />
                        {t('normalize.clearAll')}
                    </button>
                </div>
            )}

            {items.length > 0 ? (
                <div className="flex flex-wrap gap-2">
                    {items.map((item, index) => (
                        <Badge
                            key={`${item}-${index}`}
                            variant="outline"
                            className="cursor-pointer gap-1.5 rounded-lg px-2.5 py-1 font-mono text-xs transition-colors hover:bg-destructive/10 hover:text-destructive hover:border-destructive/30"
                            onClick={() => onRemove(index)}
                            title={t('normalize.removeHint')}
                        >
                            {item}
                            <X className="size-3" />
                        </Badge>
                    ))}
                </div>
            ) : (
                <p className="text-xs text-muted-foreground">{emptyHint}</p>
            )}
        </div>
    );
}

export function SettingNormalize() {
    const t = useTranslations('setting');
    const isSettingActive = useNavStore((state) => state.activeItem === 'setting');
    const { data: settings } = useSettingList();
    const saveRules = useSaveModelNormalizeRules();
    const { data: channelModels, isLoading: channelsLoading, refetch: refetchChannels } = useModelChannelList();

    const [routerPrefixes, setRouterPrefixes] = useState<string[]>([]);
    const [functionalSuffixes, setFunctionalSuffixes] = useState<string[]>([]);
    const [explicitMappings, setExplicitMappings] = useState<ExplicitMapping[]>([]);
    // 模型广场是否默认开启归一化去重。独立于上述规则，单独持久化与保存。
    const [marketDedupeDefault, setMarketDedupeDefault] = useState(true);
    const [loaded, setLoaded] = useState(false);
    const [saving, setSaving] = useState(false);
    const [analyzing, setAnalyzing] = useState(false);
    const [exporting, setExporting] = useState(false);
    const [copyingPrompt, setCopyingPrompt] = useState(false);
    const [importPreview, setImportPreview] = useState<ImportPreview | null>(null);
    const importFileRef = useRef<HTMLInputElement | null>(null);
    const [analysis, setAnalysis] = useState<{ merged: MergedCluster[]; suspected: SuspectedCluster[] } | null>(null);
    const [aiTaskId, setAiTaskId] = useState<string | null>(null);
    const startAITask = useStartModelNormalize();
    const { data: aiTask } = useModelNormalizeProgress(aiTaskId, isSettingActive);

    // 从 DB Setting 加载规则到本地编辑态（仅首次加载）。
    useEffect(() => {
        if (!settings || loaded) return;
        setRouterPrefixes(parseStringArray(settings.find((s) => s.key === SettingKey.ModelNormalizeRouterPrefixes)?.value));
        setFunctionalSuffixes(parseStringArray(settings.find((s) => s.key === SettingKey.ModelNormalizeFunctionalSuffixes)?.value));
        setExplicitMappings(parseExplicitMappings(settings.find((s) => s.key === SettingKey.ModelNormalizeExplicitMappings)?.value));
        setMarketDedupeDefault(settings.find((s) => s.key === SettingKey.ModelNormalizeMarketDedupeDefault)?.value === 'true');
        setLoaded(true);
    }, [settings, loaded]);

    const handleAddPrefix = (value: string) => {
        const v = normalizeRouterPrefix(value);
        if (routerPrefixes.includes(v)) {
            toast.error(t('normalize.duplicate'));
            return;
        }
        setRouterPrefixes((prev) => [...prev, v]);
    };
    const handleRemovePrefix = (index: number) => setRouterPrefixes((prev) => prev.filter((_, i) => i !== index));

    const handleAddSuffix = (value: string) => {
        const v = normalizeFunctionalSuffix(value);
        if (functionalSuffixes.includes(v)) {
            toast.error(t('normalize.duplicate'));
            return;
        }
        setFunctionalSuffixes((prev) => [...prev, v]);
    };
    const handleRemoveSuffix = (index: number) => setFunctionalSuffixes((prev) => prev.filter((_, i) => i !== index));

    const handleSave = async () => {
        // 冲突检测：dotDashKey（-/. 统一）后相同的变体不得映射到不同基准名
        // （如 claude-opus-4-6→claude-opus-4.6 与 claude-opus-4.6→claude-opus-4-6
        // 是同一模型两种命名，但映射到相反方向，去重失效）。
        const normToCanonical = new Map<string, string>();
        const conflicts: string[] = [];
        for (const m of explicitMappings) {
            const key = dotDashKey(normalizeToBase(m.variant));
            if (!key) continue;
            const canon = m.canonical.toLowerCase().trim();
            const existing = normToCanonical.get(key);
            if (existing !== undefined && existing !== canon) {
                conflicts.push(`${m.variant} → ${m.canonical}`);
            } else {
                normToCanonical.set(key, canon);
            }
        }
        if (conflicts.length > 0) {
            setSaving(false);
            toast.error(t('normalize.conflict', { count: conflicts.length, first: conflicts.slice(0, 3).join('、') }));
            return;
        }

        setSaving(true);
        try {
            await saveRules.mutateAsync({
                router_prefixes: routerPrefixes,
                functional_suffixes: functionalSuffixes,
                explicit_mappings: explicitMappings,
                market_dedupe_default: marketDedupeDefault,
            });
            // 立即注入运行时，无需等设置列表刷新。
            setNormalizeRules({ routerPrefixes, functionalSuffixes, explicitMappings });
            toast.success(t('normalize.saved'));
        } catch (e) {
            toast.error(e instanceof Error ? e.message : t('normalize.saveFailed'));
        } finally {
            setSaving(false);
        }
    };

    const handleAnalyze = () => {
        if (!channelModels || channelModels.length === 0) {
            toast.error(t('normalize.analysis.empty'));
            return;
        }
        setAnalyzing(true);
        try {
            const result = analyzeCoverage(channelModels, {
                routerPrefixes,
                functionalSuffixes,
                explicitMappings,
            });
            setAnalysis(result);
        } catch (e) {
            toast.error(e instanceof Error ? e.message : t('normalize.analysis.failed'));
        } finally {
            setAnalyzing(false);
        }
    };

    const addPrefixFromAnalysis = (prefix: string) => {
        const v = normalizeRouterPrefix(prefix);
        if (routerPrefixes.includes(v)) return;
        setRouterPrefixes((prev) => [...prev, v]);
        toast.success(t('normalize.addedFromAnalysis'));
    };
    const addSuffixFromAnalysis = (suffix: string) => {
        const v = normalizeFunctionalSuffix(suffix);
        if (functionalSuffixes.includes(v)) return;
        setFunctionalSuffixes((prev) => [...prev, v]);
        toast.success(t('normalize.addedFromAnalysis'));
    };

    // 复制离线 AI 分析提示词到剪贴板。提示词指导 AI 把导出的渠道模型名 JSON
    // 归并为基础名，并产出 router_prefixes / functional_suffixes / explicit_mappings。
    const handleCopyPrompt = async () => {
        setCopyingPrompt(true);
        try {
            await writeClipboardText(t.raw('normalize.workflow.prompt'));
            toast.success(t('normalize.workflow.promptCopied'));
        } catch (e) {
            toast.error(e instanceof Error ? e.message : t('normalize.workflow.promptCopyFailed'));
        } finally {
            // 保持「已复制」勾选状态短暂可见，再恢复按钮文案。
            setTimeout(() => setCopyingPrompt(false), 1500);
        }
    };

    const handleStartAI = async () => {
        if (!channelModels || channelModels.length === 0) {
            toast.error(t('normalize.analysis.empty'));
            return;
        }
        try {
            const task = await startAITask.mutateAsync();
            setAiTaskId(task.id);
        } catch (e) {
            toast.error(e instanceof Error ? e.message : t('normalize.workflow.aiFailed'));
        }
    };

    const applyAITask = () => {
        const rules = aiTask?.result?.rules;
        if (!rules) return;
        setRouterPrefixes(dedupe(rules.router_prefixes.map(normalizeRouterPrefix).filter(Boolean)));
        setFunctionalSuffixes(dedupe(rules.functional_suffixes.map(normalizeFunctionalSuffix).filter(Boolean)));
        setExplicitMappings(rules.explicit_mappings);
        setImportPreview(null);
        toast.success(t('normalize.workflow.aiApplied'));
    };

    // 导出脱敏的去重模型名语料，供离线 AI/agent 分析变体关系。
    const handleExportChannels = async () => {
        let channels = channelModels;
        if (!channels || channels.length === 0) {
            try {
                channels = await refetchChannels().then((r: { data?: LLMChannel[] }) => r.data ?? undefined);
            } catch {
                /* fall through to empty handling */
            }
        }
        const list = channels ?? [];
        const modelNames = Array.from(new Set(list.map((channel) => channel.name?.trim()).filter(Boolean))).sort();
        if (modelNames.length === 0) {
            toast.error(t('normalize.export.empty'));
            return;
        }
        setExporting(true);
        try {
            const payload = {
                exported_at: new Date().toISOString(),
                model_names: modelNames,
            };
            const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
            const d = new Date();
            const pad = (n: number) => String(n).padStart(2, '0');
            const ts = `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`;
            await downloadBlob(blob, `model-channels-${ts}.json`);
            toast.success(t('normalize.export.success'));
        } catch (e) {
            toast.error(e instanceof Error ? e.message : t('normalize.export.failed'));
        } finally {
            setExporting(false);
        }
    };

    // 导入 AI 离线分析产出的归一化规则文件。
    // 支持字段：router_prefixes / functional_suffixes / explicit_mappings（均可选）。
    const handleImportRules = (file: File | null) => {
        if (!file) return;
        const reader = new FileReader();
        reader.onload = () => {
            try {
                const parsed = parseRulesFile(String(reader.result ?? ''));
                const candidate = {
                    routerPrefixes: dedupe(parsed.routerPrefixes.map(normalizeRouterPrefix).filter(Boolean)),
                    functionalSuffixes: dedupe(parsed.functionalSuffixes.map(normalizeFunctionalSuffix).filter(Boolean)),
                    explicitMappings: parsed.explicitMappings,
                };
                const coverage = analyzeCoverage(channelModels ?? [], candidate).coverage;
                setImportPreview({
                    ...parsed,
                    ...candidate,
                    prefixCount: candidate.routerPrefixes.length,
                    suffixCount: candidate.functionalSuffixes.length,
                    mappingCount: candidate.explicitMappings.length,
                    coverage,
                });
            } catch (e) {
                toast.error(e instanceof Error ? e.message : t('normalize.import.parseFailed'));
            }
        };
        reader.onerror = () => toast.error(t('normalize.import.parseFailed'));
        reader.readAsText(file);
    };

    // 确认应用导入预览：将 AI 输出作为完整规则集候选载入本地编辑态，
    // 不立即保存，由用户检查后点击保存落库。
    const applyImport = () => {
        if (!importPreview) return;
        setRouterPrefixes(dedupe(importPreview.routerPrefixes.map(normalizeRouterPrefix).filter(Boolean)));
        setFunctionalSuffixes(dedupe(importPreview.functionalSuffixes.map(normalizeFunctionalSuffix).filter(Boolean)));
        setExplicitMappings(importPreview.explicitMappings);
        setImportPreview(null);
        if (importFileRef.current) importFileRef.current.value = '';
        toast.success(t('normalize.import.applied'));
    };

    const channelCount = channelModels?.length ?? 0;
    const hasUnsavedChanges = useMemo(() => {
        if (!loaded || !settings) return false;
        const savedPrefixes = JSON.stringify(parseStringArray(settings.find((s) => s.key === SettingKey.ModelNormalizeRouterPrefixes)?.value));
        const savedSuffixes = JSON.stringify(parseStringArray(settings.find((s) => s.key === SettingKey.ModelNormalizeFunctionalSuffixes)?.value));
        const savedMappings = JSON.stringify(parseExplicitMappings(settings.find((s) => s.key === SettingKey.ModelNormalizeExplicitMappings)?.value));
        const savedMarketDedupe = settings.find((s) => s.key === SettingKey.ModelNormalizeMarketDedupeDefault)?.value === 'true';
        return JSON.stringify(routerPrefixes) !== savedPrefixes
            || JSON.stringify(functionalSuffixes) !== savedSuffixes
            || JSON.stringify(explicitMappings) !== savedMappings
            || marketDedupeDefault !== savedMarketDedupe;
    }, [loaded, settings, routerPrefixes, functionalSuffixes, explicitMappings, marketDedupeDefault]);

    return (
        <div className="rounded-xl border-border/35 bg-card p-4 sm:p-6 space-y-4 sm:space-y-5 text-card-foreground shadow-md">
            <div className="space-y-1">
                <h2 className="text-lg font-bold text-card-foreground flex items-center gap-2">
                    <Wand2 className="h-5 w-5" />
                    {t('normalize.title')}
                </h2>
                <p className="text-sm text-muted-foreground">{t('normalize.description')}</p>
            </div>

            {/* 模型广场默认开启归一化去重：独立于规则编辑，随保存按钮一起落库。 */}
            <div className="flex items-center justify-between gap-3 rounded-lg border-border/30 bg-card p-3 sm:p-4 shadow-sm">
                <div className="space-y-0.5">
                    <div className="text-sm font-semibold text-card-foreground">
                        {t('normalize.marketDedupeDefault.title')}
                        <Hint text={t('normalize.marketDedupeDefault.hint')} />
                    </div>
                </div>
                <Switch checked={marketDedupeDefault} onCheckedChange={setMarketDedupeDefault} aria-label={t('normalize.marketDedupeDefault.title')} />
            </div>

            {/* 路由前缀 */}
            <div className="space-y-3 rounded-lg border-border/30 bg-card p-3 sm:p-4 shadow-sm">
                <div className="flex items-center gap-2">
                    <span className="text-sm font-semibold text-card-foreground">
                        {t('normalize.routerPrefixes.title')}
                        <Hint text={t('normalize.routerPrefixes.hint')} />
                    </span>
                    <Badge variant="secondary" className="text-xs">{routerPrefixes.length}</Badge>
                </div>
                <RuleList
                    items={routerPrefixes}
                    onAdd={handleAddPrefix}
                    onRemove={handleRemovePrefix}
                    onClearAll={() => setRouterPrefixes([])}
                    addPlaceholder={t('normalize.routerPrefixes.placeholder')}
                    emptyHint={t('normalize.routerPrefixes.empty')}
                />
            </div>

            {/* 功能后缀 */}
            <div className="space-y-3 rounded-lg border-border/30 bg-card p-3 sm:p-4 shadow-sm">
                <div className="flex items-center gap-2">
                    <span className="text-sm font-semibold text-card-foreground">
                        {t('normalize.functionalSuffixes.title')}
                        <Hint text={t('normalize.functionalSuffixes.hint')} />
                    </span>
                    <Badge variant="secondary" className="text-xs">{functionalSuffixes.length}</Badge>
                </div>
                <RuleList
                    items={functionalSuffixes}
                    onAdd={handleAddSuffix}
                    onRemove={handleRemoveSuffix}
                    onClearAll={() => setFunctionalSuffixes([])}
                    addPlaceholder={t('normalize.functionalSuffixes.placeholder')}
                    emptyHint={t('normalize.functionalSuffixes.empty')}
                />
            </div>

            {/* 显式映射（变体→基准名，主要由 AI 离线分析后导入；支持删除） */}
            <div className="space-y-3 rounded-lg border-border/30 bg-card p-3 sm:p-4 shadow-sm">
                <div className="flex items-center gap-2">
                    <span className="text-sm font-semibold text-card-foreground">
                        {t('normalize.explicitMappings.title')}
                        <Hint text={t('normalize.explicitMappings.hint')} />
                    </span>
                    <Badge variant="secondary" className="text-xs">{explicitMappings.length}</Badge>
                </div>
                {explicitMappings.length > 0 && (
                    <div className="flex items-center justify-end">
                        <button
                            type="button"
                            onClick={() => {
                                if (window.confirm(t('normalize.clearAllConfirm'))) setExplicitMappings([]);
                            }}
                            className="flex items-center gap-1 rounded-md px-1.5 py-0.5 text-xs text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
                        >
                            <Trash2 className="size-3" />
                            {t('normalize.clearAll')}
                        </button>
                    </div>
                )}
                {explicitMappings.length > 0 ? (
                    <div className="max-h-56 space-y-1 overflow-y-auto">
                        {explicitMappings.map((m, index) => (
                            <div
                                key={`${m.variant}-${index}`}
                                className="group flex items-center gap-2 rounded-md border border-border/30 bg-card px-2.5 py-1.5"
                            >
                                <span className="font-mono text-xs text-muted-foreground break-all">{m.variant}</span>
                                <span className="shrink-0 text-muted-foreground">→</span>
                                <span className="font-mono text-xs font-medium text-foreground break-all">{m.canonical}</span>
                                <button
                                    type="button"
                                    className="ml-auto shrink-0 rounded p-0.5 text-muted-foreground opacity-0 transition-opacity hover:text-destructive group-hover:opacity-100"
                                    onClick={() => setExplicitMappings((prev) => prev.filter((_, i) => i !== index))}
                                    title={t('normalize.removeHint')}
                                >
                                    <X className="size-3.5" />
                                </button>
                            </div>
                        ))}
                    </div>
                ) : (
                    <p className="text-xs text-muted-foreground">{t('normalize.explicitMappings.empty')}</p>
                )}
            </div>

            <Button
                type="button"
                variant="outline"
                className="w-full rounded-xl"
                onClick={handleSave}
                disabled={saving || !loaded || !hasUnsavedChanges}
            >
                {saving ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
                {saving ? t('normalize.saving') : t('normalize.save')}
            </Button>

            <div className="h-px bg-border/50" />

            {/* 导出 / 导入（离线 AI 归一化工作流） */}
            <div className="space-y-3 rounded-lg border-border/30 bg-card p-3 sm:p-4 shadow-sm">
                <div className="space-y-0.5">
                    <div className="text-sm font-semibold text-card-foreground flex items-center gap-2">
                        <Download className="size-4" />
                        {t('normalize.workflow.title')}
                    </div>
                    <p className="text-xs leading-5 text-muted-foreground">{t('normalize.workflow.description')}</p>
                </div>

                {/* 复制分析提示词：供用户粘贴到离线 AI，指导其分析导出的渠道模型名 JSON */}
                <div className="space-y-1.5">
                    <div className="text-xs font-medium text-muted-foreground">
                        {t('normalize.workflow.promptTitle')}
                        <Hint text={t('normalize.workflow.promptHint')} />
                    </div>
                    <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="w-full rounded-xl"
                        onClick={handleCopyPrompt}
                        disabled={copyingPrompt}
                    >
                        {copyingPrompt ? <Check className="size-3.5" /> : <ClipboardCopy className="size-3.5" />}
                        {copyingPrompt ? t('normalize.workflow.promptCopied') : t('normalize.workflow.promptCopy')}
                    </Button>
                </div>

                <div className="space-y-2 rounded-lg border border-emerald-500/20 bg-emerald-500/5 p-3">
                    <div className="text-xs font-medium text-foreground">{t('normalize.workflow.aiTitle')}</div>
                    <p className="text-xs leading-5 text-muted-foreground">{t('normalize.workflow.aiHint')}</p>
                    <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="w-full rounded-xl"
                        onClick={handleStartAI}
                        disabled={startAITask.isPending || Boolean(aiTaskId && aiTask && !aiTask.done)}
                    >
                        {startAITask.isPending || (aiTaskId && aiTask && !aiTask.done)
                            ? <Loader2 className="size-3.5 animate-spin" />
                            : <Wand2 className="size-3.5" />}
                        {startAITask.isPending || (aiTaskId && aiTask && !aiTask.done)
                            ? t('normalize.workflow.aiGenerating')
                            : t('normalize.workflow.aiGenerate')}
                    </Button>
                    {aiTask && (
                        <div className="space-y-2 rounded-md border border-border/30 bg-card p-2.5 text-xs">
                            <div className="flex items-center justify-between gap-2">
                                <span className="text-muted-foreground">{aiTask.message || aiTask.status}</span>
                                <span className="font-mono tabular-nums">{aiTask.progress_percent}%</span>
                            </div>
                            <div className="h-1.5 overflow-hidden rounded-full bg-muted">
                                <div
                                    className="h-full rounded-full bg-emerald-500 transition-[width]"
                                    style={{ width: `${Math.max(0, Math.min(100, aiTask.progress_percent))}%` }}
                                />
                            </div>
                            {aiTask.result?.coverage && (
                                <div className="grid grid-cols-3 gap-1.5 text-center">
                                    <div className="rounded bg-muted/40 p-1.5">
                                        <div className="font-mono font-semibold">{aiTask.result.coverage.input_count}</div>
                                        <div className="text-[10px] text-muted-foreground">{t('normalize.workflow.aiInput')}</div>
                                    </div>
                                    <div className="rounded bg-muted/40 p-1.5">
                                        <div className="font-mono font-semibold">{aiTask.result.coverage.canonical_count}</div>
                                        <div className="text-[10px] text-muted-foreground">{t('normalize.workflow.aiCanonical')}</div>
                                    </div>
                                    <div className="rounded bg-muted/40 p-1.5">
                                        <div className="font-mono font-semibold">{aiTask.result.coverage.merged_variant_count}</div>
                                        <div className="text-[10px] text-muted-foreground">{t('normalize.workflow.aiMerged')}</div>
                                    </div>
                                </div>
                            )}
                            {aiTask.result?.coverage && (
                                <div className="flex flex-wrap gap-1.5 text-[10px]">
                                    <span className="rounded bg-amber-500/10 px-1.5 py-1 text-amber-700 dark:text-amber-300">
                                        {t('normalize.workflow.aiRisk', {
                                            count: aiTask.result.coverage.high_risk_merges?.length ?? 0,
                                        })}
                                    </span>
                                    <span className="rounded bg-red-500/10 px-1.5 py-1 text-red-700 dark:text-red-300">
                                        {t('normalize.workflow.aiInvalid', {
                                            count: aiTask.result.validation_errors?.length
                                                ?? aiTask.result.coverage.diagnostics?.filter((item) => item.severity === 'error').length
                                                ?? 0,
                                        })}
                                    </span>
                                </div>
                            )}
                            {aiTask.error_reason && (
                                <p className="text-destructive">{aiTask.error_reason}</p>
                            )}
                            {aiTask.result?.validation_errors?.length ? (
                                <p className="text-[10px] text-amber-700 dark:text-amber-300">
                                    {t('normalize.workflow.aiConflict')}
                                </p>
                            ) : null}
                            {aiTask.result_ready && (
                                <Button type="button" size="sm" className="w-full rounded-xl" onClick={applyAITask}>
                                    <Check className="size-3.5" />
                                    {t('normalize.workflow.aiApply')}
                                </Button>
                            )}
                        </div>
                    )}
                </div>

                <div className="h-px bg-border/30" />

                {/* 导出：含变体的渠道模型名，供离线 AI 分析 */}
                <div className="space-y-1.5">
                    <div className="text-xs font-medium text-muted-foreground">
                        {t('normalize.export.title')}
                        <Hint text={t('normalize.export.hint')} />
                    </div>
                    <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="w-full rounded-xl"
                        onClick={handleExportChannels}
                        disabled={exporting || channelCount === 0}
                    >
                        {exporting ? <Loader2 className="size-3.5 animate-spin" /> : <Download className="size-3.5" />}
                        {exporting ? t('normalize.export.exporting') : t('normalize.export.button')}
                    </Button>
                    {channelCount > 0 && (
                        <p className="text-[10px] text-muted-foreground">{t('normalize.export.count', { count: channelCount })}</p>
                    )}
                </div>

                <div className="h-px bg-border/30" />

                {/* 导入：AI 产出的归一化规则文件 */}
                <div className="space-y-1.5">
                    <div className="text-xs font-medium text-muted-foreground">
                        {t('normalize.import.title')}
                        <Hint text={t('normalize.import.hint')} />
                    </div>
                    <Input
                        ref={importFileRef}
                        type="file"
                        accept="application/json,.json"
                        onChange={(e) => handleImportRules(e.target.files?.[0] ?? null)}
                        className="rounded-xl"
                    />
                </div>

                {importPreview && (
                    <div className="space-y-2 rounded-lg border border-blue-500/20 bg-blue-500/5 p-3">
                        <div className="text-xs font-semibold text-foreground">{t('normalize.import.preview')}</div>
                        <div className="grid grid-cols-3 gap-2 text-center text-xs">
                            <div className="rounded-md bg-card p-2">
                                <div className="font-mono font-semibold text-foreground">{importPreview.prefixCount}</div>
                                <div className="text-muted-foreground">{t('normalize.routerPrefixes.title')}</div>
                            </div>
                            <div className="rounded-md bg-card p-2">
                                <div className="font-mono font-semibold text-foreground">{importPreview.suffixCount}</div>
                                <div className="text-muted-foreground">{t('normalize.functionalSuffixes.title')}</div>
                            </div>
                            <div className="rounded-md bg-card p-2">
                                <div className="font-mono font-semibold text-foreground">{importPreview.mappingCount}</div>
                                <div className="text-muted-foreground">{t('normalize.explicitMappings.title')}</div>
                            </div>
                        </div>
                        <div className="text-xs font-semibold text-foreground">{t('normalize.import.coverage')}</div>
                        <div className="grid grid-cols-2 gap-2 text-center text-xs sm:grid-cols-3">
                            <div className="rounded-md bg-card p-2">
                                <div className="font-mono font-semibold text-foreground">{importPreview.coverage.inputCount}</div>
                                <div className="text-muted-foreground">{t('normalize.import.input')}</div>
                            </div>
                            <div className="rounded-md bg-card p-2">
                                <div className="font-mono font-semibold text-foreground">{importPreview.coverage.canonicalCount}</div>
                                <div className="text-muted-foreground">{t('normalize.import.canonical')}</div>
                            </div>
                            <div className="rounded-md bg-card p-2">
                                <div className="font-mono font-semibold text-foreground">{importPreview.coverage.mergedVariantCount}</div>
                                <div className="text-muted-foreground">{t('normalize.import.merged')}</div>
                            </div>
                            <div className="rounded-md bg-card p-2">
                                <div className="font-mono font-semibold text-foreground">{importPreview.coverage.highRiskCount}</div>
                                <div className="text-muted-foreground">{t('normalize.import.risk')}</div>
                            </div>
                            <div className="rounded-md bg-card p-2">
                                <div className="font-mono font-semibold text-foreground">{importPreview.coverage.validationIssueCount}</div>
                                <div className="text-muted-foreground">{t('normalize.import.invalid')}</div>
                            </div>
                            <div className="rounded-md bg-card p-2">
                                <div className="font-mono font-semibold text-foreground">{importPreview.coverage.unresolvedCount}</div>
                                <div className="text-muted-foreground">{t('normalize.import.unresolved')}</div>
                            </div>
                        </div>
                        <div className="flex gap-2">
                            <Button
                                type="button"
                                size="sm"
                                className="flex-1 rounded-xl"
                                onClick={applyImport}
                            >
                                <Check className="mr-1 size-3.5" />
                                {t('normalize.import.apply')}
                            </Button>
                            <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                className="flex-1 rounded-xl"
                                onClick={() => { setImportPreview(null); if (importFileRef.current) importFileRef.current.value = ''; }}
                            >
                                {t('normalize.import.cancel')}
                            </Button>
                        </div>
                    </div>
                )}
            </div>

            <div className="h-px bg-border/50" />

            {/* 覆盖分析 */}
            <div className="space-y-3 rounded-lg border-border/30 bg-card p-3 sm:p-4 shadow-sm">
                <div className="flex items-center justify-between gap-2">
                    <div className="space-y-0.5">
                        <div className="text-sm font-semibold text-card-foreground flex items-center gap-2">
                            <Search className="size-4" />
                            {t('normalize.analysis.title')}
                        </div>
                        <p className="text-xs leading-5 text-muted-foreground">{t('normalize.analysis.description')}</p>
                    </div>
                    <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="shrink-0 rounded-xl"
                        onClick={handleAnalyze}
                        disabled={analyzing || channelsLoading || channelCount === 0}
                    >
                        {analyzing || channelsLoading ? <Loader2 className="size-3.5 animate-spin" /> : <Search className="size-3.5" />}
                        {analyzing ? t('normalize.analysis.analyzing') : t('normalize.analysis.button')}
                    </Button>
                </div>

                {channelCount > 0 && (
                    <p className="text-xs text-muted-foreground">
                        {t('normalize.analysis.channelCount', { count: channelCount })}
                    </p>
                )}

                {analysis && (
                    <div className="space-y-4 pt-1">
                        {/* 已归并簇 */}
                        <div className="space-y-2">
                            <div className="text-xs font-semibold text-emerald-600 dark:text-emerald-400 flex items-center gap-1.5">
                                <CheckCircle2 className="size-3.5" />
                                {t('normalize.analysis.merged', { count: analysis.merged.length })}
                            </div>
                            {analysis.merged.length === 0 ? (
                                <p className="text-xs text-muted-foreground pl-5">{t('normalize.analysis.mergedEmpty')}</p>
                            ) : (
                                <div className="max-h-48 space-y-1.5 overflow-y-auto pl-5">
                                    {analysis.merged.slice(0, 50).map((cluster) => (
                                        <div key={cluster.canonical} className="rounded-md border border-border/30 bg-card px-2.5 py-1.5">
                                            <div className="text-xs font-mono font-medium text-foreground break-all">{cluster.canonical}</div>
                                            <div className="mt-0.5 flex flex-wrap gap-1">
                                                {cluster.originals.map((o) => (
                                                    <span key={o} className="text-[10px] font-mono text-muted-foreground break-all">{o}</span>
                                                ))}
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            )}
                        </div>

                        {/* 疑似漏归并 */}
                        <div className="space-y-2">
                            <div className="text-xs font-semibold text-amber-600 dark:text-amber-400 flex items-center gap-1.5">
                                <AlertCircle className="size-3.5" />
                                {t('normalize.analysis.suspected', { count: analysis.suspected.length })}
                            </div>
                            {analysis.suspected.length === 0 ? (
                                <p className="text-xs text-muted-foreground pl-5">{t('normalize.analysis.suspectedEmpty')}</p>
                            ) : (
                                <div className="max-h-64 space-y-1.5 overflow-y-auto pl-5">
                                    {analysis.suspected.slice(0, 50).map((cluster) => (
                                        <div key={cluster.base} className="rounded-md border border-amber-500/20 bg-amber-500/5 px-2.5 py-1.5">
                                            <div className="text-xs font-mono font-medium text-foreground break-all">
                                                {cluster.base}
                                                <span className="ml-1.5 text-muted-foreground">→</span>
                                            </div>
                                            <div className="mt-0.5 space-y-0.5">
                                                {cluster.variants.map((v) => (
                                                    <div key={v.original} className="flex items-center gap-1.5 text-[10px]">
                                                        <span className="font-mono text-muted-foreground break-all">{v.original}</span>
                                                        <span className="shrink-0 text-amber-600 dark:text-amber-400 font-mono">[{v.diff}]</span>
                                                        <button
                                                            type="button"
                                                            className={cn(
                                                                'shrink-0 rounded px-1 py-0.5 text-[9px] font-medium transition-colors',
                                                                v.diff.startsWith('-')
                                                                    ? 'text-amber-600 hover:bg-amber-500/15 dark:text-amber-400'
                                                                    : 'text-blue-600 hover:bg-blue-500/15 dark:text-blue-400'
                                                            )}
                                                            onClick={() => v.diff.startsWith('-') ? addSuffixFromAnalysis(v.diff) : addPrefixFromAnalysis(v.diff)}
                                                        >
                                                            +{v.diff.startsWith('-') ? t('normalize.analysis.addSuffix') : t('normalize.analysis.addPrefix')}
                                                        </button>
                                                    </div>
                                                ))}
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            )}
                            <p className="text-[10px] text-muted-foreground/70 pl-5">{t('normalize.analysis.disclaimer')}</p>
                        </div>
                    </div>
                )}
            </div>
        </div>
    );
}
