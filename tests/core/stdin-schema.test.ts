import { expect, test } from "bun:test";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { parseInput } from "../../plugin/core/input";

// Captured from a statusLine command in an isolated Claude Code 2.1.327
// session; paths and the session ID are replaced. Recapture with a command
// that tees stdin to a file, then classify every new key below.
const fixture = join(import.meta.dir, "../fixtures/stdin-2.1.327.json");

const consumed = new Set([
	"cwd",
	"transcript_path",
	"model.id",
	"model.display_name",
	"effort.level",
	"thinking.enabled",
	"fast_mode",
	"workspace.current_dir",
	"cost.total_cost_usd",
	"context_window.used_percentage",
]);

const ignored = new Set([
	"session_id",
	"version",
	"exceeds_200k_tokens",
	"output_style.name",
	"workspace.project_dir",
	"workspace.added_dirs",
	"cost.total_duration_ms",
	"cost.total_api_duration_ms",
	"cost.total_lines_added",
	"cost.total_lines_removed",
	"context_window.total_input_tokens",
	"context_window.total_output_tokens",
	"context_window.context_window_size",
	"context_window.current_usage",
	"context_window.remaining_percentage",
]);

function keyPaths(value: unknown, prefix = ""): string[] {
	if (typeof value !== "object" || value === null || Array.isArray(value))
		return [prefix];
	const paths: string[] = [];
	for (const [key, child] of Object.entries(value)) {
		const path = prefix === "" ? key : `${prefix}.${key}`;
		const nested =
			typeof child === "object" && child !== null && !Array.isArray(child);
		paths.push(...(nested ? keyPaths(child, path) : [path]));
	}
	return paths;
}

test("every field of the captured host payload is consumed or deliberately ignored", async () => {
	const payload: unknown = JSON.parse(await readFile(fixture, "utf8"));
	const unclassified = keyPaths(payload).filter(
		(path) => !consumed.has(path) && !ignored.has(path),
	);
	expect(unclassified).toEqual([]);
});

test("the captured host payload decodes without warnings", async () => {
	const parsed = parseInput(await readFile(fixture, "utf8"));
	expect(parsed.warnings).toEqual([]);
	expect(parsed.input.model).toEqual({
		id: "claude-opus-5-5",
		displayName: "Opus 5.5",
	});
	expect(parsed.input.effortLevel).toBe("medium");
	expect(parsed.input.thinking).toBe(true);
	expect(parsed.input.contextPct).toBe(0);
	expect(parsed.input.workspace.currentDir).toBe("/home/test-user/workspace");
});
