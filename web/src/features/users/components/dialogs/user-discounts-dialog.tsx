/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Loader2, Plus, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  getSystemOptions,
  updateSystemOption,
} from '@/features/system-settings/api'

const USER_MODEL_RATIO_KEY = 'UserModelRatio'

type UserModelRatioMap = Record<string, Record<string, number>>

interface DiscountRow {
  model: string
  ratio: string
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: { id: number; username: string }
}

// 管理单个用户的按模型折扣。数据仍存于全局 UserModelRatio 选项（分组定价页的
// JSON 编辑器为备用入口），此处读改写该 JSON 中当前用户的条目。
export function UserDiscountsDialog({ open, onOpenChange, user }: Props) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<DiscountRow[]>([])
  const [fullMap, setFullMap] = useState<UserModelRatioMap>({})
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    let cancelled = false
    setLoading(true)
    getSystemOptions()
      .then((res) => {
        if (cancelled) return
        const raw =
          res.data?.find((o) => o.key === USER_MODEL_RATIO_KEY)?.value ?? '{}'
        let parsed: UserModelRatioMap = {}
        try {
          parsed = JSON.parse(raw || '{}') as UserModelRatioMap
        } catch {
          parsed = {}
        }
        setFullMap(parsed)
        const mine = parsed[String(user.id)] ?? {}
        setRows(
          Object.entries(mine)
            .sort(([a], [b]) => a.localeCompare(b))
            .map(([model, ratio]) => ({ model, ratio: String(ratio) }))
        )
      })
      .catch(() => {
        if (!cancelled) toast.error(t('Failed to load discounts'))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [open, user.id, t])

  const validate = (): string | null => {
    const seen = new Set<string>()
    for (const row of rows) {
      const model = row.model.trim()
      if (!model || model === '*') return t('Model name is required')
      const star = model.indexOf('*')
      if (star >= 0 && star !== model.length - 1)
        return t("'*' is only allowed at the end as a prefix wildcard")
      if (seen.has(model)) return t('Duplicate model: {{model}}', { model })
      seen.add(model)
      const ratio = Number(row.ratio)
      if (!Number.isFinite(ratio) || ratio <= 0 || ratio > 10)
        return t('Ratio must be greater than 0 and at most 10')
    }
    return null
  }

  const handleSave = async () => {
    const error = validate()
    if (error) {
      toast.error(error)
      return
    }
    setSaving(true)
    try {
      const next: UserModelRatioMap = { ...fullMap }
      const mine: Record<string, number> = {}
      for (const row of rows) {
        mine[row.model.trim()] = Number(row.ratio)
      }
      if (Object.keys(mine).length > 0) {
        next[String(user.id)] = mine
      } else {
        delete next[String(user.id)]
      }
      const res = await updateSystemOption({
        key: USER_MODEL_RATIO_KEY,
        value: JSON.stringify(next),
      })
      if (res.success) {
        toast.success(t('Discounts saved'))
        onOpenChange(false)
      } else {
        toast.error(res.message || t('Failed to save discounts'))
      }
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : t('Failed to save discounts')
      )
    } finally {
      setSaving(false)
    }
  }

  const updateRow = (index: number, patch: Partial<DiscountRow>) => {
    setRows((prev) =>
      prev.map((row, i) => (i === index ? { ...row, ...patch } : row))
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Manage Discounts')}
      description={t(
        'Per-model price multipliers for {{username}} (user ID {{id}}). A trailing * matches model prefixes; exact names beat the longest prefix. 0.55 bills at 55% of the list price.',
        { username: user.username, id: user.id }
      )}
      contentClassName='sm:max-w-lg'
      footer={
        <>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSave} disabled={saving || loading}>
            {saving ? t('Saving...') : t('Save')}
          </Button>
        </>
      }
    >
      {loading ? (
        <div className='flex justify-center py-8'>
          <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
        </div>
      ) : (
        <div className='space-y-2'>
          {rows.length > 0 && (
            <div className='text-muted-foreground flex gap-2 text-xs font-medium'>
              <span className='flex-1'>{t('Model')}</span>
              <span className='w-24'>{t('Ratio')}</span>
              <span className='w-8' />
            </div>
          )}
          {rows.map((row, index) => (
            <div key={index} className='flex items-center gap-2'>
              <Input
                value={row.model}
                placeholder={t('Model or prefix*')}
                onChange={(e) => updateRow(index, { model: e.target.value })}
                className='flex-1'
              />
              <Input
                value={row.ratio}
                type='number'
                step='0.05'
                min={0}
                max={10}
                onChange={(e) => updateRow(index, { ratio: e.target.value })}
                className='w-24'
              />
              <Button
                variant='ghost'
                size='icon-sm'
                aria-label={t('Delete')}
                onClick={() =>
                  setRows((prev) => prev.filter((_, i) => i !== index))
                }
              >
                <Trash2 className='h-4 w-4' />
              </Button>
            </div>
          ))}
          {rows.length === 0 && (
            <p className='text-muted-foreground py-2 text-sm'>
              {t('No discounts configured')}
            </p>
          )}
          <Button
            variant='outline'
            size='sm'
            onClick={() =>
              setRows((prev) => [...prev, { model: '', ratio: '1' }])
            }
          >
            <Plus className='mr-1 h-4 w-4' />
            {t('Add discount')}
          </Button>
        </div>
      )}
    </Dialog>
  )
}
