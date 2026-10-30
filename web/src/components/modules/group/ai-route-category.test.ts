import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { AI_ROUTE_CATEGORY_CHOICES, buildAIRouteRequest } from './ai-route-category.ts';
import en from '../../../../public/locale/en.json' with { type: 'json' };
import zhHans from '../../../../public/locale/zh_hans.json' with { type: 'json' };
import zhHant from '../../../../public/locale/zh_hant.json' with { type: 'json' };

void describe('AI route category choices', () => {
    void it('offers no category by default and both generation strategies', () => {
        assert.deepEqual(AI_ROUTE_CATEGORY_CHOICES, ['', 'vendor', 'channel_tier']);
    });

    void it('omits an empty strategy and table group ID', () => {
        assert.deepEqual(buildAIRouteRequest('table', 0, ''), { scope: 'table', group_id: undefined, category_strategy: undefined });
    });

    void it('includes the selected strategy for table and single-group tasks', () => {
        assert.deepEqual(buildAIRouteRequest('table', 0, 'vendor'), { scope: 'table', group_id: undefined, category_strategy: 'vendor' });
        assert.deepEqual(buildAIRouteRequest('group', 7, 'channel_tier'), { scope: 'group', group_id: 7, category_strategy: 'channel_tier' });
    });

    void it('resolves the backend conflict message key in every locale', () => {
        for (const locale of [en, zhHans, zhHant]) {
            assert.equal(typeof locale.group.aiRoute.categoryStrategy.conflict, 'string');
        }
    });
});
