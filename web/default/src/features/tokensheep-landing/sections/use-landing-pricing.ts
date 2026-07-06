import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { PricingData, PricingModel } from '@/features/pricing/types'

/**
 * Landing pricing hook — fetches /api/pricing (public endpoint) and picks a
 * curated subset of models per provider. If the fetch fails or a curated
 * model isn't in the response, we fall back to the static preset so the
 * homepage never renders empty.
 *
 * All numeric prices displayed on the landing come from the backend where
 * possible — we don't hardcode current USD rates because they drift.
 */

export interface LandingPriceModel {
  displayName: string
  /** i18n key for the model's chip tag (e.g. "全能旗舰" / "Flagship") */
  tagKey: string
  officialInputUsd?: number
  officialOutputUsd?: number
  cacheReadRatio?: number | null
  cacheWriteRatio?: number | null
  fromBackend: boolean
}

export interface LandingProvider {
  id: string
  label: string
  iconKey: 'openai' | 'claude' | 'gemini'
  models: LandingPriceModel[]
}

// Curated 3-per-provider list. `matchPatterns` = regexes tested against
// model_name from backend response, in order. First hit wins.
interface CurationEntry {
  displayName: string
  tagKey: string
  matchPatterns: RegExp[]
}

interface CurationBucket {
  id: string
  label: string
  iconKey: 'openai' | 'claude' | 'gemini'
  entries: CurationEntry[]
}

const CURATION: CurationBucket[] = [
  {
    id: 'openai',
    label: 'OpenAI',
    iconKey: 'openai',
    entries: [
      {
        displayName: 'gpt-5',
        tagKey: 'landing.pricing.tag.flagship',
        matchPatterns: [/^gpt-5$/i, /^gpt-5-\d+/i],
      },
      {
        displayName: 'o3',
        tagKey: 'landing.pricing.tag.reasoning',
        matchPatterns: [/^o3$/i, /^o3-\d+/i, /^o3-mini$/i],
      },
      {
        displayName: 'gpt-5-mini',
        tagKey: 'landing.pricing.tag.economy',
        matchPatterns: [/^gpt-5-mini/i, /^gpt-4o-mini/i],
      },
    ],
  },
  {
    id: 'anthropic',
    label: 'Anthropic',
    iconKey: 'claude',
    entries: [
      {
        displayName: 'claude-opus-4-8',
        tagKey: 'landing.pricing.tag.top',
        matchPatterns: [/claude.*opus/i],
      },
      {
        displayName: 'claude-sonnet-5',
        tagKey: 'landing.pricing.tag.workhorse',
        matchPatterns: [/claude.*sonnet-5/i, /claude.*sonnet/i],
      },
      {
        displayName: 'claude-haiku-4-5',
        tagKey: 'landing.pricing.tag.fastLight',
        matchPatterns: [/claude.*haiku/i],
      },
    ],
  },
  {
    id: 'google',
    label: 'Google',
    iconKey: 'gemini',
    entries: [
      {
        displayName: 'gemini-2.5-pro',
        tagKey: 'landing.pricing.tag.longContext',
        matchPatterns: [/gemini.*2\.5.*pro/i, /gemini.*pro/i],
      },
      {
        displayName: 'gemini-2.5-flash',
        tagKey: 'landing.pricing.tag.fast',
        matchPatterns: [/gemini.*2\.5.*flash$/i, /gemini.*flash$/i],
      },
      {
        displayName: 'gemini-2.5-flash-lite',
        tagKey: 'landing.pricing.tag.freeCredit',
        matchPatterns: [/gemini.*flash-lite/i, /gemini.*lite/i],
      },
    ],
  },
]

// Static fallback prices (approximate current USD list) — only used when the
// backend has no matching model for a curated entry.
const FALLBACK: Record<
  string,
  Pick<LandingPriceModel, 'officialInputUsd' | 'officialOutputUsd' | 'cacheReadRatio' | 'cacheWriteRatio'>
> = {
  'gpt-5': {
    officialInputUsd: 1.25,
    officialOutputUsd: 10,
    cacheReadRatio: 0.1,
    cacheWriteRatio: 1,
  },
  o3: {
    officialInputUsd: 2,
    officialOutputUsd: 8,
    cacheReadRatio: 0.25,
    cacheWriteRatio: 1,
  },
  'gpt-5-mini': {
    officialInputUsd: 0.25,
    officialOutputUsd: 2,
    cacheReadRatio: 0.1,
    cacheWriteRatio: 1,
  },
  'claude-opus-4-8': {
    officialInputUsd: 15,
    officialOutputUsd: 75,
    cacheReadRatio: 0.1,
    cacheWriteRatio: 1.25,
  },
  'claude-sonnet-5': {
    officialInputUsd: 3,
    officialOutputUsd: 15,
    cacheReadRatio: 0.1,
    cacheWriteRatio: 1.25,
  },
  'claude-haiku-4-5': {
    officialInputUsd: 1,
    officialOutputUsd: 5,
    cacheReadRatio: 0.1,
    cacheWriteRatio: 1.25,
  },
  'gemini-2.5-pro': {
    officialInputUsd: 1.25,
    officialOutputUsd: 10,
    cacheReadRatio: 0.25,
    cacheWriteRatio: 1,
  },
  'gemini-2.5-flash': {
    officialInputUsd: 0.3,
    officialOutputUsd: 2.5,
    cacheReadRatio: 0.25,
    cacheWriteRatio: 1,
  },
  'gemini-2.5-flash-lite': {
    officialInputUsd: 0.1,
    officialOutputUsd: 0.4,
    cacheReadRatio: 0.25,
    cacheWriteRatio: 1,
  },
}

function pickBackendModel(
  models: PricingModel[],
  patterns: RegExp[]
): PricingModel | undefined {
  for (const p of patterns) {
    const hit = models.find((m) => p.test(m.model_name))
    if (hit) return hit
  }
  return undefined
}

/**
 * Backend model ratios are relative to a fixed base ($0.002 / 1k tokens for
 * quota_type=0). Convert to per-million USD by multiplying by 2.
 */
function backendModelToOfficialUsd(model: PricingModel): {
  input?: number
  output?: number
} {
  // quota_type=1 = per-call fixed price; skip for landing display
  if (model.quota_type === 1) return {}
  const inputPerM = model.model_ratio * 2
  const outputPerM = model.model_ratio * model.completion_ratio * 2
  return {
    input: Number.isFinite(inputPerM) ? inputPerM : undefined,
    output: Number.isFinite(outputPerM) ? outputPerM : undefined,
  }
}

async function fetchPricing(): Promise<PricingData | null> {
  try {
    const res = await api.get<PricingData>('/api/pricing')
    return res.data ?? null
  } catch {
    return null
  }
}

export function useLandingPricing(): {
  providers: LandingProvider[]
  isLoading: boolean
  fromBackend: boolean
} {
  const { data, isLoading } = useQuery({
    queryKey: ['landing', 'pricing'],
    queryFn: fetchPricing,
    staleTime: 5 * 60 * 1000,
    retry: 1,
  })

  const backendModels = data?.data ?? []

  const providers: LandingProvider[] = CURATION.map((bucket) => ({
    id: bucket.id,
    label: bucket.label,
    iconKey: bucket.iconKey,
    models: bucket.entries.map((entry) => {
      const hit = pickBackendModel(backendModels, entry.matchPatterns)
      if (hit) {
        const usd = backendModelToOfficialUsd(hit)
        return {
          displayName: hit.model_name,
          tagKey: entry.tagKey,
          officialInputUsd: usd.input,
          officialOutputUsd: usd.output,
          cacheReadRatio: hit.cache_ratio,
          cacheWriteRatio: hit.create_cache_ratio,
          fromBackend: true,
        }
      }
      const fb = FALLBACK[entry.displayName]
      return {
        displayName: entry.displayName,
        tagKey: entry.tagKey,
        officialInputUsd: fb?.officialInputUsd,
        officialOutputUsd: fb?.officialOutputUsd,
        cacheReadRatio: fb?.cacheReadRatio,
        cacheWriteRatio: fb?.cacheWriteRatio,
        fromBackend: false,
      }
    }),
  }))

  const fromBackend = providers.some((p) => p.models.some((m) => m.fromBackend))

  return { providers, isLoading, fromBackend }
}
