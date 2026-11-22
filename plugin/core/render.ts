import type { Config } from "./config";
import { type CompactionCache, countCompactions } from "./compaction";
import {
	contextSegment,
	findExhaustedWindow,
	fiveHourWindowMinutes,
	formatFixed,
	formatQuotaWindow,
	formatRateLimitSegment,
	formatStaleQuotaWindow,
	part,
	promptCacheSegment,
	sevenDayWindowMinutes,
} from "./format";
import { readGitInfo } from "./gitinfo";
import type {
	PromptCacheInfo,
	RateWindowInput,
	RepoInfo,
	StatusInput,
} from "./input";
import { link } from "./links";
import type { FileReader, UsageRuntime } from "./runtime";
import {
	matchesScopedModel,
	mostBinding,
	perModelCandidates,
	perModelWindows,
	scopedLabel,
} from "./scoped";
import { fetchStatus } from "./status";
import type {
	Mode,
	QuotaData,
	QuotaWindow,
	ScopedWindow,
	Theme,
} from "./types";
import { fetchLastGood, fetchUsage } from "./usage";
import { joinPipeWrap } from "./wrap";

export type RenderRuntime = UsageRuntime & {
	files: FileReader;
	compactionCache?: CompactionCache;
};
export type RenderOptions = { maxWidth: number; hyperlinks: boolean };

const missLabels: Readonly<Record<string, string>> = {
	system_prompt_changed: "prompt",
	tools_changed: "tools",
	model_changed: "model",
	fast_mode_changed: "fast",
	cache_scope_or_ttl_changed: "scope",
	betas_changed: "betas",
	effort_changed: "effort",
	auto_mode_changed: "auto",
	overage_changed: "limits",
	extra_body_changed: "request",
	defer_loading_changed: "defer",
	messages_rewritten: "history",
	ttl_expired_5m: "ttl",
	ttl_expired_1h: "ttl",
	likely_server_side: "server",
	unknown: "",
};

function effortIcon(level: string): string {
	switch (level) {
		case "low":
			return "⬇️";
		case "high":
			return "⬆️";
		case "xhigh":
			return "⏫";
		case "max":
			return "🚀";
		default:
			return "";
	}
}

function cacheVisible(cache: PromptCacheInfo | undefined): boolean {
	if (cache === undefined) return false;
	return (
		cache.cachingObserved &&
		!cache.warm &&
		(cache.cacheWriteTokens === undefined || cache.cacheWriteTokens > 0)
	);
}

function cacheCause(causes: readonly string[] | undefined): string {
	const first = causes?.[0];
	if (first === undefined) return "";
	const label = Object.hasOwn(missLabels, first)
		? (missLabels[first] ?? "")
		: first.replace(/_changed$/, "");
	return label !== "" && causes !== undefined && causes.length > 1
		? `${label}+`
		: label;
}

function inputWindow(
	window: RateWindowInput | undefined,
	minutes: number,
	nowMs: number,
): QuotaWindow | undefined {
	if (window?.resetsAtMs === undefined) return undefined;
	return {
		utilization: window.usedPercentage,
		resetsAtMs: window.resetsAtMs,
		totalMinutes: minutes,
		remainingMinutes: Math.max(
			Math.trunc((window.resetsAtMs - nowMs) / 60000),
			0,
		),
	};
}

function selectedWindows(
	input: StatusInput,
	data: QuotaData | undefined,
	mode: Mode,
): ScopedWindow[] {
	if (!data || mode === "false") return [];
	if (mode === "true") return perModelWindows(data);
	const matched: ScopedWindow[] = [];
	for (const candidate of perModelCandidates(data)) {
		if (matchesScopedModel(input.model.id, input.model.displayName, candidate))
			matched.push(candidate);
	}
	return mostBinding(matched);
}

function appendWindows(
	segments: string[],
	data: QuotaData,
	windows: readonly ScopedWindow[],
	theme: Theme,
	stale: boolean,
): void {
	const labeled = [
		{ window: data.sevenDay, label: "7d" },
		{ window: data.fiveHour, label: "5h" },
		...windows.map((window) => ({
			window: window.window,
			label: scopedLabel(window.name),
		})),
	];
	for (const entry of labeled) {
		if (entry.window === undefined) continue;
		segments.push(
			stale
				? formatStaleQuotaWindow(theme, entry.window, entry.label)
				: formatQuotaWindow(theme, entry.window, entry.label),
		);
	}
}

function appendStale(
	segments: string[],
	data: QuotaData | undefined,
	windows: readonly ScopedWindow[],
	theme: Theme,
): void {
	if (data === undefined) {
		segments.push(
			part(theme, "7d: ?% (?d)", "⏳"),
			part(theme, "5h: ?% (?h)", "⏳"),
		);
		return;
	}
	appendWindows(segments, data, windows, theme, true);
}

async function appendUsage(
	segments: string[],
	input: StatusInput,
	config: Config,
	runtime: RenderRuntime,
): Promise<void> {
	const flags = config.segments;
	const theme = config.theme;
	if (!config.macInsecure) {
		if (flags.quota) {
			appendWindows(
				segments,
				{
					fiveHour: inputWindow(
						input.rateLimits.fiveHour,
						fiveHourWindowMinutes,
						runtime.now(),
					),
					sevenDay: inputWindow(
						input.rateLimits.sevenDay,
						sevenDayWindowMinutes,
						runtime.now(),
					),
				},
				[],
				theme,
				false,
			);
		}
		return;
	}
	const result = await fetchUsage(runtime, config.cache.usageTtlMs);
	if (result.error !== undefined) {
		if (flags.quota) {
			const lastGood = await fetchLastGood(runtime);
			appendStale(
				segments,
				lastGood,
				selectedWindows(input, lastGood, flags.perModelQuota),
				theme,
			);
		}
		return;
	}
	const data = result.data;
	if (data.errorType) {
		if (data.errorType !== "rate_limit_error") {
			segments.push(part(theme, "/login needed", "⚠️"));
			return;
		}
		const lastGood = await fetchLastGood(runtime);
		const windows = selectedWindows(input, lastGood, flags.perModelQuota);
		segments.push(
			formatRateLimitSegment(theme, findExhaustedWindow(lastGood, windows)),
		);
		if (flags.quota) appendStale(segments, lastGood, windows, theme);
		return;
	}
	if (flags.quota)
		appendWindows(
			segments,
			data,
			selectedWindows(input, data, flags.perModelQuota),
			theme,
			false,
		);
	if (flags.credits && data.extra !== undefined && data.extra.usedCredits > 0) {
		segments.push(
			part(
				theme,
				`$${formatFixed(data.extra.usedCredits, 0)}/$${formatFixed(data.extra.monthlyLimit, 0)}`,
				"💳",
			),
		);
	}
}

function repoIdentity(repo: RepoInfo): {
	icon: string;
	text: string;
	url: string;
} {
	let icon = "📦";
	let prefix = "";
	if (repo.host === "github.com") icon = "🐙";
	else if (repo.host === "gitlab.com") icon = "🦊";
	else if (repo.host === "bitbucket.org") icon = "🪣";
	else if (repo.host !== "") prefix = `${repo.host}/`;
	const url =
		repo.host && repo.owner && repo.name
			? `https://${repo.host}/${repo.owner}/${repo.name}`
			: "";
	return { icon, text: `${prefix}${repo.owner}/${repo.name}`, url };
}

function reviewIcon(state: string): string {
	switch (state) {
		case "draft":
			return "📝";
		case "approved":
			return "✅";
		case "changes_requested":
			return "🔴";
		case "commented":
			return "💬";
		case "pending":
			return "👀";
		default:
			return "";
	}
}

async function appendRepo(
	segments: string[],
	input: StatusInput,
	config: Config,
	runtime: RenderRuntime,
	hyperlinks: boolean,
): Promise<void> {
	const combined = config.segments.repo && input.workspace.repo !== undefined;
	if (!combined && !config.segments.worktree) return;
	const info = await readGitInfo(
		runtime.files,
		input.workspace.currentDir || input.cwd,
	);
	const worktree: string[] = [];
	if (info.worktree !== "")
		worktree.push(part(config.theme, info.worktree, "🌳"));
	if (info.branch !== "" && info.branch !== info.worktree)
		worktree.push(part(config.theme, info.branch, "🌿"));
	else if (
		info.branch === "" &&
		info.worktree === "" &&
		input.workspace.gitWorktree !== ""
	)
		worktree.push(part(config.theme, input.workspace.gitWorktree, "🌿"));
	if (!combined || input.workspace.repo === undefined) {
		if (worktree.length) segments.push(worktree.join(" "));
		return;
	}
	const repo = repoIdentity(input.workspace.repo);
	const parts = [
		part(config.theme, link(repo.text, repo.url, hyperlinks), repo.icon),
	];
	if (input.pr !== undefined && input.pr.number > 0) {
		const number = link(
			`${input.pr.kind === "mr" ? "!" : "#"}${input.pr.number}`,
			input.pr.url,
			hyperlinks,
		);
		const icon = reviewIcon(input.pr.reviewState);
		parts.push(icon === "" ? number : part(config.theme, number, icon));
	}
	parts.push(...worktree);
	segments.push(parts.join(" "));
}

export async function buildSegments(
	input: StatusInput,
	config: Config,
	runtime: RenderRuntime,
	hyperlinks: boolean,
): Promise<string[]> {
	const segments: string[] = [];
	const flags = config.segments;
	const theme = config.theme;
	if (flags.model) {
		const qualifiers: string[] = [];
		const effort = flags.effort ? effortIcon(input.effortLevel) : "";
		if (effort !== "") qualifiers.push(effort);
		if (flags.thinking && input.thinking) qualifiers.push("💭");
		if (flags.fastMode && input.fastMode) qualifiers.push("⚡");
		segments.push(
			part(theme, input.model.displayName || "Claude", "🤖", ...qualifiers),
		);
	}
	const subscriber =
		input.rateLimits.fiveHour !== undefined ||
		input.rateLimits.sevenDay !== undefined;
	if (flags.cost === "true" || (flags.cost === "auto" && !subscriber))
		segments.push(part(theme, `$${formatFixed(input.costUsd, 2)}`, "💰"));
	if (flags.status) {
		const status = await fetchStatus(runtime, theme, config.cache.statusTtlMs);
		if (status !== "") segments.push(status);
	}
	if (flags.context && input.contextPct > 0)
		segments.push(contextSegment(theme, input.contextPct));
	if (flags.promptCache && cacheVisible(input.promptCache))
		segments.push(
			promptCacheSegment(theme, cacheCause(input.promptCache?.lastMissCauses)),
		);
	if (flags.compactions) {
		const count = await countCompactions(
			runtime.files,
			input.transcriptPath,
			runtime.compactionCache,
		);
		if (count > 0) segments.push(part(theme, String(count), "🔄"));
	}
	if (flags.quota || flags.credits)
		await appendUsage(segments, input, config, runtime);
	await appendRepo(segments, input, config, runtime, hyperlinks);
	return segments;
}

export async function renderStatusline(
	input: StatusInput,
	config: Config,
	runtime: RenderRuntime,
	options: RenderOptions,
): Promise<string> {
	return joinPipeWrap(
		await buildSegments(input, config, runtime, options.hyperlinks),
		options.maxWidth,
	);
}
