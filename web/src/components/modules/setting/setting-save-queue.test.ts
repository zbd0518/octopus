import assert from 'node:assert/strict';
import test from 'node:test';

import { createSettingSaveQueue } from './setting-save-queue.ts';

// 覆盖范围说明：本文件只覆盖纯逻辑 helper（setting-save-queue）——
// 失败 / 成功 / 连续保存（旧失败不覆盖新乐观操作、回滚到已确认值）、
// 同 key 串行、跨 key 独立（reset 两 key 独立成败的底层机制）。
// 组件侧胶水（乐观更新与快照捕获的时序、reset 双 key 结果聚合、toast 条件、
// 整段 pending 门与 disabled 守卫、与 app.tsx 设置水合的交互）需要 React 渲染
// 环境，本仓库 strip-types 单测不含组件渲染，不在本文件覆盖范围。

interface Deferred {
    promise: Promise<void>;
    resolve: () => void;
    reject: (error: unknown) => void;
}

function createDeferred(): Deferred {
    let resolve!: () => void;
    let reject!: (error: unknown) => void;
    const promise = new Promise<void>((res, rej) => {
        resolve = res;
        reject = rej;
    });
    return { promise, resolve, reject };
}

/** 等待微任务与已排程宏任务清空，让串行链推进到稳定状态。 */
async function flushAsync(): Promise<void> {
    await new Promise<void>((resolve) => setTimeout(resolve, 0));
    await new Promise<void>((resolve) => setTimeout(resolve, 0));
}

test('save resolves ok, runs perform and never rolls back on success', async () => {
    const queue = createSettingSaveQueue();
    const rollbacks: string[] = [];
    const performed: string[] = [];

    const outcome = await queue.save(
        { key: 'nav_order', next: ['a', 'b'], confirmedSnapshot: ['b', 'a'], rollback: (confirmed) => rollbacks.push(confirmed.join(',')) },
        async (next) => {
            performed.push(next.join(','));
        }
    );

    assert.deepEqual(outcome, { ok: true });
    assert.deepEqual(performed, ['a,b']);
    assert.equal(rollbacks.length, 0);
});

test('save resolves not-ok and rolls back to the confirmed snapshot when the latest save fails', async () => {
    const queue = createSettingSaveQueue();
    const rollbacks: string[] = [];

    const outcome = await queue.save(
        { key: 'nav_order', next: 'op1', confirmedSnapshot: 'confirmed-0', rollback: (confirmed) => rollbacks.push(confirmed) },
        async () => {
            throw new Error('network down');
        }
    );

    assert.deepEqual(outcome, { ok: false });
    assert.deepEqual(rollbacks, ['confirmed-0']);
});

test('an older failed save never rolls back a newer optimistic operation', async () => {
    const queue = createSettingSaveQueue();
    const rollbacks: string[] = [];
    const first = createDeferred();

    const firstResult = queue.save(
        { key: 'k', next: 'op1', confirmedSnapshot: 'confirmed-0', rollback: (confirmed) => rollbacks.push(confirmed) },
        () => first.promise
    );
    const secondResult = queue.save(
        { key: 'k', next: 'op2', confirmedSnapshot: 'op1-optimistic', rollback: (confirmed) => rollbacks.push(confirmed) },
        async () => undefined
    );

    first.reject(new Error('network down'));
    assert.deepEqual(await firstResult, { ok: false });
    assert.deepEqual(await secondResult, { ok: true });
    // op1 失败时 op2 已入队：op1 不得回滚（否则覆盖 op2 的乐观状态），由 op2 决定最终状态。
    assert.equal(rollbacks.length, 0);
});

test('when every save fails the field rolls back to the last confirmed value, not intermediate optimistic state', async () => {
    const queue = createSettingSaveQueue();
    const rollbacks: string[] = [];
    const first = createDeferred();

    const firstResult = queue.save(
        { key: 'k', next: 'op1', confirmedSnapshot: 'confirmed-0', rollback: (confirmed) => rollbacks.push(confirmed) },
        () => first.promise
    );
    const secondResult = queue.save(
        { key: 'k', next: 'op2', confirmedSnapshot: 'op1-optimistic', rollback: (confirmed) => rollbacks.push(confirmed) },
        async () => {
            throw new Error('network down');
        }
    );

    first.reject(new Error('network down'));
    assert.deepEqual(await firstResult, { ok: false });
    assert.deepEqual(await secondResult, { ok: false });
    // 只有最新的 op2 触发回滚，且目标是 op1 之前的已确认值 confirmed-0——
    // 不允许把 op1 未持久化的乐观状态留在回滚结果里。
    assert.deepEqual(rollbacks, ['confirmed-0']);
});

test('after the chain drains the baseline is re-seeded, so externally hydrated state wins on a later failure', async () => {
    const queue = createSettingSaveQueue();
    const rollbacks: string[] = [];

    const first = await queue.save(
        { key: 'k', next: 'A', confirmedSnapshot: 'A-prev', rollback: (confirmed) => rollbacks.push(confirmed) },
        async () => undefined
    );
    // 链已结束：外部（如 app.tsx 设置水合）把 state 改成 B，下一次提交捕获的
    // confirmedSnapshot 是 B；失败必须回滚到 B 而非首次保存过的 A。
    const second = await queue.save(
        { key: 'k', next: 'C', confirmedSnapshot: 'B', rollback: (confirmed) => rollbacks.push(confirmed) },
        async () => {
            throw new Error('network down');
        }
    );

    assert.deepEqual(first, { ok: true });
    assert.deepEqual(second, { ok: false });
    assert.deepEqual(rollbacks, ['B']);
});

test('a success inside a busy chain becomes the rollback baseline for a later failure', async () => {
    const queue = createSettingSaveQueue();
    const rollbacks: string[] = [];
    const first = createDeferred();

    const firstResult = queue.save(
        { key: 'k', next: 'op1', confirmedSnapshot: 'c0', rollback: (confirmed) => rollbacks.push(confirmed) },
        () => first.promise
    );
    const secondResult = queue.save(
        { key: 'k', next: 'op2', confirmedSnapshot: 'op1-optimistic', rollback: (confirmed) => rollbacks.push(confirmed) },
        async () => {
            throw new Error('network down');
        }
    );

    first.resolve();
    assert.deepEqual(await firstResult, { ok: true });
    assert.deepEqual(await secondResult, { ok: false });
    // 链繁忙时不得重新采纳 op2 的快照；op1 成功后服务端是 op1，基准随之推进。
    assert.deepEqual(rollbacks, ['op1']);
});

test('saves for the same key run strictly in submission order', async () => {
    const queue = createSettingSaveQueue();
    const events: string[] = [];
    const first = createDeferred();

    void queue.save(
        { key: 'k', next: 'op1', confirmedSnapshot: 'c0', rollback: () => undefined },
        () => {
            events.push('start-1');
            return first.promise;
        }
    );
    void queue.save(
        { key: 'k', next: 'op2', confirmedSnapshot: 'c1', rollback: () => undefined },
        async () => {
            events.push('start-2');
        }
    );

    await flushAsync();
    // op1 未结算前 op2 不得开始：服务端不会观察到同 key 乱序写入。
    assert.deepEqual(events, ['start-1']);

    first.resolve();
    await flushAsync();
    assert.deepEqual(events, ['start-1', 'start-2']);
});

test('the save chain keeps running after a failure', async () => {
    const queue = createSettingSaveQueue();
    const performed: string[] = [];

    await queue.save(
        { key: 'k', next: 'op1', confirmedSnapshot: 'c0', rollback: () => undefined },
        async () => {
            throw new Error('network down');
        }
    );
    const second = await queue.save(
        { key: 'k', next: 'op2', confirmedSnapshot: 'c1', rollback: () => undefined },
        async (next) => {
            performed.push(next);
        }
    );

    assert.deepEqual(second, { ok: true });
    assert.deepEqual(performed, ['op2']);
});

test('saves for different keys do not block each other', async () => {
    const queue = createSettingSaveQueue();
    const events: string[] = [];
    const slow = createDeferred();

    const slowResult = queue.save(
        { key: 'a', next: 'a1', confirmedSnapshot: 'a0', rollback: () => undefined },
        () => {
            events.push('start-a');
            return slow.promise;
        }
    );
    const fastResult = queue.save(
        { key: 'b', next: 'b1', confirmedSnapshot: 'b0', rollback: () => undefined },
        async () => {
            events.push('start-b');
        }
    );

    await flushAsync();
    assert.deepEqual(events, ['start-a', 'start-b']);

    slow.resolve();
    assert.deepEqual(await slowResult, { ok: true });
    assert.deepEqual(await fastResult, { ok: true });
});

test('a failing key rolls back only its own confirmed state', async () => {
    const queue = createSettingSaveQueue();
    const rollbacks: string[] = [];

    const a = await queue.save(
        { key: 'a', next: 'a1', confirmedSnapshot: 'a0', rollback: (confirmed) => rollbacks.push(confirmed) },
        async () => {
            throw new Error('network down');
        }
    );
    const b = await queue.save(
        { key: 'b', next: 'b1', confirmedSnapshot: 'b0', rollback: (confirmed) => rollbacks.push(confirmed) },
        async () => undefined
    );

    assert.deepEqual(a, { ok: false });
    assert.deepEqual(b, { ok: true });
    assert.deepEqual(rollbacks, ['a0']);
});

test('independent keys roll back independently when both fail (reset two-key independence)', async () => {
    const queue = createSettingSaveQueue();
    const rollbacks: string[] = [];

    const [order, visible] = await Promise.all([
        queue.save(
            { key: 'order', next: 'o1', confirmedSnapshot: 'o0', rollback: (confirmed) => rollbacks.push(confirmed) },
            async () => {
                throw new Error('network down');
            }
        ),
        queue.save(
            { key: 'visible', next: 'v1', confirmedSnapshot: 'v0', rollback: (confirmed) => rollbacks.push(confirmed) },
            async () => {
                throw new Error('network down');
            }
        ),
    ]);

    assert.deepEqual(order, { ok: false });
    assert.deepEqual(visible, { ok: false });
    assert.deepEqual(rollbacks.sort(), ['o0', 'v0']);
});
