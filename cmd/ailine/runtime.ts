import { randomUUID } from "node:crypto";
import { open, rename, stat, unlink } from "node:fs/promises";
import type { CacheKey, FileReader, HttpGet } from "../../plugin/core/runtime";

export const cliFiles: FileReader = {
	async stat(path) {
		try {
			const info = await stat(path);
			return {
				kind: info.isFile()
					? "file"
					: info.isDirectory()
						? "directory"
						: "other",
				size: info.size,
				mtimeMs: info.mtimeMs,
			};
		} catch {
			return undefined;
		}
	},
	async read(path, offset = 0, length) {
		if (
			!Number.isSafeInteger(offset) ||
			offset < 0 ||
			(length !== undefined && (!Number.isSafeInteger(length) || length < 0))
		) {
			return undefined;
		}
		try {
			const file = await open(path, "r");
			try {
				if (offset === 0 && length === undefined) return await file.readFile();
				const info = await file.stat();
				if (!info.isFile()) return undefined;
				const remaining = Math.max(0, info.size - offset);
				const bytes = new Uint8Array(Math.min(length ?? remaining, remaining));
				let total = 0;
				while (total < bytes.length) {
					const { bytesRead } = await file.read(
						bytes,
						total,
						bytes.length - total,
						offset + total,
					);
					if (bytesRead === 0) break;
					total += bytesRead;
				}
				return bytes.subarray(0, total);
			} finally {
				await file.close();
			}
		} catch {
			return undefined;
		}
	},
};

export const legacyCachePaths: Readonly<Record<CacheKey, string>> = {
	status: "/tmp/claude-status-cache.json",
	usage: "/tmp/claude-usage-cache.json",
	"usage-last-good": "/tmp/claude-usage-last-good.json",
	"usage-retry-after": "/tmp/claude-usage-retry-after",
	"usage-auth-failed": "/tmp/claude-usage-auth-failed",
};

export async function readCache(
	paths: Readonly<Record<CacheKey, string>>,
	key: CacheKey,
	ttlMs: number | undefined,
	nowMs: number,
): Promise<string | undefined> {
	try {
		const file = await open(paths[key], "r");
		try {
			if (ttlMs !== undefined && nowMs - (await file.stat()).mtimeMs >= ttlMs)
				return undefined;
			return await file.readFile("utf8");
		} finally {
			await file.close();
		}
	} catch {
		return undefined;
	}
}

export async function writeCache(
	paths: Readonly<Record<CacheKey, string>>,
	key: CacheKey,
	value: string,
): Promise<void> {
	const path = paths[key];
	const temporary = `${path}.${randomUUID()}.tmp`;
	let created = false;
	try {
		const file = await open(temporary, "wx", 0o600);
		created = true;
		try {
			await file.writeFile(value, "utf8");
		} finally {
			await file.close();
		}
		await rename(temporary, path);
		created = false;
	} catch {
		// Cache failures must not prevent statusline output.
	} finally {
		if (created) await unlink(temporary).catch(() => {});
	}
}

export const httpGet: HttpGet = async (url, headers, timeoutMs) => {
	const controller = new AbortController();
	const timer = setTimeout(() => controller.abort(), timeoutMs);
	timer.unref();
	try {
		const response = await fetch(url, {
			headers,
			signal: controller.signal,
			tls: { rejectUnauthorized: true },
		});
		return {
			status: response.status,
			headers: Object.fromEntries(response.headers),
			body: await response.text(),
		};
	} finally {
		clearTimeout(timer);
	}
};
