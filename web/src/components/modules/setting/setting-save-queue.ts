/**
 * 按设置项 key 串行化保存、失败时按字段回滚的纯逻辑队列。
 * 不可从代码直接看出的并发约束：
 * - 同 key 严格按提交顺序执行；不同 key 互不阻塞。
 * - confirmedSnapshot 仅在链空闲（上一链已结算释放）时被采纳为回滚基准；链繁忙
 *   时新提交的快照含未落库的乐观变更，不得覆盖链上基准。
 * - 回滚仅当本次仍是该 key 最新提交时执行；链尾结算后释放该 key 全部状态。
 * - save() 返回的 Promise 永不 reject。
 */

export interface SettingSaveRequest<T> {
    key: string;
    /** 乐观更新前捕获的当前值；链空闲时作为回滚基准，繁忙时忽略。 */
    confirmedSnapshot: T;
    /** 本次要保存的值（调用方应已完成乐观更新）。 */
    next: T;
    /** 通过该字段自己的 store setter 恢复 confirmed；至多调用一次。 */
    rollback: (confirmed: T) => void;
}

/** 单次保存结果；ok 为 false 表示失败（是否回滚已按上述规则决定）。 */
export type SettingSaveOutcome = { ok: true } | { ok: false };

export interface SettingSaveQueue {
    save<T>(request: SettingSaveRequest<T>, perform: (next: T) => Promise<void>): Promise<SettingSaveOutcome>;
}

export function createSettingSaveQueue(): SettingSaveQueue {
    const tails = new Map<string, Promise<void>>();
    const confirmedValues = new Map<string, unknown>();
    const latestOpIds = new Map<string, number>();
    let opCounter = 0;

    return {
        save<T>(request: SettingSaveRequest<T>, perform: (next: T) => Promise<void>): Promise<SettingSaveOutcome> {
            const opId = ++opCounter;
            latestOpIds.set(request.key, opId);
            if (!tails.has(request.key)) {
                confirmedValues.set(request.key, request.confirmedSnapshot);
            }

            const releaseIfDrained = (): void => {
                // 有更新提交在途时不得释放：其失败回滚仍需链上基准。
                if (latestOpIds.get(request.key) !== opId) return;
                tails.delete(request.key);
                confirmedValues.delete(request.key);
                latestOpIds.delete(request.key);
            };

            const previous = tails.get(request.key) ?? Promise.resolve();
            const settled = previous
                .then(() => perform(request.next))
                .then((): SettingSaveOutcome => {
                    confirmedValues.set(request.key, request.next);
                    return { ok: true };
                })
                .catch((): SettingSaveOutcome => {
                    if (latestOpIds.get(request.key) === opId) {
                        request.rollback(confirmedValues.get(request.key) as T);
                    }
                    return { ok: false };
                });

            tails.set(request.key, settled.then(releaseIfDrained, releaseIfDrained));
            return settled;
        },
    };
}
