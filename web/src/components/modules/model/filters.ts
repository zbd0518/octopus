import { canonicalEndpoint } from '../apikey/endpoint-grouping.ts';

export const MODEL_CAPABILITY_OPTIONS = [
  'all', 'chat', 'embeddings', 'rerank', 'moderations', 'image_generation',
  'audio_speech', 'audio_transcription', 'video_generation', 'music_generation', 'search',
] as const;

export type ModelCapabilityFilter = typeof MODEL_CAPABILITY_OPTIONS[number];

interface FilterItem {
  name: string;
  canonical_name?: string;
  variant_names?: string[];
  input: number;
  output: number;
  cache_read: number;
  cache_write: number;
}

interface Capability {
  name: string;
  canonical_name?: string;
  aliases?: string[];
  endpoints: string[];
  conversation: boolean;
}

export function filterMarketItems<T extends FilterItem>({
  items, capabilities, searchTerm, capability, provider, dedupe, pricing,
  inferProvider, normalizeName,
}: {
  items: T[];
  capabilities: Capability[];
  searchTerm: string;
  capability: ModelCapabilityFilter;
  provider: string;
  dedupe: boolean;
  pricing: 'all' | 'priced' | 'free';
  inferProvider: (name: string) => string;
  normalizeName: (name: string) => string;
}): T[] {
  const term = searchTerm.trim().toLowerCase();
  const capabilityByName = new Map<string, Capability[]>();
  for (const entry of capabilities) {
    const names = [entry.name, entry.canonical_name ?? '', ...(entry.aliases ?? [])].filter(Boolean);
    for (const name of names) {
      const values = capabilityByName.get(name) ?? [];
      values.push(entry);
      capabilityByName.set(name, values);
    }
  }

  return items.filter((item) => {
    const searchableNames = [
      item.name,
      item.canonical_name ?? '',
      ...(item.variant_names ?? []),
    ].filter(Boolean);
    if (term && !searchableNames.some((name) => name.toLowerCase().includes(term))) return false;
    const priced = [item.input, item.output, item.cache_read, item.cache_write].some((value) => value > 0);
    if (pricing === 'priced' && !priced || pricing === 'free' && priced) return false;
    if (provider && !searchableNames.some((name) => inferProvider(name) === provider)) return false;
    if (capability !== 'all') {
      const entries = searchableNames.flatMap((name) => capabilityByName.get(name) ?? []);
      const matches = entries.some((entry) => capability === 'chat'
        ? entry.conversation || entry.endpoints.length === 0 || entry.endpoints.some((endpoint) => canonicalEndpoint(endpoint) === 'chat')
        : entry.endpoints.includes(capability));
      if (!matches) return false;
    }
    // The backend is authoritative for dedupe=true/false. Keep this argument
    // for callers and old tests, but never hide raw items client-side.
    void dedupe;
    void normalizeName;
    return true;
  });
}
