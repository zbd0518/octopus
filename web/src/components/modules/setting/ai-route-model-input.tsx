'use client';

import { useEffect, useRef, useState } from 'react';
import { Check, ChevronDown, LoaderCircle, RefreshCw } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useFetchAIRouteModels } from '@/api/endpoints/setting';
import { Input } from '@/components/ui/input';
import { Popover, PopoverAnchor, PopoverContent } from '@/components/ui/popover';
import { cn } from '@/lib/utils';
import { modelFetchSignature } from './ai-route-services';

type AIRouteModelInputProps = {
    id?: string;
    value: string;
    baseURL: string;
    apiKey: string;
    placeholder: string;
    onChange: (value: string) => void;
    onSelect?: (value: string) => void;
    onBlur?: () => void;
};

export function AIRouteModelInput({
    id,
    value,
    baseURL,
    apiKey,
    placeholder,
    onChange,
    onSelect,
    onBlur,
}: AIRouteModelInputProps) {
    const t = useTranslations('setting.aiRoute.model');
    const { mutateAsync: fetchModels } = useFetchAIRouteModels();
    const signature = modelFetchSignature(baseURL, apiKey);
    const [open, setOpen] = useState(false);
    const [loadingSignature, setLoadingSignature] = useState<string | null>(null);
    const loading = loadingSignature === signature;
    const [result, setResult] = useState<{ signature: string; models: string[]; error: string }>({
        signature: '',
        models: [],
        error: '',
    });
    const cache = useRef(new Map<string, string[]>());
    const activeRequest = useRef<{ cancel: () => void } | null>(null);

    useEffect(() => {
        if (!signature) return;

        const loadModels = async (force = false) => {
            if (!force) {
                const cached = cache.current.get(signature);
                if (cached) {
                    setResult({ signature, models: cached, error: '' });
                    return;
                }
            }

            const version = { cancelled: false };
            activeRequest.current = { cancel: () => { version.cancelled = true; } };
            setLoadingSignature(signature);
            setResult((current) => ({ ...current, signature, error: '' }));
            try {
                const models = await fetchModels({ base_url: baseURL.trim(), api_key: apiKey.trim() });
                if (version.cancelled) return;
                cache.current.set(signature, models);
                setResult({ signature, models, error: '' });
            } catch (error) {
                if (version.cancelled) return;
                setResult({
                    signature,
                    models: [],
                    error: error instanceof Error && error.message ? error.message : t('fetchFailed'),
                });
            } finally {
                if (!version.cancelled) setLoadingSignature(null);
            }
        };

        const timer = window.setTimeout(() => void loadModels(), 600);
        return () => {
            window.clearTimeout(timer);
            activeRequest.current?.cancel();
        };
    }, [apiKey, baseURL, fetchModels, signature, t]);

    const refreshModels = async () => {
        if (!signature) return;
        const version = { cancelled: false };
        activeRequest.current = { cancel: () => { version.cancelled = true; } };
        setLoadingSignature(signature);
        setResult((current) => ({ ...current, signature, error: '' }));
        try {
            const models = await fetchModels({ base_url: baseURL.trim(), api_key: apiKey.trim() });
            if (version.cancelled) return;
            cache.current.set(signature, models);
            setResult({ signature, models, error: '' });
        } catch (error) {
            if (version.cancelled) return;
            setResult({
                signature,
                models: [],
                error: error instanceof Error && error.message ? error.message : t('fetchFailed'),
            });
        } finally {
            if (!version.cancelled) setLoadingSignature(null);
        }
    };

    const models = result.signature === signature ? result.models : [];
    const error = result.signature === signature ? result.error : '';
    const query = value.trim().toLowerCase();
    const filteredModels = query
        ? models.filter((item) => item.toLowerCase().includes(query))
        : models;

    return (
        <div className="space-y-1.5">
            <Popover open={open} onOpenChange={setOpen}>
                <PopoverAnchor asChild>
                    <div className="relative">
                        <Input
                            id={id}
                            value={value}
                            onChange={(event) => {
                                onChange(event.target.value);
                                setOpen(models.length > 0);
                            }}
                            onFocus={() => setOpen(models.length > 0)}
                            onBlur={onBlur}
                            onKeyDown={(event) => {
                                if (event.key === 'Escape') setOpen(false);
                            }}
                            placeholder={placeholder}
                            className="w-full rounded-lg pr-9"
                            role="combobox"
                            aria-expanded={open}
                            aria-autocomplete="list"
                            autoComplete="off"
                        />
                        <button
                            type="button"
                            tabIndex={-1}
                            className="absolute right-2 top-1/2 -translate-y-1/2 rounded-sm p-1 text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
                            aria-label={t('showModels')}
                            aria-expanded={open}
                            onClick={() => setOpen((current) => !current)}
                        >
                            <ChevronDown className="size-4" />
                        </button>
                    </div>
                </PopoverAnchor>
                <PopoverContent
                    align="start"
                    className="w-(--radix-popover-trigger-width) min-w-64 rounded-lg p-1.5"
                    onOpenAutoFocus={(event) => event.preventDefault()}
                >
                    <div className="mb-1 flex items-center justify-between gap-2 px-2 py-1 text-xs text-muted-foreground">
                        <span>{loading ? t('fetching') : t('available', { count: filteredModels.length })}</span>
                        <button
                            type="button"
                            className="inline-flex items-center gap-1 rounded-sm px-1.5 py-1 hover:bg-accent hover:text-foreground disabled:opacity-50"
                            disabled={!signature || loading}
                            onClick={() => void refreshModels()}
                        >
                            <RefreshCw className={cn('size-3.5', loading && 'animate-spin')} />
                            {t('refresh')}
                        </button>
                    </div>
                    <div className="max-h-60 overflow-y-auto" role="listbox" aria-label={t('listLabel')}>
                        {loading && models.length === 0 ? (
                            <div className="flex items-center gap-2 px-2 py-3 text-sm text-muted-foreground">
                                <LoaderCircle className="size-4 animate-spin" />
                                {t('fetching')}
                            </div>
                        ) : filteredModels.length === 0 ? (
                            <p className="px-2 py-3 text-sm text-muted-foreground">
                                {models.length === 0 ? t('empty') : t('noMatches')}
                            </p>
                        ) : (
                            filteredModels.map((item) => (
                                <button
                                    key={item}
                                    type="button"
                                    role="option"
                                    aria-selected={value === item}
                                    className="flex w-full items-center justify-between gap-2 rounded-md px-2 py-2 text-left text-sm hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
                                    onClick={() => {
                                        onChange(item);
                                        onSelect?.(item);
                                        setOpen(false);
                                    }}
                                    onMouseDown={(event) => event.preventDefault()}
                                >
                                    <span className="truncate">{item}</span>
                                    {value === item && <Check className="size-4 shrink-0 text-primary" />}
                                </button>
                            ))
                        )}
                    </div>
                </PopoverContent>
            </Popover>
            {loading && <p className="text-xs text-muted-foreground">{t('fetching')}</p>}
            {!loading && error && <p role="alert" className="text-xs text-destructive">{error}</p>}
            {!loading && !error && models.length > 0 && (
                <p className="text-xs text-muted-foreground">{t('available', { count: models.length })}</p>
            )}
        </div>
    );
}
