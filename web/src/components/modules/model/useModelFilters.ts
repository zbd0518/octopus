'use client';

import { useModelCapabilities, type ModelMarketItem } from '@/api/endpoints/model';
import { getModelIcon } from '@/lib/model-icons';
import { normalizeModelName, useNormalizeRulesVersion } from './normalize';
import { filterMarketItems, type ModelCapabilityFilter } from './filters';
export { MODEL_CAPABILITY_OPTIONS, type ModelCapabilityFilter } from './filters';

export function inferModelProvider(name: string): string {
  return getModelIcon(name).label;
}

export function useModelFilters({ items, searchTerm, capability, provider, dedupe, pricing, enabled = true }: {
  items: ModelMarketItem[];
  searchTerm: string;
  capability: ModelCapabilityFilter;
  provider: string;
  dedupe: boolean;
  pricing: 'all' | 'priced' | 'free';
  enabled?: boolean;
}) {
  const capabilityQuery = useModelCapabilities(enabled);
  useNormalizeRulesVersion();
  const providers = Array.from(new Set(items.flatMap((item) => [
    item.name,
    item.canonical_name,
    ...(item.variant_names ?? []),
  ]).filter((name): name is string => Boolean(name)).map(inferModelProvider)))
    .sort((a, b) => a.localeCompare(b));
  const visible = filterMarketItems({
    items, searchTerm, capability, provider, dedupe, pricing,
    capabilities: capabilityQuery.data ?? [],
    inferProvider: inferModelProvider,
    normalizeName: normalizeModelName,
  });

  return { visible, providers, capabilityQuery };
}
