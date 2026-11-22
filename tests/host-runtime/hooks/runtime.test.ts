import type { FsStat, On, SessionInfo } from "claude-code";
import type { Engine } from "claude-code/testing";
import { expect, mock, test } from "claude-code/testing";
import { isRecord } from "../core/json";

function files(on: On) {
	const state = {
		stat: { kind: "file", size: 4, mtimeMs: 123, isLink: false } as FsStat,
		fail: false,
		chunks: ["77u/YQ=="],
		reads: [] as {
			path: string;
			offset: number | undefined;
			length: number | undefined;
			as: string;
		}[],
	};
	on("fs.stat", () => {
		if (state.fail) throw new Error("unreadable");
		return { value: state.stat };
	});
	on("fs.read", (_, e) => {
		state.reads.push({
			path: e.path,
			offset: e.offset,
			length: e.length,
			as: e.as,
		});
		if (state.fail) throw new Error("unreadable");
		return { value: { base64: state.chunks.shift() ?? "" } };
	});
	return state;
}

function storage(on: On) {
	const state = {
		value: undefined as unknown,
		fail: false,
		keys: [] as string[],
	};
	mock.clock(on, { now: 1000 });
	on("store.get", (_, e) => {
		state.keys.push(e.key);
		if (state.fail) throw new Error("private storage error");
		return { value: state.value };
	});
	on("store.set", (_, e) => {
		state.keys.push(e.key);
		if (state.fail) throw new Error("private storage error");
		state.value = e.value;
		return { value: undefined };
	});
	return state;
}

function network(on: On) {
	const clock = mock.clock(on);
	const state = {
		calls: 0,
		delay: 0,
		fail: false,
		headers: {} as Record<string, string>,
	};
	on("http.fetch", async (_, e) => {
		state.calls++;
		state.headers = { ...e.init?.headers };
		if (state.delay) await clock.sleep(state.delay);
		if (state.fail) throw new Error("late network failure");
		return {
			value: {
				status: 200,
				ok: true,
				headers: { "Retry-After": "5" },
				text: "body",
			},
		};
	});
	return { clock, state };
}

function credentials(on: On) {
	const state = {
		info: { platform: "macos", transcriptPath: "" } as SessionInfo,
		user: "fixture-user" as string | undefined,
		calls: [] as { argv: readonly string[]; timeoutMs: number | undefined }[],
		output: {
			exitCode: 0,
			stdout: '{"claudeAiOauth":{"accessToken":"fixture-token"}}',
			stderr: "",
			isStdoutTruncated: false,
			isStderrTruncated: false,
		},
	};
	on("session.info", () => ({ value: state.info }));
	on("env.get", () => ({ value: state.user }));
	on("process.run", (_, e) => {
		state.calls.push({ argv: e.argv, timeoutMs: e.init?.timeoutMs });
		return { value: state.output };
	});
	return state;
}

async function command(
	$: Engine,
	args: Record<string, unknown>,
): Promise<Record<string, unknown>> {
	const result = await $.command.run({
		command: "runtime-test",
		args: JSON.stringify(args),
		origin: { kind: "composer" },
		presentation: { isFullscreen: false, columns: 80 },
	});
	const value: unknown = JSON.parse(result.text ?? "");
	if (!isRecord(value)) throw new Error("invalid test response");
	return value;
}

test("host stat maps native directories and retains file metadata and symlink targets", async ($, on) => {
	const state = files(on);
	expect(await command($, { op: "stat" })).toEqual({
		value: { kind: "file", size: 4, mtimeMs: 123 },
	});
	state.stat = { kind: "dir", size: 0, mtimeMs: 456, isLink: true };
	expect(await command($, { op: "stat" })).toEqual({
		value: { kind: "directory", size: 0, mtimeMs: 456 },
	});
	state.stat.kind = "other";
	expect(await command($, { op: "stat" })).toEqual({
		value: { kind: "other", size: 0, mtimeMs: 456 },
	});
	state.fail = true;
	expect(await command($, { op: "stat" })).toEqual({});
});

test("host read preserves BOM bytes and distinguishes empty EOF from unreadable", async ($, on) => {
	const state = files(on);
	expect(await command($, { op: "read", offset: 2, length: 4 })).toEqual({
		value: [239, 187, 191, 97],
	});
	expect(state.reads[0]).toEqual({
		path: "/fixture",
		as: "bytes",
		offset: 2,
		length: 4,
	});
	expect(await command($, { op: "read", offset: 100, length: 4 })).toEqual({
		value: [],
	});
	expect(await command($, { op: "read", offset: 0, length: 0 })).toEqual({
		value: [],
	});
	state.fail = true;
	expect(await command($, { op: "read" })).toEqual({});
});

test("host read chunks unbounded and large ranged reads at the native four MiB limit", async ($, on) => {
	const state = files(on);
	const limit = 4 * 1024 * 1024;
	const full = `${"YWFh".repeat(1398101)}YQ==`;
	state.chunks = [full, "Yg=="];
	expect(await command($, { op: "read", summary: true })).toEqual({
		value: { length: limit + 1, last: 98 },
	});
	expect(state.reads.map((read) => [read.offset, read.length])).toEqual([
		[0, limit],
		[limit, limit],
	]);
	state.reads = [];
	state.chunks = [full, "Yg=="];
	expect(
		await command($, {
			op: "read",
			offset: 10,
			length: limit + 1,
			summary: true,
		}),
	).toEqual({ value: { length: limit + 1, last: 98 } });
	expect(state.reads.map((read) => [read.offset, read.length])).toEqual([
		[10, limit],
		[limit + 10, 1],
	]);
});

test("host cache writes versioned JSON and expires exactly at the TTL boundary", async ($, on) => {
	const state = storage(on);
	expect(await command($, { op: "cache-write", value: "cached" })).toEqual({});
	expect(state.value).toEqual({
		version: 1,
		writtenAtMs: 1000,
		value: "cached",
	});
	expect(await command($, { op: "cache-read", ttl: 100, now: 1099 })).toEqual({
		value: "cached",
	});
	expect(await command($, { op: "cache-read", ttl: 100, now: 1100 })).toEqual(
		{},
	);
	expect(await command($, { op: "cache-read", now: 999999 })).toEqual({
		value: "cached",
	});
	expect(state.keys.every((key) => key === "cache.status.v1")).toBe(true);
});

test("host cache rejects malformed records and invalid times without throwing", async ($, on) => {
	const state = storage(on);
	for (const value of [
		null,
		[],
		"text",
		{},
		{ version: 2, writtenAtMs: 1, value: "x" },
		{ version: 1, writtenAtMs: NaN, value: "x" },
		{ version: 1, writtenAtMs: Infinity, value: "x" },
		{ version: 1, writtenAtMs: "1", value: "x" },
		{ version: 1, writtenAtMs: 1, value: 3 },
	]) {
		state.value = value;
		expect(await command($, { op: "cache-read", now: 1000 })).toEqual({});
	}
	state.value = { version: 1, writtenAtMs: 1000, value: "x" };
	expect(await command($, { op: "cache-read" })).toEqual({});
	expect(await command($, { op: "cache-read", ttl: 0, now: 1000 })).toEqual({});
	expect(await command($, { op: "cache-read", ttl: 100, now: 999 })).toEqual(
		{},
	);
});

test("host cache treats storage failures as misses and best effort writes", async ($, on) => {
	const state = storage(on);
	state.fail = true;
	expect(await command($, { op: "cache-read", ttl: 100, now: 1000 })).toEqual(
		{},
	);
	expect(await command($, { op: "cache-write", value: "cached" })).toEqual({});
});

test("host HTTP maps fast responses and releases its pool slot before the timeout", async ($, on) => {
	const { clock, state } = network(on);
	expect(await command($, { op: "http" })).toEqual({
		value: {
			status: 200,
			headers: { "retry-after": "5" },
			body: "body",
		},
	});
	expect(state.headers).toEqual({ Accept: "application/json" });
	expect(await command($, { op: "pool" })).toEqual({ value: 0 });
	await clock.advance(1000);
	expect(await command($, { op: "pool" })).toEqual({ value: 0 });
	expect(state.calls).toBe(1);
});

test("host HTTP bounds the wait and holds one pending request per URL until late completion", async ($, on) => {
	const { clock, state } = network(on);
	state.delay = 15000;
	const result = command($, { op: "http" });
	await clock.settle();
	await clock.advance(100);
	expect(await result).toEqual({ error: "HTTP request timed out" });
	expect(clock.now()).toBe(100);
	expect(await command($, { op: "pool" })).toEqual({ value: 1 });
	expect(await command($, { op: "http" })).toEqual({
		error: "HTTP request pending",
	});
	expect(state.calls).toBe(1);
	await clock.advance(14900);
	expect(await command($, { op: "pool" })).toEqual({ value: 0 });
	state.delay = 0;
	expect((await command($, { op: "http" })).error).toBeUndefined();
	expect(state.calls).toBe(2);
});

test("host HTTP consumes late rejection and frees the slot without caching a late outcome", async ($, on) => {
	const { clock, state } = network(on);
	state.delay = 1000;
	state.fail = true;
	const result = command($, { op: "http", timeout: 10 });
	await clock.settle();
	await clock.advance(10);
	expect(await result).toEqual({ error: "HTTP request timed out" });
	await clock.advance(990);
	expect(await command($, { op: "pool" })).toEqual({ value: 0 });
	state.delay = 0;
	state.fail = false;
	expect((await command($, { op: "http" })).error).toBeUndefined();
	expect(state.calls).toBe(2);
});

test("host HTTP permits different URLs and rejects immediate network errors", async ($, on) => {
	const { clock, state } = network(on);
	state.delay = 100;
	const first = command($, {
		op: "http",
		url: "https://example.com/one",
		timeout: 200,
	});
	const second = command($, {
		op: "http",
		url: "https://example.com/two",
		timeout: 200,
	});
	await clock.settle();
	expect(state.calls).toBe(2);
	await clock.advance(100);
	expect((await first).error).toBeUndefined();
	expect((await second).error).toBeUndefined();
	state.delay = 0;
	state.fail = true;
	expect(typeof (await command($, { op: "http" })).error).toBe("string");
	expect(await command($, { op: "pool" })).toEqual({ value: 0 });
});

test("host keychain is called only explicitly on macOS with a nonempty username", async ($, on) => {
	const state = credentials(on);
	expect(state.calls.length).toBe(0);
	state.info.platform = "linux";
	expect(await command($, { op: "token" })).toEqual({});
	state.info.platform = "macos";
	state.user = undefined;
	expect(await command($, { op: "token" })).toEqual({});
	state.user = "";
	expect(await command($, { op: "token" })).toEqual({});
	expect(state.calls.length).toBe(0);
	state.user = "fixture-user";
	expect(await command($, { op: "token" })).toEqual({ value: "fixture-token" });
	expect(state.calls).toEqual([
		{
			argv: [
				"/usr/bin/security",
				"find-generic-password",
				"-s",
				"Claude Code-credentials",
				"-a",
				"fixture-user",
				"-w",
			],
			timeoutMs: 5000,
		},
	]);
});

test("host keychain rejects truncated output and failed commands without exposing output", async ($, on) => {
	const state = credentials(on);
	state.output.isStdoutTruncated = true;
	expect(await command($, { op: "token" })).toEqual({
		error: "reading keychain",
	});
	state.output.isStdoutTruncated = false;
	state.output.exitCode = 1;
	expect(await command($, { op: "token" })).toEqual({
		error: "reading keychain",
	});
});

test("host token hash is the lowercase SHA-256 hex digest", async ($) => {
	expect(await command($, { op: "hash", value: "abc" })).toEqual({
		value: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
	});
});

test("host HTTP cancels its native timeout timer when the response wins", async ($, on) => {
	let cancellations = 0;
	on("clock.after", { ms: 100 }, async (_, e, next) => {
		next.signal.addEventListener("abort", () => {
			cancellations++;
		});
		return next(e);
	});
	const { clock } = network(on);
	expect((await command($, { op: "http" })).error).toBeUndefined();
	await clock.settle();
	expect(cancellations).toBe(1);
	await clock.advance(100);
	expect(cancellations).toBe(1);
});
