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
// R17-A test coverage for R16-5 GroupKindBadge. The combobox exposes
// three namespaces (tier / commercial / channel) plus the legacy `auto`
// group and an older-backend "no kind" fallback. Each branch has a
// specific reason to exist and is easy to break with a one-line refactor
// — pin them all.
//
// R18 (R16-5 follow-up): the badge now renders in three call sites —
// the CommandItem (already pinned by api-key-group-combobox.test.tsx),
// the PopoverTrigger (collapsed view after a user picks a group), and
// the AutoGroupOrderItem (each row of the custom Auto order list) plus
// the inherited chip row. R16-5 only wired the CommandItem; verify
// caught the trigger + auto-order gap. This file extends coverage to
// pin those two new call sites at the DOM level so a future refactor
// that drops the badge from either surface fails here first.
import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

let shouldReduceMotion = false
const reducedMotionMediaQuery = window.matchMedia('(prefers-reduced-motion)')
Object.defineProperty(reducedMotionMediaQuery, 'matches', {
  configurable: true,
  get: () => shouldReduceMotion,
})
Object.defineProperty(window, 'matchMedia', {
  configurable: true,
  value: () => reducedMotionMediaQuery,
})

const { useState } = await import('react')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { ApiKeyGroupCombobox, GroupKindBadge } = await import(
  '../api-key-group-combobox'
)
const { AutoGroupOrderEditor } = await import('../auto-group-order-editor')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'keys.groupKind.tier': 'tier',
        'keys.groupKind.commercial': 'commercial',
        'keys.groupKind.channel': 'channel',
        Auto: 'Auto',
        Ratio: 'Ratio',
        'Search...': 'Search...',
        'No group found.': 'No group found.',
        'Select a group': 'Select a group',
        '{{count}} / {{max}} groups selected':
          '{{count}} / {{max}} groups selected',
        'Add Auto group': 'Add Auto group',
        'Auto group order': 'Auto group order',
        'Drag {{group}} to reorder': 'Drag {{group}} to reorder',
        'Inherit global Auto order': 'Inherit global Auto order',
        'Maximum {{max}} groups selected': 'Maximum {{max}} groups selected',
        'Move {{group}} down': 'Move {{group}} down',
        'Move {{group}} up': 'Move {{group}} up',
        'Remove {{group}}': 'Remove {{group}}',
        'Restore global Auto': 'Restore global Auto',
        'Using the complete global Auto order ({{count}} groups)':
          'Using the complete global Auto order ({{count}} groups)',
      },
    },
  },
})

function wrap(node: React.ReactNode) {
  return render(<I18nextProvider i18n={i18n}>{node}</I18nextProvider>)
}

describe('GroupKindBadge R16-5', () => {
  test("kind='tier' renders the tier label with the muted style", () => {
    const { container } = wrap(<GroupKindBadge kind='tier' />)
    const badge = container.querySelector('span')
    expect(badge).not.toBe(null)
    expect(badge?.textContent).toBe('tier')
    // Tier is a "user identity" namespace and uses the muted pill —
    // amber is reserved for commercial to draw the eye to reseller
    // accounts. Verifying the muted colour class prevents accidentally
    // swapping the two amber vs muted branches.
    expect(badge?.className).toContain('bg-muted')
    expect(badge?.className).toContain('text-muted-foreground')
    expect(badge?.className).not.toContain('amber')
  })

  test("kind='commercial' renders the commercial label with amber styling", () => {
    // R16-5 headline: commercial groups (retail / wholesale) look like
    // any other group in the dropdown otherwise. The amber pill is the
    // only visual cue that picking one of these binds the key to a
    // reseller identity rather than a normal ratio group.
    const { container } = wrap(<GroupKindBadge kind='commercial' />)
    const badge = container.querySelector('span')
    expect(badge).not.toBe(null)
    expect(badge?.textContent).toBe('commercial')
    expect(badge?.className).toContain('amber')
  })

  test("kind='channel' renders the channel label with muted style", () => {
    const { container } = wrap(<GroupKindBadge kind='channel' />)
    const badge = container.querySelector('span')
    expect(badge).not.toBe(null)
    expect(badge?.textContent).toBe('channel')
    expect(badge?.className).toContain('bg-muted')
    expect(badge?.className).not.toContain('amber')
  })

  test("kind='auto' renders nothing", () => {
    // auto has its own AutoGroupFlowBorder + gradient effect already —
    // an extra "auto" pill would double-label the row. The badge is a
    // no-op for this kind on purpose.
    const { container } = wrap(<GroupKindBadge kind='auto' />)
    expect(container).toBeEmptyDOMElement()
  })

  test('kind=undefined renders nothing (older backend compatibility)', () => {
    // R16-5 explicit compat clause: pre-R16 backends omit the `kind`
    // field on /api/user/self/groups. The badge must fall back to
    // "render nothing" so a rolling deploy where the frontend ships
    // first doesn't show a broken/empty pill.
    const { container } = wrap(<GroupKindBadge />)
    expect(container).toBeEmptyDOMElement()
  })
})

// R18 gap 1: combobox PopoverTrigger — collapsed state after a user has
// picked a group. R16-5 shipped the badge only inside CommandItem, so
// once the dropdown closed the identity/routing signal disappeared. The
// tests below drive the real combobox and read from the trigger button
// itself, so a refactor that renders the pill only in the open popover
// still breaks here.
const triggerOptions = [
  { value: 'auto', label: 'auto', desc: 'Global automatic routing' },
  { value: 'bestie', label: 'bestie', desc: 'Tier identity', ratio: 1, kind: 'tier' as const },
  {
    value: 'retail',
    label: 'retail',
    desc: 'Reseller identity',
    ratio: 2,
    kind: 'commercial' as const,
  },
  {
    value: 'claude-max',
    label: 'claude-max',
    desc: 'Upstream channel',
    ratio: 3,
    kind: 'channel' as const,
  },
  { value: 'legacy', label: 'legacy', desc: 'No kind field', ratio: 1 },
]

function ComboboxHarness(props: { initialValue: string }) {
  const [value, setValue] = useState(props.initialValue)
  return (
    <I18nextProvider i18n={i18n}>
      <ApiKeyGroupCombobox
        options={triggerOptions}
        value={value}
        onValueChange={setValue}
      />
    </I18nextProvider>
  )
}

describe('R18 GroupKindBadge in PopoverTrigger', () => {
  test("collapsed trigger with kind='tier' surfaces the muted tier pill", () => {
    // Selecting a tier identity from the picker must keep the badge in
    // the trigger. Muted colour is the "user identity" signal — amber
    // is reserved for commercial so an operator can pick a reseller
    // account out of the list at a glance.
    const { container } = render(<ComboboxHarness initialValue='bestie' />)
    const trigger = screen.getByRole('combobox')
    // Trigger button text is Label + kind label + optional ratio badge
    // text — assert the trigger contains the kind label and that the
    // pill uses the tier (muted) styling. Scope the query to inside the
    // trigger so we don't pick up the popover once it opens.
    expect(within(trigger).getByText('tier')).toBeInTheDocument()
    const badges = trigger.querySelectorAll('span')
    const kindBadge = [...badges].find(
      (node) => node.textContent === 'tier' && node.className.includes('rounded')
    )
    expect(kindBadge).toBeDefined()
    expect(kindBadge?.className).toContain('bg-muted')
    expect(kindBadge?.className).not.toContain('amber')
    // The label should still be present and unaffected.
    expect(within(trigger).getByText('bestie')).toBeInTheDocument()
    // Sanity: only the trigger is rendered before the popover opens.
    expect(container.querySelector('[role="listbox"]')).toBe(null)
  })

  test("collapsed trigger with kind='commercial' uses the amber pill", () => {
    // Commercial is the load-bearing signal per R16-5: retail /
    // wholesale groups otherwise look like any other pick. Trigger
    // has to keep the amber pill for the same reason the CommandItem
    // does — this is the one that catches "I bound the key to a
    // reseller identity without noticing".
    render(<ComboboxHarness initialValue='retail' />)
    const trigger = screen.getByRole('combobox')
    const badge = within(trigger).getByText('commercial')
    expect(badge.className).toContain('amber')
  })

  test("collapsed trigger with kind='channel' uses the muted pill", () => {
    render(<ComboboxHarness initialValue='claude-max' />)
    const trigger = screen.getByRole('combobox')
    const badge = within(trigger).getByText('channel')
    expect(badge.className).toContain('bg-muted')
    expect(badge.className).not.toContain('amber')
  })

  test("collapsed trigger renders no kind badge for kind='auto'", () => {
    // Auto has its own animated framing on the trigger already; the
    // kind pill would be redundant noise. Same rule as CommandItem.
    render(<ComboboxHarness initialValue='auto' />)
    const trigger = screen.getByRole('combobox')
    expect(within(trigger).queryByText('tier')).toBe(null)
    expect(within(trigger).queryByText('commercial')).toBe(null)
    expect(within(trigger).queryByText('channel')).toBe(null)
  })

  test('collapsed trigger renders no kind badge when kind is undefined', () => {
    // Older-backend compat: a group missing `kind` on
    // /api/user/self/groups shouldn't force a placeholder pill into
    // the collapsed trigger.
    render(<ComboboxHarness initialValue='legacy' />)
    const trigger = screen.getByRole('combobox')
    expect(within(trigger).queryByText('tier')).toBe(null)
    expect(within(trigger).queryByText('commercial')).toBe(null)
    expect(within(trigger).queryByText('channel')).toBe(null)
    // The label itself is still visible.
    expect(within(trigger).getByText('legacy')).toBeInTheDocument()
  })

  test('badge stays in place after picking a group from the open popover', () => {
    // End-to-end: user opens picker, picks commercial retail, popover
    // collapses, trigger shows the amber pill. Regressing to the
    // R16-5 state (badge only in CommandItem) drops the pill on
    // collapse and this test fails.
    render(<ComboboxHarness initialValue='auto' />)
    const trigger = screen.getByRole('combobox')
    fireEvent.click(trigger)

    const retail = [
      ...document.querySelectorAll<HTMLElement>('[data-slot="command-item"]'),
    ].find((node) => node.textContent?.includes('Reseller identity'))
    if (!retail) throw new Error('Expected retail command item')
    fireEvent.click(retail)

    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    const badge = within(trigger).getByText('commercial')
    expect(badge.className).toContain('amber')
  })
})

// R18 gap 2: AutoGroupOrderItem — the compact chip row for the custom
// Auto order. R16-5 also missed this surface; the auto-order scenario
// is precisely where identity vs channel mixing is the point of the
// UI, so a channel row without the channel badge is the failure mode
// this covers.
const autoOptions = [
  { value: 'auto', label: 'auto' },
  {
    value: 'bestie',
    label: 'bestie',
    desc: 'Tier identity',
    ratio: 1,
    kind: 'tier' as const,
  },
  {
    value: 'retail',
    label: 'retail',
    desc: 'Reseller identity',
    ratio: 2,
    kind: 'commercial' as const,
  },
  {
    value: 'claude-max',
    label: 'claude-max',
    desc: 'Upstream channel',
    ratio: 3,
    kind: 'channel' as const,
  },
  { value: 'legacy', label: 'legacy', desc: 'Older-backend row', ratio: 1 },
]

function OrderHarness(props: { initialGroups: string[] }) {
  const [groups, setGroups] = useState(props.initialGroups)
  return (
    <I18nextProvider i18n={i18n}>
      <AutoGroupOrderEditor
        value={groups}
        mode='custom'
        options={autoOptions}
        globalOptions={autoOptions.filter((option) => option.value !== 'auto')}
        maxCount={5}
        onChange={(value) => setGroups(value.groups)}
      />
    </I18nextProvider>
  )
}

function findRow(group: string): HTMLElement {
  const dragButton = screen.getByRole('button', {
    name: `Drag ${group} to reorder`,
  })
  const row = dragButton.closest('li')
  if (!row) throw new Error(`Expected reorder row for ${group}`)
  return row as HTMLElement
}

describe('R18 GroupKindBadge in AutoGroupOrderItem', () => {
  test('tier / commercial / channel rows all render their kind pill', () => {
    // The auto-order screen mixes identity and channel groups
    // deliberately — the user is telling the router which pools to
    // fall through in what order. Without the pill on each row the
    // configured order looks like a flat list of names and the
    // identity-vs-channel distinction has to be inferred. Pin all
    // three kinds together so any refactor that drops one is caught.
    render(
      <OrderHarness initialGroups={['bestie', 'retail', 'claude-max']} />
    )

    const bestieRow = findRow('bestie')
    const tierPill = within(bestieRow).getByText('tier')
    expect(tierPill.className).toContain('bg-muted')
    expect(tierPill.className).not.toContain('amber')

    const retailRow = findRow('retail')
    const commercialPill = within(retailRow).getByText('commercial')
    expect(commercialPill.className).toContain('amber')

    const channelRow = findRow('claude-max')
    const channelPill = within(channelRow).getByText('channel')
    expect(channelPill.className).toContain('bg-muted')
    expect(channelPill.className).not.toContain('amber')
  })

  test('row with undefined kind renders no badge (legacy backend row)', () => {
    // If /api/user/self/groups omits kind for a still-configured group
    // (rolling deploy, or the group was removed from the map after
    // being written into the custom order), the row must degrade to
    // "no pill" rather than showing an empty placeholder or crashing
    // the render.
    render(<OrderHarness initialGroups={['legacy']} />)
    const row = findRow('legacy')
    expect(within(row).queryByText('tier')).toBe(null)
    expect(within(row).queryByText('commercial')).toBe(null)
    expect(within(row).queryByText('channel')).toBe(null)
  })

  test('row falls back to globalOptions when the option is filtered out of the picker', () => {
    // The picker often excludes already-selected values from `options`
    // to keep the dropdown clean. That means resolving the kind by
    // scanning `options` alone would leave every configured row
    // unbadged the moment the user opens the picker. The editor has
    // to fall through to `globalOptions` too. Simulate that here by
    // using an options list that only contains `auto` while the
    // custom order references a group present in globalOptions only.
    function Harness() {
      const [groups, setGroups] = useState(['retail'])
      return (
        <I18nextProvider i18n={i18n}>
          <AutoGroupOrderEditor
            value={groups}
            mode='custom'
            options={[{ value: 'auto', label: 'auto' }]}
            globalOptions={autoOptions.filter(
              (option) => option.value !== 'auto'
            )}
            maxCount={5}
            onChange={(value) => setGroups(value.groups)}
          />
        </I18nextProvider>
      )
    }
    render(<Harness />)
    const row = findRow('retail')
    const commercialPill = within(row).getByText('commercial')
    expect(commercialPill.className).toContain('amber')
  })
})

// R18 gap 3 (inherited chip row): admins promote commercial groups into
// the global Auto order occasionally; the read-only inherited chip row
// is where that gets reviewed. Same reasoning as AutoGroupOrderItem —
// the pill is the disambiguation that keeps commercial from looking
// like a regular tier promotion.
function InheritHarness() {
  return (
    <I18nextProvider i18n={i18n}>
      <AutoGroupOrderEditor
        value={[]}
        mode='inherit'
        options={autoOptions}
        globalOptions={autoOptions.filter((option) => option.value !== 'auto')}
        maxCount={5}
        onChange={() => {}}
      />
    </I18nextProvider>
  )
}

describe('R18 GroupKindBadge in inherited chip row', () => {
  test('inherited chips carry their kind pills', () => {
    const { container } = render(<InheritHarness />)
    const order = container.querySelector('[data-slot="global-auto-order"]')
    if (!order) throw new Error('Expected inherited order list')
    // Locate the chip whose name matches, then read its adjacent kind
    // pill from within the same <li>.
    const rows = [...order.querySelectorAll('li')]
    const byName = (name: string) =>
      rows.find(
        (row) =>
          row.querySelector('[data-slot="global-auto-order-name"]')
            ?.textContent === name
      )
    const bestie = byName('bestie')
    const retail = byName('retail')
    const channel = byName('claude-max')
    if (!bestie || !retail || !channel) {
      throw new Error('Expected all three inherited chips')
    }
    expect(within(bestie).getByText('tier').className).toContain('bg-muted')
    expect(within(retail).getByText('commercial').className).toContain('amber')
    expect(within(channel).getByText('channel').className).toContain('bg-muted')
    // Legacy row (no kind) still shows the name but no pill.
    const legacy = byName('legacy')
    if (!legacy) throw new Error('Expected legacy inherited chip')
    expect(within(legacy).queryByText('tier')).toBe(null)
    expect(within(legacy).queryByText('commercial')).toBe(null)
    expect(within(legacy).queryByText('channel')).toBe(null)
  })
})
