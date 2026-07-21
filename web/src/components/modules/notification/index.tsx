'use client';

import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { Archive, Bell, Check, Clock, Inbox, Loader2, Megaphone, Pencil, RefreshCw, Save, Search, Send, Trash2, X } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { PageWrapper } from '@/components/common/PageWrapper';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { useIsMobile } from '@/hooks/use-mobile';
import { AlertSections, type AlertSection } from '@/components/modules/alert';
import { ReportHistoryList } from '@/components/modules/report/ReportHistoryList';
import { ReportScheduleManager } from '@/components/modules/report/ReportScheduleManager';
import { resolveNotifContent, resolveNotifTitle } from './notif-text';
import {
    useArchiveNotification,
    useDeleteNotification,
    useMarkAllNotificationsRead,
    useMarkNotificationRead,
    useMarkNotificationUnread,
    useNotificationDetail,
    useNotificationPolicies,
    useNotificationPreferences,
    useSaveNotificationPreference,
    useDeleteNotificationPreference,
    useCreateNotificationPolicy,
    useUpdateNotificationPolicy,
    useDeleteNotificationPolicy,
    useNotificationsInfinite,
    useUnreadNotificationCount,
    type NotificationDelivery,
    type NotificationFilter,
    type NotificationItem,
    type NotificationPolicy,
    type NotificationPreference,
} from '@/api/endpoints/notification';

const TYPES = ['', 'alert', 'report', 'channel_expire', 'system', 'site', 'backup', 'usage'];
const SEVERITIES = ['', 'info', 'success', 'warning', 'error', 'critical'];
type NotificationGroup = 'messages' | 'alerts' | 'delivery' | 'reports';
type NotificationSubTab = 'inbox' | 'archived' | AlertSection | 'policies' | 'preferences' | 'reportSchedules' | 'reportHistory';

function formatTime(ts?: number) {
    if (!ts) return '-';
    return new Date(ts).toLocaleString();
}

function severityClass(severity: string) {
    switch (severity) {
        case 'success': return 'bg-emerald-500/10 text-emerald-600 border-emerald-500/30';
        case 'warning': return 'bg-amber-500/10 text-amber-600 border-amber-500/30';
        case 'error':
        case 'critical': return 'bg-destructive/10 text-destructive border-destructive/30';
        default: return 'bg-muted text-muted-foreground';
    }
}

function groupIcon(group: NotificationGroup) {
    switch (group) {
        case 'messages': return <Inbox className="h-4 w-4" />;
        case 'alerts': return <Bell className="h-4 w-4" />;
        case 'delivery': return <Send className="h-4 w-4" />;
        case 'reports': return <Clock className="h-4 w-4" />;
    }
}

function SectionButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
    return (
        <Button variant={active ? 'default' : 'outline'} onClick={onClick} className="shrink-0">
            {children}
        </Button>
    );
}

function NotificationCard({ item, selected, onSelect }: { item: NotificationItem; selected: boolean; onSelect: () => void }) {
    const t = useTranslations('notification');
    const tn = useTranslations('notif');
    return (
        <button onClick={onSelect} className={`w-full rounded-2xl border p-3 text-left transition hover:bg-muted/60 sm:p-4 ${selected ? 'border-primary bg-primary/5' : 'border-border bg-card'}`}>
            <div className="flex items-start justify-between gap-3">
                <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                        {!item.read_at && <span className="h-2 w-2 rounded-full bg-primary" />}
                        <Badge variant="outline">{t(`type.${item.type}`)}</Badge>
                        <Badge variant="outline" className={severityClass(item.severity)}>{t(`severity.${item.severity}`)}</Badge>
                    </div>
                    <h3 className="mt-2 truncate text-base font-semibold">{resolveNotifTitle(item, tn)}</h3>
                    <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">{resolveNotifContent(item, tn)}</p>
                </div>
                <div className="shrink-0 text-xs text-muted-foreground">{formatTime(item.created_at)}</div>
            </div>
        </button>
    );
}

function NotificationDetailContent({ selected, deliveries, onMarkRead, onMarkUnread, onArchive, onDelete }: {
    selected: NotificationItem;
    deliveries: NotificationDelivery[];
    onMarkRead: () => void;
    onMarkUnread: () => void;
    onArchive: () => void;
    onDelete: () => void;
}) {
    const t = useTranslations('notification');
    const tn = useTranslations('notif');
    return (
        <div className="space-y-4">
            <div>
                <div className="flex flex-wrap gap-2"><Badge>{t(`type.${selected.type}`)}</Badge><Badge variant="outline" className={severityClass(selected.severity)}>{t(`severity.${selected.severity}`)}</Badge></div>
                <h2 className="mt-3 break-words text-lg font-semibold">{resolveNotifTitle(selected, tn)}</h2>
                <p className="mt-1 text-xs text-muted-foreground">{formatTime(selected.created_at)}</p>
            </div>
            <p className="whitespace-pre-wrap break-words text-sm text-muted-foreground">{resolveNotifContent(selected, tn)}</p>
            <div className="flex flex-wrap gap-2">
                {selected.read_at ? <Button size="sm" variant="outline" onClick={onMarkUnread}>{t('actions.markUnread')}</Button> : <Button size="sm" onClick={onMarkRead}>{t('actions.markRead')}</Button>}
                {!selected.archived_at && <Button size="sm" variant="outline" onClick={onArchive}>{t('actions.archive')}</Button>}
                <Button size="sm" variant="destructive" onClick={onDelete}><Trash2 className="h-4 w-4" />{t('actions.delete')}</Button>
            </div>
            <div className="border-t pt-3">
                <h3 className="text-sm font-semibold">{t('detail.deliveries')}</h3>
                <div className="mt-2 space-y-2">
                    {deliveries.length === 0 ? <p className="text-xs text-muted-foreground">{t('detail.noDeliveries')}</p> : null}
                    {deliveries.map((d) => <div key={d.id} className="break-words rounded-xl bg-muted p-2 text-xs"><b>{d.channel_name || d.channel_id}</b> · {d.status}{d.last_error ? ` · ${d.last_error}` : ''}</div>)}
                </div>
            </div>
        </div>
    );
}

// 搜索输入防抖：避免每次击键都触发列表查询重建。
function useDebouncedValue<T>(value: T, delayMs = 300) {
    const [debounced, setDebounced] = useState(value);
    useEffect(() => {
        const timer = setTimeout(() => setDebounced(value), delayMs);
        return () => clearTimeout(timer);
    }, [value, delayMs]);
    return debounced;
}

export function Notification() {
    const t = useTranslations('notification');
    const tn = useTranslations('notif');
    const [group, setGroup] = useState<NotificationGroup>('messages');
    const [subTab, setSubTab] = useState<NotificationSubTab>('inbox');
    const [type, setType] = useState('');
    const [severity, setSeverity] = useState('');
    const [search, setSearch] = useState('');
    const [selectedID, setSelectedID] = useState<number | undefined>();
    const [policyDraft, setPolicyDraft] = useState<Partial<NotificationPolicy>>({ name: '', enabled: true, type: '', min_severity: 'info', source: '', channel_ids: '[]' });
    const [editingPolicyID, setEditingPolicyID] = useState<number | null>(null);
    const [preferenceDraft, setPreferenceDraft] = useState<Partial<NotificationPreference>>({ user_id: 0, type: 'alert', enabled: true, in_app_enabled: true, external_enabled: true, min_severity: 'info', channel_ids: '[]' });

    const debouncedSearch = useDebouncedValue(search, 300);
    const isMobile = useIsMobile();

    const filter: NotificationFilter = useMemo(() => ({
        archived: subTab === 'archived',
        type: type || undefined,
        severity: severity || undefined,
        search: debouncedSearch || undefined,
    }), [debouncedSearch, severity, subTab, type]);

    const { items, isLoading, isLoadingMore, hasMore, loadMore, refetch } = useNotificationsInfinite(filter);
    const { data: unread } = useUnreadNotificationCount();
    const detail = useNotificationDetail(selectedID);
    const markRead = useMarkNotificationRead();
    const markUnread = useMarkNotificationUnread();
    const archive = useArchiveNotification();
    const deleteNotification = useDeleteNotification();
    const markAllRead = useMarkAllNotificationsRead();
    const { data: policies = [] } = useNotificationPolicies();
    const { data: preferences = [] } = useNotificationPreferences();
    const createPolicy = useCreateNotificationPolicy();
    const updatePolicy = useUpdateNotificationPolicy();
    const deletePolicy = useDeleteNotificationPolicy();
    const savePreference = useSaveNotificationPreference();
    const deletePreference = useDeleteNotificationPreference();

    const selected = detail.data?.notification;

    const canLoadMore = hasMore && !isLoading && !isLoadingMore && items.length > 0;
    const handleReachEnd = useCallback(() => {
        if (!canLoadMore) return;
        void loadMore();
    }, [canLoadMore, loadMore]);

    const listFooter = useMemo(() => {
        if (hasMore && (isLoading || isLoadingMore)) {
            return (
                <div className="flex justify-center py-6">
                    <div className="flex items-center gap-2 rounded-full border border-border/50 bg-card/80 px-4 py-2 shadow-sm backdrop-blur">
                        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                        <span className="text-xs text-muted-foreground">{t('list.loadingMore')}</span>
                    </div>
                </div>
            );
        }
        if (!hasMore && items.length > 0) {
            return (
                <div className="flex justify-center py-6">
                    <span className="text-xs text-muted-foreground/60">{t('list.noMore')}</span>
                </div>
            );
        }
        return null;
    }, [hasMore, isLoading, isLoadingMore, items.length, t]);

    const openGroup = (nextGroup: NotificationGroup) => {
        setGroup(nextGroup);
        if (nextGroup === 'messages') setSubTab('inbox');
        if (nextGroup === 'alerts') setSubTab('rules');
        if (nextGroup === 'delivery') setSubTab('channels');
        if (nextGroup === 'reports') setSubTab('reportSchedules');
    };

    const resetPolicyDraft = () => {
        setEditingPolicyID(null);
        setPolicyDraft({ name: '', enabled: true, type: '', min_severity: 'info', source: '', channel_ids: '[]' });
    };
    const savePolicyDraft = () => {
        const payload = {
            id: editingPolicyID ?? 0,
            name: policyDraft.name || '',
            enabled: policyDraft.enabled ?? true,
            type: policyDraft.type || '',
            min_severity: policyDraft.min_severity || 'info',
            source: policyDraft.source || '',
            channel_ids: policyDraft.channel_ids || '[]',
        } as NotificationPolicy;
        if (editingPolicyID) updatePolicy.mutate(payload, { onSuccess: resetPolicyDraft });
        else createPolicy.mutate(payload, { onSuccess: resetPolicyDraft });
    };
    const editPolicy = (policy: NotificationPolicy) => {
        setEditingPolicyID(policy.id);
        setPolicyDraft(policy);
    };
    const savePreferenceDraft = () => {
        savePreference.mutate({
            id: preferenceDraft.id ?? 0,
            user_id: preferenceDraft.user_id ?? 0,
            type: preferenceDraft.type || 'alert',
            enabled: preferenceDraft.enabled ?? true,
            in_app_enabled: preferenceDraft.in_app_enabled ?? true,
            external_enabled: preferenceDraft.external_enabled ?? true,
            min_severity: preferenceDraft.min_severity || 'info',
            channel_ids: preferenceDraft.channel_ids || '[]',
            quiet_start: preferenceDraft.quiet_start || '',
            quiet_end: preferenceDraft.quiet_end || '',
        });
    };
    const deletePreferenceDraft = (pref: NotificationPreference) => {
        deletePreference.mutate(pref.id, {
            onSuccess: () => {
                if (preferenceDraft.id === pref.id) {
                    setPreferenceDraft({ user_id: 0, type: 'alert', enabled: true, in_app_enabled: true, external_enabled: true, min_severity: 'info', channel_ids: '[]' });
                }
            },
        });
    };

    const renderMessages = () => (
        <>
            <div className="flex flex-wrap items-center gap-2">
                <div className="relative flex-1 min-w-[12rem]">
                    <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                    <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder={t('filters.search')} className="h-9 pl-9" />
                </div>
                <select value={type} onChange={(e) => setType(e.target.value)} className="h-9 rounded-xl border bg-background px-3 text-sm">
                    {TYPES.map((v) => <option key={v} value={v}>{v ? t(`type.${v}`) : t('filters.allTypes')}</option>)}
                </select>
                <select value={severity} onChange={(e) => setSeverity(e.target.value)} className="h-9 rounded-xl border bg-background px-3 text-sm">
                    {SEVERITIES.map((v) => <option key={v} value={v}>{v ? t(`severity.${v}`) : t('filters.allSeverities')}</option>)}
                </select>
            </div>

            <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_24rem]">
                <div className="flex min-h-0 flex-col">
                    {isLoading ? (
                        <div className="flex min-h-[24rem] items-center justify-center rounded-2xl border bg-card">
                            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
                        </div>
                    ) : items.length === 0 ? (
                        <div className="flex min-h-[24rem] items-center justify-center rounded-2xl border bg-card p-8 text-center text-sm text-muted-foreground">{t('empty')}</div>
                    ) : (
                        // 列表限定高度、内部滚动并虚拟化渲染，滚到底部按页加载更多，
                        // 页面不再随通知数量无限变长。
                        <div className="h-[calc(100dvh-24rem)] min-h-[24rem]">
                            <VirtualizedGrid
                                items={items}
                                layout="list"
                                columns={{ default: 1 }}
                                estimateItemHeight={124}
                                gap={12}
                                overscan={6}
                                getItemKey={(item) => `notification-${item.id}`}
                                renderItem={(item) => <NotificationCard item={item} selected={selectedID === item.id} onSelect={() => setSelectedID(item.id)} />}
                                footer={listFooter}
                                onReachEnd={handleReachEnd}
                                reachEndEnabled={canLoadMore}
                                reachEndOffset={2}
                                bottomPaddingClassName="pb-4"
                            />
                        </div>
                    )}
                </div>

                <aside className="hidden self-start rounded-2xl border bg-card p-4 lg:sticky lg:top-4 lg:block">
                    {!selected ? (
                        <div className="flex min-h-[16rem] items-center justify-center text-sm text-muted-foreground">{t('detail.placeholder')}</div>
                    ) : (
                        <NotificationDetailContent
                            selected={selected}
                            deliveries={detail.data?.deliveries ?? []}
                            onMarkRead={() => markRead.mutate([selected.id])}
                            onMarkUnread={() => markUnread.mutate([selected.id])}
                            onArchive={() => archive.mutate([selected.id])}
                            onDelete={() => deleteNotification.mutate(selected.id)}
                        />
                    )}
                </aside>
            </div>

            {/* 窄屏下详情以弹窗呈现，避免详情面板排在列表下方需要长距离滚动 */}
            <Dialog open={isMobile && selectedID !== undefined} onOpenChange={(open) => { if (!open) setSelectedID(undefined); }}>
                <DialogContent className="max-h-[85dvh] overflow-y-auto">
                    {selected ? (
                        <>
                            <DialogHeader className="sr-only">
                                <DialogTitle>{resolveNotifTitle(selected, tn)}</DialogTitle>
                                <DialogDescription>{formatTime(selected.created_at)}</DialogDescription>
                            </DialogHeader>
                            <NotificationDetailContent
                                selected={selected}
                                deliveries={detail.data?.deliveries ?? []}
                                onMarkRead={() => markRead.mutate([selected.id])}
                                onMarkUnread={() => markUnread.mutate([selected.id])}
                                onArchive={() => { archive.mutate([selected.id]); setSelectedID(undefined); }}
                                onDelete={() => { deleteNotification.mutate(selected.id); setSelectedID(undefined); }}
                            />
                        </>
                    ) : (
                        <div className="flex items-center justify-center py-8">
                            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                        </div>
                    )}
                </DialogContent>
            </Dialog>
        </>
    );

    const renderPolicies = () => (
        <div className="space-y-4 rounded-2xl border bg-card p-4">
            <div>
                <h2 className="text-lg font-semibold">{t('policies.title')}</h2>
                <p className="text-sm text-muted-foreground">{t('policies.description')}</p>
            </div>
            <div className="grid gap-3 rounded-xl bg-muted/40 p-3 md:grid-cols-2">
                <Input placeholder={t('policies.form.name')} value={policyDraft.name || ''} onChange={(e) => setPolicyDraft({ ...policyDraft, name: e.target.value })} />
                <Input placeholder={t('policies.form.channelIds')} value={policyDraft.channel_ids || ''} onChange={(e) => setPolicyDraft({ ...policyDraft, channel_ids: e.target.value })} />
                <select value={policyDraft.type || ''} onChange={(e) => setPolicyDraft({ ...policyDraft, type: e.target.value as NotificationPolicy['type'] })} className="rounded-xl border bg-background px-3 py-2 text-sm">
                    {TYPES.map((v) => <option key={v} value={v}>{v ? t(`type.${v}`) : t('filters.allTypes')}</option>)}
                </select>
                <select value={policyDraft.min_severity || 'info'} onChange={(e) => setPolicyDraft({ ...policyDraft, min_severity: e.target.value as NotificationPolicy['min_severity'] })} className="rounded-xl border bg-background px-3 py-2 text-sm">
                    {SEVERITIES.filter(Boolean).map((v) => <option key={v} value={v}>{t(`severity.${v}`)}</option>)}
                </select>
                <Input placeholder={t('policies.form.source')} value={policyDraft.source || ''} onChange={(e) => setPolicyDraft({ ...policyDraft, source: e.target.value })} />
                <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={policyDraft.enabled ?? true} onChange={(e) => setPolicyDraft({ ...policyDraft, enabled: e.target.checked })} />{t('policies.form.enabled')}</label>
                <div className="flex gap-2 md:col-span-2">
                    <Button onClick={savePolicyDraft} disabled={!policyDraft.name}><Save className="h-4 w-4" />{editingPolicyID ? t('actions.save') : t('actions.create')}</Button>
                    {editingPolicyID && <Button variant="outline" onClick={resetPolicyDraft}><X className="h-4 w-4" />{t('actions.cancel')}</Button>}
                </div>
            </div>
            <div className="space-y-3">
                {policies.length === 0 ? <div className="rounded-xl bg-muted p-4 text-sm text-muted-foreground">{t('policies.empty')}</div> : null}
                {policies.map((p) => <div key={p.id} className="flex items-center justify-between gap-3 rounded-xl border p-3"><div><div className="font-medium">{p.name}</div><div className="text-xs text-muted-foreground">{p.type || t('filters.allTypes')} · {p.min_severity} · {p.channel_ids}</div></div><div className="flex gap-2"><Button size="sm" variant="outline" onClick={() => editPolicy(p)}><Pencil className="h-4 w-4" />{t('actions.edit')}</Button><Button size="sm" variant="destructive" onClick={() => deletePolicy.mutate(p.id)}><Trash2 className="h-4 w-4" /></Button></div></div>)}
            </div>
        </div>
    );

    const renderPreferences = () => (
        <div className="space-y-4 rounded-2xl border bg-card p-4">
            <div>
                <h2 className="text-lg font-semibold">{t('preferences.title')}</h2>
                <p className="text-sm text-muted-foreground">{t('preferences.description')}</p>
            </div>
            <div className="grid gap-3 rounded-xl bg-muted/40 p-3 md:grid-cols-2">
                <select value={preferenceDraft.type || 'alert'} onChange={(e) => setPreferenceDraft({ ...preferenceDraft, type: e.target.value as NotificationPreference['type'] })} className="rounded-xl border bg-background px-3 py-2 text-sm">
                    {TYPES.filter(Boolean).map((v) => <option key={v} value={v}>{t(`type.${v}`)}</option>)}
                </select>
                <select value={preferenceDraft.min_severity || 'info'} onChange={(e) => setPreferenceDraft({ ...preferenceDraft, min_severity: e.target.value as NotificationPreference['min_severity'] })} className="rounded-xl border bg-background px-3 py-2 text-sm">
                    {SEVERITIES.filter(Boolean).map((v) => <option key={v} value={v}>{t(`severity.${v}`)}</option>)}
                </select>
                <Input placeholder={t('preferences.form.channelIds')} value={preferenceDraft.channel_ids || ''} onChange={(e) => setPreferenceDraft({ ...preferenceDraft, channel_ids: e.target.value })} />
                <div className="grid grid-cols-2 gap-2"><Input placeholder="HH:mm" value={preferenceDraft.quiet_start || ''} onChange={(e) => setPreferenceDraft({ ...preferenceDraft, quiet_start: e.target.value })} /><Input placeholder="HH:mm" value={preferenceDraft.quiet_end || ''} onChange={(e) => setPreferenceDraft({ ...preferenceDraft, quiet_end: e.target.value })} /></div>
                <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={preferenceDraft.in_app_enabled ?? true} onChange={(e) => setPreferenceDraft({ ...preferenceDraft, in_app_enabled: e.target.checked })} />{t('preferences.form.inApp')}</label>
                <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={preferenceDraft.external_enabled ?? true} onChange={(e) => setPreferenceDraft({ ...preferenceDraft, external_enabled: e.target.checked })} />{t('preferences.form.external')}</label>
                <Button onClick={savePreferenceDraft} className="md:col-span-2"><Save className="h-4 w-4" />{t('actions.save')}</Button>
            </div>
            <div className="space-y-3">
                {preferences.length === 0 ? <div className="rounded-xl bg-muted p-4 text-sm text-muted-foreground">{t('preferences.empty')}</div> : null}
                {preferences.map((p) => <div key={p.id} className="flex items-center justify-between gap-3 rounded-xl border p-3"><button onClick={() => setPreferenceDraft(p)} className="min-w-0 flex-1 text-left"><div className="font-medium">{t(`type.${p.type}`)} · {t(`severity.${p.min_severity}`)}</div><div className="text-xs text-muted-foreground">{p.in_app_enabled ? t('preferences.form.inApp') : ''} {p.external_enabled ? t('preferences.form.external') : ''} · {p.channel_ids}</div></button><Button size="sm" variant="destructive" onClick={() => deletePreferenceDraft(p)}><Trash2 className="h-4 w-4" /></Button></div>)}
            </div>
        </div>
    );

    return (
        <PageWrapper>
            <div className="space-y-4">
                <div className="flex flex-wrap items-center justify-between gap-2 sm:gap-3">
                    <div className="min-w-0">
                        <h1 className="text-xl font-bold tracking-tight sm:text-2xl">{t('title')}</h1>
                        <p className="text-xs text-muted-foreground sm:text-sm">{t('subtitle', { count: unread?.count ?? 0 })}</p>
                    </div>
                    <div className="flex shrink-0 gap-2">
                        <Button variant="outline" onClick={() => refetch()}><RefreshCw className="h-4 w-4" />{t('actions.refresh')}</Button>
                        <Button onClick={() => markAllRead.mutate()}><Check className="h-4 w-4" />{t('actions.markAllRead')}</Button>
                    </div>
                </div>

                <div className="flex flex-wrap gap-2">
                    {(['messages', 'alerts', 'delivery', 'reports'] as NotificationGroup[]).map((item) => (
                        <SectionButton key={item} active={group === item} onClick={() => openGroup(item)}>
                            {groupIcon(item)}{t(`groups.${item}`)}
                        </SectionButton>
                    ))}
                    {group === 'messages' && <>
                        <div className="w-px h-8 bg-border mx-1" />
                        <SectionButton active={subTab === 'inbox'} onClick={() => setSubTab('inbox')}><Inbox className="h-4 w-4" />{t('tabs.inbox')}</SectionButton>
                        <SectionButton active={subTab === 'archived'} onClick={() => setSubTab('archived')}><Archive className="h-4 w-4" />{t('tabs.archived')}</SectionButton>
                    </>}
                    {group === 'alerts' && <>
                        <div className="w-px h-8 bg-border mx-1" />
                        <SectionButton active={subTab === 'rules'} onClick={() => setSubTab('rules')}><Bell className="h-4 w-4" />{t('tabs.alertRules')}</SectionButton>
                        <SectionButton active={subTab === 'history'} onClick={() => setSubTab('history')}><Clock className="h-4 w-4" />{t('tabs.alertHistory')}</SectionButton>
                    </>}
                    {group === 'delivery' && <>
                        <div className="w-px h-8 bg-border mx-1" />
                        <SectionButton active={subTab === 'channels'} onClick={() => setSubTab('channels')}><Megaphone className="h-4 w-4" />{t('tabs.channels')}</SectionButton>
                        <SectionButton active={subTab === 'policies'} onClick={() => setSubTab('policies')}><Bell className="h-4 w-4" />{t('tabs.policies')}</SectionButton>
                        <SectionButton active={subTab === 'preferences'} onClick={() => setSubTab('preferences')}><Check className="h-4 w-4" />{t('tabs.preferences')}</SectionButton>
                    </>}
                    {group === 'reports' && <>
                        <div className="w-px h-8 bg-border mx-1" />
                        <SectionButton active={subTab === 'reportSchedules'} onClick={() => setSubTab('reportSchedules')}><Clock className="h-4 w-4" />{t('tabs.reportSchedules')}</SectionButton>
                        <SectionButton active={subTab === 'reportHistory'} onClick={() => setSubTab('reportHistory')}><Archive className="h-4 w-4" />{t('tabs.reportHistory')}</SectionButton>
                    </>}
                </div>

                {(subTab === 'inbox' || subTab === 'archived') && renderMessages()}
                {subTab === 'rules' && <AlertSections section="rules" />}
                {subTab === 'history' && <AlertSections section="history" />}
                {subTab === 'channels' && <AlertSections section="channels" />}
                {subTab === 'policies' && renderPolicies()}
                {subTab === 'preferences' && renderPreferences()}
                {subTab === 'reportSchedules' && <div className="rounded-xl border border-border bg-card p-4 sm:p-6"><ReportScheduleManager /></div>}
                {subTab === 'reportHistory' && <div className="rounded-xl border border-border bg-card p-4 sm:p-6"><ReportHistoryList /></div>}
            </div>
        </PageWrapper>
    );
}
