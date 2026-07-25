/**
 * 从路由分组列表中，按 channel_id 聚合该渠道所属的路由分组及贡献模型。
 * 仅做只读 join，不改分组数据。
 */

export type RouteGroupMemberSource = {
    id?: number;
    name: string;
    items?: Array<{
        channel_id: number;
        model_name: string;
    }> | null;
};

export type ChannelRouteGroupMembership = {
    groupId: number;
    groupName: string;
    models: string[];
};

/**
 * 返回该渠道加入的路由分组；按组名排序，组内模型按名称排序并去重。
 * 无 id 的分组忽略（无法跳转编辑）。
 */
export function collectChannelRouteGroups(
    groups: RouteGroupMemberSource[] | null | undefined,
    channelId: number,
): ChannelRouteGroupMembership[] {
    if (!groups || groups.length === 0 || !Number.isFinite(channelId)) {
        return [];
    }

    const result: ChannelRouteGroupMembership[] = [];

    for (const group of groups) {
        if (typeof group.id !== 'number') continue;
        const items = group.items ?? [];
        const models: string[] = [];
        const seen = new Set<string>();

        for (const item of items) {
            if (item.channel_id !== channelId) continue;
            const model = (item.model_name ?? '').trim();
            if (!model || seen.has(model)) continue;
            seen.add(model);
            models.push(model);
        }

        if (models.length === 0) continue;
        models.sort((a, b) => a.localeCompare(b));
        result.push({
            groupId: group.id,
            groupName: group.name,
            models,
        });
    }

    result.sort((a, b) => a.groupName.localeCompare(b.groupName) || a.groupId - b.groupId);
    return result;
}
