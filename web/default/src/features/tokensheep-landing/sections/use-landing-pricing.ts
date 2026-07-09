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
        displayName: 'GPT-5.5',
        tagKey: 'landing.pricing.tag.flagship',
        matchPatterns: [/^gpt-5\.5/i],
      },
      {
        displayName: 'GPT-5.4',
        tagKey: 'landing.pricing.tag.reasoning',
        matchPatterns: [/^gpt-5\.4$/i, /^gpt-5\.4-\d/i],
      },
      {
        displayName: 'GPT-5.4-mini',
        tagKey: 'landing.pricing.tag.economy',
        matchPatterns: [/^gpt-5\.4-mini/i],
      },
    ],
  },
  {
    id: 'anthropic',
    label: 'Anthropic',
    iconKey: 'claude',
    entries: [
      {
        displayName: 'Claude Opus 4.8',
        tagKey: 'landing.pricing.tag.top',
        matchPatterns: [/claude-opus-4-8/i, /claude.*opus/i],
      },
      {
        displayName: 'Claude Sonnet 5',
        tagKey: 'landing.pricing.tag.workhorse',
        matchPatterns: [/claude-sonnet-5/i, /claude-sonnet-4-8/i, /claude.*sonnet/i],
      },
      {
        displayName: 'Claude Fable 5',
        tagKey: 'landing.pricing.tag.creative',
        matchPatterns: [/claude-fable-5/i, /claude.*fable/i],
      },
    ],
  },
  {
    id: 'google',
    label: 'Google',
    iconKey: 'gemini',
    entries: [
      {
        displayName: 'Gemini 3.1 Pro',
        tagKey: 'landing.pricing.tag.longContext',
        matchPatterns: [/gemini-3\.1-pro/i, /gemini.*pro/i],
      },
      {
        displayName: 'Gemini 3.5 Flash',
        tagKey: 'landing.pricing.tag.fast',
        matchPatterns: [/gemini-3\.5-flash/i, /gemini.*3\.5.*flash/i],
      },
      {
        displayName: 'Gemini 3 Flash',
        tagKey: 'landing.pricing.tag.freeCredit',
        matchPatterns: [/gemini-3-flash/i, /gemini.*flash/i],
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
  'GPT-5.5': {
    officialInputUsd: 5,
    officialOutputUsd: 30,
    cacheReadRatio: 0.1,
    cacheWriteRatio: 1,
  },
  'GPT-5.4': {
    officialInputUsd: 2.5,
    officialOutputUsd: 15,
    cacheReadRatio: 0.1,
    cacheWriteRatio: 1,
  },
  'GPT-5.4-mini': {
    officialInputUsd: 0.2,
    officialOutputUsd: 1.25,
    cacheReadRatio: 0.1,
    cacheWriteRatio: 1,
  },
  'Claude Opus 4.8': {
    officialInputUsd: 5,
    officialOutputUsd: 25,
    cacheReadRatio: 0.1,
    cacheWriteRatio: 1.25,
  },
  'Claude Sonnet 5': {
    officialInputUsd: 3,
    officialOutputUsd: 15,
    cacheReadRatio: 0.1,
    cacheWriteRatio: 1.25,
  },
  'Claude Fable 5': {
    officialInputUsd: 10,
    officialOutputUsd: 50,
    cacheReadRatio: 0.1,
    cacheWriteRatio: 1.25,
  },
  'Gemini 3.1 Pro': {
    officialInputUsd: 2,
    officialOutputUsd: 12,
    cacheReadRatio: 0.25,
    cacheWriteRatio: 1,
  },
  'Gemini 3.5 Flash': {
    officialInputUsd: 1.5,
    officialOutputUsd: 9,
    cacheReadRatio: 0.25,
    cacheWriteRatio: 1,
  },
  'Gemini 3 Flash': {
    officialInputUsd: 0.25,
    officialOutputUsd: 1.5,
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
