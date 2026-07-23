import type { LLMChannel } from '@/api/endpoints/model';

/** channel_id → enabled（同一渠道多模型行取任意一致值） */
export function buildChannelEnabledMap(modelChannels: LLMChannel[]): Map<number, boolean> {
    const map = new Map<number, boolean>();
    for (const mc of modelChannels) {
        map.set(mc.channel_id, mc.enabled);
    }
    return map;
}

/** 用最新 model/channel 列表刷新已选成员的 enabled / channel_name；无变化时返回原数组引用 */
export function syncMembersChannelEnabled<T extends { channel_id: number; enabled: boolean; channel_name: string }>(
    members: T[],
    modelChannels: LLMChannel[],
): T[] {
    if (members.length === 0 || modelChannels.length === 0) {
        return members;
    }

    const enabledByChannel = buildChannelEnabledMap(modelChannels);
    const nameByChannel = new Map<number, string>();
    for (const mc of modelChannels) {
        nameByChannel.set(mc.channel_id, mc.channel_name);
    }

    let changed = false;
    const next = members.map((member) => {
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
