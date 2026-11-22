import { isRecord, parseJson } from "./json";

export type RepoInfo = { host: string; owner: string; name: string };
export type PrInfo = {
	number: number;
	url: string;
	reviewState: string;
	kind: string;
};
export type PromptCacheInfo = {
	warm: boolean;
	cachingObserved: boolean;
	cacheWriteTokens: number | undefined;
	lastMissCauses: string[] | undefined;
};
export type RateWindowInput = {
	usedPercentage: number;
	resetsAtMs: number | undefined;
};
export type StatusInput = {
	model: { id: string; displayName: string };
	effortLevel: string;
	thinking: boolean;
	fastMode: boolean;
	cwd: string;
	workspace: {
		currentDir: string;
		gitWorktree: string;
		repo: RepoInfo | undefined;
	};
	pr: PrInfo | undefined;
	costUsd: number;
	contextPct: number;
	promptCache: PromptCacheInfo | undefined;
	transcriptPath: string;
	rateLimits: {
		fiveHour: RateWindowInput | undefined;
		sevenDay: RateWindowInput | undefined;
	};
};
export type InputParseResult = { input: StatusInput; warnings: string[] };

export function emptyInput(): StatusInput {
	return {
		model: { id: "", displayName: "" },
		effortLevel: "",
		thinking: false,
		fastMode: false,
		cwd: "",
		workspace: { currentDir: "", gitWorktree: "", repo: undefined },
		pr: undefined,
		costUsd: 0,
		contextPct: 0,
		promptCache: undefined,
		transcriptPath: "",
		rateLimits: { fiveHour: undefined, sevenDay: undefined },
	};
}

function warn(warnings: string[]): void {
	if (warnings.length === 0)
		warnings.push("ailine: stdin parse error: invalid input");
}

function readRecord(
	value: unknown,
	warnings: string[],
): Record<string, unknown> | undefined {
	if (value === undefined || value === null) return undefined;
	if (isRecord(value)) return value;
	warn(warnings);
	// Go allocates pointer structs before reporting a non-null type mismatch.
	return {};
}

function readString(value: unknown, warnings: string[]): string {
	if (value === undefined || value === null) return "";
	if (typeof value === "string") return value.toWellFormed();
	warn(warnings);
	return "";
}

function readBool(value: unknown, warnings: string[]): boolean {
	if (value === undefined || value === null) return false;
	if (typeof value === "boolean") return value;
	warn(warnings);
	return false;
}

function readNumber(value: unknown, warnings: string[]): number {
	if (value === undefined || value === null) return 0;
	if (typeof value === "number" && Number.isFinite(value)) return value;
	warn(warnings);
	return 0;
}

function readPr(value: unknown, warnings: string[]): PrInfo | undefined {
	const pr = readRecord(value, warnings);
	if (pr === undefined) return undefined;
	let number = readNumber(pr.number, warnings);
	// JSON.parse cannot recover int64 precision beyond the safe integer range.
	if (!Number.isSafeInteger(number)) {
		warn(warnings);
		number = 0;
	}
	return {
		number,
		url: readString(pr.url, warnings),
		reviewState: readString(pr.review_state, warnings),
		kind: readString(pr.kind, warnings),
	};
}

function readRepo(value: unknown, warnings: string[]): RepoInfo | undefined {
	const repo = readRecord(value, warnings);
	if (repo === undefined) return undefined;
	return {
		host: readString(repo.host, warnings),
		owner: readString(repo.owner, warnings),
		name: readString(repo.name, warnings),
	};
}

function readCauses(value: unknown, warnings: string[]): string[] | undefined {
	const miss = readRecord(value, warnings);
	if (miss?.causes === undefined || miss.causes === null) return undefined;
	if (!Array.isArray(miss.causes)) {
		warn(warnings);
		return undefined;
	}
	const causes: string[] = [];
	for (const cause of miss.causes) causes.push(readString(cause, warnings));
	return causes;
}

function readPromptCache(
	value: unknown,
	warnings: string[],
): PromptCacheInfo | undefined {
	const cache = readRecord(value, warnings);
	if (cache === undefined) return undefined;
	return {
		warm: readBool(cache.warm, warnings),
		cachingObserved: readBool(cache.caching_observed, warnings),
		cacheWriteTokens:
			cache.cache_write_tokens === undefined ||
			cache.cache_write_tokens === null
				? undefined
				: readNumber(cache.cache_write_tokens, warnings),
		lastMissCauses: readCauses(cache.last_miss_cause, warnings),
	};
}

function readWindow(
	value: unknown,
	warnings: string[],
): RateWindowInput | undefined {
	const window = readRecord(value, warnings);
	if (window === undefined) return undefined;
	const seconds = readNumber(window.resets_at, warnings);
	let resetsAtMs = Math.trunc(seconds) * 1000;
	// Keep timestamps exact and finite; architecture-specific int64 overflow is not portable.
	if (!Number.isSafeInteger(resetsAtMs)) {
		warn(warnings);
		resetsAtMs = 0;
	}
	return {
		usedPercentage: readNumber(window.used_percentage, warnings),
		resetsAtMs: resetsAtMs === 0 ? 0 : resetsAtMs,
	};
}

export function parseInput(raw: string): InputParseResult {
	const input = emptyInput();
	const warnings: string[] = [];
	if (raw === "") return { input, warnings };
	let parsed: unknown;
	try {
		parsed = parseJson(raw);
	} catch {
		warn(warnings);
		return { input, warnings };
	}
	const data = readRecord(parsed, warnings) ?? {};
	const model = readRecord(data.model, warnings);
	input.model = {
		id: readString(model?.id, warnings),
		displayName: readString(model?.display_name, warnings),
	};
	const effort = readRecord(data.effort, warnings);
	input.effortLevel = readString(effort?.level, warnings);
	const thinking = readRecord(data.thinking, warnings);
	input.thinking = readBool(thinking?.enabled, warnings);
	input.fastMode = readBool(data.fast_mode, warnings);
	input.cwd = readString(data.cwd, warnings);
	const workspace = readRecord(data.workspace, warnings);
	input.workspace = {
		currentDir: readString(workspace?.current_dir, warnings),
		gitWorktree: readString(workspace?.git_worktree, warnings),
		repo: readRepo(workspace?.repo, warnings),
	};
	input.pr = readPr(data.pr, warnings);
	const cost = readRecord(data.cost, warnings);
	input.costUsd = readNumber(cost?.total_cost_usd, warnings);
	const context = readRecord(data.context_window, warnings);
	input.contextPct = readNumber(context?.used_percentage, warnings);
	input.promptCache = readPromptCache(data.prompt_cache, warnings);
	input.transcriptPath = readString(data.transcript_path, warnings);
	const limits = readRecord(data.rate_limits, warnings);
	input.rateLimits = {
		fiveHour: readWindow(limits?.five_hour, warnings),
		sevenDay: readWindow(limits?.seven_day, warnings),
	};
	return { input, warnings };
}
