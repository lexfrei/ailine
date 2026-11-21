import { scopedLabel } from "./scoped";
import { trimSpace } from "./text";
import type {
	ExhaustedWindow,
	QuotaData,
	QuotaWindow,
	ScopedWindow,
	Theme,
} from "./types";

export const fiveHourWindowMinutes = 300;
export const sevenDayWindowMinutes = 10080;
const reset = "\x1b[0m";
const statusColors: Readonly<Record<string, string>> = {
	"🟢": "\x1b[32m",
	"🟡": "\x1b[33m",
	"🟠": "\x1b[38;5;208m",
	"🔴": "\x1b[31m",
	"🔶": "\x1b[38;5;208m",
	"⚠️": "\x1b[33m",
};

export function duration(minutes: number): string {
	if (minutes <= 0) return "now";
	const days = Math.trunc(minutes / 1440);
	const hours = Math.trunc((minutes % 1440) / 60);
	if (days > 0) return `${days}d ${hours}h`;
	if (hours > 0) return `${hours}h ${minutes % 60}m`;
	return `${minutes % 60}m`;
}

export function rateIndicator(
	usagePct: number,
	remaining: number,
	total: number,
): string {
	const elapsed = Math.max(total - remaining, 0);
	const timePct = total > 0 ? Math.trunc((elapsed * 100) / total) : 0;
	const diff = usagePct - timePct;
	if (diff <= 0) return "🟢";
	if (diff <= 5) return "🟡";
	if (diff <= 15) return "🟠";
	return "🔴";
}

export function part(theme: Theme, text: string, ...icons: string[]): string {
	if (theme === "text") {
		for (const icon of icons) {
			const color = Object.hasOwn(statusColors, icon) ? statusColors[icon] : undefined;
			if (color !== undefined) return color + text + reset;
		}
		return text;
	}
	const lead = icons[0] ?? "";
	const sub = icons.slice(1).join("");
	let out = lead === "" ? text : `${lead} ${text}`;
	if (sub !== "") out += ` ${sub}`;
	return out;
}

export function contextSegment(theme: Theme, pct: number): string {
	const rounded = Math.trunc(pct + 0.5);
	const color =
		rounded >= 80 ? "\x1b[31m" : rounded >= 50 ? "\x1b[33m" : "\x1b[32m";
	return color + part(theme, `${rounded}%`, "🧠") + reset;
}

export function promptCacheSegment(theme: Theme, cause: string): string {
	if (theme === "text") return `cache: ${cause === "" ? "cold" : cause}`;
	return cause === "" ? "🧊" : part(theme, cause, "🧊");
}

export function formatQuotaWindow(
	theme: Theme,
	win: QuotaWindow,
	label: string,
): string {
	const pct = Math.trunc(win.utilization + 0.5);
	return part(
		theme,
		`${label}: ${pct}% (${duration(win.remainingMinutes)})`,
		rateIndicator(pct, win.remainingMinutes, win.totalMinutes),
	);
}

export function formatStaleQuotaWindow(
	theme: Theme,
	win: QuotaWindow,
	label: string,
): string {
	const pct = Math.trunc(win.utilization + 0.5);
	return part(
		theme,
		`${label}: ?% (${duration(win.remainingMinutes)})`,
		rateIndicator(pct, win.remainingMinutes, win.totalMinutes),
	);
}

export function formatRateLimitSegment(
	theme: Theme,
	exhausted: ExhaustedWindow | undefined,
): string {
	if (exhausted === undefined) return part(theme, "limit hit", "⛔");
	const text = `${exhausted.name} limit hit`;
	return part(
		theme,
		exhausted.minutes <= 0 ? text : `${text} (${duration(exhausted.minutes)})`,
		"⛔",
	);
}

export function findExhaustedWindow(
	data: QuotaData | undefined,
	perModel: readonly ScopedWindow[],
): ExhaustedWindow | undefined {
	if (data === undefined) return undefined;
	const windows = [
		{ name: "5h", window: data.fiveHour },
		{ name: "7d", window: data.sevenDay },
		...perModel.map((scoped) => ({
			name: scopedLabel(scoped.name),
			window: scoped.window,
		})),
	];
	let best: ExhaustedWindow | undefined;
	let bestPct = -1;
	for (const entry of windows) {
		if (entry.window === undefined || entry.window.remainingMinutes <= 0)
			continue;
		const pct = Math.trunc(entry.window.utilization + 0.5);
		if (pct < 99) continue;
		if (
			pct > bestPct ||
			(pct === bestPct &&
				(best === undefined || entry.window.remainingMinutes < best.minutes))
		) {
			bestPct = pct;
			best = { name: entry.name, minutes: entry.window.remainingMinutes };
		}
	}
	return best;
}

export function parseIsoUtc(raw: string): number | undefined {
	const match =
		/^(\d{4})-(\d{2})-(\d{2})T(\d{1,2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(
			trimSpace(raw),
		);
	if (match === null) return undefined;
	const year = Number(match[1]);
	const month = Number(match[2]);
	const day = Number(match[3]);
	const hour = Number(match[4]);
	const minute = Number(match[5]);
	const second = Number(match[6]);
	const millisecond = Number((match[7] ?? "").slice(0, 3).padEnd(3, "0"));
	if (
		month < 1 ||
		month > 12 ||
		day < 1 ||
		day > 31 ||
		hour > 23 ||
		minute > 59 ||
		second > 59
	)
		return undefined;
	const date = new Date(0);
	date.setUTCFullYear(year, month - 1, day);
	date.setUTCHours(hour, minute, second, millisecond);
	if (date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day)
		return undefined;
	const zone = match[8] ?? "Z";
	if (zone === "Z") return date.getTime();
	const zoneHours = Number(zone.slice(1, 3));
	const zoneMinutes = Number(zone.slice(4, 6));
	if (zoneHours > 24 || zoneMinutes > 60) return undefined;
	const offset = (zoneHours * 60 + zoneMinutes) * 60000;
	return date.getTime() - (zone[0] === "+" ? offset : -offset);
}
