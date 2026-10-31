export const NODE_PROBE_CODES = [
    'ok', 'disabled', 'offline', 'busy', 'timeout', 'cancelled',
    'local_unavailable', 'local_authentication', 'local_http',
    'invalid_models', 'no_models', 'upstream_failed',
] as const;

export type NodeProbeCode = typeof NODE_PROBE_CODES[number];

export type NodeProbeResult = {
    passed: boolean;
    code: NodeProbeCode;
    latency_ms: number;
    status_code: number;
    model_count: number;
};

export function nodeProbeCode(code: string | undefined): NodeProbeCode {
    return NODE_PROBE_CODES.find((known) => known === code) ?? 'upstream_failed';
}
