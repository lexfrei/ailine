import type { Register } from "claude-code";
import { isRecord } from "../core/json";
import type { CacheKey } from "../core/runtime";
import {
	getHostToken,
	hostCacheRead,
	hostCacheWrite,
	hostHashToken,
	hostHttpGet,
	hostRead,
	hostStat,
	type HttpPool,
} from "./runtime";

export const register: Register = (on) => {
	const pool: HttpPool = new Map();
	on("command.run", { command: "runtime-test" }, async ($, e) => {
		try {
			const args: unknown = JSON.parse(e.args);
			if (!isRecord(args)) throw new Error("invalid test input");
			let value: unknown;
			const path = typeof args.path === "string" ? args.path : "/fixture";
			const key: CacheKey = args.key === "usage" ? "usage" : "status";
			switch (args.op) {
				case "stat":
					value = await hostStat($, path);
					break;
				case "read": {
					const bytes = await hostRead(
						$,
						path,
						typeof args.offset === "number" ? args.offset : undefined,
						typeof args.length === "number" ? args.length : undefined,
					);
					value =
						bytes === undefined
							? undefined
							: args.summary === true
								? { length: bytes.length, last: bytes.at(-1) }
								: Array.from(bytes);
					break;
				}
				case "cache-read":
					value = await hostCacheRead(
						$,
						key,
						typeof args.ttl === "number" ? args.ttl : undefined,
						typeof args.now === "number" ? args.now : NaN,
					);
					break;
				case "cache-write":
					await hostCacheWrite(
						$,
						key,
						typeof args.value === "string" ? args.value : "",
					);
					break;
				case "http":
					value = await hostHttpGet(
						$,
						pool,
						typeof args.url === "string" ? args.url : "https://example.com",
						{ Accept: "application/json" },
						typeof args.timeout === "number" ? args.timeout : 100,
					);
					break;
				case "pool":
					value = pool.size;
					break;
				case "token":
					value = await getHostToken($);
					break;
				case "hash":
					value = await hostHashToken(
						typeof args.value === "string" ? args.value : "",
					);
					break;
				default:
					throw new Error("invalid test operation");
			}
			return { text: JSON.stringify({ value }) };
		} catch (error) {
			return {
				text: JSON.stringify({
					error: error instanceof Error ? error.message : "host error",
				}),
			};
		}
	});
};
