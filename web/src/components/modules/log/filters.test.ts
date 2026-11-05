import test from 'node:test';
import assert from 'node:assert/strict';
import { sortGroupsByName } from './filters.ts';

test('sortGroupsByName sorts groups in ascending order by name', () => {
    const groups = [
        { id: 1, name: 'zeta' },
        { id: 2, name: 'alpha' },
        { id: 3, name: 'beta' },
    ];
    const sorted = sortGroupsByName(groups);
    assert.deepEqual(
        sorted.map((g) => g.name),
        ['alpha', 'beta', 'zeta'],
    );
});

test('sortGroupsByName breaks ties by id and does not mutate input', () => {
    const groups = [
        { id: 5, name: 'chat' },
        { id: 2, name: 'chat' },
    ];
    const sorted = sortGroupsByName(groups);
    assert.deepEqual(
        sorted.map((g) => g.id),
        [2, 5],
    );
    assert.equal(groups[0].id, 5);
});
