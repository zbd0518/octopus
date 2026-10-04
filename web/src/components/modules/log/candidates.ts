import type { ChannelAttempt, RelayLog } from '../../../api/endpoints/log.ts';

interface RouteItem {
    channel_id: number;
    model_name: string;
}

export interface LogCandidate extends RouteItem {
    channel_name: string;
    attempts: ChannelAttempt[];
}

export function buildLogCandidates(log: RelayLog, items: RouteItem[] = []): LogCandidate[] {
    const candidates = new Map<string, LogCandidate>();
    const add = (item: RouteItem, channelName = '') => {
        const key = JSON.stringify([item.channel_id, item.model_name]);
        let candidate = candidates.get(key);
        if (!candidate) {
            candidate = { ...item, channel_name: channelName, attempts: [] };
            candidates.set(key, candidate);
        } else if (channelName.trim()) {
            candidate.channel_name = channelName;
        }
        return candidate;
    };

    for (const attempt of log.attempts ?? []) {
        add(attempt, attempt.channel_name).attempts.push(attempt);
    }
    for (const item of items) add(item);
    if (log.channel > 0 && log.actual_model_name) {
        add({ channel_id: log.channel, model_name: log.actual_model_name }, log.channel_name);
    }
    return [...candidates.values()];
}

export function recordedCooldownSeconds(attempt?: ChannelAttempt): number | undefined {
    if (!attempt || (attempt.status !== 'skipped' && attempt.status !== 'circuit_break')) return undefined;
    const match = attempt.msg?.match(/remaining cooldown:\s*(\d+)s\b/i);
    return match ? Number(match[1]) : undefined;
}
