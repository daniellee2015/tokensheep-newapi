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
import { Combobox as ComboboxPrimitive } from '@base-ui/react/combobox'
import { Cancel01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Combobox,
  ComboboxChip,
  ComboboxChips,
  ComboboxChipsInput,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxItem,
  ComboboxList,
  useComboboxAnchor,
} from '@/components/ui/combobox'

import { normalizeGroupQuery, type LogGroupOption } from '../lib/group-filter'

interface LogGroupFilterProps {
  options: LogGroupOption[]
  selected: string[]
  query: string
  onQueryChange: (value: string) => void
  onSelectionChange: (values: string[]) => void
  onSubmit: () => void
  className?: string
}

export function LogGroupFilter(props: LogGroupFilterProps) {
  const { t } = useTranslation()
  const anchor = useComboboxAnchor()
  const labels = new Map(props.options.map((option) => [option.value, option]))
  return (
    <Combobox
      multiple
      autoHighlight={false}
      items={props.options.map((option) => option.value)}
      value={props.selected}
      inputValue={props.query}
      onInputValueChange={(value, details) => {
        // Closing suggestions must preserve a typed filter until Search is pressed.
        if (details.reason !== 'input-clear') props.onQueryChange(value)
      }}
      onOpenChange={(_open, details) => {
        // Keep suggestions open so several groups can be picked in one pass.
        if (details.reason === 'item-press') details.cancel()
      }}
      onValueChange={(values, details) => {
        if (details.reason === 'escape-key') {
          details.cancel()
          return
        }
        props.onSelectionChange(values)
      }}
      itemToStringLabel={(value) => labels.get(value)?.label ?? value}
      filter={(value, query) => {
        const normalized = normalizeGroupQuery(query)
        return (
          labels
            .get(value)
            ?.aliases.some((alias) => alias.includes(normalized)) ?? false
        )
      }}
    >
      <ComboboxChips ref={anchor} className='w-full min-w-0'>
        {props.selected.map((value) => {
          const label = labels.get(value)?.label ?? value
          return (
            <ComboboxChip key={value} showRemove={false} className='max-w-full'>
              <span className={props.className}>
                <span className='block max-w-40 truncate'>{label}</span>
              </span>
              <ComboboxPrimitive.ChipRemove
                aria-label={`${t('Remove')} ${label}`}
                render={<Button variant='ghost' size='icon-xs' />}
              >
                <HugeiconsIcon
                  icon={Cancel01Icon}
                  strokeWidth={2}
                  aria-hidden='true'
                />
              </ComboboxPrimitive.ChipRemove>
            </ComboboxChip>
          )
        })}
        <ComboboxChipsInput
          aria-label={t('Group')}
          placeholder={t('Group')}
          className={props.className}
          onKeyDown={(event) => {
            if (
              event.key === 'Enter' &&
              !event.currentTarget.getAttribute('aria-activedescendant')
            ) {
              event.preventDefault()
              props.onSubmit()
            }
          }}
        />
      </ComboboxChips>
      <ComboboxContent anchor={anchor}>
        <ComboboxList>
          {(value: string) => (
            <ComboboxItem key={value} value={value}>
              <span className={props.className}>
                <span className='block'>
                  {labels.get(value)?.label ?? value}
                </span>
                <span className='text-muted-foreground block text-xs'>
                  {value}
                </span>
              </span>
            </ComboboxItem>
          )}
        </ComboboxList>
        <ComboboxEmpty>{t('No matching items')}</ComboboxEmpty>
        <div className='text-muted-foreground border-t px-3 py-2 text-xs'>
          {t('Select multiple groups')}
        </div>
      </ComboboxContent>
    </Combobox>
  )
}
