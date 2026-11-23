import type { QuotaData, ScopedWindow } from "./types";
import { trimSpace } from "./text";

function normalizeModelToken(raw: string): string {
	return trimSpace(raw).toLowerCase().replace(/[ ._]/g, "-");
}

function modelTokens(raw: string): string[] {
	const normalized = normalizeModelToken(raw);
	return normalized === "" ? [] : normalized.split("-");
}

function isVersionToken(token: string): boolean {
	// Go limits UTF-8 byte length, not the number of Unicode digits.
	return (
		/^[0-9]{1,2}$/.test(token) ||
		(/^\p{Nd}$/u.test(token) && (token.codePointAt(0) ?? 0) <= 0x7ff)
	);
}

export function matchesModel(
	modelID: string,
	display: string,
	bucket: string,
): boolean {
	const key = modelTokens(bucket);
	if (key.length === 0) return false;
	for (const raw of [modelID, display]) {
		const tokens = modelTokens(raw);
		for (let start = 0; start + key.length <= tokens.length; start++) {
			if (!key.every((token, index) => tokens[start + index] === token))
				continue;
			const next = start + key.length;
			if (
				isVersionToken(key[key.length - 1] ?? "") &&
				next < tokens.length &&
				isVersionToken(tokens[next] ?? "")
			)
				continue;
			return true;
		}
	}
	return false;
}

export function matchesScopedModel(
	modelID: string,
	display: string,
	bucket: ScopedWindow,
): boolean {
	if (bucket.id !== "" && modelID !== "")
		return (
			matchesModel(modelID, "", bucket.id) ||
			matchesModel(bucket.id, "", modelID)
		);
	return matchesModel(modelID, display, bucket.name);
}

export function scopedLabel(name: string): string {
	return `7d-${normalizeModelToken(name)}`;
}

export function perModelCandidates(
	data: QuotaData | undefined,
): ScopedWindow[] {
	if (data === undefined) return [];
	const candidates: ScopedWindow[] = [
		{ name: "opus", id: "", window: data.sevenDayOpus },
		{ name: "sonnet", id: "", window: data.sevenDaySonnet },
		{ name: "cowork", id: "", window: data.sevenDayCowork },
		{ name: "oauth", id: "", window: data.sevenDayOAuthApps },
		...(data.scoped ?? []),
	];
	return candidates.filter((candidate) => candidate.window !== undefined);
}

export function perModelWindows(data: QuotaData | undefined): ScopedWindow[] {
	const windows: ScopedWindow[] = [];
	const indexByLabel = new Map<string, number>();
	for (const candidate of perModelCandidates(data)) {
		const label = scopedLabel(candidate.name);
		const index = indexByLabel.get(label);
		if (index !== undefined) {
			const previous = windows[index];
			if (
				candidate.window !== undefined &&
				previous?.window !== undefined &&
				candidate.window.utilization > previous.window.utilization
			)
				windows[index] = candidate;
			continue;
		}
		indexByLabel.set(label, windows.length);
		windows.push(candidate);
	}
	return windows;
}

export function mostBinding(windows: readonly ScopedWindow[]): ScopedWindow[] {
	let best: ScopedWindow | undefined;
	for (const win of windows) {
		if (win.window === undefined) continue;
		if (
			best?.window === undefined ||
			win.window.utilization > best.window.utilization
		)
			best = win;
	}
	return best === undefined ? [] : [best];
}
