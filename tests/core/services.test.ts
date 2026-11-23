import { expect, test } from "bun:test";
import type {
	CacheKey,
	HttpResponse,
	UsageRuntime,
} from "../../plugin/core/runtime";
import { fetchStatus, renderStatus } from "../../plugin/core/status";
import { fetchLastGood, fetchUsage } from "../../plugin/core/usage";

const nowMs = Date.parse("2026-11-21T00:00:00Z");
const validBody = JSON.stringify({
	five_hour: { utilization: 25, resets_at: "2026-11-21T05:00:00Z" },
});

function fixture() {
	const entries = new Map<CacheKey, { writtenAtMs: number; value: string }>();
	const requests: {
		url: string;
		headers: Readonly<Record<string, string>>;
		timeoutMs: number;
	}[] = [];
	const state: {
		nowMs: number;
		token: string | undefined;
		tokenCalls: number;
		tokenError: Error | undefined;
		response: HttpResponse;
		httpError: Error | undefined;
	} = {
		nowMs,
		token: "token-one",
		tokenCalls: 0,
		tokenError: undefined,
		response: { status: 200, headers: {}, body: validBody },
		httpError: undefined,
	};
	const runtime: UsageRuntime = {
		now: () => state.nowMs,
		cache: {
			async read(key, ttlMs, clock) {
				const entry = entries.get(key);
				if (
					!entry ||
					(ttlMs !== undefined && clock - entry.writtenAtMs >= ttlMs)
				)
					return undefined;
				return entry.value;
			},
			async write(key, value) {
				entries.set(key, { writtenAtMs: state.nowMs, value });
			},
		},
		async getToken() {
			state.tokenCalls++;
			if (state.tokenError) throw state.tokenError;
			return state.token;
		},
		async hashToken(token) {
			return token === "token-one" ? "digest-one" : "digest-two";
		},
		async httpGet(url, headers, timeoutMs) {
			requests.push({ url, headers, timeoutMs });
			if (state.httpError) throw state.httpError;
			return state.response;
		},
	};
	return { runtime, state, entries, requests };
}

for (const [indicator, emoji, plain] of [
	["none", "", ""],
	["unknown", "", ""],
	["", "", ""],
	["minor", "⚠️ degraded", "\x1b[33mdegraded\x1b[0m"],
	["major", "🔶 major outage", "\x1b[38;5;208mmajor outage\x1b[0m"],
	["critical", "🔴 critical outage", "\x1b[31mcritical outage\x1b[0m"],
] as const) {
	test(`renders platform status ${JSON.stringify(indicator)}`, () => {
		expect(renderStatus("emoji", indicator)).toBe(emoji);
		expect(renderStatus("text", indicator)).toBe(plain);
	});
}

test("status fetch caches the indicator rather than its themed output", async () => {
	const { runtime, state, entries, requests } = fixture();
	state.response.body = '{"status":{"indicator":"minor"}}';
	expect(await fetchStatus(runtime, "emoji", 15000)).toBe("⚠️ degraded");
	expect(entries.get("status")?.value).toBe("minor");
	expect(await fetchStatus(runtime, "text", 15000)).toBe(
		"\x1b[33mdegraded\x1b[0m",
	);
	expect(requests).toEqual([
		{
			url: "https://status.claude.com/api/v2/status.json",
			headers: {},
			timeoutMs: 2000,
		},
	]);
	expect(state.tokenCalls).toBe(0);
});

test("a cached empty status suppresses another request until expiry", async () => {
	const { runtime, state, requests } = fixture();
	state.httpError = new Error("network unavailable");
	expect(await fetchStatus(runtime, "emoji", 15000)).toBe("");
	state.nowMs += 14999;
	expect(await fetchStatus(runtime, "emoji", 15000)).toBe("");
	expect(requests).toHaveLength(1);
	state.nowMs++;
	await fetchStatus(runtime, "emoji", 15000);
	expect(requests).toHaveLength(2);
});

for (const response of [
	{ status: 500, headers: {}, body: "bad" },
	{ status: 200, headers: {}, body: "not JSON" },
	{ status: 200, headers: {}, body: '{"status":{"indicator":4}}' },
	{ status: 200, headers: {}, body: '{"status":"minor"}' },
]) {
	test(`status failure caches an empty indicator: ${JSON.stringify(response)}`, async () => {
		const { runtime, state, entries } = fixture();
		state.response = response;
		expect(await fetchStatus(runtime, "emoji", 15000)).toBe("");
		expect(entries.get("status")?.value).toBe("");
	});
}

test("fresh usage cache is read before cooldown or credentials", async () => {
	const { runtime, state, entries, requests } = fixture();
	entries.set("usage", { writtenAtMs: nowMs, value: validBody });
	entries.set("usage-retry-after", {
		writtenAtMs: nowMs,
		value: "2026-11-21T01:00:00Z",
	});
	const result = await fetchUsage(runtime, 600000);
	expect(result.error).toBeUndefined();
	expect(result.data?.fiveHour?.utilization).toBe(25);
	expect(state.tokenCalls).toBe(0);
	expect(requests).toHaveLength(0);
	expect(entries.get("usage-last-good")?.value).toBe(validBody);
});

test("invalid but fresh usage cache does not read credentials or replace last-good", async () => {
	const { runtime, state, entries } = fixture();
	entries.set("usage", { writtenAtMs: nowMs, value: "" });
	entries.set("usage-last-good", { writtenAtMs: 0, value: validBody });
	const result = await fetchUsage(runtime, 600000);
	expect(result.error).toBeInstanceOf(Error);
	expect(state.tokenCalls).toBe(0);
	expect(entries.get("usage-last-good")?.value).toBe(validBody);
});

test("active cooldown prevents credential access and expires at its deadline", async () => {
	const { runtime, state, entries, requests } = fixture();
	entries.set("usage-retry-after", {
		writtenAtMs: 0,
		value: "2026-11-21T00:01:00Z",
	});
	expect((await fetchUsage(runtime, 600000)).data?.errorType).toBe(
		"rate_limit_error",
	);
	expect(state.tokenCalls).toBe(0);
	state.nowMs += 60000;
	expect((await fetchUsage(runtime, 600000)).error).toBeUndefined();
	expect(requests).toHaveLength(1);
});

test("cooldown timestamps do not acquire API timestamp whitespace trimming", async () => {
	const { runtime, entries, requests } = fixture();
	entries.set("usage-retry-after", {
		writtenAtMs: nowMs,
		value: `${String.fromCodePoint(0x85)}2026-11-21T01:00:00Z`,
	});
	await fetchUsage(runtime, 600000);
	expect(requests).toHaveLength(1);
});

test("malformed cooldown is ignored", async () => {
	const { runtime, entries, requests } = fixture();
	entries.set("usage-retry-after", { writtenAtMs: nowMs, value: "invalid" });
	await fetchUsage(runtime, 600000);
	expect(requests).toHaveLength(1);
});

test("successful request uses the OAuth endpoint and caches current and last-good data", async () => {
	const { runtime, entries, requests } = fixture();
	const result = await fetchUsage(runtime, 600000);
	expect(result.error).toBeUndefined();
	expect(result.data?.fiveHour?.remainingMinutes).toBe(300);
	expect(requests).toEqual([
		{
			url: "https://api.anthropic.com/api/oauth/usage",
			timeoutMs: 3000,
			headers: {
				Authorization: "Bearer token-one",
				"anthropic-beta": "oauth-2025-04-20",
			},
		},
	]);
	expect(entries.get("usage")?.value).toBe(validBody);
	expect(entries.get("usage-last-good")?.value).toBe(validBody);
});

for (const [header, seconds] of [
	[undefined, 35],
	["120", 125],
	["-10", 5],
	["0", 5],
	["invalid", 35],
	[" 120", 35],
	["0.5", 35],
	["9223372036854775808", 35],
] as const) {
	test(`429 preserves body-independent cooldown for ${JSON.stringify(header)}`, async () => {
		const { runtime, state, entries } = fixture();
		state.response = {
			status: 429,
			headers: header === undefined ? {} : { "Retry-After": header },
			body: "not JSON",
		};
		const result = await fetchUsage(runtime, 600000);
		expect(result.error).toBeUndefined();
		expect(result.data?.errorType).toBe("rate_limit_error");
		expect(entries.get("usage-retry-after")?.value).toBe(
			new Date(nowMs + seconds * 1000).toISOString().replace(".000Z", "Z"),
		);
		expect(entries.has("usage")).toBe(false);
	});
}

test("401 suppression is keyed by token hash and expires after one hour", async () => {
	const { runtime, state, entries, requests } = fixture();
	state.response = { status: 401, headers: {}, body: "not JSON" };
	expect((await fetchUsage(runtime, 600000)).data?.errorType).toBe(
		"authentication_error",
	);
	expect(entries.get("usage-auth-failed")?.value).toBe("digest-one");
	expect(entries.get("usage-auth-failed")?.value).not.toContain("token-one");
	expect((await fetchUsage(runtime, 600000)).data?.errorType).toBe(
		"authentication_error",
	);
	expect(requests).toHaveLength(1);
	state.token = "token-two";
	await fetchUsage(runtime, 600000);
	expect(requests).toHaveLength(2);
	state.nowMs += 3600000;
	await fetchUsage(runtime, 600000);
	expect(requests).toHaveLength(3);
});

for (const token of [undefined, ""]) {
	test(`missing token ${JSON.stringify(token)} does not call the API`, async () => {
		const { runtime, state, requests } = fixture();
		state.token = token;
		expect((await fetchUsage(runtime, 600000)).error).toBeInstanceOf(Error);
		expect(requests).toHaveLength(0);
	});
}

test("credential and transport errors stay failures rather than login data", async () => {
	const { runtime, state, requests } = fixture();
	state.tokenError = new Error("keychain unavailable");
	expect((await fetchUsage(runtime, 600000)).error).toBeInstanceOf(Error);
	expect(requests).toHaveLength(0);
	state.tokenError = undefined;
	state.httpError = new Error("timeout");
	expect((await fetchUsage(runtime, 600000)).error).toBeInstanceOf(Error);
	expect(requests).toHaveLength(1);
});

test("unexpected HTTP status does not replace last-good", async () => {
	const { runtime, state, entries } = fixture();
	state.response = { status: 503, headers: {}, body: "{}" };
	entries.set("usage-last-good", { writtenAtMs: 0, value: validBody });
	expect((await fetchUsage(runtime, 600000)).error?.message).toContain(
		"unexpected status",
	);
	expect(entries.get("usage-last-good")?.value).toBe(validBody);
});

test("200 API error bodies are cached but never overwrite last-good", async () => {
	const { runtime, state, entries } = fixture();
	state.response.body = '{"error":{"type":"authentication_error"}}';
	entries.set("usage-last-good", { writtenAtMs: 0, value: validBody });
	expect((await fetchUsage(runtime, 600000)).data?.errorType).toBe(
		"authentication_error",
	);
	expect(entries.get("usage")?.value).toBe(state.response.body);
	expect(entries.get("usage-last-good")?.value).toBe(validBody);
});

test("last-good ignores TTL, recalculates remaining time and refreshes its record", async () => {
	const { runtime, state, entries } = fixture();
	entries.set("usage-last-good", { writtenAtMs: 0, value: validBody });
	state.nowMs += 60000;
	expect((await fetchLastGood(runtime))?.fiveHour?.remainingMinutes).toBe(299);
	expect(entries.get("usage-last-good")?.writtenAtMs).toBe(state.nowMs);
});

for (const body of [
	undefined,
	"invalid",
	'{"error":{"type":"rate_limit_error"}}',
]) {
	test(`invalid last-good ${JSON.stringify(body)} returns no data`, async () => {
		const { runtime, entries } = fixture();
		if (body !== undefined)
			entries.set("usage-last-good", { writtenAtMs: 0, value: body });
		expect(await fetchLastGood(runtime)).toBeUndefined();
	});
}
