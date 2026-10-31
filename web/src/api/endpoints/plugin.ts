import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import { pluginRPCPath } from '@/components/modules/plugin/rpc';
import type { PluginUIBundle } from '@/components/modules/plugin/ui-bundle';

export interface PluginRecord {
    id: string;
    name: string;
    version: string;
    manifest: string;
    config: string;
    sha256: string;
    status: 'enabled' | 'disabled' | 'error' | 'needs_reinstall' | 'uninstalling';
    enabled: boolean;
    schema_version: number;
    last_error?: string;
    calls: number;
    failures: number;
    timeouts: number;
}

export function usePlugins() {
    return useQuery({ queryKey: ['plugins'], queryFn: () => apiClient.get<{ plugins: PluginRecord[]; can_write: boolean }>('/api/v1/plugins') });
}

function usePluginMutation<TArgs, TResult = PluginRecord>(fn: (args: TArgs) => Promise<TResult>) {
    const queryClient = useQueryClient();
    return useMutation({ mutationFn: fn, onSuccess: () => queryClient.invalidateQueries({ queryKey: ['plugins'] }) });
}

export const useInstallPlugin = () => usePluginMutation((file: File) => {
    const form = new FormData();
    form.append('file', file, file.name);
    return apiClient.upload<PluginRecord>('/api/v1/plugins/install', form);
});
export function useEnablePlugin() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
            apiClient.post<PluginRecord>(`/api/v1/plugins/${encodeURIComponent(id)}/${enabled ? 'enable' : 'disable'}`),
        onSuccess: async (plugin) => {
            const keys = plugin.id === 'octopus.magpie-node'
                ? [['plugins'], ['channels', 'list'], ['settings', 'list'], ['nodes'], ['models']]
                : [['plugins']];
            await Promise.all(keys.map(queryKey => queryClient.invalidateQueries({ queryKey })));
        },
    });
}
export const useConfigurePlugin = () => usePluginMutation(({ id, config }: { id: string; config: Record<string, unknown> }) =>
    apiClient.put<PluginRecord>(`/api/v1/plugins/${encodeURIComponent(id)}/config`, { config }));
export const useUninstallPlugin = () => usePluginMutation(({ id, deleteData }: { id: string; deleteData: boolean }) =>
    apiClient.request<unknown>(`/api/v1/plugins/${encodeURIComponent(id)}`, { method: 'DELETE', body: { delete_data: deleteData } }));

export async function pluginRPC(id: string, method: 'GET' | 'POST', path: string, body?: unknown) {
    const suffix = pluginRPCPath(path);
    return apiClient.request<unknown>(`/api/v1/plugins/${encodeURIComponent(id)}/rpc/${suffix}`, { method, body: method === 'GET' ? undefined : body });
}

export function getPluginUIBundle(id: string, version: string): Promise<PluginUIBundle> {
    return apiClient.get(`/api/v1/plugins/${encodeURIComponent(id)}/ui-bundle`, { version });
}
