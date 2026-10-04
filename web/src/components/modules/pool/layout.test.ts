import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const source = (name: string) => readFileSync(new URL(`./${name}`, import.meta.url), 'utf8');

test('pool detail contains responsive cards and a separately scrollable desktop table', () => {
  const detail = source('PoolDetail.tsx');
  assert.match(detail, /h-full min-h-0 overflow-y-auto overscroll-contain/);
  assert.match(detail, /sm:grid-cols-2 lg:hidden/);
  assert.match(detail, /overflow-auto[^"\n]*lg:block/);
  assert.match(detail, /sticky top-0/);
  assert.match(detail, /aria-label=\{label\}/);
  assert.match(detail, /indeterminate/);
  assert.match(detail, /selectedAccountIds\(selectedIds, accounts\)/);
});

test('pool lifecycle timers stop while their keep-alive page is inactive', () => {
  const detail = source('PoolDetail.tsx');
  assert.match(detail, /activeItem !== 'pool'/);
  assert.match(detail, /setInterval\(tick, 30_000\)/);
  assert.match(detail, /clearInterval\(timer\)/);
});

test('destructive account actions require confirmation and export warns about credentials', () => {
  const dialogs = source('PoolOperationDialogs.tsx');
  assert.match(dialogs, /<AlertDialog open=\{deleteTarget !== null\}/);
  assert.match(dialogs, /ui.deleteAccountDescription/);
  assert.match(dialogs, /ui.exportWarning/);
  assert.match(dialogs, /disabled=\{!importResult.valid \|\| importAccounts.isPending\}/);
  assert.match(dialogs, /disabled=\{!validMinutes \|\| pause.isPending\}/);
});

test('row action requests have per-account pending states and accessible buttons', () => {
  const actions = source('AccountActions.tsx');
  assert.match(actions, /useTestPoolAccount\(account.pool_id\)/);
  assert.match(actions, /aria-label=\{t\('editAccount'\)\}/);
  assert.match(actions, /aria-label=\{t\('testAccount'\)\}/);
  assert.match(actions, /DropdownMenu.Trigger asChild/);
  assert.match(actions, /platformSupportsQuota/);
});

test('account form uses one standard dialog close button and isolates its scroll body', () => {
  const form = source('AccountFormDialog.tsx');
  assert.match(form, /DialogContent showCloseButton=\{false\}/);
  assert.match(form, /max-h-\[calc\(100dvh-2rem\)\]/);
  assert.match(form, /min-h-0 flex-1[^"\n]*overflow-y-auto/);
  assert.doesNotMatch(form, /SelectItem value=""/);
  assert.match(form, /credentialsMissing/);
  assert.match(form, /ui.credentialsKeep/);
  assert.match(form, /overrideEligible && headerEnabled \? headerValidation.valid : true/);
  assert.match(form, /f.base_url === DEFAULT_BASE_URL_BY_PLATFORM\[f.platform as PoolPlatform\]/);
  assert.match(form, /fid\('extra-json'\)/);
});

test('pool translations use valid next-intl interpolation and localized labels', () => {
  for (const locale of ['en', 'zh_hans', 'zh_hant']) {
    const messages = JSON.parse(readFileSync(new URL(`../../../../public/locale/${locale}.json`, import.meta.url), 'utf8'));
    assert.equal(JSON.stringify(messages.pool).includes('{{'), false);
    assert.ok(messages.pool.ui.strategyLabels.round_robin);
    assert.ok(messages.pool.ui.deleteAccount);
    assert.ok(messages.pool.ui.exportWarning);
  }
});
