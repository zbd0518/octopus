import assert from 'node:assert/strict';
import { test } from 'node:test';

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

test('scheduledTestScopeAccountId normalizes the scope value', () => {
    assert.equal(scheduledTestScopeAccountId(null), null);
    assert.equal(scheduledTestScopeAccountId(undefined), null);
    assert.equal(scheduledTestScopeAccountId(0), null);
    assert.equal(scheduledTestScopeAccountId(7), 7);
});
