// 号池账号平台/类型选项 + 默认 base_url。

export type PoolPlatform =
    | 'anthropic'
    | 'openai'
    | 'gemini'
    | 'grok'
    | 'volcengine'
    | 'custom';

// NOTE: 'setup-token' was intentionally removed from the credential type
// union (route A of guide card B3-#6): it was a frontend-only entry with no
// backend test/outbound/refresh implementation. Historical setup-token rows
// keep rendering via the unknown-type fallback editor in AccountFormDialog
// and the `t.has(typeLabels.*)` fallback in PoolDetail.
export type PoolAccountType = 'oauth' | 'apikey' | 'cookie' | 'upstream';

export const POOL_PLATFORM_OPTIONS: { value: PoolPlatform; label: string }[] = [
    { value: 'anthropic', label: 'Anthropic' },
    { value: 'openai', label: 'OpenAI' },
    { value: 'gemini', label: 'Gemini' },
    { value: 'grok', label: 'Grok' },
    { value: 'volcengine', label: 'Volcengine' },
    { value: 'custom', label: 'Custom' },
];

// 各平台支持的凭据类型。
export const POOL_TYPE_OPTIONS_BY_PLATFORM: Record<PoolPlatform, PoolAccountType[]> = {
    anthropic: ['oauth', 'apikey', 'cookie'],
    openai: ['oauth', 'apikey'],
    gemini: ['oauth', 'apikey'],
    grok: ['oauth', 'apikey'],
    volcengine: ['cookie'],
    custom: ['apikey', 'upstream'],
};

// 各平台默认 base_url。
export const DEFAULT_BASE_URL_BY_PLATFORM: Record<PoolPlatform, string> = {
    anthropic: 'https://api.anthropic.com',
    openai: 'https://api.openai.com',
    gemini: 'https://generativelanguage.googleapis.com',
    grok: 'https://api.x.ai',
    volcengine: 'https://ark.cn-beijing.volces.com',
    custom: '',
};

// 是否支持 OAuth 登录流程。
export function platformSupportsOAuth(platform: PoolPlatform): boolean {
    return platform === 'anthropic' || platform === 'openai' || platform === 'gemini' || platform === 'grok';
}

// 是否支持额度查询。
export function platformSupportsQuota(platform: PoolPlatform, type: PoolAccountType): boolean {
    if (platform === 'openai' && type === 'oauth') return true;
    if (platform === 'volcengine' && type === 'cookie') return true;
    return false;
}
