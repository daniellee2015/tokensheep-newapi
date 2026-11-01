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
import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { formatUseTime } from '@/lib/format'

import { timingSeconds, type StreamTimingMetadata } from '../lib/timing'

interface StreamTimingDetailsProps {
  frtMs?: number
  timing?: StreamTimingMetadata
}

export function StreamTimingDetails(props: StreamTimingDetailsProps) {
  const { t } = useTranslation()
  const labelId = useId()
  const semantic = props.timing?.frt_metric === 'downstream_first_content'
  const event = props.timing?.frt_metric === 'upstream_first_event'
  const rows = [
    {
      field: 'event',
      label: t('First event'),
      value: event ? props.frtMs : props.timing?.frt_event_ms,
    },
    {
      field: 'content',
      label: t('First content'),
      value: semantic ? props.frtMs : undefined,
    },
    {
      field: 'thinking',
      label: t('First thinking'),
      value: props.timing?.first_thinking_ms,
    },
    {
      field: 'text',
      label: t('First text'),
      value: props.timing?.first_text_ms,
    },
    {
      field: 'tool',
      label: t('First tool'),
      value: props.timing?.first_tool_ms,
    },
    {
      field: 'upstream',
      label: t('Upstream first content'),
      value: props.timing?.upstream_first_content_ms,
    },
  ]
  let description = t('Legacy metric; first-content timing was not recorded.')
  if (semantic) {
    description = t('Server flush timing; client display may be later.')
  } else if (event) {
    description = t('First upstream event; not first model content.')
  } else {
    rows.unshift({
      field: 'legacy',
      label: t('Legacy timing'),
      value: props.frtMs,
    })
  }

  return (
    <div className='min-w-0 space-y-2 text-xs'>
      <p className='text-muted-foreground'>{description}</p>
      <dl className='grid grid-cols-[minmax(0,1fr)_auto] gap-x-3 gap-y-1'>
        {rows.map((row) => {
          const seconds = timingSeconds(row.value)
          let value =
            seconds == null ? t('Not recorded') : formatUseTime(seconds)
          if (semantic && row.field === 'content' && props.frtMs === -1) {
            value = t('No content')
          }
          return (
            <div key={row.field} className='contents'>
              <dt
                id={`${labelId}-${row.field}`}
                className='text-muted-foreground'
              >
                {row.label}
              </dt>
              <dd
                aria-labelledby={`${labelId}-${row.field}`}
                className='text-right tabular-nums'
              >
                {value}
              </dd>
            </div>
          )
        })}
      </dl>
    </div>
  )
}
