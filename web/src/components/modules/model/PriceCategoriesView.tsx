'use client'

import { useState } from 'react'
import { useTranslations } from 'next-intl'
import { Plus, Pencil, Trash2, RefreshCw, Tags } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  usePriceCategoryList,
  useCreatePriceCategory,
  useUpdatePriceCategory,
  useDeletePriceCategory,
  type ModelPriceCategory,
} from '@/api/endpoints/model'

type RuleType = 'exact' | 'prefix' | 'contains'

interface FormState {
  name: string
  rule_type: RuleType
  rule_value: string
  input: string
  output: string
  cache_read: string
  cache_write: string
  sort_order: string
  enabled: boolean
}

const EMPTY_FORM: FormState = {
  name: '',
  rule_type: 'contains',
  rule_value: '',
  input: '',
  output: '',
  cache_read: '',
  cache_write: '',
  sort_order: '0',
  enabled: true,
}

function formatPrice(v: number): string {
  return String(v ?? 0)
}

function parsePrice(v: string): number {
  const n = Number(v)
  return Number.isFinite(n) ? n : 0
}

export function PriceCategoriesView() {
  const t = useTranslations('model.priceCategory')
  const { data: categories, isLoading } = usePriceCategoryList()
  const createMutation = useCreatePriceCategory()
  const updateMutation = useUpdatePriceCategory()
  const deleteMutation = useDeletePriceCategory()

  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<ModelPriceCategory | null>(null)
  const [form, setForm] = useState<FormState>(EMPTY_FORM)

  const openCreate = () => {
    setEditing(null)
    setForm(EMPTY_FORM)
    setDialogOpen(true)
  }

  const openEdit = (cat: ModelPriceCategory) => {
    setEditing(cat)
    setForm({
      name: cat.name,
      rule_type: cat.rule_type as RuleType,
      rule_value: cat.rule_value,
      input: formatPrice(cat.input),
      output: formatPrice(cat.output),
      cache_read: formatPrice(cat.cache_read),
      cache_write: formatPrice(cat.cache_write),
      sort_order: String(cat.sort_order ?? 0),
      enabled: cat.enabled,
    })
    setDialogOpen(true)
  }

  const handleDelete = (id: number) => {
    if (window.confirm(t('confirmDelete'))) {
      deleteMutation.mutate(id, {
        onSuccess: () => toast.success(t('toastDeleted')),
        onError: (e: Error) => toast.error(e.message || t('toastError')),
      })
    }
  }

  const handleSubmit = () => {
    const payload = {
      name: form.name.trim(),
      rule_type: form.rule_type,
      rule_value: form.rule_value.trim(),
      input: parsePrice(form.input),
      output: parsePrice(form.output),
      cache_read: parsePrice(form.cache_read),
      cache_write: parsePrice(form.cache_write),
      sort_order: Number.parseInt(form.sort_order || '0', 10),
      enabled: form.enabled,
    }
    if (editing) {
      updateMutation.mutate(
        { ...payload, id: editing.id },
        {
          onSuccess: () => {
            toast.success(t('toastSaved'))
            setDialogOpen(false)
          },
          onError: (e: Error) => toast.error(e.message || t('toastError')),
        },
      )
    } else {
      createMutation.mutate(payload, {
        onSuccess: () => {
          toast.success(t('toastSaved'))
          setDialogOpen(false)
        },
        onError: (e: Error) => toast.error(e.message || t('toastError')),
      })
    }
  }

  const ruleLabel = (type: string) => {
    switch (type) {
      case 'exact':
        return <Badge variant="default">{t('exact')}</Badge>
      case 'prefix':
        return <Badge variant="secondary">{t('prefix')}</Badge>
      default:
        return <Badge variant="outline">{t('contains')}</Badge>
    }
  }

  return (
    <div className="space-y-3">
      <section className="rounded-2xl border border-border bg-card p-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-2">
            <h2 className="flex items-center gap-2 text-lg font-bold text-card-foreground">
              <Tags className="size-5" />
              {t('title')}
            </h2>
            <span className="rounded-full bg-muted/60 px-2.5 py-0.5 text-xs font-medium text-muted-foreground">
              {categories?.length ?? 0}
            </span>
          </div>
          <Button onClick={openCreate}>
            <Plus className="mr-1.5 size-4" />
            {t('add')}
          </Button>
        </div>
        <p className="mt-3 text-xs text-muted-foreground">{t('description')}</p>
      </section>

      {isLoading ? (
        <div className="flex h-32 items-center justify-center rounded-2xl border border-border bg-card">
          <RefreshCw className="size-5 animate-spin text-muted-foreground" />
        </div>
      ) : !categories || categories.length === 0 ? (
        <div className="flex h-40 flex-col items-center justify-center gap-3 rounded-2xl border border-dashed border-border text-sm text-muted-foreground">
          <Tags className="size-8 opacity-40" />
          {t('empty')}
        </div>
      ) : (
        <section className="overflow-hidden rounded-2xl border border-border bg-card">
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('name')}</TableHead>
                  <TableHead>{t('ruleType')}</TableHead>
                  <TableHead>{t('ruleValue')}</TableHead>
                  <TableHead className="text-right">{t('input')}</TableHead>
                  <TableHead className="text-right">{t('output')}</TableHead>
                  <TableHead className="text-right">{t('cacheRead')}</TableHead>
                  <TableHead className="text-right">{t('cacheWrite')}</TableHead>
                  <TableHead className="text-right">{t('sortOrder')}</TableHead>
                  <TableHead>{t('enabled')}</TableHead>
                  <TableHead className="text-right">{t('actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {categories.map((cat) => (
                  <TableRow key={cat.id}>
                    <TableCell className="font-medium">{cat.name}</TableCell>
                    <TableCell>{ruleLabel(cat.rule_type)}</TableCell>
                    <TableCell className="font-mono text-sm">{cat.rule_value}</TableCell>
                    <TableCell className="text-right font-mono text-sm">{cat.input}</TableCell>
                    <TableCell className="text-right font-mono text-sm">{cat.output}</TableCell>
                    <TableCell className="text-right font-mono text-sm">{cat.cache_read}</TableCell>
                    <TableCell className="text-right font-mono text-sm">{cat.cache_write}</TableCell>
                    <TableCell className="text-right">{cat.sort_order}</TableCell>
                    <TableCell>
                      {cat.enabled ? (
                        <Badge variant="default">{t('enabled')}</Badge>
                      ) : (
                        <Badge variant="secondary">{t('missing')}</Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => openEdit(cat)}
                          aria-label={t('edit')}
                        >
                          <Pencil className="size-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleDelete(cat.id)}
                          aria-label={t('delete')}
                        >
                          <Trash2 className="size-4 text-destructive" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </section>
      )}

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{editing ? t('edit') : t('add')}</DialogTitle>
            <DialogDescription>{t('description')}</DialogDescription>
          </DialogHeader>

          <div className="grid gap-4 py-2">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="grid gap-2">
                <Label htmlFor="pc-name">{t('name')} *</Label>
                <Input
                  id="pc-name"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  placeholder={t('namePlaceholder')}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="pc-sort">{t('sortOrder')}</Label>
                <Input
                  id="pc-sort"
                  type="number"
                  value={form.sort_order}
                  onChange={(e) => setForm({ ...form, sort_order: e.target.value })}
                />
                <p className="text-xs text-muted-foreground">{t('sortOrderHint')}</p>
              </div>
            </div>

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="grid gap-2">
                <Label htmlFor="pc-rule-type">{t('ruleType')} *</Label>
                <Select
                  value={form.rule_type}
                  onValueChange={(v) => setForm({ ...form, rule_type: v as RuleType })}
                >
                  <SelectTrigger id="pc-rule-type" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="exact">{t('exact')}</SelectItem>
                    <SelectItem value="prefix">{t('prefix')}</SelectItem>
                    <SelectItem value="contains">{t('contains')}</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="pc-rule-value">{t('ruleValue')} *</Label>
                <Input
                  id="pc-rule-value"
                  value={form.rule_value}
                  onChange={(e) => setForm({ ...form, rule_value: e.target.value })}
                  placeholder={t('ruleValuePlaceholder')}
                  className="font-mono"
                />
              </div>
            </div>

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
              <div className="grid gap-2">
                <Label htmlFor="pc-input">{t('input')}</Label>
                <Input
                  id="pc-input"
                  type="number"
                  step="any"
                  min="0"
                  value={form.input}
                  onChange={(e) => setForm({ ...form, input: e.target.value })}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="pc-output">{t('output')}</Label>
                <Input
                  id="pc-output"
                  type="number"
                  step="any"
                  min="0"
                  value={form.output}
                  onChange={(e) => setForm({ ...form, output: e.target.value })}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="pc-cache-read">{t('cacheRead')}</Label>
                <Input
                  id="pc-cache-read"
                  type="number"
                  step="any"
                  min="0"
                  value={form.cache_read}
                  onChange={(e) => setForm({ ...form, cache_read: e.target.value })}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="pc-cache-write">{t('cacheWrite')}</Label>
                <Input
                  id="pc-cache-write"
                  type="number"
                  step="any"
                  min="0"
                  value={form.cache_write}
                  onChange={(e) => setForm({ ...form, cache_write: e.target.value })}
                />
              </div>
            </div>
            <p className="text-xs text-muted-foreground">{t('priceHint')}</p>

            <div className="flex items-center gap-2">
              <Switch
                id="pc-enabled"
                checked={form.enabled}
                onCheckedChange={(checked) => setForm({ ...form, enabled: checked })}
              />
              <Label htmlFor="pc-enabled">{t('enabled')}</Label>
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>
              {t('cancel')}
            </Button>
            <Button
              onClick={handleSubmit}
              disabled={createMutation.isPending || updateMutation.isPending}
            >
              {createMutation.isPending || updateMutation.isPending ? t('saving') : t('save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}