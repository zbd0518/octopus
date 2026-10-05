import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const source = (name: string) => readFileSync(new URL(name, import.meta.url), 'utf8');

test('group editor exposes translated thinking policies and disables them for raw forwarding', () => {
  const editor = source('./Editor.tsx');
  assert.match(editor, /initial\?\.thinking_mode \?\? 'auto'/);
  assert.match(editor, /thinking_mode: thinkingMode/);
  for (const mode of ['auto', 'off', 'on']) {
    assert.ok(editor.includes(`<option value="${mode}">{t('form.thinkingMode.${mode}')}</option>`));
  }
  assert.match(editor, /disabled=\{outboundFormat === 'passthrough' \|\| outboundFormat === 'raw'\}/);
});

test('create, edit and failed-member removal preserve the thinking policy', () => {
  assert.match(source('./Create.tsx'), /thinking_mode: thinking_mode \?\? 'auto'/);
  assert.match(source('./EditDialogContent.tsx'), /thinking_mode: group.thinking_mode \?\? 'auto'/);
  for (const name of ['./Card.tsx', './GroupListItem.tsx']) {
    const card = source(name);
    assert.equal((card.match(/thinking_mode: group.thinking_mode \?\? 'auto'/g) ?? []).length, 2);
    assert.match(card, /payload.thinking_mode = nextThinkingMode/);
  }
});

test('thinking policy copy is in the group form namespace in every locale', () => {
  for (const locale of ['en', 'zh_hans', 'zh_hant']) {
    const messages = JSON.parse(source(`../../../../public/locale/${locale}.json`));
    for (const key of ['label', 'hint', 'auto', 'off', 'on']) {
      assert.equal(typeof messages.group.form.thinkingMode[key], 'string');
    }
    assert.equal(typeof messages.errors.invalidThinkingMode, 'string');
  }
});
