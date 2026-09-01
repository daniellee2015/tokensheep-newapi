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
import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'

import { useSystemOptions } from '@/features/system-settings/hooks/use-system-options'
import {
  classifyRateLimitGroup,
  parseGroupNameSet,
} from '@/features/system-settings/request-limits/classify-rate-limit-group'

import { getGroups } from '../api'
import { channelsQueryKeys } from '../lib'

// Same option keys the RPM and group-pricing editors read, so every dropdown
// classifies a group name identically.
const TIER_THRESHOLDS_OPTION_KEY = 'tokensheep_economy.tier_thresholds'
const COMMERCIAL_GROUPS_OPTION_KEY = 'tokensheep_economy.commercial_groups'

/**
 * Channel-side group names, with user tiers and commercial identities removed.
 *
 * `/api/group/` returns every key in GroupRatio, which is a flat map holding
 * three different namespaces: channel groups, user tiers, and commercial
 * identities. Only the first can meaningfully sit on a channel — a channel
 * tagged with a tier name is never selected by routing — so the tier and
 * commercial names are filtered out of anything channel-facing.
 *
 * The tier list comes from the live `/api/option/` payload rather than a
 * hard-coded whitelist, so a newly added tier is excluded automatically.
 */
export function useChannelGroupOptions() {
  const { data: groupsData, isLoading } = useQuery({
    queryKey: channelsQueryKeys.groups(),
    queryFn: getGroups,
  })

  // Shares the cache with the settings pages (5 min staleTime), so this is
  // usually free. If the request fails both name sets end up empty and every
  // name classifies as a channel group — the dropdown degrades to the full
  // list rather than silently losing entries.
  const { data: optionsData } = useSystemOptions()

  const isChannelGroup = useMemo(() => {
    const optionRows = optionsData?.data ?? []
    const findOption = (key: string) =>
      optionRows.find((row) => row.key === key)?.value
    const tierNames = parseGroupNameSet(findOption(TIER_THRESHOLDS_OPTION_KEY))
    const commercialNames = parseGroupNameSet(
      findOption(COMMERCIAL_GROUPS_OPTION_KEY)
    )
    return (name: string) =>
      classifyRateLimitGroup(name, tierNames, commercialNames) === 'channel'
  }, [optionsData])

  const groups = useMemo(
    () => (groupsData?.data ?? []).filter(isChannelGroup),
    [groupsData, isChannelGroup]
  )

  return { groups, isLoading, isChannelGroup }
}
