import { useTranslations } from 'next-intl';
import { nodeProbeCode } from '@/api/endpoints/node-probe';

export function NodeProbeFeedback({ code, latencyMS, modelCount }: { code: string; latencyMS: number; modelCount?: number }) {
    const t = useTranslations('node.test');
    return <div className="space-y-1 text-sm">
        <p className="break-words">{t(`codes.${nodeProbeCode(code)}`)}</p>
        <p className="text-xs text-muted-foreground">
            {t('latency', { milliseconds: latencyMS })}
            {modelCount !== undefined && ` · ${t('models', { count: modelCount })}`}
        </p>
    </div>;
}
