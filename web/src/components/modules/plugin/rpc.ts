export const PLUGIN_MESSAGE_TYPE = 'octopus:plugin:rpc:v1';

export interface PluginRPCMessage {
    type: typeof PLUGIN_MESSAGE_TYPE;
    pluginId: string;
    session: string;
    requestId: string;
    method: 'GET' | 'POST';
    path: string;
    body?: unknown;
}

export function pluginRPCPath(path: string): string {
    if (!/^\/?[a-zA-Z0-9_-]+(?:\/[a-zA-Z0-9_-]+)*$/.test(path) || path.length > 256) {
        throw new Error('Invalid plugin RPC path');
    }
    return path.replace(/^\//, '');
}

export function isPluginRPCMessage(value: unknown, pluginId: string, session: string): value is PluginRPCMessage {
    if (!value || typeof value !== 'object') return false;
    const message = value as Partial<PluginRPCMessage>;
    if (message.type !== PLUGIN_MESSAGE_TYPE || message.pluginId !== pluginId || message.session !== session ||
        typeof message.requestId !== 'string' || message.requestId.length === 0 || message.requestId.length > 128 ||
        (message.method !== 'GET' && message.method !== 'POST') || typeof message.path !== 'string') return false;
    try { pluginRPCPath(message.path); return true; } catch { return false; }
}

export async function dispatchPluginRPC(
    event: Pick<MessageEvent, 'origin' | 'source' | 'data'>,
    target: Window | null,
    pluginId: string,
    session: string,
    call: (id: string, method: 'GET' | 'POST', path: string, body?: unknown) => Promise<unknown>,
    isCurrent: () => boolean,
): Promise<void> {
    if (!target || event.source !== target || event.origin !== 'null' || !isCurrent() || !isPluginRPCMessage(event.data, pluginId, session)) return;
    const request = event.data;
    const response = { type: PLUGIN_MESSAGE_TYPE, pluginId, session, requestId: request.requestId };
    try {
        const result = await call(pluginId, request.method, pluginRPCPath(request.path), request.body);
        if (isCurrent()) target.postMessage({ ...response, result }, '*');
    } catch (error) {
        if (isCurrent()) target.postMessage({ ...response, error: error instanceof Error ? error.message : String(error) }, '*');
    }
}
