/** 上游成功率健康风险：none=不展示，moderate=一般，low=偏低 */
export type HealthRiskLevel = 'none' | 'moderate' | 'low';

export type ChannelKeyLike = {
    enabled: boolean;
    channel_key: string;
};

export type ChannelWithKeys = {
    id: number;
    keys?: ChannelKeyLike[] | null;
};

export type MemberRiskSource = {
    enabled: boolean;
    channel_id: number;
    upstream_metrics?: { success_rate?: number } | null;
};

export type MemberRiskFlags = {
    channelDisabled: boolean;
    noEnabledKey: boolean;
    healthRisk: HealthRiskLevel;
};

export type MemberRiskCounts = {
    disabledCount: number;
    noKeyCount: number;
    healthRiskCount: number;
};

/** 是否存在启用且非空的 Key */
export function hasEnabledChannelKey(keys: ChannelKeyLike[] | null | undefined): boolean {
    if (!keys || keys.length === 0) {
        return false;
    }
    return keys.some((key) => key.enabled && Boolean(key.channel_key?.trim()));
}

/** channel_id → 是否有可用 Key；未出现在列表中的渠道不写入 map */
export function buildChannelHasEnabledKeyMap(channels: ChannelWithKeys[]): Map<number, boolean> {
    const map = new Map<number, boolean>();
    for (const channel of channels) {
        map.set(channel.id, hasEnabledChannelKey(channel.keys));
    }
    return map;
}

/**
 * 成功率风险等级（与 UpstreamPerfBadges StatusBars 阈值对齐）：
 * - 无数据 / ≤0 / ≥0.9 → none
 * - [0.7, 0.9) → moderate
 * - (0, 0.7) → low
 */
export function healthRiskLevel(successRate: number | null | undefined): HealthRiskLevel {
    if (typeof successRate !== 'number' || !Number.isFinite(successRate) || successRate <= 0) {
        return 'none';
    }
    if (successRate >= 0.9) {
        return 'none';
    }
    if (successRate >= 0.7) {
        return 'moderate';
    }
    return 'low';
}

/** 未知渠道（map 无条目）不当作无 Key，避免误报 */
export function channelHasNoEnabledKey(
    channelId: number,
    hasEnabledKeyByChannelId: Map<number, boolean>,
): boolean {
    if (!hasEnabledKeyByChannelId.has(channelId)) {
        return false;
    }
    return hasEnabledKeyByChannelId.get(channelId) === false;
}

export function memberRiskFlags(
    member: MemberRiskSource,
    hasEnabledKeyByChannelId: Map<number, boolean>,
): MemberRiskFlags {
    return {
        channelDisabled: member.enabled === false,
        noEnabledKey: channelHasNoEnabledKey(member.channel_id, hasEnabledKeyByChannelId),
        healthRisk: healthRiskLevel(member.upstream_metrics?.success_rate),
    };
}

export function countMemberRisks(
    members: MemberRiskSource[],
    hasEnabledKeyByChannelId: Map<number, boolean>,
): MemberRiskCounts {
    let disabledCount = 0;
    let noKeyCount = 0;
    let healthRiskCount = 0;

    for (const member of members) {
        const flags = memberRiskFlags(member, hasEnabledKeyByChannelId);
        if (flags.channelDisabled) disabledCount += 1;
        if (flags.noEnabledKey) noKeyCount += 1;
        if (flags.healthRisk !== 'none') healthRiskCount += 1;
    }

    return { disabledCount, noKeyCount, healthRiskCount };
}
