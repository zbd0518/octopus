import type { AIRouteCategoryStrategy, AIRouteScope, GenerateAIRouteRequest } from '@/api/endpoints/group';

export type AIRouteCategoryChoice = AIRouteCategoryStrategy | '';

export const AI_ROUTE_CATEGORY_CHOICES: AIRouteCategoryChoice[] = ['', 'vendor', 'channel_tier'];

export function buildAIRouteRequest(
    scope: AIRouteScope,
    groupId: number,
    categoryStrategy: AIRouteCategoryChoice,
): GenerateAIRouteRequest {
    return {
        scope,
        group_id: scope === 'group' ? groupId : undefined,
        category_strategy: categoryStrategy || undefined,
    };
}
