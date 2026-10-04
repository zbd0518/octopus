import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import type { ChannelAttempt, RelayLog } from '../../../api/endpoints/log.ts';
import { buildLogCandidates, recordedCooldownSeconds } from './candidates.ts';

const log: RelayLog = {
    id: 1, time: 0, request_model_name: 'route', channel: 2,
    channel_name: 'Final channel', actual_model_name: 'deepseek-v4-flash',
    input_tokens: 0, output_tokens: 0, ftut: 0, use_time: 0, cost: 0, error: '',
};
const attempt: ChannelAttempt = {
    channel_id: 1, channel_name: 'First channel', model_name: 'glm-5.3',
    status: 'failed', attempt_num: 1, duration: 500,
};

test('candidates preserve recorded order and collect retries without duplicate rows', () => {
    const retries = [attempt, { ...attempt, status: 'success' as const, attempt_num: 2 }];
    const result = buildLogCandidates({ ...log, attempts: retries }, [
        { channel_id: 1, model_name: 'glm-5.3' },
        { channel_id: 3, model_name: 'kimi-k3' },
    ]);
    assert.equal(result.length, 3);
    assert.deepEqual(result[0].attempts, retries);
    assert.equal(result[0].channel_name, 'First channel');
    assert.equal(result[1].attempts.length, 0);
    assert.equal(result[2].channel_name, 'Final channel');
});

test('same model on different channels and same channel with different models remain distinct', () => {
    const result = buildLogCandidates({ ...log, channel: 0 }, [
        { channel_id: 1, model_name: 'glm-5.3' },
        { channel_id: 2, model_name: 'glm-5.3' },
        { channel_id: 1, model_name: 'glm-5.2' },
    ]);
    assert.equal(result.length, 3);
    assert.ok(result.every((candidate) => candidate.attempts.length === 0));
});

test('old logs without attempts retain the final route, while pre-routing errors are empty', () => {
    assert.equal(buildLogCandidates(log).length, 1);
    assert.deepEqual(buildLogCandidates({ ...log, channel: 0, actual_model_name: '', error: 'No group' }), []);
});

test('log detail keeps bounded panels, accessible view switching and dialog-open reset', () => {
    const source = readFileSync(new URL('./Item.tsx', import.meta.url), 'utf8');
    assert.match(source, /h-\[calc\(100dvh-2rem\)\]/);
    assert.match(source, /grid-rows-2.*md:grid-cols-2.*md:grid-rows-1/);
    assert.match(source, /aria-pressed=\{leftView === 'group'\}/);
    assert.match(source, /aria-pressed=\{leftView === 'request'\}/);
    assert.match(source, /onOpen=\{\(\) => \{\s*setLeftView\('group'\)/);
    assert.match(source, /<LogCandidatesPanel log=\{detail \?\? log\}/);
});

test('candidate group query runs only while the detail is open and does not poll', () => {
    const source = readFileSync(new URL('./LogCandidatesPanel.tsx', import.meta.url), 'utf8');
    assert.match(source, /enabled: isOpen/);
    assert.doesNotMatch(source, /refetchInterval:/);
    assert.match(source, /item.name === log.request_model_name/);
    assert.match(source, /<summary/);
});

test('cooldown seconds are parsed only from explicitly recorded skip durations', () => {
    assert.equal(recordedCooldownSeconds({ ...attempt, status: 'circuit_break', msg: 'circuit breaker tripped, remaining cooldown: 44s' }), 44);
    assert.equal(recordedCooldownSeconds({ ...attempt, status: 'skipped', msg: 'remaining cooldown: 0s' }), 0);
    assert.equal(recordedCooldownSeconds({ ...attempt, status: 'skipped', msg: 'no available key (all keys in cooldown or disabled)' }), undefined);
    assert.equal(recordedCooldownSeconds({ ...attempt, status: 'failed', msg: 'remaining cooldown: 60s' }), undefined);
    assert.equal(recordedCooldownSeconds(undefined), undefined);
});
