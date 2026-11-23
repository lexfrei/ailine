import {
	fiveHourWindowMinutes,
	parseIsoUtc,
	sevenDayWindowMinutes,
} from "./format";
import { isRecord, parseJson } from "./json";
import { toWellFormed, trimSpace } from "./text";
import type { QuotaData, QuotaWindow, ScopedWindow } from "./types";

export type UsageParseResult =
	| { data: QuotaData; error: undefined; cacheable: boolean }
	| { data: undefined; error: Error; cacheable: false };

function readRecord(value: unknown): Record<string, unknown> | undefined {
	if (value === undefined || value === null) return undefined;
	if (!isRecord(value)) throw new Error("Invalid usage object");
	return value;
}

function readString(value: unknown): string {
	if (value === undefined || value === null) return "";
	if (typeof value !== "string") throw new Error("Invalid usage string");
	return toWellFormed(value);
}

function readNumber(value: unknown): number {
	if (value === undefined || value === null) return 0;
	if (typeof value !== "number" || !Number.isFinite(value))
		throw new Error("Invalid usage number");
	return value;
}

function readBool(value: unknown): boolean {
	if (value === undefined || value === null) return false;
	if (typeof value !== "boolean") throw new Error("Invalid usage boolean");
	return value;
}

function parseWindow(
	utilization: number,
	resetsAt: string,
	totalMinutes: number,
	nowMs: number,
): QuotaWindow | undefined {
	const resetsAtMs = parseIsoUtc(resetsAt);
	if (resetsAtMs === undefined) return undefined;
	return {
		utilization,
		resetsAtMs,
		totalMinutes,
		remainingMinutes: Math.max(Math.trunc((resetsAtMs - nowMs) / 60000), 0),
	};
}

function parseLegacyWindow(
	value: unknown,
	totalMinutes: number,
	nowMs: number,
): QuotaWindow | undefined {
	const window = readRecord(value);
	if (window === undefined) return undefined;
	return parseWindow(
		readNumber(window.utilization),
		readString(window.resets_at),
		totalMinutes,
		nowMs,
	);
}

function parseScopedWindows(
	value: unknown,
	nowMs: number,
): ScopedWindow[] | undefined {
	if (value === undefined || value === null) return undefined;
	if (!Array.isArray(value)) throw new Error("Invalid usage limits");
	const scoped: ScopedWindow[] = [];
	for (const entry of value) {
		const limit = readRecord(entry) ?? {};
		const kind = readString(limit.kind);
		const percent = readNumber(limit.percent);
		const resetsAt = readString(limit.resets_at);
		const scope = readRecord(limit.scope);
		const model = readRecord(scope?.model);
		const id = trimSpace(readString(model?.id));
		const name = trimSpace(readString(model?.display_name));
		if (kind !== "weekly_scoped" || model === undefined || name === "")
			continue;
		const window = parseWindow(percent, resetsAt, sevenDayWindowMinutes, nowMs);
		if (window === undefined) continue;
		scoped.push({ name, id, window });
	}
	return scoped.length === 0 ? undefined : scoped;
}

export function parseUsageBody(raw: string, nowMs: number): UsageParseResult {
	try {
		const response = readRecord(parseJson(raw)) ?? {};
		const data: QuotaData = {
			fiveHour: parseLegacyWindow(
				response.five_hour,
				fiveHourWindowMinutes,
				nowMs,
			),
			sevenDay: parseLegacyWindow(
				response.seven_day,
				sevenDayWindowMinutes,
				nowMs,
			),
			sevenDayOpus: parseLegacyWindow(
				response.seven_day_opus,
				sevenDayWindowMinutes,
				nowMs,
			),
			sevenDaySonnet: parseLegacyWindow(
				response.seven_day_sonnet,
				sevenDayWindowMinutes,
				nowMs,
			),
			sevenDayCowork: parseLegacyWindow(
				response.seven_day_cowork,
				sevenDayWindowMinutes,
				nowMs,
			),
			sevenDayOAuthApps: parseLegacyWindow(
				response.seven_day_oauth_apps,
				sevenDayWindowMinutes,
				nowMs,
			),
			scoped: parseScopedWindows(response.limits, nowMs),
		};
		const extra = readRecord(response.extra_usage);
		if (extra !== undefined) {
			const enabled = readBool(extra.is_enabled);
			const monthlyLimit = readNumber(extra.monthly_limit);
			const usedCredits = readNumber(extra.used_credits);
			if (enabled && usedCredits > 0)
				data.extra = { monthlyLimit, usedCredits };
		}
		const error = readRecord(response.error);
		if (error !== undefined)
			return {
				data: { errorType: readString(error.type) },
				error: undefined,
				cacheable: false,
			};
		return { data, error: undefined, cacheable: true };
	} catch (error) {
		return {
			data: undefined,
			error:
				error instanceof Error ? error : new Error("Invalid usage payload"),
			cacheable: false,
		};
	}
}
