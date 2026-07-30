'use client';

import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type LogFieldName =
    | 'endpointType'
    | 'channelName'
    | 'actualModel'
    | 'apiKeyName'
    | 'clientIP'
    | 'cost'
    | 'tps'
    | 'cacheHitRate'
    | 'reasoningEffort'
    | 'reasoningTokens';

export type LogFieldVisibility = Record<LogFieldName, boolean>;

export const DEFAULT_LOG_FIELD_VISIBILITY: LogFieldVisibility = {
    endpointType: true,
    channelName: true,
    actualModel: true,
    apiKeyName: true,
    clientIP: true,
    cost: true,
    tps: true,
    cacheHitRate: true,
    reasoningEffort: true,
    reasoningTokens: true,
};

type LogFieldVisibilityState = {
    visibility: LogFieldVisibility;
    toggleField: (field: LogFieldName) => void;
    resetFields: () => void;
};

export const useLogFieldVisibilityStore = create<LogFieldVisibilityState>()(
    persist(
        (set) => ({
            visibility: { ...DEFAULT_LOG_FIELD_VISIBILITY },
            toggleField: (field) =>
                set((state) => ({
                    visibility: {
                        ...state.visibility,
                        [field]: !state.visibility[field],
                    },
                })),
            resetFields: () =>
                set({ visibility: { ...DEFAULT_LOG_FIELD_VISIBILITY } }),
        }),
        {
            name: 'log-field-visibility-storage',
            partialize: (state) => ({
                visibility: state.visibility,
            }),
        },
    ),
);

export function useLogFieldVisibility() {
    return useLogFieldVisibilityStore((s) => s.visibility);
}

/**
 * 日志模型搜索文本，在标题栏搜索框与 Log 组件筛选逻辑之间共享。
 */
export const useLogModelSearchStore = create<{
    modelSearch: string;
    setModelSearch: (value: string) => void;
}>()((set) => ({
    modelSearch: '',
    setModelSearch: (value) => set({ modelSearch: value }),
}));

/**
 * 日志列表自动刷新间隔（秒）。0 表示关闭。
 * 属于浏览器本地偏好（不同客户端可各自设置），持久化到 localStorage。
 */
export const LOG_AUTO_REFRESH_OPTIONS = [0, 5, 10, 30] as const;
export type LogAutoRefreshInterval = (typeof LOG_AUTO_REFRESH_OPTIONS)[number];

export const useLogAutoRefreshStore = create<{
    interval: LogAutoRefreshInterval;
    setInterval: (value: LogAutoRefreshInterval) => void;
}>()(
    persist(
        (set) => ({
            interval: 0 as LogAutoRefreshInterval,
            setInterval: (value) => set({ interval: value }),
        }),
        {
            name: 'log-auto-refresh-storage',
            partialize: (state) => ({ interval: state.interval }),
        },
    ),
);

