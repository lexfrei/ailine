import { isRecord, parseJson } from "./json";
import type { FileReader } from "./runtime";
import { decodeUtf8 } from "./text";

const maxLine = 1024 * 1024;
const chunkSize = 1024 * 1024;
const marker = new TextEncoder().encode("compact_boundary");

export type CompactionCache = {
	path: string;
	offset: number;
	count: number;
	pending: Uint8Array;
	stopped: boolean;
};

export function emptyCompactionCache(): CompactionCache {
	return {
		path: "",
		offset: 0,
		count: 0,
		pending: new Uint8Array(),
		stopped: false,
	};
}

function containsMarker(bytes: Uint8Array): boolean {
	let index = bytes.indexOf(marker[0] ?? 0);
	while (index !== -1 && index + marker.length <= bytes.length) {
		let matched = true;
		for (let offset = 1; offset < marker.length; offset++) {
			if (bytes[index + offset] !== marker[offset]) {
				matched = false;
				break;
			}
		}
		if (matched) return true;
		index = bytes.indexOf(marker[0] ?? 0, index + 1);
	}
	return false;
}

function isCompaction(bytes: Uint8Array): boolean {
	if (!containsMarker(bytes)) return false;
	try {
		const record = parseJson(decodeUtf8(bytes));
		return isRecord(record) && record.subtype === "compact_boundary";
	} catch {
		return false;
	}
}

function concat(first: Uint8Array, second: Uint8Array): Uint8Array {
	if (first.length === 0) return second;
	const joined = new Uint8Array(first.length + second.length);
	joined.set(first);
	joined.set(second, first.length);
	return joined;
}

// The cache assumes an append-only transcript: it restarts only when the
// path changes or the file shrinks, not when earlier bytes are rewritten.
export async function countCompactions(
	fs: FileReader,
	path: string,
	cache: CompactionCache = emptyCompactionCache(),
): Promise<number> {
	if (path === "") return 0;
	if (cache.path !== path)
		Object.assign(cache, emptyCompactionCache(), { path });
	else if (cache.offset > 0) {
		const info = await fs.stat(path);
		if (info === undefined || info.size < cache.offset)
			Object.assign(cache, emptyCompactionCache(), { path });
	}
	for (;;) {
		if (cache.stopped) return cache.count;
		const chunk = await fs.read(path, cache.offset, chunkSize);
		if (chunk === undefined) return cache.count;
		if (chunk.length === 0)
			return cache.count + (isCompaction(cache.pending) ? 1 : 0);
		cache.offset += chunk.length;
		const data = concat(cache.pending, chunk);
		let start = 0;
		for (;;) {
			const end = data.indexOf(10, start);
			if (end === -1) break;
			// Scanner needs room for the delimiter even at EOF.
			if (end - start >= maxLine) {
				cache.stopped = true;
				return cache.count;
			}
			if (isCompaction(data.subarray(start, end))) cache.count++;
			start = end + 1;
		}
		cache.pending = data.slice(start);
		if (cache.pending.length >= maxLine) {
			cache.stopped = true;
			return cache.count;
		}
	}
}
