// Scheduled test plan pure helpers (B4-#11): narrowed cron grammar shared by
// the plan card UI. Mirrors the backend parser in
// internal/poolscheduledtest/cron.go so invalid shapes are rejected before
// they ever reach the API. No runtime imports (unit tests load this file via
// --experimental-strip-types).

export const CRON_PRESETS = ['*/30 * * * *', '*/15 * * * *', '0 * * * *', '0 9,21 * * *'] as const;

const STEP_MINUTE_RE = /^\*\/(\d+)$/;

function parseMinuteField(field: string): boolean {
    if (field === '*') return true;
    const step = STEP_MINUTE_RE.exec(field);
    if (step) {
        const n = Number(step[1]);
        return Number.isInteger(n) && n >= 1 && n <= 59;
    }
    if (!/^\d+$/.test(field)) return false;
    const v = Number(field);
    return v >= 0 && v <= 59;
}

function parseHourField(field: string): boolean {
    if (field === '*') return true;
    if (field === '') return false;
    return field.split(',').every((part) => {
        if (!/^\d+$/.test(part.trim())) return false;
        const v = Number(part.trim());
        return v >= 0 && v <= 23;
    });
}

/**
 * Validates a cron expression against the narrowed grammar accepted by the
 * backend: minute = "*" | step (1-59) | fixed 0-59; hour = "*" | comma list;
 * day-of-month / month / day-of-week must be "*".
 */
export function validateCronExpr(expr: string): boolean {
    const fields = expr.trim().split(/\s+/).filter(Boolean);
    if (fields.length !== 5) return false;
    if (!parseMinuteField(fields[0])) return false;
    if (!parseHourField(fields[1])) return false;
    return fields[2] === '*' && fields[3] === '*' && fields[4] === '*';
}

/** Human-readable scope label payload: null = whole pool. */
export function scheduledTestScopeAccountId(accountId: number | null | undefined): number | null {
    if (accountId === null || accountId === undefined) return null;
    return accountId > 0 ? accountId : null;
}
