import assert from 'node:assert/strict';
import test from 'node:test';

import {
    DEFAULT_BASE_URL_BY_PLATFORM,
    POOL_PLATFORM_OPTIONS,
    POOL_TYPE_OPTIONS_BY_PLATFORM,
    platformSupportsOAuth,
    platformSupportsQuota,
    type PoolPlatform,
} from './type-options.ts';

// B3-#6 route A: 'setup-token' was removed from the frontend credential type
// union and every platform's option list. The backend keeps the
// model.PoolTypeSetupToken constant for historical rows, and unknown types
// still render through the fallback editor in AccountFormDialog, so the UI
// must never offer setup-token again.
test('setup-token is not offered for any platform', () => {
    for (const [platform, types] of Object.entries(POOL_TYPE_OPTIONS_BY_PLATFORM)) {
        assert.ok(!types.includes('setup-token' as never), `platform ${platform} must not offer setup-token`);
        assert.ok(types.length > 0, `platform ${platform} must offer at least one type`);
    }
});

test('type option lists stay platform-complete and unique', () => {
    const platforms = POOL_PLATFORM_OPTIONS.map((opt) => opt.value);
    for (const platform of platforms) {
        const types = POOL_TYPE_OPTIONS_BY_PLATFORM[platform as PoolPlatform];
        assert.ok(Array.isArray(types), `missing type options for platform ${platform}`);
        assert.equal(new Set(types).size, types.length, `duplicate type options for platform ${platform}`);
    }
});

test('oauth-capable platforms match the OAuth flow support list', () => {
    for (const [platform, types] of Object.entries(POOL_TYPE_OPTIONS_BY_PLATFORM)) {
        const hasOAuth = types.includes('oauth' as never);
        const supportsOAuth = platformSupportsOAuth(platform as PoolPlatform);
        assert.equal(hasOAuth, supportsOAuth, `platform ${platform} oauth option vs platformSupportsOAuth mismatch`);
    }
});

test('default base urls exist for every platform and quota support stays narrow', () => {
    for (const platform of POOL_PLATFORM_OPTIONS.map((opt) => opt.value)) {
        assert.ok(platform in DEFAULT_BASE_URL_BY_PLATFORM, `missing default base url for ${platform}`);
    }
    assert.equal(platformSupportsQuota('openai', 'oauth'), true);
    assert.equal(platformSupportsQuota('volcengine', 'cookie'), true);
    assert.equal(platformSupportsQuota('gemini', 'oauth'), false);
    assert.equal(platformSupportsQuota('openai', 'apikey'), false);
});
