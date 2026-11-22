import { describe, expect, test } from "bun:test";
import { applyFlags, helpText, parseCliArgs } from "../../cmd/ailine/args";
import { defaults } from "../../plugin/core/config";

const disabled = [
	["model", "model"],
	["effort", "effort"],
	["thinking", "thinking"],
	["fast-mode", "fastMode"],
	["repo", "repo"],
	["worktree", "worktree"],
	["status", "status"],
	["context", "context"],
	["prompt-cache", "promptCache"],
	["compactions", "compactions"],
	["quota", "quota"],
	["credits", "credits"],
] as const;

describe("CLI arguments", () => {
	test("defaults and explicit presence are separate", () => {
		const args = parseCliArgs([]);
		expect(args.command).toBe("render");
		expect(args.target).toBeUndefined();
		expect(args.present.size).toBe(0);
		expect(args.noDescriptions).toBe(false);
		expect(parseCliArgs(["--config", "private.toml"]).values.config).toBe(
			"private.toml",
		);
	});

	for (const [flag, field] of disabled) {
		test(`--no-${flag}=false still disables ${field} without mutation`, () => {
			const source = defaults();
			const result = applyFlags(source, parseCliArgs([`--no-${flag}=false`]));
			expect(result.config.segments[field]).toBe(false);
			expect(source.segments[field]).toBe(true);
		});
	}

	for (const value of ["false", "False", "FALSE", "f", "F", "0"]) {
		test(`explicit privileged false ${value} never enables credentials`, () => {
			const source = defaults();
			source.macInsecure = true;
			const args = parseCliArgs(["--mac-insecure", `--mac-insecure=${value}`]);
			expect(args.values["mac-insecure"]).toBe(false);
			expect(applyFlags(source, args).config.macInsecure).toBe(false);
		});
	}
	for (const value of ["true", "True", "TRUE", "t", "T", "1"]) {
		test(`Go boolean true ${value}`, () => {
			expect(
				parseCliArgs([`--mac-insecure=${value}`]).values["mac-insecure"],
			).toBe(true);
		});
	}
	test("repeated flags preserve last occurrence", () => {
		expect(
			parseCliArgs(["--mac-insecure=false", "--mac-insecure"]).values[
				"mac-insecure"
			],
		).toBe(true);
		expect(parseCliArgs(["--cost=true", "--cost", "off"]).values.cost).toBe(
			"off",
		);
	});
	test("per-model quota only consumes an attached mode", () => {
		expect(parseCliArgs(["--per-model-quota"]).values["per-model-quota"]).toBe(
			"true",
		);
		expect(
			parseCliArgs(["--per-model-quota=auto"]).values["per-model-quota"],
		).toBe("auto");
		expect(() => parseCliArgs(["--per-model-quota", "auto"])).toThrow();
	});
	test("empty mode flags preserve existing configuration", () => {
		const source = defaults();
		source.theme = "text";
		source.hyperlinks = "false";
		source.segments.cost = "true";
		source.segments.perModelQuota = "false";
		expect(
			applyFlags(
				source,
				parseCliArgs([
					"--theme=",
					"--hyperlinks=",
					"--cost=",
					"--per-model-quota=",
				]),
			),
		).toEqual({ config: source, warnings: [] });
	});
	test("mode normalization and invalid values warn and fall back", () => {
		const valid = applyFlags(
			defaults(),
			parseCliArgs([
				"--cost=ON",
				"--hyperlinks=off",
				"--per-model-quota=T",
				"--theme=text",
			]),
		);
		expect(valid.config.segments.cost).toBe("true");
		expect(valid.config.hyperlinks).toBe("false");
		expect(valid.config.segments.perModelQuota).toBe("true");
		expect(valid.config.theme).toBe("text");
		const invalid = applyFlags(
			valid.config,
			parseCliArgs([
				"--cost=secret\u001b",
				"--hyperlinks=bad",
				"--per-model-quota=bad",
				"--theme=bad",
			]),
		);
		expect(invalid.config.segments.cost).toBe("auto");
		expect(invalid.config.segments.perModelQuota).toBe("auto");
		expect(invalid.config.hyperlinks).toBe("auto");
		expect(invalid.config.theme).toBe("emoji");
		expect(invalid.warnings).toHaveLength(4);
		expect(invalid.warnings.join("\n")).not.toContain("secret");
	});
	test("hidden compatibility flag is a no-op", () => {
		expect(
			applyFlags(defaults(), parseCliArgs(["--no-offpeak=false"])),
		).toEqual({ config: defaults(), warnings: [] });
	});
	test("command and help routing", () => {
		expect(parseCliArgs(["validate", "--config=x", "--no-model"]).command).toBe(
			"validate",
		);
		for (const option of ["--help", "-h"])
			expect(parseCliArgs([option]).command).toBe("help");
		for (const option of ["--version", "-v"])
			expect(parseCliArgs([option]).command).toBe("version");
		expect(parseCliArgs(["--help=false"]).command).toBe("render");
		expect(parseCliArgs(["--version=false"]).command).toBe("render");
		expect(parseCliArgs(["help", "validate"]).target).toBe("validate");
		expect(parseCliArgs(["validate", "--help"]).target).toBe("validate");
		expect(parseCliArgs(["completion"]).command).toBe("help");
		for (const shell of ["bash", "zsh", "fish", "powershell"]) {
			const args = parseCliArgs(["completion", shell, "--no-descriptions"]);
			expect(args.command).toBe("completion");
			expect(args.target).toBe(shell);
			expect(args.noDescriptions).toBe(true);
			expect(parseCliArgs(["help", "completion", shell]).target).toBe(
				`completion ${shell}`,
			);
		}
	});
	for (const argv of [
		["unknown-secret"],
		["--unknown=secret"],
		["validate", "extra-secret"],
		["--mac-insecure=on"],
		["--no-model=off"],
		["--mac-insecure="],
		["--cost"],
		["--config"],
		["--theme", "--no-model"],
		["validate", "--version"],
		["--version", "validate"],
		["--no-descriptions"],
		["completion", "--no-descriptions"],
		["completion", "nu"],
		["completion", "bash", "extra-secret"],
		["--", "validate"],
		["--", "--help"],
		["help", "unknown-secret"],
	]) {
		test(`rejects invalid input ${JSON.stringify(argv)} without exposing values`, () => {
			let message = "";
			try {
				parseCliArgs(argv);
			} catch (error) {
				message = String(error);
			}
			expect(message).not.toBe("");
			expect(message).not.toContain("secret");
		});
	}
	test("terminator permits an empty positional tail", () => {
		expect(parseCliArgs(["--"]).command).toBe("render");
		expect(parseCliArgs(["validate", "--"]).command).toBe("validate");
		expect(parseCliArgs(["--config=--help"]).values.config).toBe("--help");
	});
	test("help is scoped and hides the deprecated no-op", () => {
		const root = helpText(undefined, "/example/config.toml");
		expect(root).toContain("--version");
		expect(root).toContain("/example/config.toml");
		expect(root).toContain("--per-model-quota=auto");
		expect(root).not.toContain("--no-offpeak");
		expect(helpText("validate")).not.toContain("--version");
		expect(helpText("validate")).toContain("--config");
		expect(helpText("completion fish")).toContain("--no-descriptions");
		expect(helpText("completion")).not.toContain("--no-descriptions");
		expect(() => helpText("unknown-secret")).toThrow();
	});
});
