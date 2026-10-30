import type { Channel } from '@/api/endpoints/channel';
import type { ChannelFormData } from './Form';

export function createChannelConnectionDraft(channel: Pick<Channel, 'node_id' | 'base_urls' | 'keys'>): Pick<ChannelFormData, 'base_urls' | 'keys'> {
    // Server-owned node placeholders must never become an editable HTTP credential.
    if (channel.node_id > 0) {
        return {
            base_urls: [{ url: '', delay: 0, suffix_mode: 'auto' }],
            keys: [{ enabled: true, channel_key: '', priority: 0, remark: '' }],
        };
    }
    return {
        base_urls: channel.base_urls?.length ? channel.base_urls : [{ url: '', delay: 0, suffix_mode: 'auto' }],
        keys: channel.keys.length > 0
            ? channel.keys.map((key) => ({
                id: key.id,
                enabled: key.enabled,
                channel_key: key.channel_key,
                status_code: key.status_code,
                last_use_time_stamp: key.last_use_time_stamp,
                total_cost: key.total_cost,
                priority: key.priority ?? 0,
                remark: key.remark,
                supported_models: key.supported_models ?? '',
            }))
            : [{ enabled: true, channel_key: '', priority: 0, remark: '' }],
    };
}

export function newChannelKeys(keys: ChannelFormData['keys']) {
    return keys
        .filter((key) => !key.id)
        .map((key) => ({ enabled: key.enabled, channel_key: key.channel_key.trim(), priority: Number(key.priority ?? 0), remark: key.remark ?? '', supported_models: key.supported_models ?? '' }));
}

type NodeTransport = {
    node_id: number;
    base_urls: unknown[];
    keys: unknown[];
    pool_id?: number;
    proxy_mode?: string;
    proxy_config_id?: number | null;
    proxy?: boolean;
    channel_proxy?: string | null;
};

/** Keep HTTP draft credentials in the form, but never send them to a local node. */
export function withNodeTransport<T extends NodeTransport>(payload: T): T {
    if (payload.node_id <= 0) return payload;
    return {
        ...payload,
        base_urls: [],
        keys: [],
        pool_id: 0,
        proxy_mode: 'direct',
        proxy_config_id: null,
        proxy: false,
        channel_proxy: '',
    };
}

type ChannelPatch = {
    name?: string;
    node_id?: number;
    base_urls?: unknown[];
    pool_id?: number;
    proxy_mode?: string;
    proxy_config_id?: number | null;
    proxy?: boolean;
    channel_proxy?: string | null;
    keys_to_add?: unknown[];
    keys_to_update?: unknown[];
    keys_to_delete?: number[];
};

export function withNodeUpdate<T extends ChannelPatch>(patch: T, nodeId: number, originalNodeId: number): T & ChannelPatch {
    const result: T & ChannelPatch = { ...patch };
    if (nodeId !== originalNodeId) result.node_id = nodeId;
    if (nodeId > 0) {
        delete result.base_urls;
        delete result.keys_to_add;
        delete result.keys_to_update;
        delete result.keys_to_delete;
        result.pool_id = 0;
        result.proxy_mode = 'direct';
        result.proxy_config_id = null;
        result.proxy = false;
        result.channel_proxy = '';
    }
    return result;
}
