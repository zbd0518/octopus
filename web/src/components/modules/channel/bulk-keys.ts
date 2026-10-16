export type ParsedBulkKey = {
    channel_key: string;
    remark?: string;
};

export type DecodedBase64Key = {
    source: string;
    channel_key: string;
    remark?: string;
};

export type FailedBase64Key = {
    source: string;
    reason: 'invalid-base64' | 'invalid-utf8' | 'not-a-key';
};

export type DecodeBulkBase64Result = {
    decoded: DecodedBase64Key[];
    failed: FailedBase64Key[];
};

const BASE64_PAYLOAD_PATTERN = /^[A-Za-z0-9+/]+={0,2}$/;

/** 去掉常见包装：data URL 前缀、JSON/PEM 风格的引号。 */
function stripBase64Wrapping(value: string): string {
    let next = value.trim();
    next = next.replace(/^data:[^,]*;base64,/i, '');
    const quoted = next.match(/^['"`](.*)['"`]$/);
    if (quoted) {
        next = quoted[1].trim();
    }
    return next;
}

export function looksLikeBase64Payload(value: string): boolean {
    const trimmed = stripBase64Wrapping(value);
    if (!trimmed || trimmed.length % 4 !== 0) return false;
    if (!BASE64_PAYLOAD_PATTERN.test(trimmed)) return false;
    // 纯字母数字且无 padding 的短串（如 sk-xxx 之外的普通词）不足以判定
    return /[+/=]/.test(trimmed) || trimmed.length >= 16;
}

/**
 * 拆分粘贴文本为候选 base64 串：
 *
 * - 默认每行一个；
 * - 单行、无空白、整体像 base64 时（编码换行后的多 Key 就是这种形态）整行一个；
 * - 其余情况按空白拆分，兼容换行 / 空格 / 逗号分隔。
 */
export function splitBase64Payloads(text: string): string[] {
    const lines = text
        .split(/\r?\n/)
        .map((line) => line.trim())
        .filter(Boolean);

    if (lines.length === 1) {
        return looksLikeBase64Payload(lines[0])
            ? lines
            : lines[0].split(/[\s,;]+/).map((part) => part.trim()).filter(Boolean);
    }

    return lines
        .flatMap((line) => line.split(/[\s,;]+/))
        .map((part) => part.trim())
        .filter(Boolean);
}

function decodeBase64ToText(payload: string): string {
    const normalized = payload.replace(/-/g, '+').replace(/_/g, '/');
    const padded = normalized + '='.repeat((4 - (normalized.length % 4)) % 4);
    const binary = atob(padded);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
}

/**
 * 中文字符与中文标点：解码后的明文常混入中文备注 / 说明行，导入前整体剔除，只留 Key。
 *
 * - `\p{Script=Han}` 覆盖所有汉字（含扩展区与 〇）；
 * - CJK 标点（、。「」《》等）、全角标点（！，：（）等，不含全角字母数字）；
 * - 中文引号 ‘’ “”。
 *
 * 剔除后：纯中文说明行变空行被丢弃，`key|中文备注` 只剩 Key，全角括号包裹的备注也一并消失。
 */
const CHINESE_PATTERN = /[\p{Script=Han}\u3000-\u303F\uFF01-\uFF0F\uFF1A-\uFF20\uFF3B-\uFF40\uFF5B-\uFF5E\u2018\u2019\u201C\u201D]/gu;

/** 解出明文后可能还需要按普通批量导入规则拆成多条 Key（明文本身带换行 / key|备注）。 */
function decodePayload(payload: string): ParsedBulkKey[] {
    const plain = decodeBase64ToText(stripBase64Wrapping(payload)).replace(CHINESE_PATTERN, '');
    const parsed = parseBulkKeys(plain);
    // 碎片（比如折断 base64 里的一小段）解出来是短无意义字符串，不能当 Key——会凭空造出脏 Key。
    // 明文本该是完整 Key，因此只接受至少有一个像 Key 的片段的结果。
    return parsed.some((item) => looksLikeApiKey(item.channel_key)) ? parsed : [];
}

/**
 * 批量 base64 解码：把每段候选 base64 解出明文，再交给 parseBulkKeys 解析成 Key。
 *
 * 无法解码的片段进入 `failed`，其余照常返回，避免一条脏数据阻断整批导入。
 * 若一段都没解出来且不止一段，则回退为拼接整体解码——覆盖单个 base64 被换行折断粘贴的场景。
 */
export function decodeBulkBase64Keys(text: string): DecodeBulkBase64Result {
    const payloads = splitBase64Payloads(text);
    const direct = decodeBase64Payloads(payloads);

    if (direct.decoded.length === 0 && direct.failed.length > 1) {
        const joined = decodeBase64Payloads([payloads.join('')]);
        if (joined.decoded.length > 0) {
            return joined;
        }
    }

    return direct;
}

function decodeBase64Payloads(payloads: string[]): DecodeBulkBase64Result {
    const decoded: DecodedBase64Key[] = [];
    const failed: FailedBase64Key[] = [];

    for (const payload of payloads) {
        let parsed: ParsedBulkKey[];
        try {
            parsed = decodePayload(payload);
        } catch (error) {
            const reason = error instanceof TypeError ? 'invalid-utf8' : 'invalid-base64';
            failed.push({ source: payload, reason });
            continue;
        }

        if (parsed.length === 0) {
            // 能解码但明文不像 Key（例如折断碎片解出的短串），单独归因便于排查。
            failed.push({ source: payload, reason: 'not-a-key' });
            continue;
        }

        for (const item of parsed) {
            decoded.push({ source: payload, channel_key: item.channel_key, remark: item.remark });
        }
    }

    return { decoded, failed };
}

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
