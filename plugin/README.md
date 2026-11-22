# Native Claude Code plugin (experimental)

The plugin displays ailine above the prompt using the same TypeScript renderer as the CLI. It needs no separately installed `ailine`, Bun, or Node. The regular `statusLine.command` integration remains available and receives fields missing from the native API.

Tested against Claude Code 2.1.327. The early-access mods API can change between builds.

## Install

Distribution is planned through the `lexfrei/ccc` marketplace using this repository's `plugin` subdirectory. The marketplace entry has not been added yet. For now, use a local checkout:

```bash
claude --plugin-dir ./plugin
```

The plugin reads `~/.ailinerc.toml`, falling back to `~/.claudelinerc.toml`. Its optional `config` setting selects another TOML file. Dependencies are included inside the plugin; installation does not download packages or executables.

The plugin does not edit settings or disable an existing `statusLine.command`. If both are enabled, both rows appear. Disable the existing command yourself if you want only the native row.

## Behavior and limitations

The row refreshes every five seconds, including while idle. Refreshes do not overlap, and results from an earlier session cannot replace a newer session's row. Noninteractive sessions do not run the renderer. No model calls or context-breakdown requests are made.

| Data | Native plugin behavior |
| --- | --- |
| Model | Shows the identifier returned by the session API, not the CLI's display name |
| Context | Uses the API's context percentage when available |
| Quotas | Uses five-hour and seven-day windows; an unknown reset is omitted |
| Cost | Uses the API's session cost estimate; omitted when unavailable, even if enabled in config |
| Compactions | Reads the current transcript and counts compaction records |
| Worktree and branch | Reads local Git metadata without running Git |
| Platform status | Uses a cached HTTPS status check |
| Prompt cache, effort, thinking, fast mode | Not exposed by the inspected session API |
| Repository identity and PR/review state | Not exposed; branch/worktree fallback remains available |
| Per-model quotas and credits | Require the explicit `mac_insecure` opt-in |
| Gateway spend limit | Exposed by the API but not rendered |
| Formatting | Preserves text and emoji, removes ANSI colors, disables terminal hyperlinks, and leaves wrapping to the host |

Unexpected query or rendering failures display `ailine: unavailable (check config and permissions)`. The plugin retries on the next refresh without exposing exception details. Expected file, cache, and network failures use the shared renderer's fallback behavior.

## Files, processes, and network access

The plugin reads the active model, context percentage, quota windows, estimated cost, working directory, and transcript path through Claude Code's API. It reads the TOML config, transcript, and local Git metadata through the host's file API. It does not send conversation text to a service or executable.

Cache records use the host's persistent JSON store. They are separate from the CLI's file cache; file permission and atomic-rename guarantees do not apply to this store. Raw OAuth tokens are never stored.

When the platform-status segment is enabled, the plugin makes a cached unauthenticated HTTPS request to `https://status.claude.com/api/v2/status.json`. Set `status = false` under `[segments]` to avoid that request.

The host HTTP API has no per-request cancellation signal. The plugin limits how long rendering waits, permits only one outstanding request per endpoint, and discards late responses. A timed-out underlying request may remain pending until the host completes it; the next refresh does not start a duplicate.

`mac_insecure = true` explicitly enables reading Claude Code's OAuth credentials from the macOS Keychain through `/usr/bin/security` and sending an authenticated request to `https://api.anthropic.com/api/oauth/usage`. This is the only subprocess path. Leave it off to use only quota windows supplied by Claude Code. Do not enable it merely to install the plugin.

## Publication status

The plugin has not been submitted for directory review. Portal validation requires a Verified organization for hooks modules and their TypeScript sources. Replacing the external executable does not remove that publisher restriction. A successful local validation is not directory approval.

## Checks

Run from the repository root with the pinned Bun and Claude Code versions:

```bash
bun run test:host
claude plugin validate --strict --profile directory ./plugin
```

Host tests use mocked session data, timers, files, storage, HTTP responses, and credential commands. The adapter tests run in a temporary source-only plugin, without parent `node_modules`. These tests do not exercise real credentials, external services, or terminal painting.

See the official [mods overview](https://code.claude.com/docs/en/plugins/mods/overview) and [API reference](https://code.claude.com/docs/en/plugins/mods/reference). The installed build's declarations are the compatibility reference.
