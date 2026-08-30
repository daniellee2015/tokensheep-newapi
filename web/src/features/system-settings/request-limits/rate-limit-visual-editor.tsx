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
import { Plus, Search } from 'lucide-react'
import { useState, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { useSystemOptions } from '../hooks/use-system-options'
import { safeJsonParseWithValidation } from '../utils/json-parser'
import { isObjectRecord } from '../utils/json-validators'
import {
  classifyRateLimitGroup,
  parseGroupNameSet,
  type RateLimitGroupKind,
} from './classify-rate-limit-group'
import { RateLimitDialog, type RateLimitEntryData } from './rate-limit-dialog'

type RateLimitVisualEditorProps = {
  value: string
  onChange: (value: string) => void
}

type RateLimitEntry = RateLimitEntryData & {
  kind: RateLimitGroupKind
}

// R17-C: the tokensheep_economy option keys we pull classification data
// from. Both are also fetched by the billing tokensheep-economy-section,
// and `useSystemOptions` is a shared 5-min-staleTime query, so mounting
// this editor does not trigger an extra network round-trip.
const TIER_THRESHOLDS_KEY = 'tokensheep_economy.tier_thresholds'
const COMMERCIAL_GROUPS_KEY = 'tokensheep_economy.commercial_groups'

export function RateLimitVisualEditor({
  value,
  onChange,
}: RateLimitVisualEditorProps) {
  const { t } = useTranslation()
  const [searchText, setSearchText] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editData, setEditData] = useState<RateLimitEntry | null>(null)

  // Read classification data from the shared /api/option/ query. The parent
  // SettingsPage has already primed this cache, so this call is free.
  const { data: optionsData } = useSystemOptions()

  const { tierNames, commercialNames } = useMemo(() => {
    const rows = optionsData?.data ?? []
    const find = (key: string) => rows.find((r) => r.key === key)?.value
    return {
      tierNames: parseGroupNameSet(find(TIER_THRESHOLDS_KEY)),
      commercialNames: parseGroupNameSet(find(COMMERCIAL_GROUPS_KEY)),
    }
  }, [optionsData])

  const rateLimits = useMemo<RateLimitEntry[]>(() => {
    if (!value || value.trim() === '') return []

    const parsed = safeJsonParseWithValidation<Record<string, unknown>>(value, {
      fallback: {},
      validator: isObjectRecord,
      validatorMessage: 'Rate limits must be a JSON object',
      context: 'rate limits',
    })

    return Object.entries(parsed)
      .map(([groupName, limits]) => {
        if (
          Array.isArray(limits) &&
          limits.length === 2 &&
          typeof limits[0] === 'number' &&
          typeof limits[1] === 'number'
        ) {
          return {
            groupName,
            maxRequests: limits[0],
            maxSuccess: limits[1],
            kind: classifyRateLimitGroup(groupName, tierNames, commercialNames),
          }
        }
        return null
      })
      .filter((item): item is RateLimitEntry => item !== null)
  }, [value, tierNames, commercialNames])

  const filteredRateLimits = useMemo(() => {
    if (!searchText) return rateLimits
    const lowerSearch = searchText.toLowerCase()
    return rateLimits.filter((limit) =>
      limit.groupName.toLowerCase().includes(lowerSearch)
    )
  }, [rateLimits, searchText])

  // Split the filtered list into user-facing rows (tier + commercial) and
  // upstream channel rows. Sort within each section: tier group is ordered
  // by threshold-index-ish alphabetical (stable, no priority data on the
  // client), commercial after tiers, channels alphabetical.
  const { userSectionRows, channelSectionRows } = useMemo(() => {
    const userRows: RateLimitEntry[] = []
    const channelRows: RateLimitEntry[] = []
    for (const row of filteredRateLimits) {
      if (row.kind === 'channel') channelRows.push(row)
      else userRows.push(row)
    }
    const byName = (a: RateLimitEntry, b: RateLimitEntry) =>
      a.groupName.localeCompare(b.groupName)
    // Keep commercial rows after tier rows within the user section — same
    // reasoning as R16-5: the reseller identities aren't ladder rungs and
    // shouldn't visually blur into them.
    userRows.sort((a, b) => {
      if (a.kind !== b.kind) return a.kind === 'tier' ? -1 : 1
      return byName(a, b)
    })
    channelRows.sort(byName)
    return { userSectionRows: userRows, channelSectionRows: channelRows }
  }, [filteredRateLimits])

  const handleSave = (data: RateLimitEntryData) => {
    const parsed = safeJsonParseWithValidation<Record<string, unknown>>(value, {
      fallback: {},
      validator: isObjectRecord,
      silent: true,
    })

    if (editData && editData.groupName !== data.groupName) {
      delete parsed[editData.groupName]
    }

    parsed[data.groupName] = [data.maxRequests, data.maxSuccess]

    onChange(JSON.stringify(parsed, null, 2))
  }

  const handleDelete = (groupName: string) => {
    const parsed = safeJsonParseWithValidation<Record<string, unknown>>(value, {
      fallback: {},
      validator: isObjectRecord,
      silent: true,
    })

    delete parsed[groupName]

    onChange(JSON.stringify(parsed, null, 2))
  }

  const handleEdit = (limit: RateLimitEntry) => {
    setEditData(limit)
    setDialogOpen(true)
  }

  const handleAdd = () => {
    setEditData(null)
    setDialogOpen(true)
  }

  const renderTable = (rows: RateLimitEntry[], emptyKey: string) => (
    <StaticDataTable
      data={rows}
      getRowKey={(limit) => limit.groupName}
      emptyContent={searchText ? t('No groups match your search') : t(emptyKey)}
      columns={[
        {
          id: 'group',
          header: t('Group Name'),
          cellClassName: 'font-medium',
          cell: (limit) => (
            <div className='flex items-center gap-2'>
              <span>{limit.groupName}</span>
              {limit.kind === 'commercial' ? (
                <span
                  className='bg-amber-500/15 shrink-0 rounded px-1.5 py-0.5 text-[10px] leading-none font-medium text-amber-700 dark:text-amber-400'
                  data-testid='rate-limit-kind-badge'
                  data-kind={limit.kind}
                >
                  {t('keys.groupKind.commercial')}
                </span>
              ) : null}
            </div>
          ),
        },
        {
          id: 'max-requests',
          header: t('Max Requests (incl. failures)'),
          className: 'text-right',
          cellClassName: 'text-right',
          cell: (limit) => (
            <span className='font-mono'>
              {limit.maxRequests === 0
                ? t('Unlimited')
                : limit.maxRequests.toLocaleString()}
            </span>
          ),
        },
        {
          id: 'max-success',
          header: t('Max Success'),
          className: 'text-right',
          cellClassName: 'text-right',
          cell: (limit) => (
            <span className='font-mono'>
              {limit.maxSuccess.toLocaleString()}
            </span>
          ),
        },
        {
          id: 'actions',
          header: t('Actions'),
          className: 'text-right',
          cellClassName: 'text-right',
          cell: (limit) => (
            <StaticRowActions
              editLabel={t('Edit')}
              deleteLabel={t('Delete')}
              menuLabel={t('Open menu')}
              onEdit={() => handleEdit(limit)}
              onDelete={() => handleDelete(limit.groupName)}
            />
          ),
        },
      ]}
    />
  )

  return (
    <div className='space-y-4'>
      <div className='flex items-center gap-4'>
        <div className='relative flex-1'>
          <Search className='text-muted-foreground absolute top-2.5 left-2.5 h-4 w-4' />
          <Input
            placeholder={t('Search group names...')}
            value={searchText}
            onChange={(e) => setSearchText(e.target.value)}
            className='pl-9'
          />
        </div>
        <Button onClick={handleAdd}>
          <Plus className='mr-2 h-4 w-4' />
          {t('Add group')}
        </Button>
      </div>

      <section
        aria-label={t('rateLimit.section.userTier')}
        className='space-y-2'
      >
        <header className='space-y-0.5'>
          <h4 className='text-sm font-semibold'>
            {t('rateLimit.section.userTier')}
          </h4>
          <p className='text-muted-foreground text-xs'>
            {t('rateLimit.section.userTier.help')}
          </p>
        </header>
        {renderTable(
          userSectionRows,
          'rateLimit.section.userTier.empty'
        )}
      </section>

      <section
        aria-label={t('rateLimit.section.channel')}
        className='space-y-2'
      >
        <header className='space-y-0.5'>
          <h4 className='text-sm font-semibold'>
            {t('rateLimit.section.channel')}
          </h4>
          <p className='text-muted-foreground text-xs'>
            {t('rateLimit.section.channel.help')}
          </p>
        </header>
        {renderTable(
          channelSectionRows,
          'rateLimit.section.channel.empty'
        )}
      </section>

      <RateLimitDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSave={handleSave}
        editData={editData}
      />
    </div>
  )
}
