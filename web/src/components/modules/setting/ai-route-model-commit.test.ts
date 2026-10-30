import assert from 'node:assert/strict';
import test from 'node:test';

import { createAIRouteModelCommit } from './ai-route-model-commit.ts';
import { parseServicesJSON, serializeServicesRows } from './ai-route-services.ts';

test('model selection saves the selected value without requiring blur', () => {
    const saved: string[] = [];
    const commit = createAIRouteModelCommit((value) => saved.push(value));

    commit.select('new-model');

    assert.deepEqual(saved, ['new-model']);
});

test('selection followed by blur does not issue a duplicate save', () => {
    const saved: string[] = [];
    const commit = createAIRouteModelCommit((value) => saved.push(value));

    commit.select('new-model');
    commit.blur('new-model');

    assert.deepEqual(saved, ['new-model']);
});

test('manual input still saves on blur and can save a later value', () => {
    const saved: string[] = [];
    const commit = createAIRouteModelCommit((value) => saved.push(value));

    commit.change();
    commit.blur('typed-model');
    commit.change();
    commit.blur('edited-model');

    assert.deepEqual(saved, ['typed-model', 'edited-model']);
});

test('editing after a selection allows the new manual value to save', () => {
    const saved: string[] = [];
    const commit = createAIRouteModelCommit((value) => saved.push(value));

    commit.select('selected-model');
    commit.change();
    commit.blur('replacement-model');

    assert.deepEqual(saved, ['selected-model', 'replacement-model']);
});

test('service-pool rows keep their explicit serialization without model commits', () => {
    const rows = parseServicesJSON('[{"name":"pool","base_url":"https://service.invalid","api_key":"test-only","model":"old-model"}]');
    assert.equal(rows.invalid, false);
    assert.ok(rows.rows);
    const changed = rows.rows.map((row) => ({ ...row, model: 'selected-model' }));

    assert.equal(serializeServicesRows(rows.rows), serializeServicesRows(rows.rows));
    assert.match(serializeServicesRows(changed), /"model":"selected-model"/);
    assert.doesNotMatch(serializeServicesRows(rows.rows), /selected-model/);
});
