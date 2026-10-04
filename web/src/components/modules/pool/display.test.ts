import assert from 'node:assert/strict';
import test from 'node:test';
import { accountState, modelNames, parseQuota, parseAccountImport, selectedAccountIds } from './display.ts';

const active = { status: 'active', schedulable: true, type: 'apikey', token_expires_at: 0, expires_at: 0, auto_pause_on_expired: false, temp_unsched_until: 0, rate_limit_reset_at: 0, overload_until: 0 };

test('account status reflects lifecycle timestamps and scheduling availability', () => {
  assert.equal(accountState(active, 1000), 'active');
  assert.equal(accountState({ ...active, schedulable: false }, 1000), 'unavailable');
  assert.equal(accountState({ ...active, type: 'oauth', token_expires_at: 1060 }, 1000), 'tokenExpired');
  assert.equal(accountState({ ...active, expires_at: 900, auto_pause_on_expired: true }, 1000), 'expired');
  assert.equal(accountState({ ...active, expires_at: 900 }, 1000), 'active');
  assert.equal(accountState({ ...active, temp_unsched_until: 1100 }, 1000), 'temporary');
  assert.equal(accountState({ ...active, rate_limit_reset_at: 1100 }, 1000), 'cooling');
  assert.equal(accountState({ ...active, overload_until: 1100 }, 1100), 'active');
});

test('model previews trim, deduplicate and discard empty names', () => {
  assert.deepEqual(modelNames(' a, ,b,a, b '), ['a', 'b']);
  assert.deepEqual(modelNames(''), []);
});

test('quota parsing rejects malformed payloads and clamps progress', () => {
  for (const value of ['', 'null', '[]', 'true', '{}', '{"used":-1}', '{"used":"5"}']) assert.equal(parseQuota(value), null);
  assert.deepEqual(parseQuota('{"used":20,"total":10}'), { used: 20, total: 10, percent: 100 });
  assert.deepEqual(parseQuota('{"used":5}'), { used: 5, total: 0, percent: 0 });
});

test('account import requires a nonempty array of records', () => {
  for (const raw of ['invalid', '{}', '[]', '[null]', '[1]', '[[]]']) assert.deepEqual(parseAccountImport(raw), { valid: false, count: 0 });
  assert.deepEqual(parseAccountImport('[{"name":"one"},{"name":"two"}]'), { valid: true, count: 2 });
});

test('batch selections exclude accounts removed by a refetch', () => {
  assert.deepEqual(selectedAccountIds(new Set([1, 2, 99]), [{ id: 2 }, { id: 3 }]), [2]);
});
