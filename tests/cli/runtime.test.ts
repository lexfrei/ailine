import { afterEach, beforeEach, describe, expect, spyOn, test } from "bun:test";
import * as crypto from "node:crypto";
import * as fs from "node:fs/promises";
import {
	chmod,
	lstat,
	mkdir,
	mkdtemp,
	readFile,
	readdir,
	rm,
	stat,
	symlink,
	utimes,
	writeFile,
} from "node:fs/promises";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
	cliFiles,
	httpGet,
	legacyCachePaths,
	readCache,
	writeCache,
} from "../../cmd/ailine/runtime";
import type { CacheKey } from "../../plugin/core/runtime";

let scratch: string;
let paths: Readonly<Record<CacheKey, string>>;

beforeEach(async () => {
	scratch = await mkdtemp(join(tmpdir(), "ailine-runtime-test-"));
	paths = {
		status: join(scratch, "status"),
		usage: join(scratch, "usage"),
		"usage-last-good": join(scratch, "last-good"),
		"usage-retry-after": join(scratch, "retry-after"),
		"usage-auth-failed": join(scratch, "auth-failed"),
	};
});

afterEach(async () => {
	await rm(scratch, { recursive: true, force: true });
});

describe("CLI files", () => {
	test("stats files and directories, following symlinks", async () => {
		await writeFile(paths.status, "hello");
		const link = join(scratch, "link");
		await symlink(paths.status, link);
		const info = await stat(paths.status);
		expect(await cliFiles.stat(link)).toEqual({
			kind: "file",
			size: 5,
			mtimeMs: info.mtimeMs,
		});
		expect((await cliFiles.stat(scratch))?.kind).toBe("directory");
		expect(await cliFiles.stat(join(scratch, "missing"))).toBeUndefined();
	});

	test("stats other file types", async () => {
		const socket = join(scratch, "socket");
		const server = createServer();
		try {
			await new Promise<void>((resolve, reject) => {
				server.once("error", reject);
				server.listen(socket, resolve);
			});
			expect((await cliFiles.stat(socket))?.kind).toBe("other");
		} finally {
			await new Promise<void>((resolve) => server.close(() => resolve()));
		}
	});

	test("reads full bytes, byte ranges, remaining bytes, and EOF", async () => {
		const bytes = new Uint8Array([0, 255, 65, 66, 67]);
		await writeFile(paths.status, bytes);
		expect(await cliFiles.read(paths.status)).toEqual(bytes);
		expect(await cliFiles.read(paths.status, 1, 2)).toEqual(bytes.slice(1, 3));
		expect(await cliFiles.read(paths.status, 2)).toEqual(bytes.slice(2));
		expect(
			await cliFiles.read(paths.status, 4, Number.MAX_SAFE_INTEGER),
		).toEqual(bytes.slice(4));
		expect(await cliFiles.read(paths.status, 5, 10)).toEqual(new Uint8Array());
		expect(await cliFiles.read(paths.status, 50, 10)).toEqual(new Uint8Array());
		expect(await cliFiles.read(paths.status, 0, 0)).toEqual(new Uint8Array());
	});

	test("treats missing, unreadable and invalid ranges as best-effort failures", async () => {
		await writeFile(paths.status, "value");
		expect(await cliFiles.read(paths.usage)).toBeUndefined();
		expect(await cliFiles.read(scratch)).toBeUndefined();
		for (const offset of [-1, 0.5, Number.NaN, Number.POSITIVE_INFINITY]) {
			expect(await cliFiles.read(paths.status, offset, 1)).toBeUndefined();
		}
		for (const length of [-1, 0.5, Number.NaN, Number.POSITIVE_INFINITY]) {
			expect(await cliFiles.read(paths.status, 0, length)).toBeUndefined();
		}
		await chmod(paths.status, 0);
		expect(await cliFiles.read(paths.status)).toBeUndefined();
	});
});

test("closes file handles after full-read, range-stat and cache-read failures", async () => {
	await writeFile(paths.status, "value");
	for (const operation of ["full", "range", "cache"] as const) {
		const file = await fs.open(paths.status, "r");
		const close = spyOn(file, "close");
		const failure =
			operation === "range"
				? spyOn(file, "stat").mockRejectedValue(new Error("stat failed"))
				: spyOn(file, "readFile").mockRejectedValue(new Error("read failed"));
		const opening = spyOn(fs, "open").mockResolvedValue(file);
		try {
			const result =
				operation === "cache"
					? await readCache(paths, "status", undefined, 0)
					: await cliFiles.read(
							paths.status,
							0,
							operation === "range" ? 1 : undefined,
						);
			expect(result).toBeUndefined();
			expect(close).toHaveBeenCalledTimes(1);
		} finally {
			opening.mockRestore();
			failure.mockRestore();
			close.mockRestore();
			await file.close();
		}
	}
});

describe("CLI cache", () => {
	test("retains the exact legacy paths without accessing them", () => {
		expect(legacyCachePaths).toEqual({
			status: "/tmp/claude-status-cache.json",
			usage: "/tmp/claude-usage-cache.json",
			"usage-last-good": "/tmp/claude-usage-last-good.json",
			"usage-retry-after": "/tmp/claude-usage-retry-after",
			"usage-auth-failed": "/tmp/claude-usage-auth-failed",
		});
	});

	test("expires at age >= TTL and reads any age for undefined TTL", async () => {
		await writeFile(paths.status, "cached");
		await utimes(paths.status, new Date(100_000), new Date(100_000));
		const modified = (await stat(paths.status)).mtimeMs;
		expect(await readCache(paths, "status", 1000, modified + 999)).toBe(
			"cached",
		);
		expect(
			await readCache(paths, "status", 1000, modified + 1000),
		).toBeUndefined();
		expect(
			await readCache(paths, "status", 1000, modified + 1001),
		).toBeUndefined();
		expect(
			await readCache(paths, "status", undefined, modified + 1_000_000),
		).toBe("cached");
		expect(await readCache(paths, "status", 0, modified)).toBeUndefined();
		expect(await readCache(paths, "status", 1000, modified - 1)).toBe("cached");
	});

	test("preserves empty strings and treats read errors as misses", async () => {
		await writeCache(paths, "status", "");
		expect(await readCache(paths, "status", undefined, 0)).toBe("");
		expect(await readCache(paths, "usage", undefined, 0)).toBeUndefined();
		await mkdir(paths.usage);
		expect(await readCache(paths, "usage", undefined, 0)).toBeUndefined();
		await chmod(paths.status, 0);
		expect(await readCache(paths, "status", undefined, 0)).toBeUndefined();
	});

	test("writes private files and replaces unsafe existing permissions", async () => {
		await writeCache(paths, "status", "first");
		expect((await stat(paths.status)).mode & 0o777).toBe(0o600);
		await chmod(paths.status, 0o666);
		await writeCache(paths, "status", "second");
		expect(await readFile(paths.status, "utf8")).toBe("second");
		expect((await stat(paths.status)).mode & 0o777).toBe(0o600);
		expect(await readdir(scratch)).toEqual(["status"]);
	});

	test("does not follow fixed temp or destination symlinks", async () => {
		const victim = join(scratch, "victim");
		await writeFile(victim, "untouched");
		await symlink(victim, `${paths.status}.tmp`);
		await symlink(victim, paths.status);
		await writeCache(paths, "status", "replacement");
		expect(await readFile(victim, "utf8")).toBe("untouched");
		expect((await lstat(`${paths.status}.tmp`)).isSymbolicLink()).toBe(true);
		expect((await lstat(paths.status)).isSymbolicLink()).toBe(false);
		expect(await readFile(paths.status, "utf8")).toBe("replacement");
	});

	test("failed exclusive create neither follows nor removes a preexisting temp symlink", async () => {
		const id = "00000000-0000-4000-8000-000000000000";
		const victim = join(scratch, "victim");
		const temporary = `${paths.status}.${id}.tmp`;
		await writeFile(victim, "untouched");
		await symlink(victim, temporary);
		const random = spyOn(crypto, "randomUUID").mockReturnValue(id);
		try {
			await writeCache(paths, "status", "must not follow");
			expect(await readFile(victim, "utf8")).toBe("untouched");
			expect((await lstat(temporary)).isSymbolicLink()).toBe(true);
			expect(await cliFiles.stat(paths.status)).toBeUndefined();
		} finally {
			random.mockRestore();
		}
	});

	test("write errors close the file and clean up the created temp file", async () => {
		const realOpen = fs.open;
		let close: ReturnType<typeof spyOn> | undefined;
		const opening = spyOn(fs, "open").mockImplementation(async (...args) => {
			const file = await realOpen(...args);
			close = spyOn(file, "close");
			spyOn(file, "writeFile").mockRejectedValue(new Error("write failed"));
			return file;
		});
		try {
			await writeCache(paths, "status", "ignored");
			expect(close).toHaveBeenCalledTimes(1);
			expect(await readdir(scratch)).toEqual([]);
		} finally {
			opening.mockRestore();
		}
	});

	test("concurrent writes are complete and leave no temp files", async () => {
		const values = Array.from({ length: 20 }, (_, index) =>
			`${index}:`.repeat(4096),
		);
		await Promise.all(values.map((value) => writeCache(paths, "usage", value)));
		expect(values).toContain(await readFile(paths.usage, "utf8"));
		expect((await stat(paths.usage)).mode & 0o777).toBe(0o600);
		expect(await readdir(scratch)).toEqual(["usage"]);
	});

	test("write failures are best effort and clean up only their own temp files", async () => {
		await mkdir(paths.status);
		await writeFile(`${paths.status}.tmp`, "preexisting");
		await writeCache(paths, "status", "cannot replace directory");
		expect(await readFile(`${paths.status}.tmp`, "utf8")).toBe("preexisting");
		expect((await stat(paths.status)).isDirectory()).toBe(true);
		expect((await readdir(scratch)).sort()).toEqual(["status", "status.tmp"]);
		const missing = { ...paths, usage: join(scratch, "missing", "usage") };
		await writeCache(missing, "usage", "ignored");
		expect(await cliFiles.stat(join(scratch, "missing"))).toBeUndefined();
	});
});

describe("CLI HTTP", () => {
	test("returns body, lowercase headers, and every status without classification", async () => {
		let calls = 0;
		const server = Bun.serve({
			hostname: "127.0.0.1",
			port: 0,
			fetch(request) {
				calls++;
				expect(request.method).toBe("GET");
				expect(request.headers.get("authorization")).toBe("Bearer dummy");
				return new Response("response text", {
					status: Number(new URL(request.url).pathname.slice(1)),
					headers: { "Retry-After": "10", "X-Test": "value" },
				});
			},
		});
		try {
			for (const status of [200, 401, 429]) {
				const response = await httpGet(
					new URL(`/${status}`, server.url).href,
					{ Authorization: "Bearer dummy" },
					1000,
				);
				expect(response.status).toBe(status);
				expect(response.body).toBe("response text");
				expect(response.headers["retry-after"]).toBe("10");
				expect(response.headers["x-test"]).toBe("value");
				expect(
					Object.keys(response.headers).every(
						(name) => name === name.toLowerCase(),
					),
				).toBe(true);
			}
			expect(calls).toBe(3);
		} finally {
			await server.stop(true);
		}
	});

	test("unrefs and clears its deadline after a fast response", async () => {
		const server = Bun.serve({
			hostname: "127.0.0.1",
			port: 0,
			fetch: () => new Response("fast"),
		});
		const schedule = spyOn(globalThis, "setTimeout");
		const clear = spyOn(globalThis, "clearTimeout");
		try {
			expect((await httpGet(server.url.href, {}, 60_000)).body).toBe("fast");
			const timer = schedule.mock.results[0]?.value as ReturnType<
				typeof setTimeout
			>;
			expect(timer.hasRef()).toBe(false);
			expect(clear).toHaveBeenCalledWith(timer);
		} finally {
			schedule.mockRestore();
			clear.mockRestore();
			await server.stop(true);
		}
	});

	test("times out while waiting for headers", async () => {
		const server = Bun.serve({
			hostname: "127.0.0.1",
			port: 0,
			fetch: () => new Promise<Response>(() => {}),
		});
		try {
			await expect(httpGet(server.url.href, {}, 30)).rejects.toThrow();
		} finally {
			await server.stop(true);
		}
	});

	test("timeout covers response body consumption", async () => {
		let cancelled = false;
		const server = Bun.serve({
			hostname: "127.0.0.1",
			port: 0,
			fetch: () =>
				new Response(
					new ReadableStream({
						start(controller) {
							controller.enqueue(new TextEncoder().encode("partial"));
						},
						cancel() {
							cancelled = true;
						},
					}),
				),
		});
		try {
			await expect(httpGet(server.url.href, {}, 50)).rejects.toThrow();
		} finally {
			await server.stop(true);
		}
		expect(cancelled).toBe(true);
	});

	test("does not forward authorization on cross-origin redirects", async () => {
		let received: string | null | undefined;
		const target = Bun.serve({
			hostname: "127.0.0.1",
			port: 0,
			fetch(request) {
				received = request.headers.get("authorization");
				return new Response("redirected");
			},
		});
		const source = Bun.serve({
			hostname: "127.0.0.1",
			port: 0,
			fetch: () => Response.redirect(target.url.href, 302),
		});
		try {
			expect(
				(
					await httpGet(
						source.url.href,
						{ Authorization: "Bearer dummy" },
						1000,
					)
				).body,
			).toBe("redirected");
			expect(received).toBeNull();
		} finally {
			await source.stop(true);
			await target.stop(true);
		}
	});

	test("rejects connection failures", async () => {
		const server = Bun.serve({
			hostname: "127.0.0.1",
			port: 0,
			fetch: () => new Response("unused"),
		});
		const url = server.url.href;
		await server.stop(true);
		await expect(httpGet(url, {}, 100)).rejects.toThrow();
	});

	test("rejects an untrusted certificate before sending the request", async () => {
		const directory = await fs.mkdtemp(join(tmpdir(), "ailine-tls-test-"));
		try {
			const key = join(directory, "key.pem");
			const cert = join(directory, "cert.pem");
			const generated = Bun.spawnSync(
				[
					"openssl",
					"req",
					"-x509",
					"-newkey",
					"rsa:2048",
					"-nodes",
					"-keyout",
					key,
					"-out",
					cert,
					"-days",
					"1",
					"-subj",
					"/CN=127.0.0.1",
					"-addext",
					"subjectAltName=IP:127.0.0.1",
				],
				{ stdout: "ignore", stderr: "pipe" },
			);
			expect(generated.exitCode, generated.stderr.toString()).toBe(0);
			let requests = 0;
			const server = Bun.serve({
				hostname: "127.0.0.1",
				port: 0,
				tls: { key: Bun.file(key), cert: Bun.file(cert) },
				fetch: () => {
					requests++;
					return new Response("must not be read");
				},
			});
			try {
				await expect(
					httpGet(
						server.url.href,
						{ Authorization: "Bearer test-token" },
						2000,
					),
				).rejects.toThrow();
				expect(requests).toBe(0);
			} finally {
				await server.stop(true);
			}
		} finally {
			await fs.rm(directory, { recursive: true, force: true });
		}
	});
});
