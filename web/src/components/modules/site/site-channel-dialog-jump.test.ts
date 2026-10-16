import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";

const sitePage = readFileSync(
  new URL("../site/index.tsx", import.meta.url),
  "utf8",
);
const siteChannel = readFileSync(
  new URL("../site-channel/index.tsx", import.meta.url),
  "utf8",
);
const host = readFileSync(
  new URL("../site-channel/SiteChannelDialogHost.tsx", import.meta.url),
  "utf8",
);

test("site tab opens the site-channel dialog in place instead of switching tabs", () => {
  // 站点/账号的「查看站点渠道」都走同一个请求 store，不再 requestJump 到站点渠道页签。
  assert.match(sitePage, /requestSiteChannelDialog\(siteId\)/);
  assert.match(sitePage, /requestSiteChannelDialog\(siteId, accountId\)/);
  assert.doesNotMatch(sitePage, /kind: "site-channel-card"/);
  assert.doesNotMatch(sitePage, /kind: "site-channel-account"/);
  assert.doesNotMatch(sitePage, /kind: 'site-channel-card'/);

  // 弹窗宿主常驻在站点页签内。
  assert.match(sitePage, /<SiteChannelDialogHost \/>/);
  assert.match(host, /useSiteChannelDialogRequestStore/);
  assert.match(host, /setIsOpen\(true\)/);
  // 不再切换 Hub 页签。
  assert.doesNotMatch(host, /useHubTabStore/);
});

test("site tab jump retries until the card ref is registered, then always clears pending", () => {
  const effect = sitePage.slice(
    sitePage.indexOf("if (!pendingSiteJump) return;"),
    sitePage.indexOf("[pendingSiteJump, visibleSites, clearPendingJump, flashTarget]"),
  );
  // 卡片 ref 由 ResizeObserver 异步登记：必须有重试，不能只试一次。
  assert.match(effect, /const tryLocate = \(\) =>/);
  assert.match(effect, /window\.setTimeout\(tryLocate, retryDelay\)/);
  assert.match(effect, /attempts < maxAttempts/);
  // 重试耗尽也必须清理，否则一次失败会永久挡住后续跳转。
  assert.match(effect, /clearPendingJump\(requestId\);\s*\}\s*\};/);
});

test("site-channel card jump retries and clears pending instead of jumping to the list head", () => {
  const effect = siteChannel.slice(
    siteChannel.indexOf("if (!pendingSiteChannelJump) return;"),
    siteChannel.indexOf("[pendingSiteChannelJump, clearPending, cards.length]"),
  );
  assert.match(effect, /const tryScroll = \(\) =>/);
  assert.match(effect, /window\.setTimeout\(tryScroll, retryDelay\)/);
  // 兜底清理，避免 pending 卡死。
  assert.match(effect, /clearPending\(requestId\);\s*\}\s*\};/);
});

test("site-channel dialog account jump retries until the account tab ref exists", () => {
  const effect = siteChannel.slice(
    siteChannel.indexOf("if (jumpRequest.target.siteId !== card.site_id) return;"),
    siteChannel.indexOf("[jumpRequest, card.site_id, activeAccountId, onJumpHandled]"),
  );
  assert.ok(effect.length > 0, "account jump effect slice must be non-empty");
  // ref 未登记时不能直接标记为已处理（原来一帧没拿到就静默失败）。
  assert.match(effect, /const trySelectAccount = \(\) =>/);
  assert.match(effect, /window\.setTimeout\(trySelectAccount, retryDelay\)/);
  assert.match(effect, /attempts < maxAttempts/);
});