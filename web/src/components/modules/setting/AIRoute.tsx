'use client';

import { useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react';
import { Bot, Clock3, KeyRound, Link2, Plus, Sparkles, Trash2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';
import { Hint } from '@/components/ui/hint';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { useGroupList } from '@/api/endpoints/group';
import { SettingKey, useSetSetting, useSettingList } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';
import {
    parseServicesJSON,
    serializeServicesRows,
    type AIRouteServiceRow,
} from './ai-route-services';

export function SettingAIRoute() {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const { data: groups = [] } = useGroupList();
    const setSetting = useSetSetting();

    const [groupID, setGroupID] = useState('0');
    const [baseURL, setBaseURL] = useState('');
    const [apiKey, setAPIKey] = useState('');
    const [model, setModel] = useState('');
    const [timeoutSeconds, setTimeoutSeconds] = useState('180');
    const [parallelism, setParallelism] = useState('3');
    const [maxModels, setMaxModels] = useState('120');
    const [serviceRows, setServiceRows] = useState<AIRouteServiceRow[]>([]);
    const [servicesJSONInvalid, setServicesJSONInvalid] = useState(false);
    const [servicesRawFallback, setServicesRawFallback] = useState('');

    const initialGroupID = useRef('0');
    const initialBaseURL = useRef('');
    const initialAPIKey = useRef('');
    const initialModel = useRef('');
    const initialTimeoutSeconds = useRef('180');
    const initialParallelism = useRef('3');
    const initialMaxModels = useRef('120');
    const initialServicesJSON = useRef('[]');

    useEffect(() => {
        if (!settings) return;

        const groupSetting = settings.find((item) => item.key === SettingKey.AIRouteGroupID);
        const baseURLSetting = settings.find((item) => item.key === SettingKey.AIRouteBaseURL);
        const apiKeySetting = settings.find((item) => item.key === SettingKey.AIRouteAPIKey);
        const modelSetting = settings.find((item) => item.key === SettingKey.AIRouteModel);
        const timeoutSetting = settings.find((item) => item.key === SettingKey.AIRouteTimeoutSeconds);
        const parallelismSetting = settings.find((item) => item.key === SettingKey.AIRouteParallelism);
        const servicesSetting = settings.find((item) => item.key === SettingKey.AIRouteServices);

        if (groupSetting) {
            queueMicrotask(() => setGroupID(groupSetting.value || '0'));
            initialGroupID.current = groupSetting.value || '0';
        }
        if (baseURLSetting) {
            queueMicrotask(() => setBaseURL(baseURLSetting.value));
            initialBaseURL.current = baseURLSetting.value;
        }
        if (apiKeySetting) {
            queueMicrotask(() => setAPIKey(apiKeySetting.value));
            initialAPIKey.current = apiKeySetting.value;
        }
        if (modelSetting) {
            queueMicrotask(() => setModel(modelSetting.value));
            initialModel.current = modelSetting.value;
        }
        if (timeoutSetting) {
            queueMicrotask(() => setTimeoutSeconds(timeoutSetting.value || '180'));
            initialTimeoutSeconds.current = timeoutSetting.value || '180';
        }
        if (parallelismSetting) {
            queueMicrotask(() => setParallelism(parallelismSetting.value || '3'));
            initialParallelism.current = parallelismSetting.value || '3';
        }
        const maxModelsSetting = settings.find((item) => item.key === SettingKey.AIRouteMaxModelsPerRequest);
        if (maxModelsSetting) {
            queueMicrotask(() => setMaxModels(maxModelsSetting.value || '120'));
            initialMaxModels.current = maxModelsSetting.value || '120';
        }
        if (servicesSetting) {
            const raw = servicesSetting.value || '[]';
            const { rows, invalid } = parseServicesJSON(raw);
            if (invalid) {
                // 兼容回退：存量非法 JSON 时展示原文并允许手动修复，而不是只弹错误。
                setServicesJSONInvalid(true);
                queueMicrotask(() => setServicesRawFallback(raw));
            } else if (rows) {
                queueMicrotask(() => setServiceRows(rows));
                setServicesJSONInvalid(false);
                setServicesRawFallback('');
            }
            initialServicesJSON.current = raw;
        }
    }, [settings]);

    const saveSetting = (key: string, value: string, initialRef: MutableRefObject<string>) => {
        if (value === initialRef.current) return;

        setSetting.mutate(
            { key, value },
            {
                onSuccess: () => {
                    toast.success(t('saved'));
                    initialRef.current = value;
                },
            },
        );
    };

    const enabledServiceCount = useMemo(
        () => serviceRows.filter((row) => row.enabled).length,
        [serviceRows],
    );

    const updateServiceRow = (index: number, patch: Partial<AIRouteServiceRow>) => {
        setServiceRows((prev) => {
            const next = prev.map((row, i) => (i === index ? { ...row, ...patch } : row));
            return next;
        });
    };

    const addServiceRow = () => {
        setServiceRows((prev) => [...prev, { name: '', baseUrl: '', apiKey: '', model: '', enabled: true }]);
    };

    const removeServiceRow = (index: number) => {
        const next = serviceRows.filter((_, i) => i !== index);
        setServiceRows(next);
        saveServicesRows(next);
    };

    const saveServicesRows = (rows: AIRouteServiceRow[]) => {
        const normalized = serializeServicesRows(rows);
        if (normalized === initialServicesJSON.current) return;
        setSetting.mutate(
            { key: SettingKey.AIRouteServices, value: normalized },
            {
                onSuccess: () => {
                    toast.success(t('saved'));
                    initialServicesJSON.current = normalized;
                },
            },
        );
    };

    // 手动修复回退：尝试重新解析 textarea 中的原文，合法则切回结构化编辑器并保存。
    const retryParseServicesFallback = () => {
        const raw = servicesRawFallback.trim();
        const { rows, invalid } = parseServicesJSON(raw === '' ? '[]' : raw);
        if (invalid || !rows) {
            toast.error(t('aiRoute.services.invalid'));
            return;
        }
        setServiceRows(rows);
        setServicesJSONInvalid(false);
        setServicesRawFallback('');
        saveServicesRows(rows);
    };
    return (
        <div className="relative overflow-hidden rounded-xl border-border/35 bg-card p-6 text-card-foreground shadow-md ">
            <div className="space-y-5">
                <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
                    <div className="space-y-1.5">
                        <h2 className="flex items-center gap-2 text-lg font-bold text-card-foreground">
                            <Bot className="h-5 w-5" />
                            {t('aiRoute.title')}
                            <Hint text={t('aiRoute.services.hint')} />
                        </h2>
                    </div>
                    <div className="w-fit rounded-full border-border/25 bg-card px-3 py-1.5 text-xs font-medium text-muted-foreground shadow-sm">
                        {t('aiRoute.badge')}
                    </div>
                </div>

                <div className="grid gap-4 xl:grid-cols-2">
                    <div className="space-y-3 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                        <div className="flex items-center gap-3">
                            <Sparkles className="h-5 w-5 text-muted-foreground" />
                            <span className="text-sm font-medium">
                                {t('aiRoute.group.label')}
                                <Hint text={t('aiRoute.group.hint')} />
                            </span>
                        </div>
                        <Select
                            value={groupID}
                            onValueChange={(value) => {
                                setGroupID(value);
                                saveSetting(SettingKey.AIRouteGroupID, value, initialGroupID);
                            }}
                        >
                            <SelectTrigger className="w-full rounded-lg">
                                <SelectValue placeholder={t('aiRoute.group.placeholder')} />
                            </SelectTrigger>
                            <SelectContent className="rounded-lg">
                                <SelectItem value="0">{t('aiRoute.group.placeholder')}</SelectItem>
                                {groups.map((group) => (
                                    <SelectItem key={group.id} value={String(group.id)}>
                                        {group.name}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    <div className="space-y-3 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                        <div className="flex items-center gap-3">
                            <Bot className="h-5 w-5 text-muted-foreground" />
                            <span className="text-sm font-medium">{t('aiRoute.model.label')}</span>
                        </div>
                        <Input
                            value={model}
                            onChange={(event) => setModel(event.target.value)}
                            onBlur={() => saveSetting(SettingKey.AIRouteModel, model, initialModel)}
                            placeholder={t('aiRoute.model.placeholder')}
                            className="w-full rounded-lg"
                        />
                    </div>

                    <div className="space-y-3 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                        <div className="flex items-center gap-3">
                            <Link2 className="h-5 w-5 text-muted-foreground" />
                            <span className="text-sm font-medium">{t('aiRoute.baseUrl.label')}</span>
                        </div>
                        <Input
                            value={baseURL}
                            onChange={(event) => setBaseURL(event.target.value)}
                            onBlur={() => saveSetting(SettingKey.AIRouteBaseURL, baseURL, initialBaseURL)}
                            placeholder={t('aiRoute.baseUrl.placeholder')}
                            className="w-full rounded-lg"
                        />
                    </div>

                    <div className="space-y-3 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                        <div className="flex items-center gap-3">
                            <KeyRound className="h-5 w-5 text-muted-foreground" />
                            <span className="text-sm font-medium">{t('aiRoute.apiKey.label')}</span>
                        </div>
                        <Input
                            type="password"
                            value={apiKey}
                            onChange={(event) => setAPIKey(event.target.value)}
                            onBlur={() => saveSetting(SettingKey.AIRouteAPIKey, apiKey, initialAPIKey)}
                            placeholder={t('aiRoute.apiKey.placeholder')}
                            className="w-full rounded-lg"
                        />
                    </div>

                    <div className="space-y-3 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                        <div className="flex items-center gap-3">
                            <Clock3 className="h-5 w-5 text-muted-foreground" />
                            <span className="text-sm font-medium">
                                {t('aiRoute.timeoutSeconds.label')}
                                <Hint text={t('aiRoute.timeoutSeconds.hint')} />
                            </span>
                        </div>
                        <Input
                            type="number"
                            min="1"
                            value={timeoutSeconds}
                            onChange={(event) => setTimeoutSeconds(event.target.value)}
                            onBlur={() =>
                                saveSetting(
                                    SettingKey.AIRouteTimeoutSeconds,
                                    timeoutSeconds,
                                    initialTimeoutSeconds,
                                )
                            }
                            placeholder={t('aiRoute.timeoutSeconds.placeholder')}
                            className="w-full rounded-lg"
                        />
                    </div>

                    <div className="space-y-3 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                        <div className="flex items-center gap-3">
                            <Sparkles className="h-5 w-5 text-muted-foreground" />
                            <span className="text-sm font-medium">
                                {t('aiRoute.parallelism.label')}
                                <Hint text={t('aiRoute.parallelism.hint')} />
                            </span>
                        </div>
                        <Input
                            type="number"
                            min="1"
                            value={parallelism}
                            onChange={(event) => setParallelism(event.target.value)}
                            onBlur={() => saveSetting(SettingKey.AIRouteParallelism, parallelism, initialParallelism)}
                            placeholder={t('aiRoute.parallelism.placeholder')}
                            className="w-full rounded-lg"
                        />
                    </div>

                    <div className="space-y-3 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                        <div className="flex items-center gap-3">
                            <Sparkles className="h-5 w-5 text-muted-foreground" />
                            <span className="text-sm font-medium">
                                {t('aiRoute.maxModels.label')}
                                <Hint text={t('aiRoute.maxModels.hint')} />
                            </span>
                        </div>
                        <Input
                            type="number"
                            min="1"
                            value={maxModels}
                            onChange={(event) => setMaxModels(event.target.value)}
                            onBlur={() => saveSetting(SettingKey.AIRouteMaxModelsPerRequest, maxModels, initialMaxModels)}
                            placeholder={t('aiRoute.maxModels.placeholder')}
                            className="w-full rounded-lg"
                        />
                    </div>
                </div>

                <div className="space-y-3 rounded-lg border-border/30 bg-card p-4 shadow-sm">
                    <div className="flex items-center justify-between gap-3">
                        <div className="flex items-center gap-3">
                            <Link2 className="h-5 w-5 text-muted-foreground" />
                            <span className="text-sm font-medium">
                                {t('aiRoute.services.label')}
                                <Hint text={t('aiRoute.services.hint')} />
                            </span>
                        </div>
                        <Button
                            variant="outline"
                            size="sm"
                            onClick={addServiceRow}
                            disabled={servicesJSONInvalid}
                        >
                            <Plus className="h-4 w-4" />
                            {t('aiRoute.services.add')}
                        </Button>
                    </div>

                    {servicesJSONInvalid ? (
                        <div className="space-y-2.5">
                            <p className="text-sm text-destructive">{t('aiRoute.services.invalidStored')}</p>
                            <textarea
                                value={servicesRawFallback}
                                onChange={(event) => setServicesRawFallback(event.target.value)}
                                spellCheck={false}
                                className="min-h-44 w-full rounded-lg border border-border/35 bg-card px-4 py-3 font-mono text-sm text-foreground shadow-inner outline-none transition-[border-color,box-shadow] duration-300 focus-visible:border-ring focus-visible:ring-4 focus-visible:ring-ring/20"
                            />
                            <div className="flex items-center justify-end gap-2">
                                <Button
                                    variant="outline"
                                    size="sm"
                                    onClick={retryParseServicesFallback}
                                >
                                    {t('aiRoute.services.retryParse')}
                                </Button>
                            </div>
                        </div>
                    ) : (
                        <div className="space-y-3">
                            {serviceRows.length === 0 && (
                                <p className="text-sm text-muted-foreground">{t('aiRoute.services.empty')}</p>
                            )}
                            {serviceRows.map((row, index) => (
                                <div
                                    key={index}
                                    className="space-y-2.5 rounded-lg border border-border/30 bg-card p-3.5 shadow-sm"
                                >
                                    <div className="flex items-center justify-between gap-3">
                                        <span className="text-xs font-medium text-muted-foreground">
                                            {t('aiRoute.services.serviceIndex', { index: index + 1 })}
                                            {row.enabled ? '' : ` · ${t('aiRoute.services.disabledTag')}`}
                                        </span>
                                        <div className="flex items-center gap-2">
                                            <Switch
                                                checked={row.enabled}
                                                onCheckedChange={(checked) => updateServiceRow(index, { enabled: checked })}
                                            />
                                            <Button
                                                variant="ghost"
                                                size="icon"
                                                onClick={() => removeServiceRow(index)}
                                            >
                                                <Trash2 className="h-4 w-4" />
                                            </Button>
                                        </div>
                                    </div>
                                    <div className="grid gap-2.5 sm:grid-cols-2">
                                        <Input
                                            value={row.name}
                                            onChange={(event) => updateServiceRow(index, { name: event.target.value })}
                                            placeholder={t('aiRoute.services.namePlaceholder')}
                                            className="w-full rounded-lg"
                                        />
                                        <Input
                                            value={row.model}
                                            onChange={(event) => updateServiceRow(index, { model: event.target.value })}
                                            placeholder={t('aiRoute.services.modelPlaceholder')}
                                            className="w-full rounded-lg"
                                        />
                                        <Input
                                            value={row.baseUrl}
                                            onChange={(event) => updateServiceRow(index, { baseUrl: event.target.value })}
                                            placeholder={t('aiRoute.services.baseUrlPlaceholder')}
                                            className="w-full rounded-lg sm:col-span-2"
                                        />
                                        <Input
                                            type="password"
                                            value={row.apiKey}
                                            onChange={(event) => updateServiceRow(index, { apiKey: event.target.value })}
                                            placeholder={t('aiRoute.services.apiKeyPlaceholder')}
                                            className="w-full rounded-lg sm:col-span-2"
                                        />
                                    </div>
                                </div>
                            ))}
                            {serviceRows.length > 0 && (
                                <div className="flex items-center justify-between gap-3 pt-1">
                                    <span className="text-xs text-muted-foreground">
                                        {t('aiRoute.services.enabledCount', {
                                            enabled: enabledServiceCount,
                                            total: serviceRows.length,
                                        })}
                                    </span>
                                    <Button
                                        variant="outline"
                                        size="sm"
                                        onClick={() => saveServicesRows(serviceRows)}
                                    >
                                        {t('aiRoute.services.save')}
                                    </Button>
                                </div>
                            )}
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}
