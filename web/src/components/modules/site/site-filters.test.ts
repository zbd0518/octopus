import assert from "node:assert/strict";
import test from "node:test";
import {
  collectSitePlatforms,
  countSitesByPlatform,
  platformLabel,
  siteMatchesPlatformFilters,
} from "./site-filters.ts";

test("siteMatchesPlatformFilters: empty filters match all", () => {
  assert.equal(siteMatchesPlatformFilters("new-api", []), true);
  assert.equal(siteMatchesPlatformFilters("claude", []), true);
});

test("siteMatchesPlatformFilters: only selected platforms pass", () => {
  const filters = ["new-api", "one-api"];
  assert.equal(siteMatchesPlatformFilters("new-api", filters), true);
  assert.equal(siteMatchesPlatformFilters("one-api", filters), true);
  assert.equal(siteMatchesPlatformFilters("claude", filters), false);
});

test("collectSitePlatforms: unique + sorted by label", () => {
  const platforms = collectSitePlatforms([
    { platform: "claude" },
    { platform: "new-api" },
    { platform: "claude" },
    { platform: "anyrouter" },
  ]);
  const labels = platforms.map((p) => platformLabel(p));
  assert.deepEqual(
    labels,
    [...labels].sort((a, b) => a.localeCompare(b, "en")),
  );
  assert.equal(platforms.length, 3);
  assert.ok(platforms.includes("new-api"));
  assert.ok(platforms.includes("claude"));
  assert.ok(platforms.includes("anyrouter"));
});

test("collectSitePlatforms: empty / undefined", () => {
  assert.deepEqual(collectSitePlatforms(undefined), []);
  assert.deepEqual(collectSitePlatforms([]), []);
});

test("countSitesByPlatform: counts per platform", () => {
  const counts = countSitesByPlatform([
    { platform: "new-api" },
    { platform: "new-api" },
    { platform: "gemini" },
  ]);
  assert.equal(counts.get("new-api"), 2);
  assert.equal(counts.get("gemini"), 1);
  assert.equal(counts.get("claude"), undefined);
});

test("platformLabel: known and unknown", () => {
  assert.equal(platformLabel("new-api"), "New API");
  assert.equal(platformLabel("custom-x"), "custom-x");
});
