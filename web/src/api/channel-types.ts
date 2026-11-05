export const ChannelType = {
    OpenAIChat: 0,
    OpenAIResponse: 1,
    Anthropic: 2,
    Gemini: 3,
    Volcengine: 4,
    OpenAIEmbedding: 5,
    MiMoChat: 6,
    Cloudflare: 7,
} as const;
export type ChannelType = (typeof ChannelType)[keyof typeof ChannelType];

export const RequestRewriteHeaderProfile = {
    None: '',
    Codex: 'codex',
    ClaudeCode: 'claude_code',
} as const;
export type RequestRewriteHeaderProfile = (typeof RequestRewriteHeaderProfile)[keyof typeof RequestRewriteHeaderProfile];

export function isClaudeCodeHeaderProfileSupportedChannelType(channelType: ChannelType): boolean {
    return channelType === ChannelType.Anthropic;
}

export function normalizeRequestRewriteHeaderProfile(
    channelType: ChannelType,
    headerProfile?: RequestRewriteHeaderProfile | null,
): RequestRewriteHeaderProfile {
    if (!headerProfile) {
        return RequestRewriteHeaderProfile.None;
    }

    if (
        headerProfile === RequestRewriteHeaderProfile.ClaudeCode
        && !isClaudeCodeHeaderProfileSupportedChannelType(channelType)
    ) {
        return RequestRewriteHeaderProfile.None;
    }

    return headerProfile;
}
