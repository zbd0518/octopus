/**
 * 模型名归一化工具
 *
 * 同一基础模型在不同渠道/路由商下会有多种命名变体，例如：
 *   kimi-k2.5
 *   @cf/moonshotai/kimi-k2.5
 *   dmxapi-kimi-k2.5
 *   moonshotai/kimi-k2.5
 *   agent/kimi-k2.5
 *   kimi-k2.5-cc
 *
 * 这些都应归一为 `kimi-k2.5`，用于「按模型聚合/去重」视图。
 *
 * 归一化只用于分组展示与去重，不会修改原始模型名。
 *
 * 规则来源分三层（按优先级）：
 *   1. 显式映射（runtimeExplicitMappings）—— 来自 DB Setting，点对点 variant→canonical，
 *      通常由离线 AI 分析产出。命中即返回，不再走前缀/后缀剥离。
 *   2. 内置默认（BUILTIN_ROUTER_PREFIXES / BUILTIN_FUNCTIONAL_SUFFIXES）—— 编译期兜底。
 *   3. 运行时覆盖（setNormalizeRules 注入）—— 来自 DB Setting，用户可在设置页增删。
 * 注入为空数组 / null 时回退到内置默认，保证未配置时行为与历史一致。
 *
 * 规则变化通过订阅机制对外广播，使依赖归一化结果的 React memo 能在规则更新时
 * 失效重算（见 useNormalizeRulesVersion）。
 */

import { useSyncExternalStore } from 'react';

export interface NormalizeExplicitMapping {
    variant: string;
    canonical: string;
}

// 内置的默认路由商 / 平台前缀（出现在模型名开头，与底层模型无关）。
const BUILTIN_ROUTER_PREFIXES = [
    'dmxapi-',
    'agent-',
    'openai-',   // openai-/anthropic- 等路由前缀（不区分大小写匹配）
    'anthropic-',
];

// 内置的默认功能性后缀（与模型本体无关，常见于渠道商二次命名）。
const BUILTIN_FUNCTIONAL_SUFFIXES = [
    '-cc',
    '-free',
    '-:free',
    '-(free)',
    '-vip',
    '-ssvip',
    '-latest',
];

// 显式变体→基准名映射的元素结构。
export interface ExplicitMapping {
    variant: string;
    canonical: string;
}

export interface NormalizeRulesInput {
    routerPrefixes?: string[] | null;
    functionalSuffixes?: string[] | null;
    explicitMappings?: ExplicitMapping[] | null;
}

// These tokens identify model families, capabilities, or execution modes.
// They must not be suggested as generic cleanup suffixes.
export const PROTECTED_MODEL_TOKENS = [
    'air', 'audio', 'base', 'chat', 'coder',
    'embed', 'embedding', 'fast', 'flash', 'image',
    'instruct', 'large', 'lite', 'max', 'medium',
    'mini', 'nano', 'non-reasoning', 'omni', 'plus',
    'pro', 'reasoning', 'rerank', 'reranker', 'search',
    'searching', 'small', 'think', 'thinking', 'tiny',
    'turbo', 'tts', 'video', 'vision', 'vl',
    'preview', 'beta', '8k', '16k', '32k', '64k',
    '128k', '192k', '256k', '512k',
] as const;

interface CompiledNormalizeRules {
    routerPrefixes: string[];
    functionalSuffixes: string[];
    explicitMappings: ExplicitMapping[];
    explicitFullByKey: Map<string, string>;
    explicitBaseByKey: Map<string, string>;
}

// 运行时覆盖规则。null 表示未配置，使用内置默认（显式映射无内置默认）。
let runtimeRouterPrefixes: string[] | null = null;
let runtimeFunctionalSuffixes: string[] | null = null;
// variant(小写) → canonical。空表示未配置显式映射。
let runtimeExplicitMappings: Map<string, string> | null = null;
let runtimeCompiledRules: CompiledNormalizeRules;
// 正则后缀编译缓存（pattern → RegExp）。
const suffixRegexCache = new Map<string, RegExp>();

// 规则版本号：每次 setNormalizeRules 改变规则时自增，供订阅者判断是否需要重算。
let rulesVersion = 0;
// 订阅者集合，规则变化时逐个通知。
const subscribers = new Set<() => void>();

function activeRouterPrefixesFor(values: string[] | null | undefined): string[] {
    return values && values.length > 0 ? values : BUILTIN_ROUTER_PREFIXES;
}

function activeFunctionalSuffixesFor(values: string[] | null | undefined): string[] {
    return values && values.length > 0 ? values : BUILTIN_FUNCTIONAL_SUFFIXES;
}

function compileNormalizeRules(rules: NormalizeRulesInput = {}): CompiledNormalizeRules {
    const explicitMappings = (rules.explicitMappings ?? [])
        .map((mapping) => ({
            variant: mapping.variant.trim(),
            canonical: mapping.canonical.trim().toLowerCase(),
        }))
        .filter((mapping) => mapping.variant !== '' && mapping.canonical !== '');
    const compiled: CompiledNormalizeRules = {
        routerPrefixes: activeRouterPrefixesFor(rules.routerPrefixes),
        functionalSuffixes: activeFunctionalSuffixesFor(rules.functionalSuffixes),
        explicitMappings,
        explicitFullByKey: new Map(),
        explicitBaseByKey: new Map(),
    };
    for (const mapping of explicitMappings) {
        const fullKey = dotDashKey(normalizeToRouterBaseWithRules(mapping.variant, compiled));
        if (fullKey && !compiled.explicitFullByKey.has(fullKey)) {
            compiled.explicitFullByKey.set(fullKey, mapping.canonical);
        }
        const baseKey = dotDashKey(normalizeToBaseWithRules(mapping.variant, compiled));
        if (baseKey && !compiled.explicitBaseByKey.has(baseKey)) {
            compiled.explicitBaseByKey.set(baseKey, mapping.canonical);
        }
    }
    return compiled;
}

runtimeCompiledRules = compileNormalizeRules();

function notifyRulesChange() {
    rulesVersion += 1;
    for (const sub of subscribers) {
        try {
            sub();
        } catch {
            /* 单个订阅者异常不影响其他订阅者 */
        }
    }
}

/**
 * 注入运行时归一化规则（来自 DB Setting）。
 * 传入空数组或 null/undefined 表示清除对应覆盖，前缀/后缀回退到内置默认。
 * 显式映射无内置默认，空表示不启用。
 *
 * 规则发生实质变化时会自增版本号并通知订阅者，使依赖该规则的 React memo
 * 失效重算（避免规则已更新但视图仍按旧规则去重的 bug）。
 */
export function setNormalizeRules(rules?: NormalizeRulesInput) {
    const nextRouterPrefixes = rules?.routerPrefixes && rules.routerPrefixes.length > 0
        ? [...rules.routerPrefixes]
        : null;
    const nextFunctionalSuffixes = rules?.functionalSuffixes && rules.functionalSuffixes.length > 0
        ? [...rules.functionalSuffixes]
        : null;
    const mappings = rules?.explicitMappings && rules.explicitMappings.length > 0
        ? rules.explicitMappings.map((mapping) => ({ ...mapping }))
        : null;
    const nextExplicitMappings = mappings
        ? new Map(mappings.map((m) => [m.variant.toLowerCase(), m.canonical]))
        : null;

    const nextCompiledRules = compileNormalizeRules({
        routerPrefixes: nextRouterPrefixes,
        functionalSuffixes: nextFunctionalSuffixes,
        explicitMappings: mappings,
    });
    const previousSignature = JSON.stringify({
        routerPrefixes: runtimeRouterPrefixes,
        functionalSuffixes: runtimeFunctionalSuffixes,
        explicitMappings: runtimeExplicitMappings ? [...runtimeExplicitMappings.entries()] : null,
    });
    const nextSignature = JSON.stringify({
        routerPrefixes: nextRouterPrefixes,
        functionalSuffixes: nextFunctionalSuffixes,
        explicitMappings: nextExplicitMappings ? [...nextExplicitMappings.entries()] : null,
    });
    if (previousSignature === nextSignature) {
        return;
    }
    runtimeRouterPrefixes = nextRouterPrefixes;
    runtimeFunctionalSuffixes = nextFunctionalSuffixes;
    runtimeExplicitMappings = nextExplicitMappings;
    runtimeCompiledRules = nextCompiledRules;
    notifyRulesChange();
}

// dotDashKey 把 - 和 . 统一为 . 并小写，用于显式映射的等价匹配与冲突检测。
// claude-opus-4-6 与 claude-opus-4.6 得到相同 key（同一模型两种命名），
// 而 gemini-2-5-pro 与 gemini-25-pro 得到不同 key（不同模型，不误并）。
export function dotDashKey(s: string): string {
    return s.toLowerCase().trim().replaceAll('-', '.');
}

function subscribeRulesVersion(callback: () => void): () => void {
    subscribers.add(callback);
    return () => {
        subscribers.delete(callback);
    };
}

function getRulesVersionSnapshot(): number {
    return rulesVersion;
}

/**
 * 订阅归一化规则版本号的 React hook。
 *
 * 规则保存在模块级可变变量里，本身在 React 体系之外。本 hook 通过
 * useSyncExternalStore 把版本号接入 React，使依赖归一化结果的 useMemo
 * 能把返回值加进依赖数组，规则一变即失效重算。
 */
export function useNormalizeRulesVersion(): number {
    return useSyncExternalStore(subscribeRulesVersion, getRulesVersionSnapshot, getRulesVersionSnapshot);
}

/**
 * 返回当前生效的规则列表（运行时覆盖 ?? 内置默认）。
 * 供设置页展示与分析工具使用。
 */
export function getActiveRouterPrefixes(): string[] {
    return runtimeRouterPrefixes ?? BUILTIN_ROUTER_PREFIXES;
}

export function getActiveFunctionalSuffixes(): string[] {
    return runtimeFunctionalSuffixes ?? BUILTIN_FUNCTIONAL_SUFFIXES;
}

/**
 * 返回当前生效的显式映射列表（无内置默认，未配置时返回空数组）。
 */
export function getActiveExplicitMappings(): ExplicitMapping[] {
    if (!runtimeExplicitMappings) return [];
    return Array.from(runtimeExplicitMappings, ([variant, canonical]) => ({ variant, canonical }));
}

/**
 * 将模型名归一化为基础模型名。
 *
 * 处理步骤：
 * 1. 取最后一个 `/` 之后的部分（剥离 `provider/`、`@cf/org/`、`agent/` 等路径前缀）。
 * 2. 剥离已知的路由商前缀（大小写不敏感）。
 * 3. 剥离已知的功能性后缀（大小写不敏感，可能多个叠加）。
 * 4. 规范化为小写，便于跨命名变体聚合。
 *
 * @example
 * normalizeModelName('kimi-k2.5')                       // 'kimi-k2.5'
 * normalizeModelName('@cf/moonshotai/kimi-k2.5')        // 'kimi-k2.5'
 * normalizeModelName('dmxapi-kimi-k2.5')                // 'kimi-k2.5'
 * normalizeModelName('moonshotai/kimi-k2.5')            // 'kimi-k2.5'
 * normalizeModelName('agent/kimi-k2.5')                 // 'kimi-k2.5'
 * normalizeModelName('kimi-k2.5-cc')                    // 'kimi-k2.5'
 * normalizeModelName('Kimi-K2.5-CC')                    // 'kimi-k2.5'
 */
export function normalizeModelName(name: string): string {
    return normalizeModelNameWithCompiled(name, runtimeCompiledRules);
}

export function normalizeModelNameWithRules(name: string, rules: NormalizeRulesInput = {}): string {
    return normalizeModelNameWithCompiled(name, compileNormalizeRules(rules));
}

function normalizeModelNameWithCompiled(name: string, rules: CompiledNormalizeRules): string {
    if (!name) return '';
    const trimmed = name.trim();

    if (rules.explicitFullByKey.size > 0 || rules.explicitBaseByKey.size > 0) {
        const full = normalizeToRouterBaseWithRules(trimmed, rules);
        const fullCanonical = rules.explicitFullByKey.get(dotDashKey(full));
        if (fullCanonical) return fullCanonical;
        const base = normalizeToBaseWithRules(trimmed, rules);
        const baseCanonical = rules.explicitBaseByKey.get(dotDashKey(base));
        if (baseCanonical) return baseCanonical;
    }

    let result = stripPathAndRouterPrefixWithRules(trimmed, rules.routerPrefixes);
    result = stripFunctionalSuffixesWithRules(result, rules.functionalSuffixes);

    // 4. 规范化为小写，便于聚合。不要让 malformed path-like inputs
    // disappear from a coverage report or the model market.
    const normalized = result.trim().toLowerCase();
    return normalized || trimmed.toLowerCase();
}

// normalizeToRouterBase 剥路径和路由前缀，但保留功能性后缀。
export function normalizeToRouterBase(name: string): string {
    return normalizeToRouterBaseWithRules(name, runtimeCompiledRules);
}

// normalizeToBase 完整规范化：剥路径 + 路由前缀 + 功能性后缀，返回小写基础名。
export function normalizeToBase(name: string): string {
    return normalizeToBaseWithRules(name, runtimeCompiledRules);
}

function normalizeToRouterBaseWithRules(name: string, rules: CompiledNormalizeRules): string {
    return stripPathAndRouterPrefixWithRules(name, rules.routerPrefixes).toLowerCase();
}

function normalizeToBaseWithRules(name: string, rules: CompiledNormalizeRules): string {
    return stripFunctionalSuffixesWithRules(normalizeToRouterBaseWithRules(name, rules), rules.functionalSuffixes).toLowerCase();
}

// stripPathAndRouterPrefix 剥离路径前缀（最后一个 / 之后）与路由前缀，返回中间名。
// 前缀匹配支持「去尾 - 变体」：用户常把 [官B]- 写成带连字符，但渠道模型名是
// [官B]claude-opus-4-6（[官B] 后无连字符），原样匹配不上；去尾 - 变体覆盖该写法差异。
function stripPathAndRouterPrefixWithRules(name: string, prefixes: string[]): string {
    let result = name;

    // 1. 剥离路径前缀：取最后一个 `/` 之后的部分。
    const slashIndex = result.lastIndexOf('/');
    if (slashIndex >= 0) {
        result = result.slice(slashIndex + 1);
    }

    // 2. 剥离已知的路由商前缀（大小写不敏感）。
    const lower = result.toLowerCase();
    for (const prefix of prefixes) {
        const p = prefix.trim();
        if (!p) continue;
        // 原样匹配（dmxapi-kimi-k2.5 → dmxapi-）。
        if (lower.startsWith(p.toLowerCase())) {
            result = result.slice(p.length);
            break;
        }
        // 去尾 - 变体（[官B]- → [官B] 匹配 [官B]claude-opus-4-6）。
        if (p.endsWith('-')) {
            const base = p.slice(0, -1);
            if (base && !endsWithAlphanumeric(base) && lower.startsWith(base.toLowerCase())) {
                result = result.slice(base.length);
                // 若原名字带 -（dmxapi-kimi → 剥 dmxapi 剩 -kimi），去掉。
                result = result.replace(/^-+/, '');
                break;
            }
        }
    }
    return result;
}

function endsWithAlphanumeric(value: string): boolean {
    const characters = Array.from(value);
    if (characters.length === 0) return false;
    return /^[\p{L}\p{N}]$/u.test(characters[characters.length - 1]);
}

// stripFunctionalSuffixes 循环剥离功能性后缀。
// 每个后缀的匹配候选：
//  1. 字面原样（如 -cc 匹配结尾 -cc）；
//  2. -: 开头 → : 形式（-:free 匹配结尾 :free）；
//  3. -( 开头 → ( 形式（-(free) 匹配结尾 (free)）；
//  4. 含正则元字符（\d \w { } * + ? |）→ 编译为正则并锚定结尾
//     （-\d{8} 匹配 -20250514 这类日期后缀）。
function stripFunctionalSuffixesWithRules(name: string, suffixes: string[]): string {
    let result = name;
    let changed = true;
    while (changed) {
        changed = false;
        const lower = result.toLowerCase();
        for (const suffix of suffixes) {
            const s = suffix.trim();
            if (!s) continue;
            const n = matchSuffixCandidate(lower, s);
            if (n > 0 && result.length > n) {
                result = result.slice(0, -n);
                changed = true;
                break;
            }
        }
    }
    return result;
}

// matchSuffixCandidate 尝试字面变体与正则变体，返回匹配的字符数。
function matchSuffixCandidate(lower: string, suffix: string): number {
    // 字面候选：原样、-: → :、-( → (。
    for (const cand of literalSuffixCandidates(suffix)) {
        if (lower.endsWith(cand) && lower.length > cand.length) {
            return cand.length;
        }
    }
    // 正则候选：仅当含正则元字符，避免把 (free) 这类字面当作分组。
    if (isRegexSuffix(suffix)) {
        for (const cand of regexSuffixCandidates(suffix)) {
            const re = getSuffixRegex(cand);
            if (!re) continue;
            const loc = re.exec(lower);
            if (loc && loc.index !== undefined) {
                return lower.length - loc.index;
            }
        }
    }
    return 0;
}

// literalSuffixCandidates 生成字面后缀候选：原样、-: 前缀 → :、-( 前缀 → (。
function literalSuffixCandidates(suffix: string): string[] {
    const cands = [suffix];
    if (suffix.startsWith('-:')) cands.push(':' + suffix.slice(2));
    if (suffix.startsWith('-(')) cands.push('(' + suffix.slice(2));
    return cands;
}

// regexSuffixCandidates 生成正则后缀候选：原样、-@ 开头 → @ 变体
// （-@\w+ → @\w+，覆盖 claude-3-haiku@20240307 这类无连字符 @ 变体）。
// 其余正则（如 -\d{8}）只按原样匹配，避免变体 \d{8} 误剥 @20240307 这类无 - 前缀的日期。
function regexSuffixCandidates(suffix: string): string[] {
    const cands = [suffix];
    if (suffix.startsWith('-@')) cands.push(suffix.slice(1));
    return cands;
}

// getSuffixRegex 返回锚定结尾的正则（带编译缓存）。
function getSuffixRegex(pattern: string): RegExp | null {
    const cached = suffixRegexCache.get(pattern);
    if (cached) return cached;
    try {
        const re = new RegExp(pattern + '$');
        suffixRegexCache.set(pattern, re);
        return re;
    } catch {
        return null;
    }
}

// isRegexSuffix 判断后缀是否为「显式正则」：含 \d \w { } * + ? | 之一。
// 排除 ( ) [ ] - 等常见字面字符，避免 -(free) 被误当分组。
function isRegexSuffix(suffix: string): boolean {
    return /[\\dw{}*+?|]/.test(suffix);
}
