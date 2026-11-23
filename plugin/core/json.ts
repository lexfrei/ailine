export function parseJson(raw: string): unknown {
	let depth = 0;
	let inString = false;
	let escaped = false;
	// Native JSON decoding has no Go-compatible nesting ceiling.
	for (const char of raw) {
		if (inString) {
			if (escaped) escaped = false;
			else if (char === "\\") escaped = true;
			else if (char === '"') inString = false;
			continue;
		}
		if (char === '"') inString = true;
		else if (char === "[" || char === "{") {
			depth++;
			if (depth > 10000) throw new Error("JSON nesting limit exceeded");
		} else if (char === "]" || char === "}") depth--;
	}
	try {
		return JSON.parse(raw) as unknown;
	} catch {
		throw new Error("Invalid JSON payload");
	}
}

export function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}
