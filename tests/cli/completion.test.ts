import { expect, test } from "bun:test";
import { completionScript } from "../../cmd/ailine/completion";

for (const shell of ["bash", "zsh", "fish", "powershell"]) {
	test(`${shell} completion covers commands and visible flags without a backend`, () => {
		const script = completionScript(shell);
		for (const name of [
			"validate",
			"help",
			"completion",
			"bash",
			"zsh",
			"fish",
			"powershell",
			"config",
			"cost",
			"theme",
			"hyperlinks",
			"per-model-quota",
			"mac-insecure",
			"no-prompt-cache",
			"no-compactions",
			"version",
			"no-descriptions",
		])
			expect(script).toContain(name);
		expect(script).not.toContain("no-offpeak");
		expect(script).not.toContain("__complete");
		expect(script).not.toContain("ailine --");
		expect(script.endsWith("\n")).toBe(true);
	});
	test(`${shell} supports no-descriptions output`, () => {
		expect(completionScript(shell, true)).not.toContain(
			"disable model segment",
		);
	});
}

test("fish and zsh descriptions reuse CLI flag descriptions", () => {
	for (const shell of ["fish", "zsh"])
		expect(completionScript(shell)).toContain("disable model segment");
});

test("bash completion scopes version and no-descriptions to their commands", () => {
	const script = completionScript("bash");
	const root = script.match(/root\) candidates='([^']*)'/)?.[1] ?? "";
	const validate = script.match(/validate\) candidates='([^']*)'/)?.[1] ?? "";
	const shell = script.match(/shell\) candidates='([^']*)'/)?.[1] ?? "";
	expect(root).toContain("--version");
	expect(validate).toContain("--config");
	expect(validate).not.toContain("--version");
	expect(root).not.toContain("--no-descriptions");
	expect(shell).toContain("--no-descriptions");
});

test("unsupported shell fails without reflecting arbitrary payload", () => {
	expect(() => completionScript("secret\u001b")).toThrow(
		"unsupported completion shell",
	);
});

function shellPath(name: string): string | null {
	return Bun.which(name, {
		PATH: "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin",
	});
}

const bashes = [...new Set(["/bin/bash", shellPath("bash")])].filter(
	(path): path is string => path !== null,
);

function bashComplete(bash: string, words: readonly string[]) {
	const script = `${completionScript("bash")}
COMP_WORDS=(${words.map((word) => `'${word}'`).join(" ")})
COMP_CWORD=${words.length - 1}
_ailine
printf '%s\\n' "\${COMPREPLY[@]}"
`;
	const result = Bun.spawnSync([bash, "--norc", "--noprofile", "-c", script], {
		env: { PATH: "/usr/bin:/bin" },
		stdout: "pipe",
		stderr: "pipe",
	});
	return {
		exitCode: result.exitCode,
		stdout: result.stdout.toString().split("\n").filter(Boolean),
		stderr: result.stderr.toString(),
	};
}

for (const bash of bashes) {
	test(`bash completion runs in ${bash}`, () => {
		expect(bashComplete(bash, ["ailine", "comp"])).toEqual({
			exitCode: 0,
			stdout: ["completion"],
			stderr: "",
		});
		expect(bashComplete(bash, ["ailine", "--theme", ""])).toEqual({
			exitCode: 0,
			stdout: ["emoji", "text"],
			stderr: "",
		});
		const shells = bashComplete(bash, ["ailine", "completion", "f"]);
		expect(shells).toEqual({ exitCode: 0, stdout: ["fish"], stderr: "" });
		const fixture = `${import.meta.dir}/completion.test.ts`;
		expect(
			bashComplete(bash, ["ailine", "--config", fixture.slice(0, -8)]),
		).toEqual({ exitCode: 0, stdout: [fixture], stderr: "" });
		expect(bashComplete(bash, ["ailine", "--theme", "text", "val"])).toEqual({
			exitCode: 0,
			stdout: ["validate"],
			stderr: "",
		});
	});
}

const syntaxChecks: [string, string, string[]][] = [
	["zsh", "zsh", ["-n"]],
	["fish", "fish", ["--no-execute"]],
];

for (const [shell, binary, flags] of syntaxChecks) {
	const path = shellPath(binary);
	test.skipIf(path === null)(`${shell} completion parses in ${binary}`, () => {
		const result = Bun.spawnSync([path ?? binary, ...flags], {
			stdin: new TextEncoder().encode(completionScript(shell)),
			stdout: "pipe",
			stderr: "pipe",
		});
		expect({
			exitCode: result.exitCode,
			stderr: result.stderr.toString(),
		}).toEqual({ exitCode: 0, stderr: "" });
	});
}

const pwsh = shellPath("pwsh");
test.skipIf(pwsh === null)("powershell completion parses in pwsh", () => {
	const command =
		"$errors = $null; [System.Management.Automation.Language.Parser]::ParseInput([Console]::In.ReadToEnd(), [ref]$null, [ref]$errors) | Out-Null; if ($errors.Count) { $errors | ForEach-Object { $_.Message }; exit 1 }";
	const result = Bun.spawnSync(
		[pwsh ?? "pwsh", "-NoProfile", "-NonInteractive", "-Command", command],
		{
			stdin: new TextEncoder().encode(completionScript("powershell")),
			stdout: "pipe",
			stderr: "pipe",
		},
	);
	expect(result.exitCode, result.stdout.toString()).toBe(0);
});
