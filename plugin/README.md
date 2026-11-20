# Native Claude Code plugin (experimental)

An optional hooks-module plugin runs the installed `ailine` executable and displays its output in a native band above the prompt. The regular `statusLine.command` integration remains the recommended option: the plugin API does not expose enough data to reproduce every segment.

Requires Claude Code with mods support (2.1.287+) and `ailine` on PATH. Tested against the API and plugin test runner in Claude Code 2.1.327; this early-access API can change between builds. The plugin does not install or download the executable.

## Install

Once the plugin is available on the repository's default branch, install it inside Claude Code:

```text
/plugin install ailine --marketplace lexfrei/ailine
```

The executable must already be installed separately. Installing the plugin does not replace a Homebrew, apt, or Go installation.

## Try it locally

From a checkout of this repository:

```bash
claude --plugin-dir ./plugin
```

The plugin reads `~/.ailinerc.toml`, including the legacy config fallback, through the executable. Set the plugin's `binary` option to an absolute executable path if `ailine` is not on Claude Code's PATH. The option is a single executable, not a shell command; arguments belong in the existing TOML config.

The plugin does not edit settings or disable an existing `statusLine.command`. If both are enabled, both rows appear. Disable the existing command yourself if you want only the experimental row.

## Behavior and limitations

The row refreshes every five seconds, including while idle. A refresh reads current session data without model calls or token-count requests, then starts one child process with an eight-second timeout. Refreshes do not overlap. Noninteractive sessions do not start the renderer.

| Data | Native plugin behavior |
| --- | --- |
| Model | Shows the identifier returned by the session API, not the regular statusline's display name |
| Context | Uses the API's context percentage when available |
| Quotas | Uses five-hour and seven-day windows with valid reset timestamps; an unknown reset is omitted, not shown as an epoch timestamp |
| Cost | Uses the API's session cost estimate, including plugin model calls; omitted when unavailable |
| Compactions | Reads the current transcript through the existing Go renderer |
| Worktree and branch | Uses the existing local Git inspection in the current working directory |
| Platform status | Uses the existing renderer's cached status check |
| Prompt cache, effort, thinking, fast mode | Not supplied by this wrapper |
| Repository identity and PR/review state | Not supplied by this wrapper; branch/worktree fallback remains available |
| Per-model quotas and credits | No new source in this API; the existing opt-in `mac_insecure` config still applies |
| Gateway spend limit | Exposed by the API but not rendered by ailine |
| Formatting | Preserves text and emoji, removes ANSI colors, disables terminal hyperlinks, and leaves wrapping to the host |

A missing or failing executable, timeout, or truncated output replaces the row with `ailine: unavailable (check executable and config)`. The plugin retries on the next refresh and never shows child stderr in the row. Run `ailine validate` and check the configured executable path when diagnosing this message.

## Files, processes, and network access

The mod reads the active model, context percentage, quota windows, estimated cost, working directory, and transcript path through Claude Code's API. It passes these fields to the configured executable over stdin, without a shell. It does not send the prompt or conversation text to the executable, change permission settings, or make model requests.

The executable reads the existing TOML config, the transcript for compaction counts, and local Git metadata for the worktree and branch. It reads and writes its existing runtime cache. When the platform-status segment is enabled, it makes a cached unauthenticated HTTPS request to `https://status.claude.com/api/v2/status.json`; disable that segment with `status = false` under `[segments]` to avoid the request.

The existing `mac_insecure = true` config is an explicit opt-in, not enabled by the plugin. It lets the executable read Claude Code's OAuth credentials from the macOS Keychain and send an authenticated usage request to `https://api.anthropic.com/api/oauth/usage` for per-model quotas and credits. Leave it off to use only the quota windows Claude Code supplies. Do not enable it merely to install the plugin.

## Publication status

This repository supplies its own marketplace. That is not an Anthropic directory listing or an endorsement. The [directory checklist](https://claude.com/docs/plugins/pre-submission-checklist) permits hooks modules, but also requires files used by hooks to be inside the plugin folder. It does not clearly settle whether a mod may depend on a separately installed executable. Directory compatibility for this wrapper remains unconfirmed until portal validation and review; a successful local validation is not approval.

Putting a compiled executable in the plugin instead would trigger a reviewer hold. A source-only TypeScript renderer could remove the separate ailine dependency, but would not provide the session fields missing from the API, and would still need review for its file and network access. No directory submission is included in this release.

## Checks

Run from the repository root with a Claude Code build that supports hooks modules:

```bash
claude plugin test ./plugin
claude plugin validate --strict ./plugin
```

The tests exercise the module through the host's plugin test runner, with mocked session data, timers, and child-process results. They do not verify terminal painting or run the real executable. `validate` also type-checks the module and tests against the installed build's API.

## API opportunities

The wrapper uses `session.usage()` to read context, rate limits, and estimated cost without a separate quota request, and `clock.every()` to keep countdowns refreshing while idle. It does not request a context breakdown: `breakdown: "full"` would make token-count API requests.

A future native panel could use `session.usage({ breakdown: "summary" })` for a locally estimated context breakdown or offer buttons for actions the user requests. Neither is needed for this status row. Credits, per-model quotas, multiple PRs, and prompt-cache state still lack a direct equivalent in the inspected API, so this plugin does not resolve those data-source limitations.

See the official [mods overview](https://code.claude.com/docs/en/plugins/mods/overview) and [API reference](https://code.claude.com/docs/en/plugins/mods/reference). The declarations generated by the installed Claude Code build are the compatibility reference.
