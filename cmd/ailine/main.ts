import { createHash } from "node:crypto";
import { loadConfigText, validateConfigText } from "../../plugin/core/config";
import { defaultConfigPath } from "../../plugin/core/config-file";
import { part } from "../../plugin/core/format";
import { parseInput } from "../../plugin/core/input";
import { supportsHyperlinks } from "../../plugin/core/links";
import { renderStatusline, type RenderRuntime } from "../../plugin/core/render";
import { decodeUtf8 } from "../../plugin/core/text";
import { applyFlags, helpText, parseCliArgs, type CliArgs } from "./args";
import { completionScript } from "./completion";
import { getToken } from "./keychain";
import {
	cliFiles,
	httpGet,
	legacyCachePaths,
	readCache,
	writeCache,
} from "./runtime";

declare const AILINE_VERSION: string;
declare const AILINE_COMMIT: string;

export function wrapWidth(raw: string | undefined): number {
	if (raw === undefined || !/^[+-]?\d+$/.test(raw)) return 0;
	const value = BigInt(raw);
	if (value <= 2n || value > 9223372036854775807n) return 0;
	return Number(value - 2n);
}

export async function main(): Promise<void> {
	let args: CliArgs;
	try {
		args = parseCliArgs(process.argv.slice(2));
	} catch (error) {
		process.stderr.write(
			`Error: ${error instanceof Error ? error.message : "invalid arguments"}\n`,
		);
		process.exitCode = 1;
		return;
	}
	if (args.command === "version") {
		const version =
			typeof AILINE_VERSION === "undefined" ? "dev" : AILINE_VERSION;
		const commit =
			typeof AILINE_COMMIT === "undefined" ? "none" : AILINE_COMMIT;
		process.stdout.write(`ailine ${version} (${commit})\n`);
		return;
	}
	if (args.command === "completion" && args.target !== undefined) {
		process.stdout.write(completionScript(args.target, args.noDescriptions));
		return;
	}
	const selectedPath =
		typeof args.values.config === "string"
			? args.values.config
			: await defaultConfigPath(cliFiles, process.env.HOME ?? "");
	if (args.command === "help") {
		process.stdout.write(helpText(args.target, selectedPath));
		return;
	}
	const content =
		selectedPath === "" ? undefined : await cliFiles.read(selectedPath);
	const source = content === undefined ? undefined : decodeUtf8(content);
	const loaded = loadConfigText(source);
	const applied = applyFlags(loaded.config, args);
	for (const warning of [...loaded.warnings, ...applied.warnings])
		process.stderr.write(`${warning}\n`);
	if (args.command === "validate") {
		const problems =
			selectedPath !== "" && content === undefined
				? [`cannot read config: ${selectedPath}`]
				: validateConfigText(source);
		if (problems.length > 0) {
			for (const problem of problems)
				process.stderr.write(`error: ${problem}\n`);
			process.exitCode = 1;
		} else process.stdout.write("config ok\n");
		return;
	}
	let raw: string;
	try {
		raw = decodeUtf8(new Uint8Array(await Bun.stdin.arrayBuffer()));
	} catch {
		process.stdout.write(part(applied.config.theme, "stdin error", "⚠️"));
		return;
	}
	const parsed = parseInput(raw);
	for (const warning of parsed.warnings) process.stderr.write(`${warning}\n`);
	const runtime: RenderRuntime = {
		files: cliFiles,
		cache: {
			read: (key, ttlMs, nowMs) =>
				readCache(legacyCachePaths, key, ttlMs, nowMs),
			write: (key, value) => writeCache(legacyCachePaths, key, value),
		},
		httpGet,
		now: () => Date.now(),
		getToken,
		async hashToken(value) {
			return createHash("sha256").update(value).digest("hex");
		},
	};
	const hyperlinks =
		applied.config.hyperlinks === "true" ||
		(applied.config.hyperlinks === "auto" && supportsHyperlinks(process.env));
	process.stdout.write(
		await renderStatusline(parsed.input, applied.config, runtime, {
			maxWidth: wrapWidth(process.env.COLUMNS),
			hyperlinks,
		}),
	);
}

if (import.meta.main) {
	main().catch(() => {
		process.stderr.write("ailine: unexpected failure\n");
		process.exitCode = 1;
	});
}
