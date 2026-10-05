// 号池账号表单纯函数助手：时区安全的 datetime-local 转换、整数草稿解析、
// 请求头草稿校验、extra JSON 安全解析、Radix Select 空值哨兵。
// 供 AccountFormDialog 与单测使用；本文件不允许有运行时 import（单测经
// --experimental-strip-types 直接加载，别名路径无法解析），跨包类型一律 import type。

export type PoolAccountExtraLike = Record<string, unknown>;

// ---------------------------------------------------------------------------
// datetime-local <-> unix 秒（指定时区，替代旧的 UTC 显示 + 本地解析不对称实现）
// ---------------------------------------------------------------------------

export type ZonedParts = { year: number; month: number; day: number; hour: number; minute: number };

const DATE_TIME_LOCAL_RE = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/;
const MAX_VALID_MS = 8.64e15; // ECMAScript 时间范围上限

function pad2(n: number): string {
    return String(n).padStart(2, '0');
}

/** 取某瞬时在指定时区下的年月日时分（h23，避免午夜 24 点）。时区非法时抛 RangeError。 */
function zonedParts(date: Date, timeZone: string): ZonedParts {
    const dtf = new Intl.DateTimeFormat('en-US', {
        timeZone,
        hourCycle: 'h23',
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
    });
    const parts: Partial<ZonedParts> = {};
    for (const part of dtf.formatToParts(date)) {
        if (part.type === 'literal') continue;
        parts[part.type as keyof ZonedParts] = Number(part.value);
    }
    return {
        year: parts.year ?? 1970,
        month: parts.month ?? 1,
        day: parts.day ?? 1,
        hour: parts.hour ?? 0,
        minute: parts.minute ?? 0,
    };
}

/** 指定时区在给定瞬时（截断到分钟）的 UTC 偏移毫秒数。 */
function zoneOffsetMillis(timeZone: string, instantMs: number): number {
    const truncated = instantMs - (((instantMs % 60000) + 60000) % 60000);
    const p = zonedParts(new Date(truncated), timeZone);
    return Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute) - truncated;
}

/**
 * unix 秒 → datetime-local 值（'YYYY-MM-DDTHH:mm'，按 timeZone 的墙上时钟）。
 * seconds<=0 / 非法数字 / 时区非法 → ''（空值约定，表示未设置）。
 */
export function unixSecondsToDateTimeLocal(seconds: number, timeZone: string): string {
    if (!Number.isFinite(seconds) || seconds <= 0) return '';
    const ms = seconds * 1000;
    if (!Number.isFinite(ms) || Math.abs(ms) > MAX_VALID_MS) return '';
    try {
        const p = zonedParts(new Date(ms), timeZone);
        return `${p.year}-${pad2(p.month)}-${pad2(p.day)}T${pad2(p.hour)}:${pad2(p.minute)}`;
    } catch {
        return '';
    }
}

/**
 * datetime-local 值 → unix 秒（按 timeZone 的墙上时钟解释）。
 * '' / 空白 → 0（用户清空到期时间）；无效日期或时区 → null（调用方应阻断提交）。
 * 不存在的本地时间（DST 春季跳变缺口）解析为缺口后的确定值；歧义时刻取偏移较小的首个出现。
 */
export function dateTimeLocalToUnixSeconds(value: string | null | undefined, timeZone: string): number | null {
    if (value == null) return null;
    const trimmed = value.trim();
    if (trimmed === '') return 0;
    const m = DATE_TIME_LOCAL_RE.exec(trimmed);
    if (!m) return null;
    const year = Number(m[1]);
    const month = Number(m[2]);
    const day = Number(m[3]);
    const hour = Number(m[4]);
    const minute = Number(m[5]);
    try {
        const guess = Date.UTC(year, month - 1, day, hour, minute);
        if (!Number.isFinite(guess) || Math.abs(guess) > MAX_VALID_MS) return null;
        const offsetAtGuess = zoneOffsetMillis(timeZone, guess);
        let ts = guess - offsetAtGuess;
        const offsetAtResult = zoneOffsetMillis(timeZone, ts);
        if (offsetAtResult !== offsetAtGuess) {
            // 落在 DST 边界上：用结果侧偏移再校正一次，保证墙上时钟与输入一致。
            ts = guess - offsetAtResult;
        }
        // 反向校验：能格式化回同一墙上时钟才算合法日期（过滤 2026-02-30 / 25:61 之类）。
        const check = zonedParts(new Date(ts), timeZone);
        if (
            check.year !== year ||
            check.month !== month ||
            check.day !== day ||
            check.hour !== hour ||
            check.minute !== minute
        ) {
            return null;
        }
        return Math.floor(ts / 1000);
    } catch {
        return null;
    }
}

// ---------------------------------------------------------------------------
// 整数草稿：数字输入框以字符串形态编辑，提交前统一校验/取值
// ---------------------------------------------------------------------------

export type IntegerDraft = { raw: string; value: number; valid: boolean };

const INTEGER_RE = /^[+-]?\d+$/;

/**
 * 把数字输入框的原始字符串解析为整数草稿。
 * '' / 空白 → 回落 fallback（默认 0，即“0=继承池默认”语义）；非整数字面量 → valid=false。
 * 正负号均允许：后端 priority/weight 为 *int 直传，不在此收紧业务策略。
 */
export function parseIntegerDraft(raw: string, fallback = 0): IntegerDraft {
    const trimmed = raw.trim();
    if (trimmed === '') return { raw, value: fallback, valid: true };
    if (!INTEGER_RE.test(trimmed) || !Number.isSafeInteger(Number(trimmed))) {
        return { raw, value: fallback, valid: false };
    }
    return { raw, value: Number(trimmed), valid: true };
}

// ---------------------------------------------------------------------------
// 自定义请求头草稿：逐字编辑不丢行，提交时才序列化
// ---------------------------------------------------------------------------

export type HeaderRow = { key: string; value: string };

export const MAX_HEADER_ROWS = 20;

// RFC 7230 token 字符集（HTTP 头名合法字符）。
const HEADER_NAME_RE = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;

export type HeaderRowsValidation = {
    /** 非空但非法（空 key 却有值、含非法字符）的行下标 */
    invalidKeys: number[];
    /** 与其它行大小写不敏感重复的行下标 */
    duplicates: number[];
    /** 值含 CR/LF 等换行注入风险的行下标 */
    invalidValues: number[];
    valid: boolean;
};

/** 校验请求头草稿。key/value 均按 trim 后判定；全空行（key 与 value 皆空）不算错误，序列化时丢弃。 */
export function validateHeaderRows(rows: HeaderRow[]): HeaderRowsValidation {
    const invalidKeys: number[] = [];
    const invalidValues: number[] = [];
    const seen = new Map<string, number[]>();

    rows.forEach((row, idx) => {
        const key = row.key.trim();
        const value = row.value;
        if (key === '' && value.trim() === '') return; // 整行空：视为占位行
        if (key === '' || !HEADER_NAME_RE.test(key)) invalidKeys.push(idx);
        if (/[\r\n]/.test(value)) invalidValues.push(idx);
        const normalized = key.toLowerCase();
        const bucket = seen.get(normalized);
        if (bucket) bucket.push(idx);
        else seen.set(normalized, [idx]);
    });

    const duplicates: number[] = [];
    for (const bucket of seen.values()) {
        if (bucket.length > 1) duplicates.push(...bucket);
    }

    return { invalidKeys, duplicates, invalidValues, valid: invalidKeys.length === 0 && duplicates.length === 0 && invalidValues.length === 0 };
}

/** 草稿行 → header_overrides 对象：丢弃整行空行，key trim，大小写敏感保留原样。 */
export function headerRowsToRecord(rows: HeaderRow[]): Record<string, string> {
    const record: Record<string, string> = {};
    for (const row of rows) {
        const key = row.key.trim();
        if (key === '') continue;
        record[key] = row.value;
    }
    return record;
}

/**
 * 把请求头草稿合并进 extra 对象（仅当平台/类型具备 override 资格时）。
 * 覆盖旧的 header_overrides / header_overrides_enabled，保留其余字段（含后端写入的
 * refresh_failure_count 等未知字段）；无行且未启用时不落 header_overrides 键。
 */
export function applyHeaderOverridesToExtra(
    extra: PoolAccountExtraLike,
    eligible: boolean,
    rows: HeaderRow[],
    enabled: boolean,
): PoolAccountExtraLike {
    if (!eligible) return { ...extra };
    const next: PoolAccountExtraLike = { ...extra };
    next.header_overrides_enabled = enabled;
    const record = headerRowsToRecord(rows);
    if (Object.keys(record).length > 0) {
        next.header_overrides = record;
    } else {
        delete next.header_overrides;
    }
    return next;
}

/**
 * Writes the backup-proxy selection into the extra object (B4-#13). A null or
 * non-positive backupProxyId removes the backup_proxy_config_id key (default
 * off); all other fields are preserved.
 */
export function applyBackupProxyToExtra(extra: PoolAccountExtraLike, backupProxyId: number | null): PoolAccountExtraLike {
    const next: PoolAccountExtraLike = { ...extra };
    if (backupProxyId != null && backupProxyId > 0) {
        next.backup_proxy_config_id = backupProxyId;
    } else {
        delete next.backup_proxy_config_id;
    }
    return next;
}

// ---------------------------------------------------------------------------
// extra JSON 安全解析：坏 JSON 不静默丢弃（调用方禁保存并提示）
// ---------------------------------------------------------------------------

export type ExtraParseResult = { ok: true; value: PoolAccountExtraLike } | { ok: false };

/** 解析 extra 原始字符串为对象。空串 → {}；非对象（数组/标量）或语法错误 → ok:false。 */
export function parseJsonObject(raw: string | null | undefined): ExtraParseResult {
    const trimmed = (raw ?? '').trim();
    if (trimmed === '') return { ok: true, value: {} };
    try {
        const parsed: unknown = JSON.parse(trimmed);
        if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) return { ok: false };
        return { ok: true, value: parsed as PoolAccountExtraLike };
    } catch {
        return { ok: false };
    }
}

// ---------------------------------------------------------------------------
// Radix Select 空值哨兵：<SelectItem value=""> 会抛错，用非空哨兵代替并双向转换
// ---------------------------------------------------------------------------

export const SELECT_NONE_SENTINEL = '__octopus_none__';

export function toSelectSentinel(value: string): string {
    return value === '' ? SELECT_NONE_SENTINEL : value;
}

export function fromSelectSentinel(value: string): string {
    return value === SELECT_NONE_SENTINEL ? '' : value;
}

// ---------------------------------------------------------------------------
// 模型预览
// ---------------------------------------------------------------------------

export const MODEL_PREVIEW_LIMIT = 8;

/** 拆分逗号分隔的模型串（trim + 去空），供徽标预览使用。 */
export function splitModels(raw: string): string[] {
    return raw
        .split(',')
        .map((name) => name.trim())
        .filter(Boolean);
}
