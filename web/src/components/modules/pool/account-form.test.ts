import assert from 'node:assert/strict';
import test from 'node:test';

import {
    MAX_HEADER_ROWS,
    SELECT_NONE_SENTINEL,
    applyHeaderOverridesToExtra,
    dateTimeLocalToUnixSeconds,
    fromSelectSentinel,
    headerRowsToRecord,
    parseIntegerDraft,
    parseJsonObject,
    splitModels,
    toSelectSentinel,
    unixSecondsToDateTimeLocal,
    validateHeaderRows,
    type HeaderRow,
} from './account-form.ts';

// 固定历元：2026-01-01T00:00:00Z = 1767225600
const EPOCH_2026 = 1767225600;

// --- datetime-local <-> unix 秒（时区对称） ---

test('unixSecondsToDateTimeLocal renders wall clock in the given zone', () => {
    assert.equal(unixSecondsToDateTimeLocal(EPOCH_2026, 'UTC'), '2026-01-01T00:00');
    assert.equal(unixSecondsToDateTimeLocal(EPOCH_2026, 'Asia/Shanghai'), '2026-01-01T08:00');
    assert.equal(unixSecondsToDateTimeLocal(EPOCH_2026, 'America/New_York'), '2025-12-31T19:00');
});

test('unixSecondsToDateTimeLocal treats non-positive and invalid input as empty', () => {
    assert.equal(unixSecondsToDateTimeLocal(0, 'UTC'), '');
    assert.equal(unixSecondsToDateTimeLocal(-5, 'UTC'), '');
    assert.equal(unixSecondsToDateTimeLocal(Number.NaN, 'UTC'), '');
    assert.equal(unixSecondsToDateTimeLocal(EPOCH_2026, 'Not/AZone'), '');
});

test('dateTimeLocalToUnixSeconds parses wall clock in the given zone', () => {
    // 同一墙上时钟在不同时区指向不同瞬时
    assert.equal(dateTimeLocalToUnixSeconds('2026-01-01T00:00', 'UTC'), EPOCH_2026);
    assert.equal(dateTimeLocalToUnixSeconds('2026-01-01T08:00', 'Asia/Shanghai'), EPOCH_2026);
    assert.equal(dateTimeLocalToUnixSeconds('2025-12-31T19:00', 'America/New_York'), EPOCH_2026);
});

test('dateTimeLocalToUnixSeconds distinguishes EST from EDT offsets', () => {
    // 冬季 EST = UTC-5，夏季 EDT = UTC-4
    assert.equal(dateTimeLocalToUnixSeconds('2026-01-01T12:00', 'America/New_York'), EPOCH_2026 + 17 * 3600);
    assert.equal(dateTimeLocalToUnixSeconds('2026-07-01T12:00', 'America/New_York'), EPOCH_2026 + 181 * 86400 + 16 * 3600);
});

test('dateTimeLocalToUnixSeconds rejects DST spring-forward gap wall times', () => {
    // 2026-03-08 02:30 在纽约不存在（02:00→03:00 跳变）：任何瞬时都无法格式化回该墙上时钟 → 视为无效
    assert.equal(dateTimeLocalToUnixSeconds('2026-03-08T02:30', 'America/New_York'), null);
});

test('dateTimeLocalToUnixSeconds resolves ambiguous fall-back time to the first occurrence', () => {
    // 2026-11-01 01:30 在纽约出现两次，取偏移较小的首次（EDT = 05:30Z）
    assert.equal(dateTimeLocalToUnixSeconds('2026-11-01T01:30', 'America/New_York'), 1793511000);
});

test('dateTimeLocalToUnixSeconds round-trips through unixSecondsToDateTimeLocal', () => {
    const zones = ['UTC', 'Asia/Shanghai', 'America/New_York'];
    const values = ['2031-05-05T05:05', '2026-03-08T01:30', '2026-03-08T03:30', '2026-11-01T01:30', '2026-11-01T05:30'];
    for (const zone of zones) {
        for (const value of values) {
            const seconds = dateTimeLocalToUnixSeconds(value, zone);
            assert.ok(seconds !== null, `${zone} ${value} should parse`);
            assert.equal(unixSecondsToDateTimeLocal(seconds, zone), value, `${zone} ${value} should round-trip`);
        }
    }
});

test('dateTimeLocalToUnixSeconds returns 0 for empty and null for invalid values', () => {
    assert.equal(dateTimeLocalToUnixSeconds('', 'UTC'), 0);
    assert.equal(dateTimeLocalToUnixSeconds('   ', 'UTC'), 0);
    assert.equal(dateTimeLocalToUnixSeconds(null, 'UTC'), null);
    assert.equal(dateTimeLocalToUnixSeconds(undefined, 'UTC'), null);
    assert.equal(dateTimeLocalToUnixSeconds('abc', 'UTC'), null);
    assert.equal(dateTimeLocalToUnixSeconds('2026-01-01', 'UTC'), null);
    assert.equal(dateTimeLocalToUnixSeconds('2026-02-30T10:00', 'UTC'), null); // 不存在的日期
    assert.equal(dateTimeLocalToUnixSeconds('2026-13-01T00:00', 'UTC'), null); // 月份越界
    assert.equal(dateTimeLocalToUnixSeconds('2026-01-01T25:00', 'UTC'), null); // 小时越界
    assert.equal(dateTimeLocalToUnixSeconds('2026-01-01T10:00', 'Not/AZone'), null); // 非法时区
});

// --- 整数草稿 ---

test('parseIntegerDraft accepts integers including negatives and rejects non-integers', () => {
    assert.deepEqual(parseIntegerDraft('5'), { raw: '5', value: 5, valid: true });
    assert.deepEqual(parseIntegerDraft('-3'), { raw: '-3', value: -3, valid: true });
    assert.deepEqual(parseIntegerDraft(' 7 '), { raw: ' 7 ', value: 7, valid: true });
    assert.deepEqual(parseIntegerDraft('1.5'), { raw: '1.5', value: 0, valid: false });
    assert.deepEqual(parseIntegerDraft('abc'), { raw: 'abc', value: 0, valid: false });
    assert.deepEqual(parseIntegerDraft('1e3'), { raw: '1e3', value: 0, valid: false });
    assert.deepEqual(parseIntegerDraft('99999999999999999999'), { raw: '99999999999999999999', value: 0, valid: false });
});

test('parseIntegerDraft treats blank input as the inherit fallback', () => {
    assert.deepEqual(parseIntegerDraft(''), { raw: '', value: 0, valid: true });
    assert.deepEqual(parseIntegerDraft('  '), { raw: '  ', value: 0, valid: true });
    assert.deepEqual(parseIntegerDraft('', 4), { raw: '', value: 4, valid: true });
});

// --- 请求头草稿 ---

test('validateHeaderRows flags case-insensitive duplicates', () => {
    const rows: HeaderRow[] = [
        { key: 'X-Trace', value: '1' },
        { key: 'x-trace', value: '2' },
    ];
    const result = validateHeaderRows(rows);
    assert.deepEqual(result.duplicates, [0, 1]);
    assert.equal(result.valid, false);
});

test('validateHeaderRows flags invalid names and newline injection', () => {
    const result = validateHeaderRows([
        { key: '', value: 'orphan' },
        { key: 'has space', value: '1' },
        { key: 'ok-header', value: 'line1\nline2' },
        { key: 'also-ok', value: 'no\r\nCRLF' },
    ]);
    assert.deepEqual(result.invalidKeys, [0, 1]);
    assert.deepEqual(result.invalidValues, [2, 3]);
    assert.equal(result.valid, false);
});

test('validateHeaderRows keeps blank placeholder rows and accepts token names', () => {
    const result = validateHeaderRows([
        { key: 'X-A', value: '1' },
        { key: '', value: '' },
        { key: 'x.b-c~d', value: '2' },
    ]);
    assert.deepEqual(result.invalidKeys, []);
    assert.deepEqual(result.duplicates, []);
    assert.deepEqual(result.invalidValues, []);
    assert.equal(result.valid, true);
});

test('headerRowsToRecord drops blank rows and trims keys without touching values', () => {
    const record = headerRowsToRecord([
        { key: '  X-A  ', value: ' keep ' },
        { key: '', value: '' },
        { key: 'x-b', value: '2' },
    ]);
    assert.deepEqual(record, { 'X-A': ' keep ', 'x-b': '2' });
});

test('applyHeaderOverridesToExtra merges rows only when eligible and preserves unknown fields', () => {
    const base = { project_id: 'p1', refresh_failure_count: 2 };

    const merged = applyHeaderOverridesToExtra(base, true, [{ key: 'X-Custom', value: 'v' }], true);
    assert.deepEqual(merged, { project_id: 'p1', refresh_failure_count: 2, header_overrides_enabled: true, header_overrides: { 'X-Custom': 'v' } });

    // 无资格：原样保留（连 header 字段一起）
    const untouched = applyHeaderOverridesToExtra(
        { ...base, header_overrides: { a: 'b' }, header_overrides_enabled: true },
        false,
        [{ key: 'X-Custom', value: 'v' }],
        true,
    );
    assert.deepEqual(untouched, { project_id: 'p1', refresh_failure_count: 2, header_overrides: { a: 'b' }, header_overrides_enabled: true });

    // 空行 + 未启用：不落 header_overrides 键
    const disabled = applyHeaderOverridesToExtra(base, true, [{ key: '', value: '' }], false);
    assert.deepEqual(disabled, { project_id: 'p1', refresh_failure_count: 2, header_overrides_enabled: false });
});

test('MAX_HEADER_ROWS stays at 20', () => {
    assert.equal(MAX_HEADER_ROWS, 20);
});

// --- extra JSON 安全解析 ---

test('parseJsonObject accepts empty and object payloads, rejects non-objects', () => {
    assert.deepEqual(parseJsonObject(''), { ok: true, value: {} });
    assert.deepEqual(parseJsonObject('  '), { ok: true, value: {} });
    assert.deepEqual(parseJsonObject(undefined), { ok: true, value: {} });
    assert.deepEqual(parseJsonObject('{"a":1}'), { ok: true, value: { a: 1 } });
    assert.equal(parseJsonObject('[]').ok, false);
    assert.equal(parseJsonObject('"str"').ok, false);
    assert.equal(parseJsonObject('null').ok, false);
    assert.equal(parseJsonObject('{bad json').ok, false);
});

// --- Radix Select 空值哨兵 ---

test('select sentinel converts empty string both ways and passes normal values through', () => {
    assert.equal(toSelectSentinel(''), SELECT_NONE_SENTINEL);
    assert.equal(fromSelectSentinel(SELECT_NONE_SENTINEL), '');
    assert.equal(toSelectSentinel('code_assist'), 'code_assist');
    assert.equal(fromSelectSentinel('code_assist'), 'code_assist');
    assert.notEqual(SELECT_NONE_SENTINEL, '');
});

// --- 模型预览 ---

test('splitModels trims entries and drops empties', () => {
    assert.deepEqual(splitModels('a, b,,c '), ['a', 'b', 'c']);
    assert.deepEqual(splitModels(''), []);
});

// --- B4-#13 backup proxy extra merging ---

test('applyBackupProxyToExtra writes and clears the backup proxy id', async () => {
    const { applyBackupProxyToExtra } = await import('./account-form.ts');

    const withBackup = applyBackupProxyToExtra({ existing: 'keep-me' }, 7);
    assert.deepEqual(withBackup, { existing: 'keep-me', backup_proxy_config_id: 7 });

    const cleared = applyBackupProxyToExtra({ existing: 'keep-me', backup_proxy_config_id: 7 }, null);
    assert.deepEqual(cleared, { existing: 'keep-me' });

    const zeroIsNone = applyBackupProxyToExtra({ backup_proxy_config_id: 7 }, 0);
    assert.deepEqual(zeroIsNone, {});
});
