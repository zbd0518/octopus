import { useTranslations } from 'next-intl';
import { CheckCircle2, AlertTriangle, Trash2 } from 'lucide-react';
import type { TestChannelSummary } from '@/api/endpoints/channel';
import { NodeProbeFeedback } from '@/components/common/NodeProbeFeedback';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Hint } from '@/components/ui/hint';
import { channelTestOutcome } from './test-results';

export function ChannelTestResults({ summary, isNode, onRemoveFailedKeys }: {
    summary: TestChannelSummary;
    isNode: boolean;
    onRemoveFailedKeys: () => void;
}) {
    const t = useTranslations('channel.form.test');
    const node = useTranslations('node');
    const outcome = channelTestOutcome(summary);
    return <div role="status" className="space-y-2 rounded-lg border border-border/25 bg-card p-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2 text-sm font-medium text-card-foreground">
                {outcome === 'success' ? <CheckCircle2 className="h-4 w-4 text-green-500" /> : <AlertTriangle className="h-4 w-4 text-destructive" />}
                <span>{t(outcome)} <Hint text={isNode ? node('test.hint') : t('hint')} /></span>
            </div>
            <div className="flex items-center gap-2">
                {!isNode && summary.results.some((result) => !result.passed) && <Button type="button" variant="ghost" size="sm" onClick={onRemoveFailedKeys} className="h-6 px-2 text-xs text-destructive/70 hover:bg-transparent hover:text-destructive">
                    <Trash2 className="mr-1 h-3 w-3" />{t('removeFailedKeys')}
                </Button>}
                <Badge variant="secondary">{summary.results.length} {t('results')}</Badge>
            </div>
        </div>
        <div className="max-h-60 space-y-2 overflow-y-auto">
            {summary.results.map((result, index) => <div key={index} className="space-y-1 rounded-lg border border-border/30 bg-card p-2.5 text-xs">
                {isNode ? <NodeProbeFeedback code={result.diagnostic_code ?? (result.passed ? 'ok' : 'upstream_failed')} latencyMS={result.latency_ms} modelCount={result.model_count} /> : <>
                    <div className="flex flex-wrap items-center justify-between gap-2">
                        <span className="min-w-0 break-all font-mono">{result.base_url}</span>
                        <div className="flex shrink-0 items-center gap-1">
                            <Badge variant="secondary">{result.key_masked || '-'}</Badge>
                            <Badge variant="secondary">{result.status_code || '-'}</Badge>
                        </div>
                    </div>
                    <div className="flex items-center justify-between gap-2 text-muted-foreground">
                        <span>{result.key_remark || t('noRemark')}</span>
                        <span>{result.latency_ms}ms · {result.passed ? t('pass') : t('fail')}</span>
                    </div>
                    {result.message && <p className="break-all text-muted-foreground">{result.message}</p>}
                </>}
            </div>)}
        </div>
    </div>;
}
