import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { channelTestOutcome } from './test-results.ts';
import { NODE_PROBE_CODES, nodeProbeCode } from '../../../api/endpoints/node-probe.ts';

test('channel outcomes reflect every result rather than the any-success summary flag', () => {
    assert.equal(channelTestOutcome({ passed: false, results: [] }), 'failed');
    assert.equal(channelTestOutcome({ passed: false, results: [{ passed: false }] }), 'failed');
    assert.equal(channelTestOutcome({ passed: true, results: [{ passed: true }, { passed: false }] }), 'partialSuccess');
    assert.equal(channelTestOutcome({ passed: true, results: [{ passed: true }, { passed: true }] }), 'success');
});

test('node diagnostic codes fall back safely and all supported locales explain each code', () => {
    assert.equal(nodeProbeCode('secret not a valid diagnostic'), 'upstream_failed');
    for (const locale of ['en', 'zh_hans', 'zh_hant']) {
        const messages = JSON.parse(readFileSync(new URL(`../../../../public/locale/${locale}.json`, import.meta.url), 'utf8'));
        for (const code of NODE_PROBE_CODES) {
            assert.equal(nodeProbeCode(code), code);
            assert.equal(typeof messages.node.test.codes[code], 'string', `${locale}: ${code}`);
        }
    }
});

test('channel result display is outside HTTP-only sections and rejects stale asynchronous results', () => {
    const form = readFileSync(new URL('./Form.tsx', import.meta.url), 'utf8');
    const results = readFileSync(new URL('./TestResults.tsx', import.meta.url), 'utf8');
    assert.match(form, /\(testSummary \|\| testError\) && <section/);
    assert.match(form, /<ChannelTestResults summary=\{testSummary\} isNode=\{isNode\}/);
    assert.doesNotMatch(form, /toast\.success\(data\.passed/);
    assert.match(form, /testGeneration.current !== generation/);
    assert.match(form, /\[formData.node_id, formData.type\]/);
    assert.match(results, /!isNode && summary.results.some/);
    assert.match(results, /isNode \? <NodeProbeFeedback/);
});
