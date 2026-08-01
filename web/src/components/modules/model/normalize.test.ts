import assert from 'node:assert/strict';
import test from 'node:test';

import { normalizeModelName, setNormalizeRules } from './normalize.ts';

// 每个测试前重置运行时规则，避免跨测试污染（默认无显式映射，回退内置规则）。
test.beforeEach(() => {
    setNormalizeRules({ routerPrefixes: null, functionalSuffixes: null, explicitMappings: null });
});

test('normalizeModelName keeps a plain model name as-is (lowercased)', () => {
    assert.equal(normalizeModelName('kimi-k2.5'), 'kimi-k2.5');
    assert.equal(normalizeModelName('GPT-5.2'), 'gpt-5.2');
});

test('normalizeModelName strips path prefixes such as provider/@cf/agent', () => {
    assert.equal(normalizeModelName('moonshotai/kimi-k2.5'), 'kimi-k2.5');
    assert.equal(normalizeModelName('@cf/moonshotai/kimi-k2.5'), 'kimi-k2.5');
    assert.equal(normalizeModelName('agent/kimi-k2.5'), 'kimi-k2.5');
});

test('normalizeModelName strips router prefixes such as dmxapi-/agent-', () => {
    assert.equal(normalizeModelName('dmxapi-kimi-k2.5'), 'kimi-k2.5');
    assert.equal(normalizeModelName('agent-kimi-k2.5'), 'kimi-k2.5');
});

test('normalizeModelName strips functional suffixes such as -cc/-fast/-thinking', () => {
    assert.equal(normalizeModelName('kimi-k2.5-cc'), 'kimi-k2.5');
    assert.equal(normalizeModelName('Kimi-K2.5-CC'), 'kimi-k2.5');
    assert.equal(normalizeModelName('gpt-5.2-fast'), 'gpt-5.2');
    assert.equal(normalizeModelName('claude-3.5-thinking'), 'claude-3.5');
});

test('normalizeModelName strips stacked suffixes', () => {
    assert.equal(normalizeModelName('kimi-k2.5-cc-fast'), 'kimi-k2.5');
});

test('normalizeModelName combines path + router prefix + suffix', () => {
    assert.equal(normalizeModelName('@cf/moonshotai/dmxapi-kimi-k2.5-cc'), 'kimi-k2.5');
});

test('normalizeModelName returns empty string for empty input', () => {
    assert.equal(normalizeModelName(''), '');
    assert.equal(normalizeModelName('   '), '');
});

test('normalizeModelName applies explicit mappings with full-name match', () => {
    setNormalizeRules({ explicitMappings: [{ variant: 'dmxapi-kimi-k2.5-cc', canonical: 'kimi-k2.5-routing' }] });
    assert.equal(normalizeModelName('dmxapi-kimi-k2.5-cc'), 'kimi-k2.5-routing');
    assert.equal(normalizeModelName('DMXAPI-KIMI-K2.5-CC'), 'kimi-k2.5-routing');
});

test('normalizeModelName explicit mapping matches stripped base name', () => {
    // 用户导入裸名 variant，渠道侧模型名带路由前缀/路径时也应命中。
    setNormalizeRules({ explicitMappings: [{ variant: 'kimi-k2.5-256k', canonical: 'kimi-k2.5' }] });
    assert.equal(normalizeModelName('kimi-k2.5-256k'), 'kimi-k2.5');
    assert.equal(normalizeModelName('dmxapi-kimi-k2.5-256k'), 'kimi-k2.5');
    assert.equal(normalizeModelName('moonshotai/kimi-k2.5-256k'), 'kimi-k2.5');
    assert.equal(normalizeModelName('@cf/org/kimi-k2.5-256k'), 'kimi-k2.5');
});

test('normalizeModelName explicit mapping does not over-match other models', () => {
    setNormalizeRules({ explicitMappings: [{ variant: 'kimi-k2.5-256k', canonical: 'kimi-k2.5' }] });
    // 其他模型的基础名不变体不应被误合并。
    assert.notEqual(normalizeModelName('dmxapi-gpt-4o-256k'), 'kimi-k2.5');
    // 基础名（非 256k 变体）走内置规则，保持独立。
    assert.equal(normalizeModelName('dmxapi-kimi-k2.5'), 'kimi-k2.5');
});

test('normalizeModelName resolves bidirectional conflicting mappings (first wins)', () => {
    // claude-opus-4-6 → 4.6 与 claude-opus-4.6 → 4-6 互斥，同 key 取第一条，
    // 同一模型两种命名归一到同一 canonical。
    setNormalizeRules({
        routerPrefixes: ['dmxapi-', '[官B]-'],
        explicitMappings: [
            { variant: 'claude-opus-4-6', canonical: 'claude-opus-4.6' },
            { variant: 'claude-opus-4.6', canonical: 'claude-opus-4-6' },
        ],
    });
    assert.equal(normalizeModelName('claude-opus-4-6'), 'claude-opus-4.6');
    assert.equal(normalizeModelName('claude-opus-4.6'), 'claude-opus-4.6');
    // 带渠道前缀/路径的变体也命中同一条映射。
    assert.equal(normalizeModelName('[官B]claude-opus-4-6'), 'claude-opus-4.6');
    assert.equal(normalizeModelName('anthropic/claude-opus-4.6'), 'claude-opus-4.6');
});

test('normalizeModelName strips colon and paren free suffixes', () => {
    // -:free → :free、-(free) → (free) 变体。
    setNormalizeRules({ functionalSuffixes: ['-cc', '-:free', '-(free)', '-@\\w+'] });
    assert.equal(normalizeModelName('deepseek/deepseek-r1:free'), 'deepseek-r1');
    assert.equal(normalizeModelName('deepseek/deepseek-r1(free)'), 'deepseek-r1');
    assert.equal(normalizeModelName('claude-3-haiku@20240307'), 'claude-3-haiku');
});

test('normalizeModelName strips regex date suffix', () => {
    setNormalizeRules({ functionalSuffixes: ['-\\d{8}', '-cc'] });
    assert.equal(normalizeModelName('claude-opus-4-20250514'), 'claude-opus-4');
    assert.equal(normalizeModelName('claude-3-7-sonnet-20250219'), 'claude-3-7-sonnet');
});

test('normalizeModelName strips bracket prefix written with trailing dash', () => {
    // 用户配置 [官B]-，但渠道名是 [官B]claude-...（无连字符），去尾 - 变体覆盖。
    setNormalizeRules({ routerPrefixes: ['dmxapi-', '[官B]-'] });
    assert.equal(normalizeModelName('[官B]claude-opus-4-6'), 'claude-opus-4-6');
    assert.equal(normalizeModelName('dmxapi-kimi-k2.5'), 'kimi-k2.5');
});
