import type { LLMChannel } from '@/api/endpoints/model';

/** channel_id → enabled（同一渠道多模型行取任意一致值） */
export function buildChannelEnabledMap(modelChannels: LLMChannel[]): Map<number, boolean> {
    const map = new Map<number, boolean>();
    for (const mc of modelChannels) {
        map.set(mc.channel_id, mc.enabled);
    }
    return map;
}

type SyncableMember = {
    name: string;
    channel_id: number;
    enabled: boolean;
    channel_name: string;
    upstream_price?: LLMChannel['upstream_price'];
    upstream_metrics?: LLMChannel['upstream_metrics'];
    channel_balance?: LLMChannel['channel_balance'];
    channel_today_income?: LLMChannel['channel_today_income'];
};

function modelChannelLookupKey(channelId: number, name: string): string {
    return `${channelId}\0${name}`;
}

/**
 * 用最新 model/channel 列表刷新已选成员的渠道态与上游展示字段；
 * 无变化时返回原数组引用。按 channel_id + model name 对齐，保证左右健康风险同口径。
 */
export function syncMembersChannelEnabled<T extends SyncableMember>(
    members: T[],
    modelChannels: LLMChannel[],
): T[] {
    if (members.length === 0 || modelChannels.length === 0) {
        return members;
    }

    const byKey = new Map<string, LLMChannel>();
    const enabledByChannel = new Map<number, boolean>();
    const nameByChannel = new Map<number, string>();
    for (const mc of modelChannels) {
        byKey.set(modelChannelLookupKey(mc.channel_id, mc.name), mc);
        enabledByChannel.set(mc.channel_id, mc.enabled);
        nameByChannel.set(mc.channel_id, mc.channel_name);
    }

    let changed = false;
    const next = members.map((member) => {
        const matched = byKey.get(modelChannelLookupKey(member.channel_id, member.name));
        if (matched) {
            if (
                member.enabled === matched.enabled &&
                member.channel_name === matched.channel_name &&
                member.upstream_price === matched.upstream_price &&
                member.upstream_metrics === matched.upstream_metrics &&
                member.channel_balance === matched.channel_balance &&
                member.channel_today_income === matched.channel_today_income
            ) {
                return member;
            }
            changed = true;
            return {
                ...member,
                enabled: matched.enabled,
                channel_name: matched.channel_name,
                upstream_price: matched.upstream_price,
                upstream_metrics: matched.upstream_metrics,
                channel_balance: matched.channel_balance,
                channel_today_income: matched.channel_today_income,
            };
        }

        // 模型行已不在列表时，仍尽量刷新渠道级 enabled / name
        if (!enabledByChannel.has(member.channel_id)) {
            return member;
        }
        const enabled = enabledByChannel.get(member.channel_id)!;
        const channelName = nameByChannel.get(member.channel_id) ?? member.channel_name;
        if (member.enabled === enabled && member.channel_name === channelName) {
            return member;
        }
        changed = true;
        return { ...member, enabled, channel_name: channelName };
    });

    return changed ? next : members;
}

/** 候选列表：默认隐藏禁用渠道；可按渠道分组过滤 */
export function filterModelChannelsForPicker(
    modelChannels: LLMChannel[],
    opts: {
        showDisabled: boolean;
        channelGroupId: number | null;
        groupIdByChannelId: Map<number, number>;
    },
): LLMChannel[] {
    return modelChannels.filter((mc) => {
        if (!opts.showDisabled && !mc.enabled) {
            return false;
        }
        if (opts.channelGroupId != null) {
            const groupId = opts.groupIdByChannelId.get(mc.channel_id);
            if (groupId !== opts.channelGroupId) {
                return false;
            }
        }
        return true;
    });
}

/** 自动添加：默认只加启用渠道 */
export function filterMatchedForAutoAdd(matched: LLMChannel[], showDisabled: boolean): LLMChannel[] {
    if (showDisabled) {
        return matched;
    }
    return matched.filter((mc) => mc.enabled);
}

export function countDisabledMembers(members: Array<{ enabled: boolean }>): number {
    return members.reduce((acc, member) => acc + (member.enabled === false ? 1 : 0), 0);
}

/** 待选择面板按渠道聚合后的结构 */
export interface PickerChannel {
    id: number;
    name: string;
    enabled: boolean;
    models: LLMChannel[];
}

/**
 * 把 model/channel 行按渠道聚合，供待选择列表展示。
 * 排序约定：渠道按名称升序（同名按 ID 兜底，与渠道页默认名称排序一致），
 * 渠道内模型按名称字母序。
 */
export function buildPickerChannelList(modelChannels: LLMChannel[]): PickerChannel[] {
    const byId = new Map<number, PickerChannel>();
    for (const mc of modelChannels) {
        const existing = byId.get(mc.channel_id);
        if (existing) {
            existing.models.push(mc);
        } else {
            byId.set(mc.channel_id, {
                id: mc.channel_id,
                name: mc.channel_name,
                enabled: mc.enabled,
                models: [mc],
            });
        }
    }

    return Array.from(byId.values())
        .map((c) => ({ ...c, models: [...c.models].sort((a, b) => a.name.localeCompare(b.name)) }))
        .sort((a, b) => a.name.localeCompare(b.name) || a.id - b.id);
}
