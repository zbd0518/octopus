'use client';

import { create } from 'zustand';

/**
 * 「在站点页签内打开某站点的渠道弹窗」的跨组件请求通道。
 *
 * 背景：站点页签与站点渠道页签是两套独立的卡片/弹窗树，且两侧都用
 * useJumpStore（跨页签定位）来跳转。之前「查看站点渠道」只能走
 * requestJump({kind:'site-channel-*'})，它会先把页签切到站点渠道，
 * 再靠 pending 副作用滚动/开弹窗 —— 多层异步叠加导致定位经常失效
 * （跳到列表开头、或完全打不开）。
 *
 * 这里改为：请求方（站点页签的菜单）只登记一条「打开某站点渠道弹窗，
 * 可选定位到某账号」的意图，由站点页签内常驻的 <SiteChannelDialogHost/>
 * 消费并直接开弹窗。请求落在本 store，不受页签切换/列表滚动/卡片虚拟化影响，
 * 因此定位不会再丢。
 */
export type SiteChannelDialogRequest = {
    /** 自增序号，保证连续两次请求同一站点也能触发重新打开 */
    requestId: number;
    siteId: number;
    /** 需要弹窗打开后定位到的账号；不传则用第一个账号 */
    accountId?: number;
};

interface SiteChannelDialogRequestState {
    request: SiteChannelDialogRequest | null;
    /** 请求方调用；accountId 省略表示只打开到站点级 */
    requestSiteChannelDialog: (siteId: number, accountId?: number) => void;
    /** 消费方在开始处理后清空，避免重复打开 */
    clearRequest: (requestId?: number) => void;
}

export const useSiteChannelDialogRequestStore = create<SiteChannelDialogRequestState>((set, get) => ({
    request: null,
    requestSiteChannelDialog: (siteId, accountId) => {
        const last = get().request;
        // 同一站点+账号的重复请求也要能重新触发（例如弹窗已关闭后再点一次）。
        const requestId = (last?.requestId ?? 0) + 1;
        set({ request: { requestId, siteId, accountId } });
    },
    clearRequest: (requestId) =>
        set((state) => {
            if (!state.request) return state;
            if (typeof requestId === 'number' && state.request.requestId !== requestId) {
                return state;
            }
            return { request: null };
        }),
}));