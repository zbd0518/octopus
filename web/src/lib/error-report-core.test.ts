import { describe, it, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { ErrorReportDedupe } from './error-report-core.ts';

describe('ErrorReportDedupe', () => {
    let dedupe: ErrorReportDedupe;

    beforeEach(() => {
        dedupe = new ErrorReportDedupe();
    });

    it('first report is not a duplicate', () => {
        assert.equal(dedupe.isDuplicate('TypeError: boom', 'stack-1'), false);
    });

    it('same message+stack within window is a duplicate', () => {
        dedupe.isDuplicate('TypeError: boom', 'stack-1');
        assert.equal(dedupe.isDuplicate('TypeError: boom', 'stack-1'), true);
    });

    it('different message is not a duplicate', () => {
        dedupe.isDuplicate('TypeError: boom', 'stack-1');
        assert.equal(dedupe.isDuplicate('RangeError: overflow', 'stack-1'), false);
    });

    it('different stack for same message is not a duplicate', () => {
        dedupe.isDuplicate('TypeError: boom', 'stack-1');
        assert.equal(dedupe.isDuplicate('TypeError: boom', 'stack-2'), false);
    });

    it('stack longer than 200 chars is truncated for key comparison', () => {
        const long = 'x'.repeat(500);
        dedupe.isDuplicate('TypeError: boom', long);
        // 前 200 字符相同即视为重复
        assert.equal(dedupe.isDuplicate('TypeError: boom', long), true);
    });

    it('reset clears dedupe state', () => {
        dedupe.isDuplicate('TypeError: boom', 'stack-1');
        dedupe.reset();
        assert.equal(dedupe.isDuplicate('TypeError: boom', 'stack-1'), false);
    });
});
