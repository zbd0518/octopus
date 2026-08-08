import {
    OpenAI,
    Claude,
    Gemini,
    DeepSeek,
    Mistral,
    Qwen,
    Meta,
    Ollama,
    Groq,
    Cohere,
    Perplexity,
    Zhipu,
    Yi,
    Kimi,
    Minimax,
    Doubao,
    Hunyuan,
    Spark,
    Wenxin,
    Nvidia,
    Azure,
    Aws,
    Together,
    Fireworks,
    Replicate,
    HuggingFace,
    Grok,
    Google,
    Cerebras,
    SambaNova,
    Cloudflare,
    OpenRouter,
    Volcengine,
    SiliconCloud,
    Novita,
    InternLM,
    Stepfun,
    Gemma,
    Microsoft,
    KwaiKAT,
} from '@lobehub/icons';

type AvatarComponent = typeof OpenAI.Avatar;

type ModelIconConfig = {
    prefixes: string[];
    Avatar: AvatarComponent;
    color: string;
    label: string;
};

/**
 * Provider configurations with prefixes, Avatar components, and brand colors
 * Similar to Go's Provider array in internal/price/price.go
 */
const MODEL_ICON_PATTERNS: ModelIconConfig[] = [
    // OpenAI - GPT series
    { prefixes: ['gpt-', 'o1', 'o3', 'o4', 'chatgpt', 'text-embedding', 'dall-e', 'openai'], Avatar: OpenAI.Avatar, color: '#10A37F', label: 'OpenAI' },
    // Anthropic - Claude series
    { prefixes: ['claude', 'anthropic'], Avatar: Claude.Avatar, color: '#D7765A', label: 'Anthropic' },
    // Google - Gemini series
    { prefixes: ['gemini'], Avatar: Gemini.Avatar, color: '#4285F4', label: 'Google' },
    { prefixes: ['gemma'], Avatar: Gemma.Avatar, color: '#4285F4', label: 'Google' },
    { prefixes: ['palm', 'google'], Avatar: Google.Avatar, color: '#4285F4', label: 'Google' },
    // DeepSeek series
    { prefixes: ['deepseek'], Avatar: DeepSeek.Avatar, color: '#4D6BFE', label: 'DeepSeek' },
    // xAI - Grok series
    { prefixes: ['grok', 'xai'], Avatar: Grok.Avatar, color: '#000000', label: 'xAI' },
    // Alibaba - Qwen series
    { prefixes: ['qwen', 'qwq', 'alibaba'], Avatar: Qwen.Avatar, color: '#6B4EFF', label: 'Qwen' },
    // Zhipu - GLM series
    { prefixes: ['glm', 'chatglm', 'zhipu', 'z-ai'], Avatar: Zhipu.Avatar, color: '#3C5BFC', label: 'Zhipu' },
    // MiniMax series
    { prefixes: ['minimax', 'abab'], Avatar: Minimax.Avatar, color: '#1A1A2E', label: 'MiniMax' },
    // Moonshot/Kimi series
    { prefixes: ['moonshot', 'kimi'], Avatar: Kimi.Avatar, color: '#000000', label: 'Kimi' },
    // Mistral series
    { prefixes: ['mistral', 'mixtral', 'codestral', 'pixtral'], Avatar: Mistral.Avatar, color: '#F7D046', label: 'Mistral' },
    // Meta - Llama series
    { prefixes: ['llama', 'meta-llama', 'meta'], Avatar: Meta.Avatar, color: '#0668E1', label: 'Meta' },
    // ByteDance - Doubao series
    { prefixes: ['doubao', 'skylark', 'bytedance'], Avatar: Doubao.Avatar, color: '#00D6C2', label: 'Doubao' },
    // Yi series
    { prefixes: ['yi-', '01-ai'], Avatar: Yi.Avatar, color: '#1B1464', label: 'Yi' },
    // Tencent - Hunyuan
    { prefixes: ['hunyuan'], Avatar: Hunyuan.Avatar, color: '#0052D9', label: 'Hunyuan' },
    // iFlytek - Spark
    { prefixes: ['spark'], Avatar: Spark.Avatar, color: '#0078FF', label: 'Spark' },
    // Baidu - ERNIE/Wenxin
    { prefixes: ['ernie', 'wenxin', 'baidu'], Avatar: Wenxin.Avatar, color: '#2932E1', label: 'Wenxin' },
    // InternLM
    { prefixes: ['internlm'], Avatar: InternLM.Avatar, color: '#2F54EB', label: 'InternLM' },
    // Stepfun
    { prefixes: ['stepfun', 'step-'], Avatar: Stepfun.Avatar, color: '#5B5CFF', label: 'Stepfun' },
    // Cloud providers
    { prefixes: ['nvidia', 'nemotron'], Avatar: Nvidia.Avatar, color: '#76B900', label: 'NVIDIA' },
    { prefixes: ['azure'], Avatar: Azure.Avatar, color: '#0078D4', label: 'Azure' },
    { prefixes: ['aws', 'amazon', 'bedrock'], Avatar: Aws.Avatar, color: '#FF9900', label: 'AWS' },
    { prefixes: ['volcengine'], Avatar: Volcengine.Avatar, color: '#3370FF', label: 'Volcengine' },
    { prefixes: ['siliconflow'], Avatar: SiliconCloud.Avatar, color: '#7C3AED', label: 'SiliconCloud' },
    // Inference providers
    { prefixes: ['groq'], Avatar: Groq.Avatar, color: '#F55036', label: 'Groq' },
    { prefixes: ['together'], Avatar: Together.Avatar, color: '#0F6FFF', label: 'Together' },
    { prefixes: ['fireworks'], Avatar: Fireworks.Avatar, color: '#FF6B00', label: 'Fireworks' },
    { prefixes: ['replicate'], Avatar: Replicate.Avatar, color: '#000000', label: 'Replicate' },
    { prefixes: ['ollama'], Avatar: Ollama.Avatar, color: '#FFFFFF', label: 'Ollama' },
    { prefixes: ['openrouter'], Avatar: OpenRouter.Avatar, color: '#6366F1', label: 'OpenRouter' },
    { prefixes: ['cloudflare'], Avatar: Cloudflare.Avatar, color: '#F38020', label: 'Cloudflare' },
    { prefixes: ['cerebras'], Avatar: Cerebras.Avatar, color: '#FF5722', label: 'Cerebras' },
    { prefixes: ['sambanova'], Avatar: SambaNova.Avatar, color: '#FF6B00', label: 'SambaNova' },
    { prefixes: ['novita'], Avatar: Novita.Avatar, color: '#7C3AED', label: 'Novita' },
    { prefixes: ['huggingface', 'hf'], Avatar: HuggingFace.Avatar, color: '#FFD21E', label: 'HuggingFace' },
    // Other models
    { prefixes: ['cohere', 'command'], Avatar: Cohere.Avatar, color: '#39594D', label: 'Cohere' },
    { prefixes: ['perplexity'], Avatar: Perplexity.Avatar, color: '#20B8CD', label: 'Perplexity' },
    { prefixes: ['phi-'], Avatar: Microsoft.Avatar, color: '#00BCF2', label: 'Microsoft' },
    { prefixes: ['kat'], Avatar: KwaiKAT.Avatar, color: '#1969FC', label: 'KwaiKAT' },
];

// Default configuration
const DEFAULT_CONFIG = { Avatar: OpenAI.Avatar, color: '#10A37F', label: 'Model' };

/**
 * 计算 hex 颜色的相对亮度（0–1），基于 WCAG 相对亮度公式。
 * 用于判断品牌色在深色/浅色背景上的可读性。
 */
export function colorLuminance(hex: string): number {
    const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
    if (!m) return 0.5; // 非标准格式返回中间值，不触发翻转
    const n = parseInt(m[1], 16);
    const toLinear = (c: number) => {
        const s = c / 255;
        return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
    };
    const r = toLinear((n >> 16) & 0xff);
    const g = toLinear((n >> 8) & 0xff);
    const b = toLinear(n & 0xff);
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/**
 * 解析在指定主题下可读的品牌色。
 *
 * 品牌色直接用作 Badge 文字/背景色时，深色系颜色（如 Grok/Kimi/Replicate 的
 * 纯黑 #000、MiniMax 的 #1A1A2E）在深色模式下会「黑字+透明黑底」看不清；
 * 同理浅色模式下 Ollama 的纯白 #FFF 白字也不可见。
 *
 * @param color 原始品牌色（hex）
 * @param isDark 当前是否为深色主题
 * @returns 可读颜色
 */
export function resolveBrandColor(color: string, isDark: boolean): string {
    const lum = colorLuminance(color);
    if (isDark && lum < 0.15) return '#E5E7EB';
    if (!isDark && lum > 0.9) return '#4B5563';
    return color;
}

/**
 * Get the Avatar component and color for a given model name
 * @param modelName - The name of the model
 * @returns Object containing Avatar component and brand color
 */
export function getModelIcon(modelName: string): { Avatar: AvatarComponent; color: string; label: string } {
    // Extract the part after the first '/' if it exists
    // e.g., "qwen/gpt-5.2" -> "gpt-5.2"
    const nameToMatch = modelName.includes('/') ? modelName.split('/')[1] : modelName;
    const lowerName = nameToMatch.toLowerCase();
    for (const { prefixes, Avatar, color, label } of MODEL_ICON_PATTERNS) {
        if (prefixes.some(prefix => lowerName.startsWith(prefix))) {
            return { Avatar, color, label };
        }
    }
    return DEFAULT_CONFIG;
}

export { brandBadgeStyle } from './brand-badge-style.ts';

