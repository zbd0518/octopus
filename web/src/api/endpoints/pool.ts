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
    backup_proxy_config_id?: number;
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
    // B4-#13 proxy fallback: non-null means the account currently runs on its
    // backup proxy and this holds the original proxy_config_id.
    proxy_fallback_origin_id?: number | null;
    created_at: string;
    updated_at: string;
};

// --- Scheduled tests (B4-#11) ---

export type PoolScheduledTest = {
    id: number;
    pool_id: number;
    account_id?: number | null;
    cron_expr: string;
    enabled: boolean;
    auto_recover: boolean;
    last_run_at: number;
    next_run_at: number;
    created_at: string;
    updated_at: string;
};

export type PoolScheduledTestResult = {
    id: number;
    test_id: number;
    account_id: number;
    success: boolean;
    detail: string;
    duration_ms: number;
    created_at: string;
};

export type PoolScheduledTestRequest = {
    account_id?: number | null;
    cron_expr: string;
    enabled?: boolean;
    auto_recover?: boolean;
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
export type PoolAccountsDeleteResponse = { deleted: number };
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
    const deleteSelected = useMutation({
        mutationFn: (accountIds: number[]) =>
            apiClient.post<PoolAccountsDeleteResponse>(`/api/v1/pool/${poolId}/account/batch-delete`, { account_ids: accountIds }),
        onSuccess: invalidate,
    });
    return { refresh, clearError, test, deleteSelected };
}

export function useClearPoolAccounts(poolId: number) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: () => apiClient.post<PoolAccountsDeleteResponse>(`/api/v1/pool/${poolId}/account/clear`, {}),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['pools', poolId, 'accounts'] }),
    });
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
    extra?: unknown;
    credentials: string;
};

export function useExportPoolAccounts(poolId: number) {
    // POST (not GET): the backend audit middleware short-circuits every
    // non-writing method, so the export route is only auditable as POST.
    return useMutation({
        mutationFn: () => apiClient.post<PoolAccountExport[]>(`/api/v1/pool/${poolId}/account/export`),
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

// --- Scheduled test plan mutations (B4-#11) ---

export function usePoolScheduledTests(poolId: number | null) {
    return useQuery({
        queryKey: ['pools', poolId, 'scheduled-tests'],
        queryFn: () => apiClient.get<PoolScheduledTest[]>(`/api/v1/pool/${poolId}/scheduled-test/list`),
        enabled: poolId !== null,
    });
}

export function usePoolScheduledTestResults(poolId: number | null, testId: number | null) {
    return useQuery({
        queryKey: ['pools', poolId, 'scheduled-tests', testId, 'results'],
        queryFn: () => apiClient.get<PoolScheduledTestResult[]>(`/api/v1/pool/${poolId}/scheduled-test/results/${testId}`),
        enabled: poolId !== null && testId !== null,
    });
}

function invalidateScheduledTests(queryClient: ReturnType<typeof useQueryClient>, poolId: number) {
    void queryClient.invalidateQueries({ queryKey: ['pools', poolId, 'scheduled-tests'] });
    void queryClient.invalidateQueries({ queryKey: ['pools', poolId, 'accounts'] });
}

export function useCreatePoolScheduledTest(poolId: number) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: PoolScheduledTestRequest) =>
            apiClient.post<PoolScheduledTest>(`/api/v1/pool/${poolId}/scheduled-test/create`, data),
        onSuccess: () => invalidateScheduledTests(queryClient, poolId),
    });
}

export function useUpdatePoolScheduledTest(poolId: number) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ testId, data }: { testId: number; data: PoolScheduledTestRequest }) =>
            apiClient.post<PoolScheduledTest>(`/api/v1/pool/${poolId}/scheduled-test/update/${testId}`, data),
        onSuccess: () => invalidateScheduledTests(queryClient, poolId),
    });
}

export function useDeletePoolScheduledTest(poolId: number) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (testId: number) => apiClient.delete(`/api/v1/pool/${poolId}/scheduled-test/delete/${testId}`),
        onSuccess: () => invalidateScheduledTests(queryClient, poolId),
    });
}

// --- Proxy fallback (B4-#13) ---

export function useRestorePoolAccountProxy(poolId: number) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (accountId: number) =>
            apiClient.post(`/api/v1/pool/${poolId}/account/restore-proxy/${accountId}`, {}),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['pools', poolId, 'accounts'] }),
    });
}

// --- Temp-unsched rules (B4-#12) ---

export type PoolUnschedRule = {
    id: number;
    name: string;
    match_status_code?: number | null;
    match_keyword: string;
    duration_minutes: number;
    enabled: boolean;
    sort_order: number;
    created_at: string;
    updated_at: string;
};

export type PoolUnschedRuleRequest = {
    name?: string;
    match_status_code?: number | null;
    match_keyword?: string;
    duration_minutes: number;
    enabled?: boolean;
    sort_order?: number;
};

export function usePoolUnschedRules() {
    return useQuery({
        queryKey: ['pools', 'unsched-rules'],
        queryFn: () => apiClient.get<PoolUnschedRule[]>('/api/v1/pool/unsched-rules/list'),
    });
}

function invalidateUnschedRules(queryClient: ReturnType<typeof useQueryClient>) {
    void queryClient.invalidateQueries({ queryKey: ['pools', 'unsched-rules'] });
}

export function useCreatePoolUnschedRule() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: PoolUnschedRuleRequest) => apiClient.post<PoolUnschedRule>('/api/v1/pool/unsched-rules/create', data),
        onSuccess: () => invalidateUnschedRules(queryClient),
    });
}

export function useUpdatePoolUnschedRule() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ id, data }: { id: number; data: PoolUnschedRuleRequest }) =>
            apiClient.post(`/api/v1/pool/unsched-rules/update/${id}`, data),
        onSuccess: () => invalidateUnschedRules(queryClient),
    });
}

export function useDeletePoolUnschedRule() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: number) => apiClient.delete(`/api/v1/pool/unsched-rules/delete/${id}`),
        onSuccess: () => invalidateUnschedRules(queryClient),
    });
}
