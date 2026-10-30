'use client';

import { useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Blocks, FileUp, LoaderCircle, Settings2, SquareArrowOutUpRight, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { getPluginUIBundle, pluginRPC, useConfigurePlugin, useEnablePlugin, useInstallPlugin, usePlugins, useUninstallPlugin, type PluginRecord } from '@/api/endpoints/plugin';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { dispatchPluginRPC, PLUGIN_MESSAGE_TYPE } from './rpc';
import { createPluginDocument } from './ui-bundle';

function parseJSON(value: string): Record<string, unknown> {
    if (!value.trim()) return {};
    const parsed: unknown = JSON.parse(value);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('Configuration must be a JSON object');
    return parsed as Record<string, unknown>;
}

function PluginPage({ plugin }: { plugin: PluginRecord }) {
    const frameRef = useRef<HTMLIFrameElement>(null);
    const manifest = JSON.parse(plugin.manifest) as { ui?: { title?: string } };
    const [document, setDocument] = useState('');
    const [error, setError] = useState('');
    useEffect(() => {
        let active = true;
        const session = crypto.randomUUID();
        setDocument('');
        setError('');
        const handler = (event: MessageEvent) => {
            const target = frameRef.current?.contentWindow ?? null;
            void dispatchPluginRPC(event, target, plugin.id, session, pluginRPC,
                () => active && frameRef.current?.contentWindow === target);
        };
        window.addEventListener('message', handler);
        void getPluginUIBundle(plugin.id, plugin.version).then(bundle => {
            if (active) setDocument(createPluginDocument(bundle, plugin.id, session, window.location.origin));
        }).catch(error => {
            if (active) setError(error instanceof Error ? error.message : String(error));
        });
        return () => { active = false; window.removeEventListener('message', handler); };
    }, [plugin.id, plugin.version]);
    return (
        <div className="space-y-3">
            <p className="text-xs text-muted-foreground">{manifest.ui?.title ?? plugin.name}</p>
            {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : document ? <iframe ref={frameRef} title={plugin.name} sandbox="allow-scripts" referrerPolicy="no-referrer" srcDoc={document} className="h-[65vh] w-full rounded-lg border bg-background" /> : <LoaderCircle className="mx-auto animate-spin" />}
        </div>
    );
}

function PluginCard({ plugin, canWrite }: { plugin: PluginRecord; canWrite: boolean }) {
    const t = useTranslations('plugin');
    const enable = useEnablePlugin();
    const configure = useConfigurePlugin();
    const uninstall = useUninstallPlugin();
    const [editing, setEditing] = useState(false);
    const [viewing, setViewing] = useState(false);
    const [config, setConfig] = useState(plugin.config || '{}');
    const manifest = JSON.parse(plugin.manifest) as { description?: string; permissions?: string[]; ui?: unknown };
    const busy = enable.isPending || configure.isPending || uninstall.isPending;
    return (
        <section className="rounded-xl border bg-card p-4 shadow-xs space-y-4">
            <div className="flex items-start gap-3">
                <div className="rounded-lg bg-primary/10 p-2 text-primary"><Blocks className="size-5" /></div>
                <div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><h2 className="font-semibold">{plugin.name}</h2><span className="rounded bg-muted px-2 py-0.5 text-xs text-muted-foreground">v{plugin.version}</span></div><p className="mt-1 break-all text-xs text-muted-foreground">{plugin.id}</p></div>
                <Switch checked={plugin.enabled} disabled={!canWrite || busy || plugin.status === 'uninstalling'} onCheckedChange={(enabled) => enable.mutate({ id: plugin.id, enabled })} aria-label={t('enabled')} />
            </div>
            {manifest.description && <p className="text-sm text-muted-foreground">{manifest.description}</p>}
            <div className="flex flex-wrap gap-1.5">{(manifest.permissions ?? []).map((permission) => <span key={permission} className="rounded-full border px-2 py-0.5 font-mono text-[11px]">{permission}</span>)}</div>
            <div className="grid grid-cols-3 gap-2 text-center text-xs"><div className="rounded-md bg-muted/50 p-2"><strong className="block text-sm">{plugin.calls}</strong>{t('calls')}</div><div className="rounded-md bg-muted/50 p-2"><strong className="block text-sm">{plugin.failures}</strong>{t('failures')}</div><div className="rounded-md bg-muted/50 p-2"><strong className="block text-sm">{plugin.timeouts}</strong>{t('timeouts')}</div></div>
            <p className="text-xs text-muted-foreground">{t(`status.${plugin.status}`)}</p>
            {plugin.last_error && <pre className="max-h-36 overflow-auto whitespace-pre-wrap rounded-md bg-destructive/10 p-3 text-xs text-destructive">{plugin.last_error}</pre>}
            <div className="flex flex-wrap gap-2"><Button size="sm" variant="outline" disabled={!canWrite || busy} onClick={() => setEditing(true)}><Settings2 />{t('configure')}</Button>{manifest.ui ? <Button size="sm" variant="outline" disabled={!plugin.enabled} onClick={() => setViewing(true)}><SquareArrowOutUpRight />{t('open')}</Button> : null}<Button size="sm" variant="destructive" disabled={!canWrite || busy || plugin.status === 'uninstalling'} onClick={() => { if (window.confirm(t('deleteConfirm'))) uninstall.mutate({ id: plugin.id, deleteData: true }); }}><Trash2 />{t('uninstall')}</Button></div>
            <Dialog open={editing} onOpenChange={setEditing}><DialogContent><DialogHeader><DialogTitle>{plugin.name}</DialogTitle><DialogDescription>{t('configHint')}</DialogDescription></DialogHeader><textarea aria-label={t('configuration')} className="min-h-52 rounded-md border bg-background p-3 font-mono text-xs" value={config} onChange={(event) => setConfig(event.target.value)} /><DialogFooter><Button variant="outline" onClick={() => setEditing(false)}>{t('cancel')}</Button><Button onClick={() => { try { configure.mutate({ id: plugin.id, config: parseJSON(config) }, { onSuccess: () => setEditing(false) }); } catch { toast.error(t('invalidConfig')); } }}>{t('save')}</Button></DialogFooter></DialogContent></Dialog>
            <Dialog open={viewing} onOpenChange={setViewing}><DialogContent className="max-w-5xl"><DialogTitle>{plugin.name}</DialogTitle><PluginPage plugin={plugin} /></DialogContent></Dialog>
        </section>
    );
}

export function Plugin() {
    const t = useTranslations('plugin');
    const inputRef = useRef<HTMLInputElement>(null);
    const { data, isLoading } = usePlugins();
    const install = useInstallPlugin();
    const canWrite = data?.can_write ?? false;
    const plugins = data?.plugins ?? [];
    return (
        <div className="h-full overflow-y-auto px-4 pb-8 pt-4 md:px-6"><div className="mx-auto max-w-6xl space-y-5">
            <div className="flex flex-wrap items-center justify-between gap-3"><div><h1 className="text-xl font-semibold">{t('title')}</h1><p className="mt-1 text-sm text-muted-foreground">{t('description')}</p></div><Button disabled={!canWrite || install.isPending} onClick={() => inputRef.current?.click()}>{install.isPending ? <LoaderCircle className="animate-spin" /> : <FileUp />}{t('install')}</Button></div>
            <Input ref={inputRef} type="file" accept=".octopkg,.zip" className="hidden" onChange={(event) => { const file = event.target.files?.[0]; if (file) install.mutate(file, { onSuccess: () => toast.success(t('installSuccess')) }); event.target.value = ''; }} />
            {!canWrite && <p className="rounded-md border p-3 text-sm text-muted-foreground">{t('readOnly')}</p>}
            {isLoading ? <LoaderCircle className="mx-auto animate-spin" /> : <div className="grid gap-4 lg:grid-cols-2">{plugins.map((plugin) => <PluginCard key={plugin.id} plugin={plugin} canWrite={canWrite} />)}</div>}
            {!isLoading && plugins.length === 0 && <div className="rounded-xl border border-dashed p-12 text-center"><Blocks className="mx-auto mb-3 size-8 text-muted-foreground" /><h2>{t('empty')}</h2><p className="mt-2 text-sm text-muted-foreground">{t('emptyHint')}</p></div>}
        </div></div>
    );
}

export { PLUGIN_MESSAGE_TYPE };
