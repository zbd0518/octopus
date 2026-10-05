import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync } from 'node:fs';
import { createTranslator } from 'next-intl';

import { CRON_PRESETS, scheduledTestScopeAccountId, validateCronExpr } from './scheduled-test.ts';

test('validateCronExpr accepts the narrowed grammar shapes', () => {
    const accepted = [
        '*/30 * * * *',
        '*/1 * * * *',
        '*/59 * * * *',
        '* * * * *',
        '0 * * * *',
        '59 * * * *',
        '15 * * * *',
        '0 9,21 * * *',
        '30 0,6,12,18 * * *',
        '  */5   9,21   *   *   *  ', // loose whitespace
    ];
    for (const expr of accepted) {
        assert.equal(validateCronExpr(expr), true, `expected accepted: ${expr}`);
    }
    for (const preset of CRON_PRESETS) {
        assert.equal(validateCronExpr(preset), true, `preset must be valid: ${preset}`);
    }
});

test('validateCronExpr rejects unsupported shapes', () => {
    const rejected = [
        '',
        '   ',
        '* * * *',
        '* * * * * *',
        '*/0 * * * *',
        '*/60 * * * *',
        '60 * * * *',
        '1-30 * * * *',
        '1,31 * * * *',
        '*/5 24 * * *',
        '*/5 1-3 * * *',
        '*/5 * 1 * *',
        '*/5 * * 2 *',
        '*/5 * * * 1',
        '*/5 */2 * * *',
        '@daily',
    ];
    for (const expr of rejected) {
        assert.equal(validateCronExpr(expr), false, `expected rejected: ${expr}`);
    }
});

test('scheduled result labels interpolate duration and error in all locales', () => {
    for (const locale of ['en', 'zh_hans', 'zh_hant']) {
        const messages = JSON.parse(readFileSync(new URL(`../../../../public/locale/${locale}.json`, import.meta.url), 'utf8'));
        const t = createTranslator({ locale: locale === 'en' ? 'en' : locale === 'zh_hans' ? 'zh-Hans' : 'zh-Hant', messages, namespace: 'pool' });
        const success = t('testSuccess', { latency: 123 });
        const failure = t('testFailed', { error: 'upstream-unavailable' });
        assert.ok(success.includes('123'));
        assert.ok(failure.includes('upstream-unavailable'));
        assert.ok(!success.includes('{latency}'));
        assert.ok(!failure.includes('{error}'));
    }
    const source = readFileSync(new URL('./ScheduledTestsCard.tsx', import.meta.url), 'utf8');
    assert.match(source, /t\('testSuccess',\s*\{\s*latency:\s*result\.duration_ms\s*\}\)/);
    assert.match(source, /t\('testFailed',\s*\{\s*error:\s*result\.detail\s*\}\)/);
});

test('scheduledTestScopeAccountId normalizes the scope value', () => {
    assert.equal(scheduledTestScopeAccountId(null), null);
    assert.equal(scheduledTestScopeAccountId(undefined), null);
    assert.equal(scheduledTestScopeAccountId(0), null);
    assert.equal(scheduledTestScopeAccountId(7), 7);
});
