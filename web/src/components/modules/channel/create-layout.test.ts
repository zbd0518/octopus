import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const create = readFileSync(new URL('./Create.tsx', import.meta.url), 'utf8');
const form = readFileSync(new URL('./Form.tsx', import.meta.url), 'utf8');
const toolbar = readFileSync(new URL('../toolbar/index.tsx', import.meta.url), 'utf8');

test('channel creation starts with the form and allows presets on every viewport', () => {
  assert.match(create, /\[showPresetPicker, setShowPresetPicker\] = useState\(false\)/);
  assert.match(create, /onClick=\{\(\) => setShowPresetPicker\(!showPresetPicker\)\}/);
  assert.doesNotMatch(create, /!isMobile && showPresetPicker/);
  assert.doesNotMatch(create, /layout="create"/);
  assert.match(create, /showTemplatePicker=\{false\}/);
  assert.match(create, /cancelText=\{tForm\('modelPicker.cancel'\)\}/);
});

test('channel create dialog reuses the edit dialog sizing (no custom overrides)', () => {
  const editDialog = readFileSync(new URL('./Card.tsx', import.meta.url), 'utf8')
    .match(/<MorphingDialogContent className="([^"]*)"/)?.[1];
  const channelCreate = toolbar.match(/if \(activeItem === 'channel'\) \{\s*return '([^']*)'/)?.[1];

  assert.ok(editDialog, 'edit dialog classes required');
  assert.equal(channelCreate, editDialog, 'channel create dialog must match the edit dialog sizing exactly');
  assert.doesNotMatch(create, /radial-gradient|shadow-inner/);
  assert.match(create, /text-2xl font-semibold tracking-tight/);
  assert.match(create, /disableLayoutAnimation className="flex min-h-0 flex-1 flex-col overflow-hidden"/);
});

test('proxy mode row lives next to the enabled switch, advanced keeps pool/key strategy', () => {
  const advanced = form.slice(form.indexOf('<Accordion type="single"'), form.indexOf('</Accordion>'));
  assert.doesNotMatch(advanced, /ProxySelector/);
  assert.match(advanced, /t\('poolBinding'\)/);
  assert.match(advanced, /t\('keySelectionStrategy'\)/);
  assert.match(form, /checked=\{formData\.enabled\}[\s\S]*?<ProxySelector\s+layout="row"/);
  assert.match(form, /layout="row"/);
  assert.equal(form.split("t('poolBinding')").length - 1, 1);
  assert.equal(form.split("t('keySelectionStrategy')").length - 1, 1);
});

test('creation form scrolls independently and keeps safe-area-aware actions outside the scroll region', () => {
  assert.match(form, /layout = 'default'/);
  assert.match(form, /md:grid-cols-2/);
  assert.match(form, /overflow-y-auto overscroll-contain/);
  assert.match(form, /env\(safe-area-inset-bottom\)/);
  assert.match(form, /onClick=\{onCancel\}\s+disabled=\{isPending\}/);
  assert.match(form, /aria-label=\{`\$\{t\('apiKey'\)\} \$\{idx \+ 1\}`\}/);
  assert.match(form, /break-all whitespace-normal/);
  assert.doesNotMatch(form, /\[&_\[data-slot=select-trigger\]\]:w-full/);
  assert.match(form, /lg:grid-cols-\[minmax\(0,1fr\)_8rem_6\.5rem_2\.75rem\]/);
});
