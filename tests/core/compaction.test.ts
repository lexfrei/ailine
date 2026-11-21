import { expect, test } from "bun:test";
import {
	countCompactions,
	emptyCompactionCache,
} from "../../plugin/core/compaction";
import type { FileReader } from "../../plugin/core/runtime";

const boundary = '{"subtype":"compact_boundary"}';
const maxLine = 1024 * 1024;

function transcript(
	text: string,
	chunkSize = 4 * 1024 * 1024,
	failAt = Infinity,
): FileReader {
	const bytes = new TextEncoder().encode(text);
	return {
		async stat() {
			return { kind: "file", size: bytes.length, mtimeMs: 0 };
		},
		async read(_path, offset = 0, length) {
			expect(length).toBeDefined();
			expect(length).toBeLessThanOrEqual(4 * 1024 * 1024);
			if (offset >= failAt) return undefined;
			return bytes.slice(
				offset,
				offset + Math.min(length ?? bytes.length, chunkSize),
			);
		},
	};
}

test("empty and missing paths return zero without touching an empty path", async () => {
	const fs: FileReader = {
		async stat() {
			return undefined;
		},
		async read(path) {
			expect(path).not.toBe("");
			return undefined;
		},
	};
	expect(await countCompactions(fs, "")).toBe(0);
	expect(await countCompactions(fs, "/missing")).toBe(0);
	expect(await countCompactions(transcript(""), "/log")).toBe(0);
});

test("counts only top-level exact subtype with literal substring prefilter, CRLF and final JSON", async () => {
	const text = [
		boundary,
		'{"message":"compact_boundary"}',
		'{"nested":{"subtype":"compact_boundary"}}',
		'{"subtype":"user"}',
		"compact_boundary invalid JSON",
		'{"subtype":"compact_boundary",broken}',
		"null",
		'[{"subtype":"compact_boundary"}]',
		'{"subtype":"compact_\\u0062oundary"}',
		boundary,
	].join("\r\n");
	expect(await countCompactions(transcript(text), "/log")).toBe(2);
});

test("reads transcripts larger than host limit and handles UTF-8 split at byte boundaries", async () => {
	const message = `${JSON.stringify({ message: "é😀".repeat(100), subtype: "compact_boundary" })}\n`;
	const text = message.repeat(7000);
	expect(new TextEncoder().encode(text).length).toBeGreaterThan(
		4 * 1024 * 1024,
	);
	expect(await countCompactions(transcript(text, 65533), "/log")).toBe(7000);
	expect(
		await countCompactions(
			transcript(`{"message":"é😀","subtype":"compact_boundary"}`, 1),
			"/log",
		),
	).toBe(1);
});

test("exclusive byte line ceiling stops the entire scan and preserves prior count", async () => {
	for (const ending of ["\n", ""]) {
		const valid = boundary + " ".repeat(maxLine - 1 - boundary.length);
		expect(await countCompactions(transcript(valid + ending), "/log")).toBe(1);
		const oversized = boundary + " ".repeat(maxLine - boundary.length);
		expect(
			await countCompactions(
				transcript(`${boundary}\n${oversized}${ending}\n${boundary}\n`),
				"/log",
			),
		).toBe(1);
	}
	const oversizedUtf8 = `{"subtype":"compact_boundary","message":"${"é".repeat(maxLine / 2)}"}`;
	expect(
		await countCompactions(
			transcript(`${boundary}\n${oversizedUtf8}\n${boundary}`),
			"/log",
		),
	).toBe(1);
});

test("read errors preserve completed records but do not count an unfinished record", async () => {
	const first = `${boundary}\n`;
	expect(
		await countCompactions(
			transcript(first + boundary, first.length, first.length),
			"/log",
		),
	).toBe(1);
	expect(
		await countCompactions(
			transcript(boundary, boundary.length, boundary.length),
			"/log",
		),
	).toBe(0);
	expect(
		await countCompactions(
			transcript(`${first}${boundary}`, 1, first.length + 5),
			"/log",
		),
	).toBe(1);
});

function growing(initial: string) {
	let bytes = new TextEncoder().encode(initial);
	const reads: { offset: number; length: number }[] = [];
	const fs: FileReader = {
		async stat() {
			return { kind: "file", size: bytes.length, mtimeMs: 0 };
		},
		async read(_path, offset = 0, length = bytes.length) {
			const chunk = bytes.slice(offset, offset + length);
			reads.push({ offset, length: chunk.length });
			return chunk;
		},
	};
	return {
		fs,
		reads,
		set(text: string) {
			bytes = new TextEncoder().encode(text);
		},
	};
}

test("cached scans read only appended bytes and do not double count a growing last line", async () => {
	const file = growing(`${boundary}\n{"subtype":"user"}\n`);
	const cache = emptyCompactionCache();
	expect(await countCompactions(file.fs, "/log", cache)).toBe(1);
	const bytesRead = () =>
		file.reads.reduce((sum, read) => sum + read.length, 0);
	const first = bytesRead();
	expect(await countCompactions(file.fs, "/log", cache)).toBe(1);
	expect(bytesRead()).toBe(first);
	file.set(`${boundary}\n{"subtype":"user"}\n{"subtype":"compact_bou`);
	expect(await countCompactions(file.fs, "/log", cache)).toBe(1);
	file.set(`${boundary}\n{"subtype":"user"}\n${boundary}`);
	expect(await countCompactions(file.fs, "/log", cache)).toBe(2);
	file.set(`${boundary}\n{"subtype":"user"}\n${boundary}\n${boundary}\n`);
	expect(await countCompactions(file.fs, "/log", cache)).toBe(3);
	expect(file.reads.every((read) => read.offset >= 0)).toBe(true);
	expect(bytesRead()).toBeLessThan(first + 200);
});

test("cached scans restart for another path or a truncated file", async () => {
	const file = growing(`${boundary}\n${boundary}\n`);
	const cache = emptyCompactionCache();
	expect(await countCompactions(file.fs, "/log", cache)).toBe(2);
	file.set(`${boundary}\n`);
	expect(await countCompactions(file.fs, "/log", cache)).toBe(1);
	expect(await countCompactions(file.fs, "/other", cache)).toBe(1);
	expect(await countCompactions(file.fs, "", cache)).toBe(0);
});

test("cached scans keep the overlong-line stop and the partial count after read errors", async () => {
	const file = growing(`${boundary}\n${"x".repeat(maxLine)}\n${boundary}\n`);
	const cache = emptyCompactionCache();
	expect(await countCompactions(file.fs, "/log", cache)).toBe(1);
	file.set(`${boundary}\n${"x".repeat(maxLine)}\n${boundary}\n${boundary}\n`);
	expect(await countCompactions(file.fs, "/log", cache)).toBe(1);
	const failing = growing(`${boundary}\n`);
	const failingCache = emptyCompactionCache();
	expect(await countCompactions(failing.fs, "/log", failingCache)).toBe(1);
	failing.set(`${boundary}\n${boundary}\n`);
	failing.fs.read = async () => undefined;
	expect(await countCompactions(failing.fs, "/log", failingCache)).toBe(1);
});
