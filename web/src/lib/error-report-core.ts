/**
 * 错误上报去重纯逻辑（无任何依赖，便于单元测试）。
 *
 * 相同 message + stack 在 DEDUP_WINDOW_MS 内只上报一次，避免循环错误刷屏。
 */

export const DEDUP_WINDOW_MS = 30_000;

export class ErrorReportDedupe {
    private lastReport: { key: string; at: number } | null = null;

    /**
     * 返回 true 表示该错误在窗口内已上报过，应跳过。
     */
    isDuplicate(message: string, stack?: string): boolean {
        const key = `${message}|${(stack ?? '').slice(0, 200)}`;
        const now = Date.now();
        if (this.lastReport && this.lastReport.key === key && now - this.lastReport.at < DEDUP_WINDOW_MS) {
            return true;
        }
        this.lastReport = { key, at: now };
        return false;
    }

    reset(): void {
        this.lastReport = null;
    }
}
