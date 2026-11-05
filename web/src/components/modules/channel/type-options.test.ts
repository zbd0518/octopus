import assert from "node:assert/strict";
import test from "node:test";

import { CHANNEL_TYPE_OPTIONS, DEFAULT_CHANNEL_TYPE } from "./type-options.ts";
import {
  ChannelType,
  isClaudeCodeHeaderProfileSupportedChannelType,
  normalizeRequestRewriteHeaderProfile,
  RequestRewriteHeaderProfile,
} from "../../../api/channel-types.ts";

const OpenAIChat: number = 0;
const OpenAIResponse: number = 1;

test("channel type options merge OpenAI chat and response into one OpenAI option", () => {
  const openaiOptions = CHANNEL_TYPE_OPTIONS.filter(
    (option) => option.value === OpenAIChat || option.value === OpenAIResponse,
  );

  assert.deepEqual(openaiOptions, [
    { value: OpenAIResponse, labelKey: "typeOpenAI" },
  ]);
});

test("DEFAULT_CHANNEL_TYPE matches the first option in CHANNEL_TYPE_OPTIONS (issue #143)", () => {
  assert.equal(DEFAULT_CHANNEL_TYPE, CHANNEL_TYPE_OPTIONS[0].value);
});

test("Claude Code header profile is supported only for Anthropic channels", () => {
  assert.equal(isClaudeCodeHeaderProfileSupportedChannelType(ChannelType.Anthropic), true);
  assert.equal(isClaudeCodeHeaderProfileSupportedChannelType(ChannelType.OpenAIChat), false);
  assert.equal(isClaudeCodeHeaderProfileSupportedChannelType(ChannelType.Gemini), false);
});

test("header profile normalization clears only stale Claude Code selections", () => {
  assert.equal(
    normalizeRequestRewriteHeaderProfile(ChannelType.OpenAIChat, RequestRewriteHeaderProfile.ClaudeCode),
    RequestRewriteHeaderProfile.None,
  );
  assert.equal(
    normalizeRequestRewriteHeaderProfile(ChannelType.Anthropic, RequestRewriteHeaderProfile.ClaudeCode),
    RequestRewriteHeaderProfile.ClaudeCode,
  );
  assert.equal(
    normalizeRequestRewriteHeaderProfile(ChannelType.OpenAIChat, RequestRewriteHeaderProfile.Codex),
    RequestRewriteHeaderProfile.Codex,
  );
  assert.equal(
    normalizeRequestRewriteHeaderProfile(ChannelType.Anthropic, RequestRewriteHeaderProfile.None),
    RequestRewriteHeaderProfile.None,
  );
  assert.equal(
    normalizeRequestRewriteHeaderProfile(ChannelType.Anthropic, undefined),
    RequestRewriteHeaderProfile.None,
  );
});
