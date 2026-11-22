import type { EngineInterface, Timer } from "claude-code";
import { readKeychain } from "../core/credentials";
import { isRecord } from "../core/json";
import type { CacheKey, FileInfo, HttpResponse } from "../core/runtime";

export type HttpPool = Map<string, Promise<HttpResponse>>;

export async function hostStat(
	$: EngineInterface,
	path: string,
): Promise<FileInfo | undefined> {
	try {
		const stat = await $.fs.stat(path);
		return {
			kind: stat.kind === "dir" ? "directory" : stat.kind,
			size: stat.size,
			mtimeMs: stat.mtimeMs,
		};
	} catch {
		return undefined;
	}
}

export async function hostRead(
	$: EngineInterface,
	path: string,
	offset = 0,
	length?: number,
): Promise<Uint8Array | undefined> {
	try {
		const chunks: Uint8Array[] = [];
		let size = 0;
		do {
			const count = Math.min(
				length === undefined ? Infinity : length - size,
				4 * 1024 * 1024,
			);
			const { base64 } = await $.fs.read(path, {
				as: "bytes",
				offset: offset + size,
				length: count,
			});
			const chunk = Uint8Array.from(atob(base64), (char) => char.charCodeAt(0));
			chunks.push(chunk);
			size += chunk.length;
			if (chunk.length < count || chunk.length === 0) break;
		} while (length === undefined || size < length);
		const bytes = new Uint8Array(size);
		let position = 0;
		for (const chunk of chunks) {
			bytes.set(chunk, position);
			position += chunk.length;
		}
		return bytes;
	} catch {
		return undefined;
	}
}

export async function hostCacheRead(
	$: EngineInterface,
	key: CacheKey,
	ttlMs: number | undefined,
	nowMs: number,
): Promise<string | undefined> {
	try {
		const record = await $.store.get(`cache.${key}.v1`);
		if (
			!isRecord(record) ||
			record.version !== 1 ||
			typeof record.value !== "string" ||
			typeof record.writtenAtMs !== "number" ||
			!Number.isFinite(record.writtenAtMs) ||
			!Number.isFinite(nowMs)
		)
			return undefined;
		if (
			ttlMs !== undefined &&
			(!Number.isFinite(ttlMs) ||
				nowMs < record.writtenAtMs ||
				nowMs - record.writtenAtMs >= ttlMs)
		)
			return undefined;
		return record.value;
	} catch {
		return undefined;
	}
}

export async function hostCacheWrite(
	$: EngineInterface,
	key: CacheKey,
	value: string,
): Promise<void> {
	try {
		const writtenAtMs = await $.clock.now();
		if (!Number.isFinite(writtenAtMs)) return;
		await $.store.set(`cache.${key}.v1`, { version: 1, writtenAtMs, value });
	} catch {}
}

export async function hostHttpGet(
	$: EngineInterface,
	pool: HttpPool,
	url: string,
	headers: Readonly<Record<string, string>>,
	timeoutMs: number,
): Promise<HttpResponse> {
	if (pool.has(url)) throw new Error("HTTP request pending");
	const request = $.http
		.fetch(url, { headers: { ...headers } })
		.then((response) => {
			const headers: Record<string, string> = Object.create(null);
			for (const [name, value] of Object.entries(response.headers))
				headers[name.toLowerCase()] = value;
			return { status: response.status, headers, body: response.text };
		});
	pool.set(url, request);
	function release() {
		if (pool.get(url) === request) pool.delete(url);
	}
	void request.then(release, release);
	let timer: Timer | undefined;
	try {
		const timeout = new Promise<never>((_, reject) => {
			timer = $.clock.after(timeoutMs, () =>
				reject(new Error("HTTP request timed out")),
			);
		});
		return await Promise.race([request, timeout]);
	} finally {
		timer?.cancel();
	}
}

async function runHostCommand(
	$: EngineInterface,
	argv: readonly string[],
	timeoutMs: number,
) {
	const result = await $.process.run(argv, { timeoutMs });
	if (result.isStdoutTruncated) throw new Error("reading keychain");
	return { exitCode: result.exitCode, stdout: result.stdout };
}

export async function getHostToken(
	$: EngineInterface,
): Promise<string | undefined> {
	if ((await $.session.info()).platform !== "macos") return undefined;
	const username = await $.env.get("USER");
	if (!username) return undefined;
	return readKeychain(
		(argv, timeoutMs) => runHostCommand($, argv, timeoutMs),
		username,
	);
}

export async function hostHashToken(token: string): Promise<string> {
	const digest = await crypto.subtle.digest(
		"SHA-256",
		new TextEncoder().encode(token),
	);
	let hex = "";
	for (const byte of new Uint8Array(digest))
		hex += byte.toString(16).padStart(2, "0");
	return hex;
}
