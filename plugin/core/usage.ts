import { parseIsoUtc } from "./format";
import type { HttpResponse, UsageRuntime } from "./runtime";
import type { QuotaData } from "./types";
import { trimSpace } from "./text";
import { parseUsageBody, type UsageParseResult } from "./usage-parse";

const usageUrl = "https://api.anthropic.com/api/oauth/usage";
const authFailTtlMs = 3600000;
const int64Limit = 1n << 63n;

function failure(message: string): UsageParseResult {
	return { data: undefined, error: new Error(message), cacheable: false };
}

function retrySeconds(headers: Readonly<Record<string, string>>): bigint {
	let raw = "";
	for (const [name, value] of Object.entries(headers)) {
		if (name.toLowerCase() === "retry-after") {
			raw = value;
			break;
		}
	}
	if (!/^[+-]?\d+$/.test(raw)) return 30n;
	const seconds = BigInt(raw);
	if (seconds < -int64Limit || seconds >= int64Limit) return 30n;
	return seconds < 0n ? 0n : seconds;
}

async function parseAndCache(
	runtime: UsageRuntime,
	body: string,
): Promise<UsageParseResult> {
	const result = parseUsageBody(body, runtime.now());
	if (result.error !== undefined)
		return failure(`parsing usage response: ${result.error.message}`);
	if (result.cacheable) await runtime.cache.write("usage-last-good", body);
	return result;
}

export async function fetchUsage(
	runtime: UsageRuntime,
	ttlMs: number,
): Promise<UsageParseResult> {
	const cached = await runtime.cache.read("usage", ttlMs, runtime.now());
	if (cached !== undefined) return parseAndCache(runtime, cached);
	const retryAfter = await runtime.cache.read(
		"usage-retry-after",
		undefined,
		runtime.now(),
	);
	if (retryAfter !== undefined && retryAfter === trimSpace(retryAfter)) {
		const deadline = parseIsoUtc(retryAfter);
		if (deadline !== undefined && runtime.now() < deadline) {
			return {
				data: { errorType: "rate_limit_error" },
				error: undefined,
				cacheable: false,
			};
		}
	}
	let token: string | undefined;
	try {
		token = await runtime.getToken();
	} catch {
		return failure("getting oauth token");
	}
	if (!token) return failure("getting oauth token: no token available");
	let tokenHash: string;
	try {
		tokenHash = await runtime.hashToken(token);
	} catch {
		return failure("hashing oauth token");
	}
	const failedTokenHash = await runtime.cache.read(
		"usage-auth-failed",
		authFailTtlMs,
		runtime.now(),
	);
	if (failedTokenHash === tokenHash) {
		return {
			data: { errorType: "authentication_error" },
			error: undefined,
			cacheable: false,
		};
	}
	let response: HttpResponse;
	try {
		response = await runtime.httpGet(
			usageUrl,
			{
				Authorization: `Bearer ${token}`,
				"anthropic-beta": "oauth-2025-04-20",
			},
			3000,
		);
	} catch {
		return failure("fetching usage");
	}
	if (response.status === 429) {
		const delayNs = BigInt.asIntN(
			64,
			(retrySeconds(response.headers) + 5n) * 1000000000n,
		);
		const deadlineMs =
			Math.floor((runtime.now() + Number(delayNs) / 1000000) / 1000) * 1000;
		await runtime.cache.write(
			"usage-retry-after",
			new Date(deadlineMs).toISOString().replace(".000Z", "Z"),
		);
		return {
			data: { errorType: "rate_limit_error" },
			error: undefined,
			cacheable: false,
		};
	}
	if (response.status === 401) {
		await runtime.cache.write("usage-auth-failed", tokenHash);
		return {
			data: { errorType: "authentication_error" },
			error: undefined,
			cacheable: false,
		};
	}
	if (response.status !== 200)
		return failure(`unexpected status from usage API: ${response.status}`);
	await runtime.cache.write("usage", response.body);
	return parseAndCache(runtime, response.body);
}

export async function fetchLastGood(
	runtime: UsageRuntime,
): Promise<QuotaData | undefined> {
	const body = await runtime.cache.read(
		"usage-last-good",
		undefined,
		runtime.now(),
	);
	if (body === undefined) return undefined;
	const result = await parseAndCache(runtime, body);
	if (result.error !== undefined || result.data.errorType) return undefined;
	return result.data;
}
