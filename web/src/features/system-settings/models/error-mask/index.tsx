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
import { Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'

import { SettingsSwitchField } from '../../components/settings-form-layout'
import { SettingsPageActionsPortal } from '../../components/settings-page-context'
import { SettingsSection } from '../../components/settings-section'
import { useUpdateOption } from '../../hooks/use-update-option'
import type {
  ErrorMaskRule,
  ErrorMaskRuleRow,
  ErrorMaskSettings,
} from './types'

function parseRules(jsonStr: string): ErrorMaskRuleRow[] {
  try {
    const arr = JSON.parse(jsonStr || '[]')
    if (!Array.isArray(arr)) return []
    return arr.map((r: Record<string, unknown>, i: number) => ({
      id: i,
      pattern: typeof r.pattern === 'string' ? r.pattern : '',
      replace: typeof r.replace === 'string' ? r.replace : '',
      is_regex: Boolean(r.is_regex),
      ignore_case: Boolean(r.ignore_case),
    }))
  } catch {
    return []
  }
}

function stripIds(rules: ErrorMaskRuleRow[]): ErrorMaskRule[] {
  return rules.map(({ id: _id, ...rest }) => rest)
}

export function ErrorMaskSection(props: { defaultValues: ErrorMaskSettings }) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const [enabled, setEnabled] = useState(
    props.defaultValues['error_mask_setting.enabled']
  )
  const [fallbackMessage, setFallbackMessage] = useState(
    props.defaultValues['error_mask_setting.fallback_message'] ?? ''
  )
  const [rules, setRules] = useState<ErrorMaskRuleRow[]>(() =>
    parseRules(props.defaultValues['error_mask_setting.rules'])
  )
  const [editMode, setEditMode] = useState<'visual' | 'json'>('visual')
  const [jsonText, setJsonText] = useState(() =>
    JSON.stringify(
      stripIds(parseRules(props.defaultValues['error_mask_setting.rules'])),
      null,
      2
    )
  )
  const [saving, setSaving] = useState(false)

  const handleAddRule = () => {
    setRules((prev) => [
      ...prev,
      {
        id: prev.length ? Math.max(...prev.map((r) => r.id)) + 1 : 0,
        pattern: '',
        replace: '',
        is_regex: false,
        ignore_case: false,
      },
    ])
  }

  const handleUpdateRule = (id: number, patch: Partial<ErrorMaskRule>) => {
    setRules((prev) => prev.map((r) => (r.id === id ? { ...r, ...patch } : r)))
  }

  const handleDeleteRule = (id: number) => {
    setRules((prev) => prev.filter((r) => r.id !== id))
  }

  const handleSave = async () => {
    let payloadRules: Omit<ErrorMaskRule, 'id'>[]

    if (editMode === 'json') {
      try {
        const parsed = JSON.parse(jsonText || '[]')
        if (!Array.isArray(parsed)) {
          toast.error(t('Rules must be a JSON array'))
          return
        }
        payloadRules = parsed
      } catch {
        toast.error(t('Invalid JSON'))
        return
      }
    } else {
      const incomplete = rules.some((r) => !r.pattern.trim())
      if (incomplete) {
        toast.error(t('Every rule needs a pattern'))
        return
      }
      payloadRules = stripIds(rules)
    }

    const serializedRules = JSON.stringify(payloadRules)
    const originalRules = (() => {
      try {
        return JSON.stringify(
          JSON.parse(props.defaultValues['error_mask_setting.rules'] || '[]')
        )
      } catch {
        return '[]'
      }
    })()

    const updates: { key: string; value: string }[] = []
    if (enabled !== props.defaultValues['error_mask_setting.enabled']) {
      updates.push({
        key: 'error_mask_setting.enabled',
        value: String(enabled),
      })
    }
    if (
      fallbackMessage !==
      (props.defaultValues['error_mask_setting.fallback_message'] ?? '')
    ) {
      updates.push({
        key: 'error_mask_setting.fallback_message',
        value: fallbackMessage,
      })
    }
    if (serializedRules !== originalRules) {
      updates.push({ key: 'error_mask_setting.rules', value: serializedRules })
    }

    if (updates.length === 0) {
      toast.info(t('No changes'))
      return
    }

    setSaving(true)
    try {
      for (const update of updates) {
        await updateOption.mutateAsync(update)
      }
      // Keep both editors consistent with what was just persisted.
      setRules(payloadRules.map((r, i) => ({ ...r, id: i })))
      setJsonText(JSON.stringify(payloadRules, null, 2))
      toast.success(t('Saved successfully'))
    } catch {
      toast.error(t('Failed to save'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <SettingsSection title={t('Upstream Error Masking')}>
      <Alert>
        <AlertDescription>
          {t(
            'Rewrites upstream error messages before they reach your users, so infrastructure details stay private. Rules run top to bottom; each one sees the previous rule output. Applies to both API error responses and the log entries users can see. Per-channel rules run before these.'
          )}
        </AlertDescription>
      </Alert>

      <SettingsSwitchField
        checked={enabled}
        onCheckedChange={setEnabled}
        label={t('Enable error masking')}
        description={t(
          'When off, upstream error messages pass through unchanged.'
        )}
      />

      <div className='grid gap-2'>
        <Label htmlFor='error-mask-fallback'>{t('Fallback message')}</Label>
        <Input
          id='error-mask-fallback'
          value={fallbackMessage}
          onChange={(e) => setFallbackMessage(e.target.value)}
          placeholder='Service temporarily unavailable'
        />
        <p className='text-muted-foreground text-xs'>
          {t(
            'Used when the rules remove the entire message, so a caller never receives an empty error.'
          )}
        </p>
      </div>

      <Separator />

      <div className='flex items-center justify-between'>
        <Label>{t('Masking rules')}</Label>
        <div className='flex gap-2'>
          <Button
            size='sm'
            variant={editMode === 'visual' ? 'default' : 'outline'}
            onClick={() => {
              // Carry JSON edits into the table so switching does not lose them.
              if (editMode === 'json') {
                setRules(parseRules(jsonText))
              }
              setEditMode('visual')
            }}
          >
            {t('Visual')}
          </Button>
          <Button
            size='sm'
            variant={editMode === 'json' ? 'default' : 'outline'}
            onClick={() => {
              if (editMode === 'visual') {
                setJsonText(JSON.stringify(stripIds(rules), null, 2))
              }
              setEditMode('json')
            }}
          >
            {t('JSON')}
          </Button>
        </div>
      </div>

      {editMode === 'json' ? (
        <JsonCodeEditor
          value={jsonText}
          onChange={setJsonText}
          placeholder='[{"pattern": "...", "replace": "...", "is_regex": true}]'
          heightClassName='h-96 min-h-96'
        />
      ) : (
        <div className='grid gap-3'>
          {rules.length === 0 ? (
            <p className='text-muted-foreground py-6 text-center text-sm'>
              {t('No masking rules configured.')}
            </p>
          ) : (
            rules.map((rule) => (
              <div
                key={rule.id}
                className='grid gap-2 rounded-md border p-3 sm:grid-cols-[1fr_1fr_auto]'
              >
                <div className='grid gap-1'>
                  <Label className='text-xs'>{t('Match')}</Label>
                  <Input
                    value={rule.pattern}
                    onChange={(e) =>
                      handleUpdateRule(rule.id, { pattern: e.target.value })
                    }
                    placeholder={t('Text or regex to find')}
                  />
                </div>
                <div className='grid gap-1'>
                  <Label className='text-xs'>{t('Replace with')}</Label>
                  <Input
                    value={rule.replace}
                    onChange={(e) =>
                      handleUpdateRule(rule.id, { replace: e.target.value })
                    }
                    placeholder={t('Leave empty to remove')}
                  />
                </div>
                <div className='flex items-end gap-3'>
                  <label className='flex items-center gap-1.5 text-xs'>
                    <Checkbox
                      checked={Boolean(rule.is_regex)}
                      onCheckedChange={(v) =>
                        handleUpdateRule(rule.id, { is_regex: Boolean(v) })
                      }
                    />
                    {t('Regex')}
                  </label>
                  <label className='flex items-center gap-1.5 text-xs'>
                    <Checkbox
                      checked={Boolean(rule.ignore_case)}
                      onCheckedChange={(v) =>
                        handleUpdateRule(rule.id, { ignore_case: Boolean(v) })
                      }
                    />
                    {t('Aa')}
                  </label>
                  <Button
                    size='icon'
                    variant='ghost'
                    onClick={() => handleDeleteRule(rule.id)}
                    aria-label={t('Delete rule')}
                  >
                    <Trash2 className='size-4' />
                  </Button>
                </div>
              </div>
            ))
          )}
          <Button variant='outline' size='sm' onClick={handleAddRule}>
            <Plus className='mr-1 size-4' />
            {t('Add rule')}
          </Button>
        </div>
      )}

      <SettingsPageActionsPortal>
        <Button size='sm' onClick={handleSave} disabled={saving}>
          {saving ? t('Saving...') : t('Save')}
        </Button>
      </SettingsPageActionsPortal>
    </SettingsSection>
  )
}
