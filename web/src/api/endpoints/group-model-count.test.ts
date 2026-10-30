import assert from 'node:assert/strict';
import test from 'node:test';

import { normalizeAIRouteCurrentBatch, normalizeAIRouteRunningBatch } from './group-progress.ts';

test('omitted backend model_count is normalized to zero in both batch views', () => {
    const current = normalizeAIRouteCurrentBatch({ index: 1, total: 1 });
    const running = normalizeAIRouteRunningBatch({ index: 1, total: 1 });

    assert.equal(current.model_count, 0);
    assert.equal(running.model_count, 0);
});

test('numeric model_count remains numeric after normalization', () => {
    const current = normalizeAIRouteCurrentBatch({ index: 1, total: 1, model_count: 3 });
    const running = normalizeAIRouteRunningBatch({ index: 1, total: 1, model_count: 2 });

    assert.equal(current.model_count, 3);
    assert.equal(running.model_count, 2);
});
