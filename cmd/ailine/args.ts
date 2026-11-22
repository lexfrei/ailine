import { parseArgs, type ParseArgsOptionsConfig } from "node:util";
import {
	type Config,
	normalizeMode,
	normalizeTheme,
} from "../../plugin/core/config";

export type CliArgs = {
	command: "render" | "validate" | "help" | "version" | "completion";
	target: string | undefined;
	values: Readonly<Record<string, string | boolean | undefined>>;
	present: ReadonlySet<string>;
	noDescriptions: boolean;
};

type FlagSpec = {
	name: string;
	type: "string" | "boolean";
	description: string;
	scope?: "root" | "shell";
	hidden?: boolean;
	short?: string;
};

const disabledSegments = [
	["model", "model", "disable model segment"],
	["effort", "effort", "disable effort indicator on model segment"],
	["thinking", "thinking", "disable thinking indicator on model segment"],
	["fast-mode", "fastMode", "disable fast-mode indicator on model segment"],
	[
		"repo",
		"repo",
		"disable combined repo/PR segment (falls back to bare worktree)",
	],
	["worktree", "worktree", "disable worktree segment"],
	["status", "status", "disable status segment"],
	["context", "context", "disable context segment"],
	["prompt-cache", "promptCache", "disable prompt cache segment"],
	["compactions", "compactions", "disable compactions segment"],
	["quota", "quota", "disable quota segment"],
	["credits", "credits", "disable credits segment (only with --mac-insecure)"],
] as const;

export const completionShells = ["bash", "zsh", "fish", "powershell"] as const;
export const flagSpecs: readonly FlagSpec[] = [
	{ name: "config", type: "string", description: "config file path" },
	...disabledSegments.map(([name, , description]) => ({
		name: `no-${name}`,
		type: "boolean" as const,
		description,
	})),
	{
		name: "cost",
		type: "string",
		description: "cost segment mode: auto (default), true, false",
	},
	{
		name: "mac-insecure",
		type: "boolean",
		description:
			"use macOS Keychain + Anthropic API for per-model quotas and credits",
	},
	{
		name: "per-model-quota",
		type: "string",
		description:
			"per-model quotas, requires --mac-insecure: --per-model-quota=auto (selected model), =true (all), =false (none)",
	},
	{
		name: "theme",
		type: "string",
		description: "icon theme: emoji (default) or text",
	},
	{
		name: "hyperlinks",
		type: "string",
		description: "OSC 8 hyperlinks: auto (default), true, false",
	},
	{
		name: "no-offpeak",
		type: "boolean",
		description: "deprecated no-op",
		hidden: true,
	},
	{ name: "help", type: "boolean", short: "h", description: "help for ailine" },
	{
		name: "version",
		type: "boolean",
		short: "v",
		scope: "root",
		description: "version for ailine",
	},
	{
		name: "no-descriptions",
		type: "boolean",
		scope: "shell",
		description: "disable completion descriptions",
	},
];

export function visibleFlags(
	scope: "root" | "command" | "shell",
): readonly FlagSpec[] {
	return flagSpecs.filter(
		(flag) => !flag.hidden && (!flag.scope || flag.scope === scope),
	);
}

function goBoolean(value: string): boolean {
	if (["true", "True", "TRUE", "t", "T", "1"].includes(value)) return true;
	if (["false", "False", "FALSE", "f", "F", "0"].includes(value)) return false;
	throw new Error("invalid boolean flag value");
}

function normalizeArguments(argv: readonly string[]): string[] {
	const result: string[] = [];
	for (let index = 0; index < argv.length; index++) {
		const arg = argv[index];
		if (arg === undefined) break;
		if (arg === "--") {
			result.push(...argv.slice(index));
			break;
		}
		const equal = arg.indexOf("=");
		const name = equal < 0 ? arg : arg.slice(0, equal);
		const spec = flagSpecs.find(
			(flag) =>
				name === `--${flag.name}` ||
				(flag.short !== undefined && name === `-${flag.short}`),
		);
		if (spec?.type === "boolean") {
			const value = equal < 0 ? "true" : arg.slice(equal + 1);
			goBoolean(value);
			result.push(`--${spec.name}=${value}`);
			continue;
		}
		if (name === "--per-model-quota" && equal < 0) {
			result.push("--per-model-quota=true");
			continue;
		}
		result.push(arg);
		if (spec?.type === "string" && equal < 0) {
			const value = argv[++index];
			if (value === undefined || value.startsWith("-"))
				throw new Error(
					"missing string flag value; use = for values starting with a dash",
				);
			result.push(value);
		}
	}
	return result;
}

function commandTarget(positionals: readonly string[]): {
	command: CliArgs["command"];
	target: string | undefined;
} {
	if (positionals.length === 0) return { command: "render", target: undefined };
	const [first, second] = positionals;
	if (first === "validate" && positionals.length === 1)
		return { command: "validate", target: undefined };
	if (first === "completion") {
		if (positionals.length === 1)
			return { command: "help", target: "completion" };
		if (
			positionals.length === 2 &&
			completionShells.some((shell) => shell === second)
		)
			return { command: "completion", target: second };
	}
	if (first === "help") {
		const rest = positionals.slice(1);
		if (rest.length === 0) return { command: "help", target: undefined };
		if (rest[0] === "help" && rest.length === 1)
			return { command: "help", target: "help" };
		const result = commandTarget(rest);
		return {
			command: "help",
			target:
				result.command === "completion"
					? `completion ${result.target}`
					: (result.target ?? result.command),
		};
	}
	throw new Error("unknown command or unexpected positional arguments");
}

export function parseCliArgs(argv: readonly string[]): CliArgs {
	const options: ParseArgsOptionsConfig = {};
	// Boolean values are parsed as strings so explicit false survives util.parseArgs.
	for (const flag of flagSpecs) options[flag.name] = { type: "string" };
	let parsed: ReturnType<typeof parseArgs>;
	try {
		parsed = parseArgs({
			args: normalizeArguments(argv),
			options,
			allowPositionals: true,
			strict: true,
			tokens: true,
		});
	} catch {
		throw new Error("invalid CLI flag or missing value");
	}
	let terminated = false;
	for (const token of parsed.tokens ?? []) {
		if (token.kind === "option-terminator") terminated = true;
		if (terminated && token.kind === "positional")
			throw new Error("unexpected positional arguments after --");
	}
	const values: Record<string, string | boolean | undefined> = {};
	const present = new Set<string>();
	for (const flag of flagSpecs) {
		const raw = parsed.values[flag.name];
		if (typeof raw !== "string") continue;
		present.add(flag.name);
		values[flag.name] = flag.type === "boolean" ? goBoolean(raw) : raw;
	}
	const route = commandTarget(parsed.positionals);
	if (present.has("version") && parsed.positionals.length > 0)
		throw new Error("version flag is only available on the root command");
	if (
		present.has("no-descriptions") &&
		route.command !== "completion" &&
		!route.target?.startsWith("completion ")
	)
		throw new Error("no-descriptions flag requires a completion shell");
	let { command, target } = route;
	if (values.help === true) {
		if (command === "validate") target = "validate";
		if (command === "completion") target = `completion ${target}`;
		command = "help";
	} else if (values.version === true) command = "version";
	return {
		command,
		target,
		values,
		present,
		noDescriptions: values["no-descriptions"] === true,
	};
}

export function applyFlags(
	source: Config,
	args: CliArgs,
): { config: Config; warnings: string[] } {
	const config = {
		...source,
		segments: { ...source.segments },
		cache: { ...source.cache },
	};
	const warnings: string[] = [];
	for (const [name, field] of disabledSegments) {
		if (args.present.has(`no-${name}`)) config.segments[field] = false;
	}
	if (args.present.has("mac-insecure"))
		config.macInsecure = args.values["mac-insecure"] === true;
	for (const name of [
		"cost",
		"per-model-quota",
		"hyperlinks",
		"theme",
	] as const) {
		const raw = args.values[name];
		if (!args.present.has(name) || typeof raw !== "string" || raw === "")
			continue;
		if (name === "theme") {
			const theme = normalizeTheme(raw);
			if (theme === undefined)
				warnings.push("ailine: invalid theme, using emoji");
			config.theme = theme ?? "emoji";
			continue;
		}
		const mode = normalizeMode(raw);
		if (mode === undefined)
			warnings.push(
				`ailine: invalid ${name === "per-model-quota" ? "per-model quota" : name} mode, using auto`,
			);
		if (name === "cost") config.segments.cost = mode ?? "auto";
		if (name === "per-model-quota")
			config.segments.perModelQuota = mode ?? "auto";
		if (name === "hyperlinks") config.hyperlinks = mode ?? "auto";
	}
	return { config, warnings };
}

export function helpText(target?: string, defaultConfigPath?: string): string {
	const parts = target?.split(" ") ?? [];
	if (target !== undefined) {
		if (target === "help")
			return "Help about any command\n\nUsage:\n  ailine help [command]\n";
		commandTarget(parts);
	}
	const shell = parts[0] === "completion" && parts.length === 2;
	const scope = target === undefined ? "root" : shell ? "shell" : "command";
	const title =
		target === "validate"
			? "Validate config file"
			: parts[0] === "completion"
				? "Generate shell completion scripts"
				: "Real-time statusline for Claude Code";
	const usage =
		target === undefined
			? "ailine [flags]\n  ailine [command]"
			: `ailine ${target}${target === "completion" ? " [bash|zsh|fish|powershell]" : ""} [flags]`;
	const lines = [title, "", "Usage:", `  ${usage}`, ""];
	if (target === undefined)
		lines.push(
			"Commands:",
			"  validate    Validate config file",
			"  help        Help about any command",
			"  completion  Generate shell completion scripts",
			"",
		);
	lines.push("Flags:");
	for (const flag of visibleFlags(scope)) {
		const value =
			flag.name === "per-model-quota"
				? "[=mode]"
				: flag.type === "string"
					? " string"
					: "";
		const suffix =
			flag.name === "config" && defaultConfigPath
				? ` (default ${JSON.stringify(defaultConfigPath)})`
				: "";
		lines.push(`  --${flag.name}${value}  ${flag.description}${suffix}`);
	}
	return `${lines.join("\n")}\n`;
}
