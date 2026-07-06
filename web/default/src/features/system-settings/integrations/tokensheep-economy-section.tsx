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
// TokenSheep economy editor — visualizes the `tokensheep_economy` config
// struct (see setting/tokensheep_setting/economy.go) as an editable tier
// table + a few globals. All values on this screen live in station DOLLARS;
// the save handler converts to quota units (1 USD = 500,000 quota, matching
// common.QuotaPerUnit) before persisting so operators never touch the raw
// large integers.
//
// Save goes through PUT /api/option/ with keys of the form
// `tokensheep_economy.<field>` — the backend's handleConfigUpdate() reflects
// them onto the in-memory struct, matching the pattern the other
// GlobalConfig-registered sections use.
import { Loader2, Plus, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { useUpdateOption } from '../hooks/use-update-option'

const QUOTA_PER_DOLLAR = 500_000

// Wallet UI can survive missing tiers; the "free" row is not editable here
// because it isn't part of the paid-tier ladder — it's the default group.
const NON_EDITABLE_TIERS = new Set(['free'])

interface TierRow {
  tier: string
  threshold: string // dollars, string so the input can hold intermediate values like "10."
  award: string
  concurrency: string
}

export interface TokensheepEconomySettingsValues {
  TierThresholds: Record<string, number>
  CheckinAwardByGroup: Record<string, number>
  SessionLimits: Record<string, number>
  GiftPoolCap: number
  GiftPoolInactiveDays: number
  DowngradeInactiveDays: number
}

interface Props {
  defaultValues: TokensheepEconomySettingsValues
}

function quotaToDollarString(quota: number | undefined): string {
  if (!quota) return ''
  return String(quota / QUOTA_PER_DOLLAR)
}

function dollarStringToQuota(input: string): number {
  const trimmed = input.trim()
  if (trimmed === '') return 0
  const parsed = Number(trimmed)
  if (Number.isNaN(parsed) || parsed < 0) return 0
  return Math.round(parsed * QUOTA_PER_DOLLAR)
}

function buildInitialRows(
  defaults: TokensheepEconomySettingsValues
): TierRow[] {
  const tierNames = new Set<string>([
    ...Object.keys(defaults.TierThresholds || {}),
    ...Object.keys(defaults.CheckinAwardByGroup || {}),
    ...Object.keys(defaults.SessionLimits || {}),
  ])
  const rows: TierRow[] = []
  tierNames.forEach((tier) => {
    if (NON_EDITABLE_TIERS.has(tier)) return
    rows.push({
      tier,
      threshold: quotaToDollarString(defaults.TierThresholds?.[tier]),
      award: quotaToDollarString(defaults.CheckinAwardByGroup?.[tier]),
      concurrency: String(defaults.SessionLimits?.[tier] ?? ''),
    })
  })
  // Stable-sort by threshold ascending so the UI has a predictable order.
  rows.sort((a, b) => Number(a.threshold || 0) - Number(b.threshold || 0))
  return rows
}

export function TokensheepEconomySection({ defaultValues }: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [rows, setRows] = useState<TierRow[]>(() =>
    buildInitialRows(defaultValues)
  )
  const [giftCap, setGiftCap] = useState(
    quotaToDollarString(defaultValues.GiftPoolCap)
  )
  const [giftInactiveDays, setGiftInactiveDays] = useState(
    String(defaultValues.GiftPoolInactiveDays ?? 30)
  )
  const [downgradeInactiveDays, setDowngradeInactiveDays] = useState(
    String(defaultValues.DowngradeInactiveDays ?? 30)
  )
  const [freeSessionLimit, setFreeSessionLimit] = useState(
    String(defaultValues.SessionLimits?.free ?? 1)
  )
  const [saving, setSaving] = useState(false)
  const [dirtyStamp, setDirtyStamp] = useState(0)

  // Re-sync when parent hands us fresh defaults (e.g. after another save).
  useEffect(() => {
    setRows(buildInitialRows(defaultValues))
    setGiftCap(quotaToDollarString(defaultValues.GiftPoolCap))
    setGiftInactiveDays(String(defaultValues.GiftPoolInactiveDays ?? 30))
    setDowngradeInactiveDays(
      String(defaultValues.DowngradeInactiveDays ?? 30)
    )
    setFreeSessionLimit(String(defaultValues.SessionLimits?.free ?? 1))
    setDirtyStamp(0)
  }, [defaultValues])

  const dupTierNames = useMemo(() => {
    const counts: Record<string, number> = {}
    rows.forEach((row) => {
      const name = row.tier.trim()
      if (!name) return
      counts[name] = (counts[name] ?? 0) + 1
    })
    return new Set(
      Object.entries(counts)
        .filter(([, n]) => n > 1)
        .map(([name]) => name)
    )
  }, [rows])

  const canSave =
    !saving &&
    dirtyStamp > 0 &&
    dupTierNames.size === 0 &&
    rows.every((row) => row.tier.trim() !== '')

  const bumpDirty = () => setDirtyStamp((v) => v + 1)

  const updateRow = (index: number, patch: Partial<TierRow>) => {
    setRows((prev) =>
      prev.map((row, i) => (i === index ? { ...row, ...patch } : row))
    )
    bumpDirty()
  }

  const addRow = () => {
    setRows((prev) => [
      ...prev,
      { tier: '', threshold: '', award: '', concurrency: '' },
    ])
    bumpDirty()
  }

  const removeRow = (index: number) => {
    setRows((prev) => prev.filter((_, i) => i !== index))
    bumpDirty()
  }

  const handleSave = async () => {
    setSaving(true)
    try {
      // Assemble the three maps from the row list. Rows with a blank tier
      // name were caught by canSave; guard anyway.
      const thresholds: Record<string, number> = {}
      const awards: Record<string, number> = {}
      const sessions: Record<string, number> = {
        free: Number(freeSessionLimit) || 1,
      }
      for (const row of rows) {
        const tier = row.tier.trim()
        if (!tier) continue
        thresholds[tier] = dollarStringToQuota(row.threshold)
        awards[tier] = dollarStringToQuota(row.award)
        sessions[tier] = Math.max(0, Math.floor(Number(row.concurrency) || 0))
      }

      const updates: Array<{ key: string; value: string }> = [
        {
          key: 'tokensheep_economy.tier_thresholds',
          value: JSON.stringify(thresholds),
        },
        {
          key: 'tokensheep_economy.checkin_award_by_group',
          value: JSON.stringify(awards),
        },
        {
          key: 'tokensheep_economy.session_limits',
          value: JSON.stringify(sessions),
        },
        {
          key: 'tokensheep_economy.gift_pool_cap',
          value: String(dollarStringToQuota(giftCap)),
        },
        {
          key: 'tokensheep_economy.gift_pool_inactive_days',
          value: String(Math.max(0, Math.floor(Number(giftInactiveDays) || 0))),
        },
        {
          key: 'tokensheep_economy.downgrade_inactive_days',
          value: String(
            Math.max(0, Math.floor(Number(downgradeInactiveDays) || 0))
          ),
        },
      ]

      for (const update of updates) {
        await updateOption.mutateAsync(update)
      }
      toast.success(t('Saved'))
      setDirtyStamp(0)
    } catch (err) {
      toast.error(
        `${t('Save failed')}: ${
          err instanceof Error ? err.message : String(err)
        }`
      )
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className='space-y-6 pt-4'>
      <div>
        <h3 className='text-lg font-medium'>{t('TokenSheep Economy')}</h3>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Tier ladder + daily gift + concurrency caps. All amounts here are in station dollars — the backend stores quota units and does the conversion for you.'
          )}
        </p>
      </div>

      {dupTierNames.size > 0 && (
        <Alert variant='destructive'>
          <AlertDescription>
            {t('Duplicate tier names:')}{' '}
            {Array.from(dupTierNames).join(', ')}
          </AlertDescription>
        </Alert>
      )}

      <div className='space-y-3'>
        <div className='flex items-center justify-between'>
          <Label className='text-muted-foreground text-xs font-medium tracking-wider uppercase'>
            {t('Paid tiers')}
          </Label>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={addRow}
            className='gap-1'
          >
            <Plus className='h-3.5 w-3.5' />
            {t('Add tier')}
          </Button>
        </div>

        <div className='overflow-x-auto rounded-md border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40'>
              <tr>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Tier name')}
                </th>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Threshold ($)')}
                </th>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Daily gift ($)')}
                </th>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Concurrency')}
                </th>
                <th className='w-10 px-2 py-2' />
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 && (
                <tr>
                  <td
                    colSpan={5}
                    className='text-muted-foreground px-3 py-6 text-center text-xs'
                  >
                    {t('No tiers configured. Click "Add tier" to start.')}
                  </td>
                </tr>
              )}
              {rows.map((row, index) => (
                <tr key={index} className='border-t'>
                  <td className='px-3 py-2'>
                    <Input
                      value={row.tier}
                      onChange={(e) =>
                        updateRow(index, { tier: e.target.value })
                      }
                      placeholder='supporter'
                      className='h-8'
                    />
                  </td>
                  <td className='px-3 py-2'>
                    <Input
                      value={row.threshold}
                      onChange={(e) =>
                        updateRow(index, { threshold: e.target.value })
                      }
                      inputMode='decimal'
                      placeholder='10'
                      className='h-8'
                    />
                  </td>
                  <td className='px-3 py-2'>
                    <Input
                      value={row.award}
                      onChange={(e) =>
                        updateRow(index, { award: e.target.value })
                      }
                      inputMode='decimal'
                      placeholder='0.5'
                      className='h-8'
                    />
                  </td>
                  <td className='px-3 py-2'>
                    <Input
                      value={row.concurrency}
                      onChange={(e) =>
                        updateRow(index, { concurrency: e.target.value })
                      }
                      inputMode='numeric'
                      placeholder='3'
                      className='h-8'
                    />
                  </td>
                  <td className='px-2 py-2'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      onClick={() => removeRow(index)}
                      className='h-8 w-8'
                    >
                      <Trash2 className='h-3.5 w-3.5' />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <div className='space-y-3'>
        <Label className='text-muted-foreground text-xs font-medium tracking-wider uppercase'>
          {t('Free tier concurrency')}
        </Label>
        <Input
          value={freeSessionLimit}
          onChange={(e) => {
            setFreeSessionLimit(e.target.value)
            bumpDirty()
          }}
          inputMode='numeric'
          placeholder='1'
          className='h-9 max-w-40'
        />
        <p className='text-muted-foreground text-xs'>
          {t(
            'Concurrency cap for the "free" default group. Free tier has no threshold, no daily gift.'
          )}
        </p>
      </div>

      <div className='grid gap-4 md:grid-cols-3'>
        <div className='space-y-2'>
          <Label>{t('Gift pool cap ($)')}</Label>
          <Input
            value={giftCap}
            onChange={(e) => {
              setGiftCap(e.target.value)
              bumpDirty()
            }}
            inputMode='decimal'
            placeholder='50'
          />
          <p className='text-muted-foreground text-xs'>
            {t('Maximum accumulated gift balance per account.')}
          </p>
        </div>
        <div className='space-y-2'>
          <Label>{t('Gift pool inactive days')}</Label>
          <Input
            value={giftInactiveDays}
            onChange={(e) => {
              setGiftInactiveDays(e.target.value)
              bumpDirty()
            }}
            inputMode='numeric'
            placeholder='30'
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Reset quota_gift to 0 after this many days without an API request.'
            )}
          </p>
        </div>
        <div className='space-y-2'>
          <Label>{t('Downgrade inactive days')}</Label>
          <Input
            value={downgradeInactiveDays}
            onChange={(e) => {
              setDowngradeInactiveDays(e.target.value)
              bumpDirty()
            }}
            inputMode='numeric'
            placeholder='30'
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Downgrade to free after this many days without a new donation.'
            )}
          </p>
        </div>
      </div>

      <div className='flex justify-end'>
        <Button type='button' onClick={handleSave} disabled={!canSave}>
          {saving && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
          {t('Save')}
        </Button>
      </div>
    </div>
  )
}
