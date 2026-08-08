import assert from 'node:assert/strict';
import test from 'node:test';

import { endpointTypeLabelKey, normalizeEndpointType } from './utils.ts';

test("endpointTypeLabelKey maps '*' to the all label key", () => {
    assert.equal(endpointTypeLabelKey('*'), 'form.endpointType.options.all');
    assert.equal(endpointTypeLabelKey(' * '), 'form.endpointType.options.all');
});

test('endpointTypeLabelKey maps known endpoint types to their label keys', () => {
    assert.equal(endpointTypeLabelKey('chat'), 'form.endpointType.options.chat');
    assert.equal(endpointTypeLabelKey('embeddings'), 'form.endpointType.options.embeddings');
    assert.equal(endpointTypeLabelKey('search'), 'form.endpointType.options.search');
});

test('normalizeEndpointType keeps a literal * so the log label can fall back to all', () => {
    assert.equal(normalizeEndpointType('*'), '*');
});