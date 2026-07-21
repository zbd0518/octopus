import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const source = fs.readFileSync(path.join(__dirname, 'navbar.tsx'), 'utf8');

test('active navbar item uses a visible accent color', () => {
    assert.match(source, /text-primary/);
    assert.equal(
        source.includes('text-sidebar-primary-foreground'),
        false,
        'active navbar items should not use the near-white sidebar foreground token',
    );
});

test('mobile nav labels escape overflow-x clipping via fixed anchors', () => {
    // Labels must not be absolute children of the overflow-x-auto nav.
    assert.match(source, /fixed z-\[60\]/);
    assert.match(source, /getBoundingClientRect/);
    assert.match(
        source,
        /overflow-x-auto[\s\S]*md:overflow-visible/,
        'nav strip still scrolls horizontally on mobile',
    );
    assert.equal(
        /absolute -top-7/.test(source),
        false,
        'legacy absolute -top-7 labels are clipped by overflow-x-auto',
    );
});
