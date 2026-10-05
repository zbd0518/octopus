'use client';

import { useEffect, useId, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Loader2, ShieldAlert } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
    Dialog,
    DialogContent,
    DialogFooter,
    DialogHeader,
    DialogTitle,
    DialogDescription,
} from '@/components/ui/dialog';
import {
    AlertDialog,
    AlertDialogContent,
    AlertDialogHeader,
    AlertDialogTitle,
    AlertDialogDescription,
    AlertDialogFooter,
    AlertDialogCancel,
} from '@/components/ui/alert-dialog';
import { toast } from '@/components/common/Toast';
import {
    useDeletePoolAccount,
    useImportPoolAccounts,
    useTempUnschedPoolAccount,
    type PoolAccount,
} from '@/api/endpoints/pool';
import { parseAccountImport } from './display';

type PoolOperationDialogsProps = {
    poolId: number;
    deleteTarget: PoolAccount | null;
    onDeleteTargetChange: (account: PoolAccount | null) => void;
    pauseTarget: PoolAccount | null;
    onPauseTargetChange: (account: PoolAccount | null) => void;
    importOpen: boolean;
    onImportOpenChange: (open: boolean) => void;
    exportOpen: boolean;
    onExportOpenChange: (open: boolean) => void;
    exporting: boolean;
    onExport: () => void;
};

export function PoolOperationDialogs({
    poolId,
    deleteTarget,
    onDeleteTargetChange,
    pauseTarget,
    onPauseTargetChange,
    importOpen,
    onImportOpenChange,
    exportOpen,
    onExportOpenChange,
    exporting,
    onExport,
}: PoolOperationDialogsProps) {
    const t = useTranslations('pool');
    const id = useId();
    const deleteAccount = useDeletePoolAccount(poolId);
    const importAccounts = useImportPoolAccounts();
    const pause = useTempUnschedPoolAccount(poolId);
    const [importText, setImportText] = useState('');
    const [minutes, setMinutes] = useState('10');
    const [reason, setReason] = useState('');

    const importResult = parseAccountImport(importText);
    const validMinutes = minutes.trim() !== '' && Number.isSafeInteger(Number(minutes));

    useEffect(() => { setMinutes('10'); setReason(''); }, [pauseTarget]);
    useEffect(() => { if (!importOpen) setImportText(''); }, [importOpen]);

    return (
        <>
            {/* 删除账号确认 */}
            <AlertDialog open={deleteTarget !== null} onOpenChange={(open) => { if (!open && !deleteAccount.isPending) onDeleteTargetChange(null); }}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle>{t('ui.deleteAccountTitle')}</AlertDialogTitle>
                        <AlertDialogDescription className="break-words">
                            {t('ui.deleteAccountDescription', { name: deleteTarget?.name || `#${deleteTarget?.id}` })}
                        </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel disabled={deleteAccount.isPending}>{t('cancel')}</AlertDialogCancel>
                        <Button
                            variant="destructive"
                            disabled={deleteAccount.isPending}
                            onClick={() => {
                                if (!deleteTarget) return;
                                deleteAccount.mutate(deleteTarget.id, {
                                    onSuccess: () => {
                                        onDeleteTargetChange(null);
                                        toast.success(t('accountDeleted'));
                                    },
                                });
                            }}
                        >
                            {deleteAccount.isPending && <Loader2 className="size-4 animate-spin" />}
                            {t(deleteAccount.isPending ? 'ui.deleting' : 'ui.confirmDelete')}
                        </Button>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>

            {/* 临时禁用账号 */}
            <Dialog open={pauseTarget !== null} onOpenChange={(open) => { if (!open && !pause.isPending) onPauseTargetChange(null); }}>
                <DialogContent className="flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden">
                    <DialogHeader className="shrink-0 pr-6 text-left">
                        <DialogTitle>{t('tempUnschedDialog.title')}</DialogTitle>
                        <DialogDescription className="break-words">
                            {t('ui.tempUnschedDescription', { name: pauseTarget?.name || `#${pauseTarget?.id}` })}
                        </DialogDescription>
                    </DialogHeader>
                    <form
                        className="flex min-h-0 flex-col gap-4"
                        onSubmit={(e) => {
                            e.preventDefault();
                            if (!pauseTarget || !validMinutes || pause.isPending) return;
                            pause.mutate(
                                { accountId: pauseTarget.id, minutes: Number(minutes), reason: reason.trim() },
                                {
                                    onSuccess: () => {
                                        onPauseTargetChange(null);
                                        toast.success(t('tempUnschedApplied'));
                                    },
                                },
                            );
                        }}
                    >
                        <div className="min-h-0 space-y-4 overflow-y-auto">
                            <div className="space-y-2">
                                <Label htmlFor={`${id}-minutes`}>{t('tempUnschedDialog.minutes')}</Label>
                                <Input
                                    id={`${id}-minutes`}
                                    type="number"
                                    step={1}
                                    required
                                    value={minutes}
                                    onChange={(e) => setMinutes(e.target.value)}
                                    aria-invalid={!validMinutes}
                                />
                                <p className="text-xs text-muted-foreground">{t('tempUnschedDialog.minutesHint')}</p>
                            </div>
                            <div className="space-y-2">
                                <Label htmlFor={`${id}-reason`}>{t('tempUnschedDialog.reason')}</Label>
                                <Input
                                    id={`${id}-reason`}
                                    value={reason}
                                    onChange={(e) => setReason(e.target.value)}
                                />
                            </div>
                        </div>
                        <DialogFooter className="shrink-0 pb-[env(safe-area-inset-bottom)]">
                            <Button type="button" variant="outline" disabled={pause.isPending} onClick={() => onPauseTargetChange(null)}>
                                {t('cancel')}
                            </Button>
                            <Button type="submit" disabled={!validMinutes || pause.isPending}>
                                {pause.isPending && <Loader2 className="size-4 animate-spin" />}
                                {t('tempUnschedDialog.confirm')}
                            </Button>
                        </DialogFooter>
                    </form>
                </DialogContent>
            </Dialog>

            {/* 批量导入账号 */}
            <Dialog open={importOpen} onOpenChange={(open) => { if (!importAccounts.isPending) onImportOpenChange(open); }}>
                <DialogContent className="flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden sm:max-w-2xl">
                    <DialogHeader className="shrink-0 pr-6 text-left">
                        <DialogTitle>{t('importAccounts')}</DialogTitle>
                        <DialogDescription>{t('ui.importDescription')}</DialogDescription>
                    </DialogHeader>
                    <form
                        className="flex min-h-0 flex-col gap-4"
                        onSubmit={(e) => {
                            e.preventDefault();
                            if (!importResult.valid || importAccounts.isPending) return;
                            importAccounts.mutate(
                                { poolId, accounts: importText.trim() },
                                {
                                    onSuccess: (result) => {
                                        onImportOpenChange(false);
                                        toast.success(t('importSuccess', { count: result.imported }));
                                    },
                                },
                            );
                        }}
                    >
                        <div className="min-h-0 space-y-2 overflow-y-auto">
                            <Label htmlFor={`${id}-import`}>{t('credentials')}</Label>
                            <textarea
                                id={`${id}-import`}
                                rows={10}
                                spellCheck={false}
                                autoComplete="off"
                                className="max-h-[45dvh] min-h-40 w-full resize-y rounded-lg border border-input bg-background p-3 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
                                value={importText}
                                onChange={(e) => setImportText(e.target.value)}
                                placeholder={t('importPlaceholder')}
                                aria-invalid={Boolean(importText.trim()) && !importResult.valid}
                            />
                            <p
                                className={`text-xs ${importText.trim() && !importResult.valid ? 'text-destructive' : 'text-muted-foreground'}`}
                                role="status"
                            >
                                {importResult.valid
                                    ? t('ui.importCount', { count: importResult.count })
                                    : importText.trim()
                                        ? t('ui.invalidImport')
                                        : t('importHint')}
                            </p>
                        </div>
                        <DialogFooter className="shrink-0 pb-[env(safe-area-inset-bottom)]">
                            <Button type="button" variant="outline" disabled={importAccounts.isPending} onClick={() => onImportOpenChange(false)}>
                                {t('cancel')}
                            </Button>
                            <Button type="submit" disabled={!importResult.valid || importAccounts.isPending}>
                                {importAccounts.isPending && <Loader2 className="size-4 animate-spin" />}
                                {t('import')}
                            </Button>
                        </DialogFooter>
                    </form>
                </DialogContent>
            </Dialog>

            {/* 导出账号二次确认 */}
            <AlertDialog open={exportOpen} onOpenChange={(open) => { if (!exporting) onExportOpenChange(open); }}>
                <AlertDialogContent>
                    <AlertDialogHeader>
                        <AlertDialogTitle className="flex items-center gap-2">
                            <ShieldAlert className="size-5 text-amber-600" />
                            {t('ui.exportTitle')}
                        </AlertDialogTitle>
                        <AlertDialogDescription>{t('ui.exportWarning')}</AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                        <AlertDialogCancel disabled={exporting}>{t('cancel')}</AlertDialogCancel>
                        <Button disabled={exporting} onClick={onExport}>
                            {exporting && <Loader2 className="size-4 animate-spin" />}
                            {t('ui.exportConfirm')}
                        </Button>
                    </AlertDialogFooter>
                </AlertDialogContent>
            </AlertDialog>
        </>
    );
}
