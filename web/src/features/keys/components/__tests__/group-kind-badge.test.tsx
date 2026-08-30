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
import { render } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { GroupKindBadge } = await import('../api-key-group-combobox')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: {
    en: {
      translation: {
        'keys.groupKind.tier': 'tier',
        'keys.groupKind.commercial': 'commercial',
        'keys.groupKind.channel': 'channel',
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
