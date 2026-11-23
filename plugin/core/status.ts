import { toWellFormed } from "./text";
import { part } from "./format";
import { isRecord, parseJson } from "./json";
import type { StatusRuntime } from "./runtime";
import type { Theme } from "./types";

const statusUrl = "https://status.claude.com/api/v2/status.json";
const statusIndicators: Readonly<
	Record<string, { icon: string; text: string }>
> = {
	minor: { icon: "⚠️", text: "degraded" },
	major: { icon: "🔶", text: "major outage" },
	critical: { icon: "🔴", text: "critical outage" },
};

export function renderStatus(theme: Theme, indicator: string): string {
	const status = Object.hasOwn(statusIndicators, indicator)
		? statusIndicators[indicator]
		: undefined;
	return status === undefined ? "" : part(theme, status.text, status.icon);
}

export async function fetchStatus(
	runtime: StatusRuntime,
	theme: Theme,
	ttlMs: number,
): Promise<string> {
	const cached = await runtime.cache.read("status", ttlMs, runtime.now());
	if (cached !== undefined) return renderStatus(theme, cached);
	let indicator = "";
	try {
		const response = await runtime.httpGet(statusUrl, {}, 2000);
		if (response.status === 200) {
			const body = parseJson(response.body);
			if (
				isRecord(body) &&
				isRecord(body.status) &&
				typeof body.status.indicator === "string"
			) {
				indicator = toWellFormed(body.status.indicator);
			}
		}
	} catch {
		indicator = "";
	}
	await runtime.cache.write("status", indicator);
	return renderStatus(theme, indicator);
}
