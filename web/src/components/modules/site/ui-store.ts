'use client';

import { create } from 'zustand';
import type { CheckinActiveFilterStatus } from './checkin-status';

type SiteUIHandlers = {
    openCreateDialog: () => void;
    openImportDialog: () => void;
    openArchivedDialog: () => void;
    syncAll: () => void;
    checkinAll: () => void;
};

type CheckinFilterStatusesUpdate =
    | CheckinActiveFilterStatus[]
    | ((current: CheckinActiveFilterStatus[]) => CheckinActiveFilterStatus[]);

type PlatformFiltersUpdate =
    | string[]
    | ((current: string[]) => string[]);

interface SiteUIState {
    handlers: SiteUIHandlers;
    checkinFilterStatuses: CheckinActiveFilterStatus[];
    /** 平台类型筛选（SitePlatform 字符串值）；空数组 = 全部 */
    platformFilters: string[];
    setCheckinFilterStatuses: (value: CheckinFilterStatusesUpdate) => void;
    setPlatformFilters: (value: PlatformFiltersUpdate) => void;
    setHandlers: (handlers: Partial<SiteUIHandlers>) => void;
    resetHandlers: () => void;
    requestOpenCreateDialog: () => void;
    requestOpenImportDialog: () => void;
    requestOpenArchivedDialog: () => void;
    requestSyncAll: () => void;
    requestCheckinAll: () => void;
}

const noop = () => {};

const defaultHandlers: SiteUIHandlers = {
    openCreateDialog: noop,
    openImportDialog: noop,
    openArchivedDialog: noop,
    syncAll: noop,
    checkinAll: noop,
};

export const useSiteUIStore = create<SiteUIState>((set, get) => ({
    handlers: defaultHandlers,
    checkinFilterStatuses: [],
    platformFilters: [],
    setCheckinFilterStatuses: (value) =>
        set((state) => ({
            checkinFilterStatuses:
                typeof value === 'function' ? value(state.checkinFilterStatuses) : value,
        })),
    setPlatformFilters: (value) =>
        set((state) => ({
            platformFilters:
                typeof value === 'function' ? value(state.platformFilters) : value,
        })),
    setHandlers: (handlers) =>
        set((state) => ({
            handlers: { ...state.handlers, ...handlers },
        })),
    resetHandlers: () => set({ handlers: defaultHandlers }),
    requestOpenCreateDialog: () => get().handlers.openCreateDialog(),
    requestOpenImportDialog: () => get().handlers.openImportDialog(),
    requestOpenArchivedDialog: () => get().handlers.openArchivedDialog(),
    requestSyncAll: () => get().handlers.syncAll(),
    requestCheckinAll: () => get().handlers.checkinAll(),
}));
