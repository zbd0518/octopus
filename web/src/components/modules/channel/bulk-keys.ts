export type ParsedBulkKey = {
    channel_key: string;
    remark?: string;
};

export type ChannelKeyLike = {
    id?: number;
    enabled: boolean;
    channel_key: string;
    priority?: number;
    remark?: string;
};

export type MergeBulkKeysResult<T extends ChannelKeyLike> = {
    keys: T[];
    added: number;
    skippedDuplicate: number;
};

/** 粗判是否像 API Key：无空白且足够长，避免把备注误拆成 key。 */
export function looksLikeApiKey(value: string): boolean {
    const trimmed = value.trim();
    return trimmed.length >= 8 && !/\s/.test(trimmed);
}

/**
 * 解析批量 Key 文本。
 *
 * 支持：
 * - 每行一个 Key
 * - `key|remark` / `key<TAB>remark`
 * - `key,remark`（右侧不像 Key 时当作备注）
 * - 单行逗号/分号分隔的多个 Key
 */
export function parseBulkKeys(text: string): ParsedBulkKey[] {
    const lines = text
        .split(/\r?\n/)
        .map((line) => line.trim())
        .filter(Boolean);

    if (lines.length === 0) {
        return [];
    }

    // 单行、无 |/TAB，但含逗号或分号：按多 Key 拆分
    if (lines.length === 1 && !/[|\t]/.test(lines[0]) && /[,;]/.test(lines[0])) {
        const parts = lines[0]
            .split(/[,;]+/)
            .map((part) => part.trim())
            .filter(Boolean);
        // 若只有两段且第二段不像 Key，当作 key,remark
        if (parts.length === 2 && looksLikeApiKey(parts[0]) && !looksLikeApiKey(parts[1])) {
            return [{ channel_key: parts[0], remark: parts[1] }];
        }
        return parts.map((channel_key) => ({ channel_key }));
    }

    const result: ParsedBulkKey[] = [];
    for (const line of lines) {
        const pipeOrTab = line.match(/^(.+?)[|\t](.*)$/);
        if (pipeOrTab) {
            const channel_key = pipeOrTab[1].trim();
            const remark = pipeOrTab[2].trim();
            if (channel_key) {
                result.push({ channel_key, remark: remark || undefined });
            }
            continue;
        }

        const commaIdx = line.indexOf(',');
        if (commaIdx > 0) {
            const left = line.slice(0, commaIdx).trim();
            const right = line.slice(commaIdx + 1).trim();
            if (left && right && !looksLikeApiKey(right)) {
                result.push({ channel_key: left, remark: right });
                continue;
            }
            const parts = line
                .split(',')
                .map((part) => part.trim())
                .filter(Boolean);
            if (parts.length > 1 && parts.every(looksLikeApiKey)) {
                for (const channel_key of parts) {
                    result.push({ channel_key });
                }
                continue;
            }
        }

        result.push({ channel_key: line });
    }

    return result;
}

/**
 * 把解析结果合并进现有 keys：
 * - 跳过与已有 channel_key 重复的项（含本次导入内去重）
 * - 若当前只有一行空 Key 占位，用第一条导入结果填入该行
 */
export function mergeBulkKeys<T extends ChannelKeyLike>(
    existing: T[],
    parsed: ParsedBulkKey[],
): MergeBulkKeysResult<T> {
    const next: T[] = existing.map((item) => ({ ...item }));
    const seen = new Set(
        next
            .map((item) => item.channel_key.trim())
            .filter(Boolean),
    );

    let added = 0;
    let skippedDuplicate = 0;
    const onlyEmptyPlaceholder =
        next.length === 1
        && !next[0].channel_key.trim()
        && next[0].id == null;

    for (const item of parsed) {
        const channel_key = item.channel_key.trim();
        if (!channel_key) {
            continue;
        }
        if (seen.has(channel_key)) {
            skippedDuplicate += 1;
            continue;
        }
        seen.add(channel_key);

        const remark = item.remark?.trim() ?? '';
        if (onlyEmptyPlaceholder && added === 0) {
            next[0] = {
                ...next[0],
                enabled: true,
                channel_key,
                remark: remark || next[0].remark || '',
                priority: next[0].priority ?? 0,
            };
        } else {
            next.push({
                enabled: true,
                channel_key,
                priority: 0,
                remark,
            } as T);
        }
        added += 1;
    }

    return { keys: next, added, skippedDuplicate };
}
