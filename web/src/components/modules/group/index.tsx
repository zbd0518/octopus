'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { GroupListItem } from './GroupListItem';
import { AutoGroupButton } from './AutoGroupButton';
import { AIRouteButton } from './AIRouteButton';
import { MaintenanceButton } from './MaintenanceButton';
import { useGroupList, type Group as RouteGroup } from '@/api/endpoints/group';
import { useModelChannelList } from '@/api/endpoints/model';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import {
    MorphingDialog,
    MorphingDialogContainer,
    MorphingDialogContent,
    MorphingDialogTrigger,
} from '@/components/ui/morphing-dialog';
import { matchesGroupEndpointFilter } from './utils';
import type { GroupEndpointFilter } from './utils';
import { CreateDialogContent } from './Create';
import { GroupedRouteModelView } from './GroupedRouteModelView';
import { buildGroupedRouteModelCategories } from './grouped-view';
import { buttonVariants } from '@/components/ui/button';
import { useSearchableList, useGroupFilter } from '@/hooks/use-searchable-list';
import { LoadingState } from '@/components/common/LoadingState';
import { ErrorState } from '@/components/common/ErrorState';
import { isGroupJumpTarget, useJumpStore } from '@/stores/jump';

function matchesGroupFilter(item: RouteGroup, filter: string) {
    if (filter === 'with-members') return (item.items?.length || 0) > 0;
    if (filter === 'empty') return (item.items?.length || 0) === 0;
    if (filter !== 'all') {
        return matchesGroupEndpointFilter(
            filter as GroupEndpointFilter,
            item.endpoint_type,
            (item.items || []).map((groupItem) => groupItem.model_name),
        );
    }
    return true;
}

export function Group() {
    const t = useTranslations('group');
    const { data: groups, isLoading, isError, refetch } = useGroupList();
    const pageKey = 'group' as const;
    const filter = useGroupFilter();
    const groupViewMode = useToolbarViewOptionsStore((s) => s.groupViewMode);
    const setGroupViewMode = useToolbarViewOptionsStore((s) => s.setGroupViewMode);
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const { data: modelChannels = [] } = useModelChannelList();
    const pendingJump = useJumpStore((s) => s.pending);
    const clearPending = useJumpStore((s) => s.clearPending);
    const [autoOpenGroupId, setAutoOpenGroupId] = useState<number | null>(null);
    const groupCardRefs = useRef<Map<number, HTMLDivElement>>(new Map());

    const pendingGroupJump =
        pendingJump && isGroupJumpTarget(pendingJump.target) ? pendingJump : null;
    const forcedGroupId = pendingGroupJump?.target.groupId ?? autoOpenGroupId;

    const registerGroupRef = useCallback((groupId: number, node: HTMLDivElement | null) => {
        if (node) {
            groupCardRefs.current.set(groupId, node);
            return;
        }
        groupCardRefs.current.delete(groupId);
    }, []);

    const handleAutoOpenConsumed = useCallback(() => {
        setAutoOpenGroupId(null);
    }, []);

    // 渠道详情 jump：切到卡片视图、置顶目标组、滚动并打开编辑器
    useEffect(() => {
        if (!pendingGroupJump) return;
        if (groupViewMode !== 'cards') {
            setGroupViewMode('cards');
        }
        setAutoOpenGroupId(pendingGroupJump.target.groupId);
        clearPending(pendingGroupJump.requestId);
    }, [pendingGroupJump, groupViewMode, setGroupViewMode, clearPending]);

    useEffect(() => {
        if (autoOpenGroupId == null) return;
        let attempts = 0;
        const maxAttempts = 20;
        let timer: number | null = null;

        const tryScroll = () => {
            const node = groupCardRefs.current.get(autoOpenGroupId);
            if (node) {
                node.scrollIntoView({ behavior: 'smooth', block: 'center' });
                return;
            }
            attempts += 1;
            if (attempts < maxAttempts) {
                timer = window.setTimeout(tryScroll, 150);
            }
        };

        timer = window.setTimeout(tryScroll, 80);
        return () => {
            if (timer !== null) window.clearTimeout(timer);
        };
    }, [autoOpenGroupId, groups?.length]);

    const { visibleItems: visibleGroups, sortedItems: sortedGroups } = useSearchableList({
        data: groups,
        pageKey,
        filter,
        filterPredicate: matchesGroupFilter,
    });

    const displayGroups = useMemo(() => {
        if (forcedGroupId == null) return visibleGroups;
        const pinned = visibleGroups.find((g) => g.id === forcedGroupId);
        // 目标组可能被当前 filter 隐藏：从全量列表补回并置顶，保证 jump 可打开
        const fallback = pinned ?? (groups ?? []).find((g) => g.id === forcedGroupId);
        if (!fallback) return visibleGroups;
        return [fallback, ...visibleGroups.filter((g) => g.id !== forcedGroupId)];
    }, [visibleGroups, forcedGroupId, groups]);

    const groupedSourceGroups = useMemo(
        () => sortedGroups.filter((group) => matchesGroupFilter(group, filter)),
        [sortedGroups, filter],
    );

    const groupedCategories = useMemo(
        () => buildGroupedRouteModelCategories(groupedSourceGroups, modelChannels, searchTerm, { assignedGroups: groups ?? [] }),
        [groupedSourceGroups, modelChannels, searchTerm, groups],
    );

    if (isLoading) {
        return (
            <div className="flex h-full min-h-0 flex-col overflow-y-auto overscroll-contain rounded-t-xl pb-3 md:pb-4">
                <section className="relative min-h-0 flex-1">
                    <LoadingState />
                </section>
            </div>
        );
    }

    if (isError) {
        return (
            <div className="flex h-full min-h-0 flex-col overflow-y-auto overscroll-contain rounded-t-xl pb-3 md:pb-4">
                <section className="relative min-h-0 flex-1">
                    <ErrorState onRetry={() => refetch()} />
                </section>
            </div>
        );
    }

    if (groups && groups.length === 0) {
        return (
            <div className="overflow-y-auto overscroll-contain rounded-t-xl px-3 py-4 pb-3 md:px-4 md:py-6 md:pb-6">
                <section className="relative w-full max-w-5xl rounded-xl border border-border bg-card p-5 text-card-foreground md:p-7">
                    <div className="relative flex flex-col gap-5 rounded-xl border border-border bg-card p-5 md:p-6">
                        <div className="space-y-3">
                            <div className="inline-flex w-fit items-center gap-2 rounded-md border border-border bg-card px-2.5 py-1 text-xs font-medium text-muted-foreground">
                                {t('card.empty')}
                            </div>
                            <div className="space-y-2">
                                <h2 className="max-w-xl text-2xl font-semibold tracking-tight text-foreground md:text-3xl">
                                    {t('emptyState.title')}
                                </h2>
                                <p className="max-w-2xl text-sm leading-6 text-muted-foreground">
                                    {t('emptyState.description')}
                                </p>
                            </div>
                        </div>

                        <div className="flex flex-col gap-3 sm:flex-row">
                            <AutoGroupButton variant="default" className="h-11 rounded-lg justify-start px-4 sm:flex-1" />
                            <AIRouteButton variant="default" className="h-11 rounded-lg justify-start px-4 sm:flex-1" />
                            <MaintenanceButton className="h-11 rounded-lg justify-start px-4 sm:flex-1" />
                            <MorphingDialog>
                                <MorphingDialogTrigger className={buttonVariants({ variant: 'outline', className: 'h-11 min-w-0 sm:min-w-36 justify-start rounded-lg border-border bg-card px-4 hover:bg-muted sm:flex-1' })}>
                                    {t('create.submit')}
                                </MorphingDialogTrigger>
                                <MorphingDialogContainer>
                                    <MorphingDialogContent className="h-[calc(100dvh-2.5rem)] w-[min(100vw-2rem,92rem)] max-w-full flex-col overflow-hidden rounded-xl border border-border bg-card px-4 pt-4 pb-[calc(env(safe-area-inset-bottom)+1rem)] text-card-foreground md:h-[calc(100dvh-3rem)] md:px-6 md:py-5">
                                        <CreateDialogContent />
                                    </MorphingDialogContent>
                                </MorphingDialogContainer>
                            </MorphingDialog>
                        </div>
                    </div>
                </section>
            </div>
        );
    }

    return (
        <div className="flex h-full min-h-0 flex-col overflow-y-auto overscroll-contain rounded-t-xl pb-3 md:pb-4">
            <section className="relative min-h-0 flex-1">
                {groupViewMode === 'grouped' ? (
                    <GroupedRouteModelView categories={groupedCategories} />
                ) : (
                    <VirtualizedGrid
                        items={displayGroups}
                        columns={{ default: 1, sm: 2, md: 2, lg: 3 }}
                        estimateItemHeight={72}
                        getItemKey={(group, index) => group.id ?? `group-${index}`}
                        renderItem={(group) => (
                            <div
                                ref={(node) => {
                                    if (typeof group.id === 'number') {
                                        registerGroupRef(group.id, node);
                                    }
                                }}
                            >
                                <GroupListItem
                                    group={group}
                                    autoOpenEditor={autoOpenGroupId === group.id}
                                    onAutoOpenEditorConsumed={handleAutoOpenConsumed}
                                />
                            </div>
                        )}
                        bottomPaddingClassName="pb-3 md:pb-4"
                    />
                )}
            </section>
        </div>
    );
}
