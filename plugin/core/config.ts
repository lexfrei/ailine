import { parse } from "smol-toml";
import type { Mode, Theme } from "./types";
import { trimSpace } from "./text";

export type Segments = {
	model: boolean;
	effort: boolean;
	thinking: boolean;
	fastMode: boolean;
	repo: boolean;
	worktree: boolean;
	cost: Mode;
	status: boolean;
	context: boolean;
	promptCache: boolean;
	compactions: boolean;
	quota: boolean;
	perModelQuota: Mode;
	credits: boolean;
};

export type Config = {
	segments: Segments;
	cache: { usageTtlMs: number; statusTtlMs: number };
	macInsecure: boolean;
	theme: Theme;
	hyperlinks: Mode;
};

export type ConfigLoadResult = { config: Config; warnings: string[] };

type ConfigIssue = {
	path: string;
	label: string;
	raw: string;
	fallback: string;
	expected: string;
};
type DecodedConfig = { config: Config; error?: string };

const booleanFields = [
	["model", "model"],
	["effort", "effort"],
	["thinking", "thinking"],
	["fast_mode", "fastMode"],
	["repo", "repo"],
	["worktree", "worktree"],
	["status", "status"],
	["context", "context"],
	["prompt_cache", "promptCache"],
	["compactions", "compactions"],
	["quota", "quota"],
	["credits", "credits"],
] as const;

const cacheFields = [
	["cache.usage_ttl", "usageTtlMs"],
	["cache.status_ttl", "statusTtlMs"],
] as const;
const stringFields = [
	"segments.cost",
	"segments.per_model_quota",
	"theme",
	"hyperlinks",
] as const;
const knownKeys = new Set<string>([
	"segments.cost",
	"segments.per_model_quota",
	"segments.offpeak",
	"cache.usage_ttl",
	"cache.status_ttl",
	"mac_insecure",
	"theme",
	"hyperlinks",
]);
for (const [key] of booleanFields) knownKeys.add(`segments.${key}`);

const durationUnits: Readonly<Record<string, bigint>> = {
	ns: 1n,
	us: 1000n,
	µs: 1000n,
	μs: 1000n,
	ms: 1000000n,
	s: 1000000000n,
	m: 60000000000n,
	h: 3600000000000n,
};
const signedLimit = 1n << 63n;

export function defaults(): Config {
	return {
		segments: {
			model: true,
			effort: true,
			thinking: true,
			fastMode: true,
			repo: true,
			worktree: true,
			cost: "auto",
			status: true,
			context: true,
			promptCache: true,
			compactions: true,
			quota: true,
			perModelQuota: "auto",
			credits: true,
		},
		cache: { usageTtlMs: 600000, statusTtlMs: 15000 },
		macInsecure: false,
		theme: "emoji",
		hyperlinks: "auto",
	};
}

export function normalizeMode(raw: string): Mode | undefined {
	switch (trimSpace(raw).toLowerCase()) {
		case "":
		case "auto":
			return "auto";
		case "true":
		case "t":
		case "1":
		case "on":
			return "true";
		case "false":
		case "f":
		case "0":
		case "off":
			return "false";
		default:
			return undefined;
	}
}

export function normalizeTheme(raw: string): Theme | undefined {
	if (raw === "" || raw === "emoji") return "emoji";
	if (raw === "text") return "text";
	return undefined;
}

function parseDurationNs(raw: string): bigint | undefined {
	const negative = raw.startsWith("-");
	let offset = raw.startsWith("-") || raw.startsWith("+") ? 1 : 0;
	if (raw.slice(offset) === "0") return 0n;
	if (offset === raw.length) return undefined;
	const part = /(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/y;
	let total = 0n;
	while (offset < raw.length) {
		part.lastIndex = offset;
		const match = part.exec(raw);
		if (!match || match[1] === undefined || match[2] === undefined)
			return undefined;
		offset = part.lastIndex;
		const unit = durationUnits[match[2]];
		if (unit === undefined) return undefined;
		const [integer = "0", fraction = ""] = match[1].split(".");
		const significant = integer.replace(/^0+/, "") || "0";
		if (significant.length > 19) return undefined;
		const whole = BigInt(significant);
		if (whole > signedLimit / unit) return undefined;
		let value = whole * unit;
		let numerator = 0n;
		let scale = 1;
		for (const character of fraction) {
			if (numerator > (signedLimit - 1n) / 10n) break;
			const next = numerator * 10n + BigInt(character);
			if (next > signedLimit) break;
			numerator = next;
			scale *= 10;
		}
		if (numerator > 0n)
			value += BigInt(Math.trunc(Number(numerator) * (Number(unit) / scale)));
		total += value;
		if (value > signedLimit || total > signedLimit) return undefined;
	}
	if (!negative && total === signedLimit) return undefined;
	return negative ? -total : total;
}

export function parseDurationMs(raw: string): number | undefined {
	const value = parseDurationNs(raw);
	return value === undefined ? undefined : Number(value) / 1000000;
}

function coerceDurationNs(value: unknown): bigint | undefined {
	if (typeof value === "string") return parseDurationNs(value);
	if (typeof value === "boolean") return value ? 1n : 0n;
	if (typeof value === "number" && Number.isFinite(value)) {
		const number = BigInt(Math.trunc(value));
		return number >= -signedLimit && number < signedLimit ? number : undefined;
	}
	if (typeof value === "bigint" && value >= -signedLimit && value < signedLimit)
		return value;
	return undefined;
}

function durationText(value: bigint): string {
	if (value === 0n) return "0s";
	const sign = value < 0n ? "-" : "";
	const ns = value < 0n ? -value : value;
	if (ns < 1000000000n) {
		const unit = ns >= 1000000n ? 1000000n : ns >= 1000n ? 1000n : 1n;
		const suffix = unit === 1000000n ? "ms" : unit === 1000n ? "µs" : "ns";
		const digits = unit === 1000000n ? 6 : unit === 1000n ? 3 : 0;
		const fraction = digits
			? (ns % unit).toString().padStart(digits, "0").replace(/0+$/, "")
			: "";
		return `${sign}${ns / unit}${fraction ? `.${fraction}` : ""}${suffix}`;
	}
	const seconds = ns / 1000000000n;
	const fraction = (ns % 1000000000n)
		.toString()
		.padStart(9, "0")
		.replace(/0+$/, "");
	const secondText = `${seconds % 60n}${fraction ? `.${fraction}` : ""}s`;
	if (seconds >= 3600n)
		return `${sign}${seconds / 3600n}h${(seconds / 60n) % 60n}m${secondText}`;
	if (seconds >= 60n) return `${sign}${seconds / 60n}m${secondText}`;
	return `${sign}${secondText}`;
}

function weakBoolean(value: unknown): boolean | undefined {
	if (typeof value === "boolean") return value;
	if (typeof value === "number" || typeof value === "bigint")
		return value !== 0 && value !== 0n;
	if (typeof value !== "string") return undefined;
	switch (value) {
		case "":
		case "false":
		case "False":
		case "FALSE":
		case "f":
		case "F":
		case "0":
			return false;
		case "true":
		case "True":
		case "TRUE":
		case "t":
		case "T":
		case "1":
			return true;
		default:
			return undefined;
	}
}

function weakString(value: unknown): string | undefined {
	if (
		typeof value === "string" ||
		typeof value === "number" ||
		typeof value === "boolean" ||
		typeof value === "bigint"
	)
		return String(value);
	return undefined;
}

function rawText(value: unknown): string {
	if (Array.isArray(value)) return `[${value.map(rawText).join(" ")}]`;
	return weakString(value) ?? "map";
}

function flatValues(source: string): ReadonlyMap<string, unknown> {
	const output = new Map<string, unknown>();
	function visit(value: unknown, prefix: string): void {
		if (value !== null && typeof value === "object" && !Array.isArray(value)) {
			const prototype = Object.getPrototypeOf(value);
			if (prototype === null || prototype === Object.prototype) {
				for (const [key, child] of Object.entries(value))
					visit(
						child,
						prefix ? `${prefix}.${key.toLowerCase()}` : key.toLowerCase(),
					);
				return;
			}
		}
		output.set(prefix, value);
	}
	visit(parse(source, { integersAsBigInt: true }), "");
	return output;
}

function failure(path: string, expected: string): DecodedConfig {
	return { config: defaults(), error: `${path}: expected ${expected}` };
}

function decode(values: ReadonlyMap<string, unknown>): DecodedConfig {
	const config = defaults();
	for (const [key, field] of booleanFields) {
		const path = `segments.${key}`;
		if (!values.has(path)) continue;
		const value = weakBoolean(values.get(path));
		if (value === undefined)
			return failure(path, "a boolean-compatible scalar");
		config.segments[field] = value;
	}
	if (values.has("mac_insecure")) {
		const value = weakBoolean(values.get("mac_insecure"));
		if (value === undefined)
			return failure("mac_insecure", "a boolean-compatible scalar");
		config.macInsecure = value;
	}
	for (const path of stringFields) {
		if (values.has(path) && weakString(values.get(path)) === undefined)
			return failure(path, "a scalar string");
	}
	config.segments.cost =
		normalizeMode(weakString(values.get("segments.cost")) ?? "auto") ?? "auto";
	config.segments.perModelQuota =
		normalizeMode(
			weakString(values.get("segments.per_model_quota")) ?? "auto",
		) ?? "auto";
	config.theme =
		normalizeTheme(weakString(values.get("theme")) ?? "emoji") ?? "emoji";
	config.hyperlinks =
		normalizeMode(weakString(values.get("hyperlinks")) ?? "auto") ?? "auto";
	for (const [path, field] of cacheFields) {
		if (!values.has(path)) continue;
		const duration = coerceDurationNs(values.get(path));
		if (duration === undefined)
			return failure(path, "a Go duration string or nanosecond number");
		config.cache[field] = Number(duration) / 1000000;
	}
	return { config };
}

function normalizationIssues(
	values: ReadonlyMap<string, unknown>,
): ConfigIssue[] {
	const issues: ConfigIssue[] = [];
	const fields = [
		["segments.cost", "cost mode"],
		["segments.per_model_quota", "per-model quota mode"],
		["hyperlinks", "hyperlinks mode"],
	] as const;
	for (const [path, label] of fields) {
		const raw = weakString(values.get(path));
		if (raw !== undefined && normalizeMode(raw) === undefined)
			issues.push({
				path,
				label,
				raw,
				fallback: "auto",
				expected: "auto, true, or false",
			});
	}
	const raw = weakString(values.get("theme"));
	if (raw !== undefined && normalizeTheme(raw) === undefined)
		issues.push({
			path: "theme",
			label: "theme",
			raw,
			fallback: "emoji",
			expected: "emoji or text",
		});
	return issues;
}

export function loadConfigText(source: string | undefined): ConfigLoadResult {
	if (source === undefined) return { config: defaults(), warnings: [] };
	let values: ReadonlyMap<string, unknown>;
	try {
		values = flatValues(source);
	} catch {
		return { config: defaults(), warnings: [] };
	}
	const result = decode(values);
	if (result.error)
		return {
			config: defaults(),
			warnings: [`ailine: config parse error: ${result.error}`],
		};
	const warnings = normalizationIssues(values).map(
		(issue) =>
			`ailine: invalid ${issue.label} ${JSON.stringify(issue.raw)}, using ${issue.fallback}`,
	);
	return { config: result.config, warnings };
}

export function validateConfigText(source: string | undefined): string[] {
	if (source === undefined) return [];
	let values: ReadonlyMap<string, unknown>;
	try {
		values = flatValues(source);
	} catch (error) {
		return [`cannot read config: ${String(error)}`];
	}
	const problems: string[] = [];
	for (const key of [...values.keys()].sort()) {
		if (!knownKeys.has(key))
			problems.push(`unknown key ${JSON.stringify(key)} (typo?)`);
	}
	const result = decode(values);
	if (result.error)
		return [...problems, `cannot parse config: ${result.error}`];
	for (const issue of normalizationIssues(values))
		problems.push(
			`${issue.path}: unknown value ${JSON.stringify(issue.raw)} (expected ${issue.expected})`,
		);
	for (const [key] of booleanFields) {
		const path = `segments.${key}`;
		if (values.has(path) && typeof values.get(path) !== "boolean")
			problems.push(
				`${path}: expected true or false, got ${rawText(values.get(path))}`,
			);
	}
	if (
		values.has("mac_insecure") &&
		typeof values.get("mac_insecure") !== "boolean"
	)
		problems.push(
			`mac_insecure: expected true or false, got ${rawText(values.get("mac_insecure"))}`,
		);
	for (const [path] of cacheFields) {
		const duration = coerceDurationNs(values.get(path));
		if (duration !== undefined && duration < 0n)
			problems.push(`${path}: negative duration ${durationText(duration)}`);
	}
	return problems;
}
