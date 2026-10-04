'use client';

import { useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Database, Layers, Timer, Gauge, Filter, TrendingDown } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Hint } from '@/components/ui/hint';
import { Switch } from '@/components/ui/switch';
import { useSettingList, useSetSetting, SettingKey } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';

export function SettingPool() {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();

    const [intervalMin, setIntervalMin] = useState('');
    const [failThreshold, setFailThreshold] = useState('');
    const [minPriority, setMinPriority] = useState('');
    const [healthEnabled, setHealthEnabled] = useState(false);
    const [layeredEnabled, setLayeredEnabled] = useState(false);
    const [stickyEscapeEnabled, setStickyEscapeEnabled] = useState(false);
    const [stickyEscapeErrorRate, setStickyEscapeErrorRate] = useState('');
    const [stickyEscapeTTFTMs, setStickyEscapeTTFTMs] = useState('');
    const [weightReset, setWeightReset] = useState('');
    const [weightQuota, setWeightQuota] = useState('');

    const initInterval = useRef('');
    const initThreshold = useRef('');
    const initMinPriority = useRef('');
    const initHealthEnabled = useRef(false);
    const initLayeredEnabled = useRef(false);
    const initStickyEscapeEnabled = useRef(false);
    const initStickyEscapeErrorRate = useRef('');
    const initStickyEscapeTTFTMs = useRef('');
    const initWeightReset = useRef('');
    const initWeightQuota = useRef('');

    useEffect(() => {
        if (!settings) return;
        const get = (k: string) => settings.find((s) => s.key === k)?.value;
        const im = get(SettingKey.PoolHealthCheckInterval);
        const ft = get(SettingKey.PoolHealthCheckFailThreshold);
        const mp = get(SettingKey.PoolMinPriority);
        const he = get(SettingKey.PoolHealthCheckEnabled);
        const le = get(SettingKey.PoolLayeredFilterEnabled);
        const se = get(SettingKey.PoolStickyEscapeEnabled);
        const sr = get(SettingKey.PoolStickyEscapeErrorRate);
        const st = get(SettingKey.PoolStickyEscapeTTFTMs);
        const wr = get(SettingKey.PoolSchedulerWeightReset);
        const wq = get(SettingKey.PoolSchedulerWeightQuota);
        queueMicrotask(() => {
            if (im !== undefined) { setIntervalMin(im); initInterval.current = im; }
            if (ft !== undefined) { setFailThreshold(ft); initThreshold.current = ft; }
            if (mp !== undefined) { setMinPriority(mp); initMinPriority.current = mp; }
            if (he !== undefined) { const v = he === 'true'; setHealthEnabled(v); initHealthEnabled.current = v; }
            if (le !== undefined) { const v = le === 'true'; setLayeredEnabled(v); initLayeredEnabled.current = v; }
            if (se !== undefined) { const v = se === 'true'; setStickyEscapeEnabled(v); initStickyEscapeEnabled.current = v; }
            if (sr !== undefined) { setStickyEscapeErrorRate(sr); initStickyEscapeErrorRate.current = sr; }
            if (st !== undefined) { setStickyEscapeTTFTMs(st); initStickyEscapeTTFTMs.current = st; }
            if (wr !== undefined) { setWeightReset(wr); initWeightReset.current = wr; }
            if (wq !== undefined) { setWeightQuota(wq); initWeightQuota.current = wq; }
        });
    }, [settings]);

    const saveValue = (key: string, value: string, initial: string) => {
        if (value === initial) return;
        setSetting.mutate({ key, value }, {
            onSuccess: () => {
                toast.success(t('saved'));
                if (key === SettingKey.PoolHealthCheckInterval) initInterval.current = value;
                else if (key === SettingKey.PoolHealthCheckFailThreshold) initThreshold.current = value;
                else if (key === SettingKey.PoolMinPriority) initMinPriority.current = value;
                else if (key === SettingKey.PoolStickyEscapeErrorRate) initStickyEscapeErrorRate.current = value;
                else if (key === SettingKey.PoolStickyEscapeTTFTMs) initStickyEscapeTTFTMs.current = value;
                else if (key === SettingKey.PoolSchedulerWeightReset) initWeightReset.current = value;
                else if (key === SettingKey.PoolSchedulerWeightQuota) initWeightQuota.current = value;
            },
        });
    };

    const saveBool = (key: string, value: boolean) => {
        const str = value ? 'true' : 'false';
        setSetting.mutate({ key, value: str }, {
            onSuccess: () => {
                toast.success(t('saved'));
                if (key === SettingKey.PoolHealthCheckEnabled) initHealthEnabled.current = value;
                else if (key === SettingKey.PoolLayeredFilterEnabled) initLayeredEnabled.current = value;
                else if (key === SettingKey.PoolStickyEscapeEnabled) initStickyEscapeEnabled.current = value;
            },
        });
    };

    return (
        <div className="rounded-xl border-border/35 bg-card p-6 space-y-5 text-card-foreground shadow-md">
            <h2 className="text-lg font-bold text-card-foreground flex items-center gap-2">
                <Layers className="h-5 w-5" />
                {t('pool.title')}
            </h2>
            <p className="text-sm text-muted-foreground">{t('pool.description')}</p>

            <div className="space-y-4">
                {/* 巡检总开关 */}
                <div className="flex items-center justify-between rounded-lg border border-border/50 p-3">
                    <div className="space-y-1">
                        <div className="text-sm font-medium flex items-center gap-2">
                            <Gauge className="h-4 w-4" />
                            {t('pool.healthCheckEnabled')}
                            <Hint text={t('pool.healthCheckEnabledHint')} />
                        </div>
                    </div>
                    <Switch
                        checked={healthEnabled}
                        onCheckedChange={(v) => { setHealthEnabled(v); saveBool(SettingKey.PoolHealthCheckEnabled, v); }}
                    />
                </div>

                {/* 巡检间隔 / 阈值 */}
                <div className="grid grid-cols-2 gap-3">
                    <div>
                        <label className="text-xs text-muted-foreground flex items-center gap-1">
                            <Timer className="h-3 w-3" />
                            {t('pool.healthCheckInterval')}
                        </label>
                        <Input
                            className="mt-1"
                            type="number"
                            value={intervalMin}
                            onChange={(e) => setIntervalMin(e.target.value)}
                            onBlur={() => saveValue(SettingKey.PoolHealthCheckInterval, intervalMin, initInterval.current)}
                            placeholder="30"
                        />
                    </div>
                    <div>
                        <label className="text-xs text-muted-foreground flex items-center gap-1">
                            <Database className="h-3 w-3" />
                            {t('pool.healthCheckFailThreshold')}
                        </label>
                        <Input
                            className="mt-1"
                            type="number"
                            value={failThreshold}
                            onChange={(e) => setFailThreshold(e.target.value)}
                            onBlur={() => saveValue(SettingKey.PoolHealthCheckFailThreshold, failThreshold, initThreshold.current)}
                            placeholder="3"
                        />
                    </div>
                </div>

                {/* 分层过滤 */}
                <div className="flex items-center justify-between rounded-lg border border-border/50 p-3">
                    <div className="space-y-1">
                        <div className="text-sm font-medium flex items-center gap-2">
                            <Filter className="h-4 w-4" />
                            {t('pool.layeredFilterEnabled')}
                            <Hint text={t('pool.layeredFilterEnabledHint')} />
                        </div>
                    </div>
                    <Switch
                        checked={layeredEnabled}
                        onCheckedChange={(v) => { setLayeredEnabled(v); saveBool(SettingKey.PoolLayeredFilterEnabled, v); }}
                    />
                </div>

                <div>
                    <label className="text-xs text-muted-foreground flex items-center gap-1">
                        {t('pool.minPriority')}
                        <Hint text={t('pool.minPriorityHint')} />
                    </label>
                    <Input
                        className="mt-1"
                        type="number"
                        value={minPriority}
                        onChange={(e) => setMinPriority(e.target.value)}
                        onBlur={() => saveValue(SettingKey.PoolMinPriority, minPriority, initMinPriority.current)}
                        placeholder="-9999"
                    />
                </div>

                {/* 粘性逃逸 */}
                <div className="flex items-center justify-between rounded-lg border border-border/50 p-3">
                    <div className="space-y-1">
                        <div className="text-sm font-medium flex items-center gap-2">
                            <TrendingDown className="h-4 w-4" />
                            {t('pool.stickyEscapeEnabled')}
                            <Hint text={t('pool.stickyEscapeEnabledHint')} />
                        </div>
                    </div>
                    <Switch
                        checked={stickyEscapeEnabled}
                        onCheckedChange={(v) => { setStickyEscapeEnabled(v); saveBool(SettingKey.PoolStickyEscapeEnabled, v); }}
                    />
                </div>

                <div className="grid grid-cols-2 gap-3">
                    <div>
                        <label className="text-xs text-muted-foreground flex items-center gap-1">
                            {t('pool.stickyEscapeErrorRate')}
                            <Hint text={t('pool.stickyEscapeErrorRateHint')} />
                        </label>
                        <Input
                            className="mt-1"
                            type="number"
                            step="0.05"
                            min="0"
                            max="1"
                            value={stickyEscapeErrorRate}
                            onChange={(e) => setStickyEscapeErrorRate(e.target.value)}
                            onBlur={() => saveValue(SettingKey.PoolStickyEscapeErrorRate, stickyEscapeErrorRate, initStickyEscapeErrorRate.current)}
                            placeholder="0.5"
                        />
                    </div>
                    <div>
                        <label className="text-xs text-muted-foreground flex items-center gap-1">
                            {t('pool.stickyEscapeTTFTMs')}
                            <Hint text={t('pool.stickyEscapeTTFTMsHint')} />
                        </label>
                        <Input
                            className="mt-1"
                            type="number"
                            min="0"
                            value={stickyEscapeTTFTMs}
                            onChange={(e) => setStickyEscapeTTFTMs(e.target.value)}
                            onBlur={() => saveValue(SettingKey.PoolStickyEscapeTTFTMs, stickyEscapeTTFTMs, initStickyEscapeTTFTMs.current)}
                            placeholder="15000"
                        />
                    </div>
                </div>

                {/* 调度因子权重（默认 0 = 关闭） */}
                <div className="grid grid-cols-2 gap-3">
                    <div>
                        <label className="text-xs text-muted-foreground flex items-center gap-1">
                            {t('pool.schedulerWeightReset')}
                            <Hint text={t('pool.schedulerWeightResetHint')} />
                        </label>
                        <Input
                            className="mt-1"
                            type="number"
                            step="0.1"
                            min="0"
                            value={weightReset}
                            onChange={(e) => setWeightReset(e.target.value)}
                            onBlur={() => saveValue(SettingKey.PoolSchedulerWeightReset, weightReset, initWeightReset.current)}
                            placeholder="0"
                        />
                    </div>
                    <div>
                        <label className="text-xs text-muted-foreground flex items-center gap-1">
                            {t('pool.schedulerWeightQuota')}
                            <Hint text={t('pool.schedulerWeightQuotaHint')} />
                        </label>
                        <Input
                            className="mt-1"
                            type="number"
                            step="0.1"
                            min="0"
                            value={weightQuota}
                            onChange={(e) => setWeightQuota(e.target.value)}
                            onBlur={() => saveValue(SettingKey.PoolSchedulerWeightQuota, weightQuota, initWeightQuota.current)}
                            placeholder="0"
                        />
                    </div>
                </div>
            </div>
        </div>
    );
}
