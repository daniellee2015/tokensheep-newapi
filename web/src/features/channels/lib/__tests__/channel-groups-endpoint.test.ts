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
import { beforeEach, describe, expect, test, vi } from 'vitest'

import { getGroups as getUserTiers } from '@/features/users/api'

import { getGroups as getChannelGroups } from '../../api'
import { channelsQueryKeys } from '../channel-actions'

const getMock = vi.fn()

vi.mock('@/lib/api', () => ({
  api: {
    get: (...args: unknown[]) => getMock(...args),
  },
}))

/**
 * The channel `group` field is matched against GroupRatio keys when a request is
 * routed, so it must be filled from the channel-group namespace. The user
 * editor writes `users.group`, which is a contribution tier. The backend serves
 * these from two endpoints on purpose (see the comment above GetGroups in
 * controller/group.go); the channel dropdown has regressed onto the tier
 * endpoint more than once, which silently offers groups no request can select.
 */
describe('channel group dropdown data source', () => {
  beforeEach(() => {
    getMock.mockReset()
    getMock.mockResolvedValue({ data: { success: true, data: [] } })
  })

  test('channel groups come from the channel-group endpoint', async () => {
    await getChannelGroups()

    expect(getMock).toHaveBeenCalledWith('/api/group/')
  })

  test('channel groups do not come from the user-tier endpoint', async () => {
    await getChannelGroups()

    expect(getMock).not.toHaveBeenCalledWith('/api/group/tiers')
  })

  test('user tiers still come from the tier endpoint', async () => {
    await getUserTiers()

    expect(getMock).toHaveBeenCalledWith('/api/group/tiers')
  })

  test('the two helpers are not the same function', () => {
    // A re-export alias is how this regressed before: the channel module
    // pointed straight at the user-tier fetcher.
    expect(getChannelGroups).not.toBe(getUserTiers)
  })
})

describe('channel group query key', () => {
  test('is namespaced under channels rather than a bare "groups"', () => {
    const key = channelsQueryKeys.groups()

    // A bare ['groups'] key is also used by the user editor; sharing it lets
    // whichever drawer mounts first populate the other one's dropdown from
    // cache, which reproduces the bug even with the endpoint fixed.
    expect(key).not.toEqual(['groups'])
    expect(key[0]).toBe('channels')
  })
})
