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
import { describe, expect, test } from 'vitest'

import {
  classifyRateLimitGroup,
  parseGroupNameSet,
} from '../classify-rate-limit-group'

// Same fixture the RPM editor sees at production: tier and commercial
// namespaces come from tokensheep_economy option payload, so we drive
// classification off `Set<string>` values.
const TIERS = new Set(['supporter', 'fan', 'bestie', 'vip'])
const COMMERCIAL = new Set(['retail', 'wholesale', 'wholesale-plus', 'promo'])

describe('classifyRateLimitGroup', () => {
  test('names in the commercial map classify as commercial even when also listed as tiers', () => {
    // Commercial takes precedence — see controller/group.go groupKind():
    // v4 spec §八 R3-1 marks retail/wholesale as commercial via a
    // dedicated flag rather than removing them from tier_thresholds.
    const ambiguous = new Set(['retail', 'wholesale'])
    const tiersWithOverlap = new Set([...TIERS, 'retail'])
    expect(
      classifyRateLimitGroup('retail', tiersWithOverlap, ambiguous)
    ).toBe('commercial')
  })

  test('names in the tier thresholds map classify as tier', () => {
    expect(classifyRateLimitGroup('bestie', TIERS, COMMERCIAL)).toBe('tier')
    expect(classifyRateLimitGroup('supporter', TIERS, COMMERCIAL)).toBe('tier')
  })

  test('free classifies as tier even without a threshold row (fallback identity)', () => {
    // Free carries no threshold, but v4 treats it as a user identity, not
    // a channel. Mirror controller/group.go special case.
    expect(classifyRateLimitGroup('free', TIERS, COMMERCIAL)).toBe('tier')
  })

  test('names in the commercial map classify as commercial', () => {
    expect(classifyRateLimitGroup('retail', TIERS, COMMERCIAL)).toBe(
      'commercial'
    )
    expect(classifyRateLimitGroup('wholesale-plus', TIERS, COMMERCIAL)).toBe(
      'commercial'
    )
  })

  test('unknown names fall through to channel (upstream capacity walls)', () => {
    // A channel-side group (GPT-Pro, aws-q, claude-max, kirobus-api …)
    // won't appear in either economy map — defaulting to channel matches
    // the larger population and is the safer display hint.
    expect(classifyRateLimitGroup('GPT-Pro', TIERS, COMMERCIAL)).toBe('channel')
    expect(classifyRateLimitGroup('kirobus-api', TIERS, COMMERCIAL)).toBe(
      'channel'
    )
    expect(classifyRateLimitGroup('aws-q', TIERS, COMMERCIAL)).toBe('channel')
  })

  test('empty tier/commercial sets still classify free as tier and everything else as channel', () => {
    const empty = new Set<string>()
    expect(classifyRateLimitGroup('free', empty, empty)).toBe('tier')
    expect(classifyRateLimitGroup('bestie', empty, empty)).toBe('channel')
    expect(classifyRateLimitGroup('claude-max', empty, empty)).toBe('channel')
  })
})

describe('parseGroupNameSet', () => {
  test('extracts keys from a JSON object payload', () => {
    const raw = JSON.stringify({ supporter: 1000, bestie: 5000 })
    expect(parseGroupNameSet(raw)).toEqual(new Set(['supporter', 'bestie']))
  })

  test('returns empty set for undefined/empty/malformed input', () => {
    expect(parseGroupNameSet(undefined)).toEqual(new Set())
    expect(parseGroupNameSet('')).toEqual(new Set())
    expect(parseGroupNameSet('not-json')).toEqual(new Set())
    expect(parseGroupNameSet('[1,2,3]')).toEqual(new Set()) // array, not object
    expect(parseGroupNameSet('null')).toEqual(new Set())
  })

  test('returns empty set for a primitive top-level value', () => {
    expect(parseGroupNameSet('42')).toEqual(new Set())
    expect(parseGroupNameSet('"str"')).toEqual(new Set())
  })
})
