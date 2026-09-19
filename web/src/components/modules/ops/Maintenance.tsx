'use client';

import { useTranslations } from 'next-intl';
import { SettingCircuitBreaker } from '@/components/modules/setting/CircuitBreaker';
import { SettingRetry } from '@/components/modules/setting/Retry';
import { SettingResponseFilter } from '@/components/modules/setting/ResponseFilter';
import { SettingPrivacyProtection } from '@/components/modules/setting/PrivacyProtection';

type MaintenanceSectionId = 'retry' | 'circuit-breaker' | 'response-filter' | 'privacy-protection';

// 先配置重试策略，再配置熔断器保护阈值，符合用户操作的自然逻辑（见 issue #95 改动4）。
// 输出拦截与隐私保护同属内容治理，排在熔断器之后（issue 020）。
const SECTIONS: MaintenanceSectionId[] = ['retry', 'circuit-breaker', 'response-filter', 'privacy-protection'];

export function Maintenance() {
    const t = useTranslations('ops');

    return (
        <section className="space-y-4">
            <div className="space-y-1 px-1">
                <h3 className="text-base font-semibold">{t('tabs.maintenance')}</h3>
                <p className="text-sm leading-6 text-muted-foreground">{t('maintenance.description')}</p>
            </div>

            <div className="space-y-4">
                {SECTIONS.map((id) => (
                    <article
                        key={id}
                        className="rounded-xl border border-border/35 bg-card p-1 text-card-foreground shadow-sm"
                    >
                        {id === 'circuit-breaker' && <SettingCircuitBreaker />}
                        {id === 'retry' && <SettingRetry />}
                        {id === 'response-filter' && <SettingResponseFilter />}
                        {id === 'privacy-protection' && <SettingPrivacyProtection />}
                    </article>
                ))}
            </div>
        </section>
    );
}
