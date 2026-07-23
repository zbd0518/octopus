import test from 'node:test';
import assert from 'node:assert/strict';

import { brandBadgeStyle } from './brand-badge-style.ts';

test('brandBadgeStyle lifts near-black brand text in dark mode', () => {
    const style = brandBadgeStyle('#000000', true);
    assert.equal(style.color, '#E8E8E8');
    assert.equal(style.backgroundColor, '#00000033');
});

test('brandBadgeStyle keeps dark brand text in light mode', () => {
    const style = brandBadgeStyle('#000000', false);
    assert.equal(style.color, '#000000');
    assert.equal(style.backgroundColor, '#00000015');
});

test('brandBadgeStyle darkens near-white brand text in light mode', () => {
    const style = brandBadgeStyle('#FFFFFF', false);
    assert.equal(style.color, '#333333');
    assert.equal(style.backgroundColor, '#FFFFFF15');
});

test('brandBadgeStyle keeps mid-tone brand colors', () => {
    const dark = brandBadgeStyle('#10A37F', true);
    const light = brandBadgeStyle('#10A37F', false);
    assert.equal(dark.color, '#10A37F');
    assert.equal(light.color, '#10A37F');
});
