import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';

export type AccountPool = {
    id: number;
    name: string;
    description: string;
    strategy: string;
    default_concurrency: number;
    cooldown_base_sec: number;
    enabled: boolean;
    created_at: string;
    updated_at: string;
};

export type PoolAccountExtra = {
    project_id?: string;
    tier_id?: string;
    oauth_type?: string;
    auth_mode?: string;
    privacy_mode?: string;
    header_overrides_enabled?: boolean;
    header_overrides?: Record<string, string>;
    tls_fingerprint_profile?: string;
    refresh_failure_count?: number;
    next_refresh_allowed_at?: number;
};

export type PoolAccount = {
    id: number;
    pool_id: number;
    name: string;
    platform: string;
    type: string;
    models: string;
    credentials: string;
    base_url: string;
    quota: string;
    status: string;
    schedulable: boolean;
    priority: number;
    concurrency: number;
    proxy_config_id?: number | null;
    rate_limit_reset_at: number;
    overload_until: number;
    token_expires_at: number;
    total_requests: number;
    total_errors: number;
    total_tokens: number;
    last_used_at?: string | null;
    error_message: string;
    notes: string;
    // P0–P3 新增
    temp_unsched_until: number;
    temp_unsched_reason: string;
    auth_error_count: number;
    auth_error_window_start: number;
    expires_at: number;
    auto_pause_on_expired: boolean;
    extra: string;
    weight: number;
    load_factor: number;
    created_at: string;
    updated_at: string;
};

export type CreatePoolRequest = {
    name: string;
    description?: string;
    strategy?: string;
    default_concurrency?: number;
    cooldown_base_sec?: number;
    enabled?: boolean;
};

export type UpdatePoolRequest = {
    id: number;
    name?: string;
    description?: string;
    strategy?: string;
    default_concurrency?: number;
    cooldown_base_sec?: number;
    enabled?: boolean;
};

export type PoolAccountRequest = {
    name?: string;
    platform?: string;
    type?: string;
    models?: string;
    credentials?: string;
    base_url?: string;
    status?: string;
    schedulable?: boolean;
    priority?: number;
    concurrency?: number;
    proxy_config_id?: number | null;
    notes?: string;
    token_expires_at?: number;
    weight?: number;
    load_factor?: number;
    auto_pause_on_expired?: boolean;
    expires_at?: number;
    extra?: string;
};

export type CreatePoolAccountRequest = PoolAccountRequest;
export type UpdatePoolAccountRequest = PoolAccountRequest;

export type AccountTestResult = {
    success: boolean;
    status: number;
    latency_ms: number;
    error?: string;
};

export type QuotaResult = {
    used: number;
    total: number;
    reset_at: number;
    raw?: string;
};

// --- Queries ---

export function usePoolList() {
    return useQuery({
        queryKey: ['pools'],
        queryFn: () => apiClient.get<AccountPool[]>('/api/v1/pool/list'),
    });
}

export function usePoolAccounts(poolId: number | null) {
    return useQuery({
        queryKey: ['pools', poolId, 'accounts'],
        queryFn: () => apiClient.get<PoolAccount[]>(`/api/v1/pool/${poolId}/account/list`),
        enabled: poolId !== null,
    });
}

export function usePoolAccount(poolId: number | null, accountId: number | null) {
    return useQuery({
        queryKey: ['pools', poolId, 'accounts', accountId],
        queryFn: () => apiClient.get<PoolAccount>(`/api/v1/pool/${poolId}/account/${accountId}`),
        enabled: poolId !== null && accountId !== null,
    });
}

// --- Mutations ---

export function useCreatePool() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: CreatePoolRequest) => apiClient.post<AccountPool>('/api/v1/pool/create', data),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['pools'] }),
    });
}

export function useUpdatePool() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: UpdatePoolRequest) => apiClient.post('/api/v1/pool/update', data),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['pools'] }),
    });
}

export function useDeletePool() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: number) => apiClient.delete(`/api/v1/pool/delete/${id}`),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['pools'] }),
    });
}

export function useCreatePoolAccount(poolId: number) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: CreatePoolAccountRequest) => apiClient.post<PoolAccount>(`/api/v1/pool/${poolId}/account/create`, data),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['pools', poolId, 'accounts'] }),
    });
}

export function useUpdatePoolAccount(poolId: number) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ accountId, data }: { accountId: number; data: UpdatePoolAccountRequest }) =>
            apiClient.post(`/api/v1/pool/${poolId}/account/update/${accountId}`, data),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['pools', poolId, 'accounts'] }),
    });
}

export function useDeletePoolAccount(poolId: number) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (accountId: number) => apiClient.delete(`/api/v1/pool/${poolId}/account/delete/${accountId}`),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['pools', poolId, 'accounts'] }),
    });
}

export function useTestPoolAccount(poolId: number) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ accountId, model }: { accountId: number; model: string }) =>
            apiClient.post<AccountTestResult>(`/api/v1/pool/${poolId}/account/test`, { account_id: accountId, model }),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['pools', poolId, 'accounts'] }),
    });
}

export function useFetchPoolQuota(poolId: number) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (accountId: number) =>
            apiClient.post<QuotaResult>(`/api/v1/pool/${poolId}/account/quota/${accountId}`, {}),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['pools', poolId, 'accounts'] }),
    });
}

export function useRefreshPoolToken(poolId: number) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (accountId: number) =>
            apiClient.post(`/api/v1/pool/${poolId}/account/refresh-token/${accountId}`, {}),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['pools', poolId, 'accounts'] }),
    });
}

export function useRecoverPoolAccount(poolId: number) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (accountId: number) =>
            apiClient.post(`/api/v1/pool/${poolId}/account/recover/${accountId}`, {}),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['pools', poolId, 'accounts'] }),
    });
}

export function useTempUnschedPoolAccount(poolId: number) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ accountId, minutes, reason }: { accountId: number; minutes: number; reason: string }) =>
            apiClient.post(`/api/v1/pool/${poolId}/account/temp-unsched/${accountId}`, { minutes, reason }),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['pools', poolId, 'accounts'] }),
    });
}

export type BatchItemError = { id: number; error: string };
export type BatchAccountsResponse = { ok: number; failed: BatchItemError[] };
export type BatchTestItemResult = { id: number; success: boolean; latency_ms: number; error?: string };

export function useBatchPoolAccounts(poolId: number) {
    const queryClient = useQueryClient();
    const invalidate = () => queryClient.invalidateQueries({ queryKey: ['pools', poolId, 'accounts'] });
    const refresh = useMutation({
        mutationFn: (accountIds: number[]) =>
            apiClient.post<BatchAccountsResponse>(`/api/v1/pool/${poolId}/account/batch-refresh`, { account_ids: accountIds }),
        onSuccess: invalidate,
    });
    const clearError = useMutation({
        mutationFn: (accountIds: number[]) =>
            apiClient.post<BatchAccountsResponse>(`/api/v1/pool/${poolId}/account/batch-clear-error`, { account_ids: accountIds }),
        onSuccess: invalidate,
    });
    const test = useMutation({
        mutationFn: ({ accountIds, model }: { accountIds: number[]; model: string }) =>
            apiClient.post<BatchTestItemResult[]>(`/api/v1/pool/${poolId}/account/batch-test`, { account_ids: accountIds, model }),
        onSuccess: invalidate,
    });
    return { refresh, clearError, test };
}

export type PoolAccountExport = {
    name: string;
    platform: string;
    type: string;
    models: string;
    base_url: string;
    priority: number;
    concurrency: number;
    weight: number;
    load_factor: number;
    notes: string;
    extra?: string;
    credentials: string;
};

export function useExportPoolAccounts(poolId: number) {
    return useMutation({
        mutationFn: () => apiClient.get<PoolAccountExport[]>(`/api/v1/pool/${poolId}/account/export`),
    });
}

export function useImportPoolAccounts() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ poolId, accounts }: { poolId: number; accounts: string }) =>
            apiClient.post<{ imported: number }>('/api/v1/pool/import', { pool_id: poolId, accounts }),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['pools'] }),
    });
}
