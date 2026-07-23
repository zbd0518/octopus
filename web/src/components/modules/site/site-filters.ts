/** 站点平台筛选纯函数（不依赖运行时 @/ 解析，便于 node:test）。 */

export const SITE_PLATFORM_LABELS: Record<string, string> = {
  "new-api": "New API",
  anyrouter: "AnyRouter",
  "one-api": "One API",
  "one-hub": "One Hub",
  "done-hub": "Done Hub",
  sub2api: "Sub2API",
  openai: "OpenAI",
  claude: "Claude",
  gemini: "Gemini",
  sapi: "SAPI",
};

export function platformLabel(platform: string): string {
  return SITE_PLATFORM_LABELS[platform] ?? platform;
}

/** 空数组表示不按平台筛选（全部）。 */
export function siteMatchesPlatformFilters(
  platform: string,
  filters: string[],
): boolean {
  if (filters.length === 0) {
    return true;
  }
  return filters.includes(platform);
}

/** 从站点列表收集去重平台，并按展示名排序。 */
export function collectSitePlatforms(
  sites: Array<{ platform: string }> | undefined,
): string[] {
  const set = new Set<string>();
  for (const site of sites ?? []) {
    set.add(site.platform);
  }
  return [...set].sort((a, b) =>
    platformLabel(a).localeCompare(platformLabel(b), "en"),
  );
}

/** 各平台站点数量（用于筛选 chip 计数）。 */
export function countSitesByPlatform(
  sites: Array<{ platform: string }> | undefined,
): Map<string, number> {
  const counts = new Map<string, number>();
  for (const site of sites ?? []) {
    counts.set(site.platform, (counts.get(site.platform) ?? 0) + 1);
  }
  return counts;
}
