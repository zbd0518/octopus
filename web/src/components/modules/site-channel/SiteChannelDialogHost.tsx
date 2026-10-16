'use client';

import { useEffect, useMemo, useState } from 'react';

import {
    MorphingDialog,
    MorphingDialogContainer,
    MorphingDialogContent,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { useJumpStore } from '@/stores/jump';
import { useSiteChannelList, type SiteChannelCard } from '@/api/endpoints/site-channel';

import { SiteChannelDialog } from './index';
import { useSiteChannelDialogRequestStore } from './dialog-request-store';

/**
 * 常驻在站点页签内的「站点渠道弹窗宿主」。
 *
 * 消费 useSiteChannelDialogRequestStore 的请求：命中站点后直接打开
 * SiteChannelDialog，可选定位到指定账号。请求不再经过「切页签 → 滚列表 →
 * 开弹窗」的多层异步，因此「查看站点渠道」不会切走页签、也不会定位失败。
 *
 * 关键修复：
 * - 不再在打开瞬间快照整个 card 对象。
 * - 始终从 useSiteChannelList 的最新 query 数据派生 liveCard。
 * - 批量移动端点格式（applyRouteChange）成功后，mutation 的 onSuccess 会
 *   通过 setQueryData 更新同一 query key，host 能拿到新 card 并把更新后的
 *   groups/models 透传给 SiteChannelDialog，UI 立即刷新。
 * - 关闭弹窗后清理目标，下次打开会重新解析最新数据。
 */
export function SiteChannelDialogHost() {
    return (
        <MorphingDialog disableSharedLayout>
            <SiteChannelDialogHostInner />
        </MorphingDialog>
    );
}

function SiteChannelDialogHostInner() {
    const { isOpen, setIsOpen } = useMorphingDialog();
    const { data, isLoading } = useSiteChannelList();
    const request = useSiteChannelDialogRequestStore((state) => state.request);
    const clearRequest = useSiteChannelDialogRequestStore((state) => state.clearRequest);
    const requestJump = useJumpStore((state) => state.requestJump);

    // 只保留“打开哪个站点 + 哪个账号”的身份标识，不快照 card。
    // 这样当 mutation（批量移动端点格式等）通过 setQueryData 更新缓存后，
    // 我们能从当前 data 里拿到最新的 card 并重新渲染内容。
    const [openIdentity, setOpenIdentity] = useState<{
        siteId: number;
        accountId: number | null;
    } | null>(null);

    // 每次 render 都从最新 query 结果里取当前站点的 card。
    // data 变化（mutation 更新）→ 自动拿到带新 route_type / groups 的 card。
    const liveCard = useMemo<SiteChannelCard | null>(() => {
        if (!openIdentity) return null;
        return (data ?? []).find((c) => c.site_id === openIdentity.siteId) ?? null;
    }, [data, openIdentity]);

    // 当前真正要展示的账号（优先用 identity 里的，找不到就退第一个）
    const resolvedAccountId = useMemo(() => {
        if (!openIdentity) return null;
        if (!liveCard) return openIdentity.accountId;
        const exists = liveCard.accounts.some((a) => a.account_id === openIdentity.accountId);
        if (exists) return openIdentity.accountId;
        return liveCard.accounts[0]?.account_id ?? null;
    }, [openIdentity, liveCard]);

    useEffect(() => {
        if (!request) return;

        // 如果已经打开同一个站点，只是想切账号，直接更新 identity 的 accountId
        if (openIdentity && openIdentity.siteId === request.siteId) {
            const found = (data ?? []).find((c) => c.site_id === request.siteId);
            const nextAccount =
                request.accountId && found?.accounts.some((a) => a.account_id === request.accountId)
                    ? request.accountId
                    : openIdentity.accountId;
            if (nextAccount !== openIdentity.accountId) {
                setOpenIdentity({ siteId: request.siteId, accountId: nextAccount });
            }
            clearRequest(request.requestId);
            return;
        }

        const found = (data ?? []).find((item) => item.site_id === request.siteId);
        if (!found) {
            // 列表还在加载，先不处理；加载完还找不到才丢弃（站点已被删）
            if (isLoading) return;
            clearRequest(request.requestId);
            return;
        }

        const accountId =
            request.accountId && found.accounts.some((a) => a.account_id === request.accountId)
                ? request.accountId
                : null;

        setOpenIdentity({ siteId: request.siteId, accountId });
        setIsOpen(true);
        clearRequest(request.requestId);
    }, [request, data, isLoading, clearRequest, setIsOpen, openIdentity]);

    // 弹窗关闭后清理身份，下次打开会重新从最新 data 解析 card
    useEffect(() => {
        if (!isOpen) {
            setOpenIdentity(null);
        }
    }, [isOpen]);

    if (!isOpen || !openIdentity) return null;

    // query 里暂时没有这个站点（极端情况：刚删、或者数据同步延迟）
    // 此时不渲染内容，避免用旧快照
    if (!liveCard) return null;

    return (
        <MorphingDialogContainer>
            <MorphingDialogContent className="!w-[calc(100vw-1rem)] !max-w-[calc(100vw-1rem)] overflow-hidden rounded-[2rem] bg-background max-h-[90vh] md:!w-[min(96vw,92rem)] md:!max-w-[min(96vw,92rem)]">
                <SiteChannelDialog
                    // 用 siteId + accountId 做 key：
                    // - 切换账号时 remount，保证 SiteChannelDialog 内部的 activeAccountId 等状态重置
                    // - 同一个站点内数据更新（route 改变）时 key 不变，card prop 变 → 子组件收到新 card 自然重渲染
                    key={`${liveCard.site_id}:${resolvedAccountId ?? 'first'}`}
                    card={liveCard}
                    jumpRequest={null}
                    onJumpHandled={() => {}}
                    initialAccountId={resolvedAccountId}
                    onNavigateToSite={() => requestJump({ kind: 'site-card', siteId: liveCard.site_id })}
                    onNavigateToSiteAccount={(id) =>
                        requestJump({ kind: 'site-account', siteId: liveCard.site_id, accountId: id })
                    }
                    onNavigateToChannel={(channelId) => requestJump({ kind: 'channel-card', channelId })}
                />
            </MorphingDialogContent>
        </MorphingDialogContainer>
    );
}