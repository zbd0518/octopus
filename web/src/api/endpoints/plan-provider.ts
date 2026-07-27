import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import type { ProxyMode } from './proxy-pool';

export interface PlanProviderCategoryInfo {
    category: string;
    name: string;
    type: 'balance' | 'tokenplan';
    base_url: string;
    models: string;
    description: string;
    help_url: string;
}

export interface PlanProvider {
    id: number;
    name: string;
    category: string;
    provider_type: 'balance' | 'tokenplan';
    api_key: string;
    forward_api_key: string;
    base_url: string;
    channel_id: number;
    balance: number;
    balance_used: number;
    quota_total: number;
    quota_used: number;
    quota_reset_at: string | null;
    weekly_total: number;
    weekly_used: number;
    weekly_reset_at: string | null;
    five_hour_total: number;
    five_hour_used: number;
    five_hour_reset_at: string | null;
    status: string;
    last_refresh: string | null;
    channel_name: string;
    channel_enabled: boolean;
    models: string;
}

// --- Balance Providers ---

export function useBalanceProviders() {
    return useQuery<PlanProvider[]>({
        queryKey: ['plan-provider', 'balance', 'list'],
        queryFn: () => apiClient.get<PlanProvider[]>('/api/v1/plan-provider/balance/list'),
        refetchInterval: 60000,
    });
}

export function useTokenPlanProviders() {
    return useQuery<PlanProvider[]>({
        queryKey: ['plan-provider', 'tokenplan', 'list'],
        queryFn: () => apiClient.get<PlanProvider[]>('/api/v1/plan-provider/tokenplan/list'),
        refetchInterval: 60000,
    });
}

export function useBalanceCategories() {
    return useQuery<PlanProviderCategoryInfo[]>({
        queryKey: ['plan-provider', 'balance', 'categories'],
        queryFn: () => apiClient.get<PlanProviderCategoryInfo[]>('/api/v1/plan-provider/balance/categories'),
        staleTime: Infinity,
    });
}

export function useTokenPlanCategories() {
    return useQuery<PlanProviderCategoryInfo[]>({
        queryKey: ['plan-provider', 'tokenplan', 'categories'],
        queryFn: () => apiClient.get<PlanProviderCategoryInfo[]>('/api/v1/plan-provider/tokenplan/categories'),
        staleTime: Infinity,
    });
}

export function useAddPlanProvider() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: {
            category: string;
            api_key: string;
            forward_api_key?: string;
            name?: string;
            // 代理配置：目前仅 Codex 类生效（chatgpt.com 国内不可直连）
            proxy_mode?: ProxyMode;
            proxy_config_id?: number | null;
        }) => apiClient.post('/api/v1/plan-provider/add', data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['plan-provider'] });
        },
    });
}

export function useRefreshPlanProvider() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: number) =>
            apiClient.post(`/api/v1/plan-provider/refresh/${id}`),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['plan-provider'] });
        },
    });
}

export function useUpdatePlanProviderCredentials() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: {
            id: number;
            api_key: string;
            forward_api_key?: string;
        }) => apiClient.put(`/api/v1/plan-provider/credentials/${data.id}`, {
            api_key: data.api_key,
            forward_api_key: data.forward_api_key,
        }),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['plan-provider'] });
        },
    });
}

export function useDeletePlanProvider() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: number) =>
            apiClient.delete(`/api/v1/plan-provider/${id}`),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['plan-provider'] });
        },
    });
}
