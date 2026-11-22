import type { On, SessionUsage } from "claude-code";
import type { Engine } from "claude-code/testing";
import { expect, mock, test } from "claude-code/testing";

const band = {
	plugin: "ailine",
	component: "AbovePrompt",
	surface: "terminal",
	props: {
		hasSurvey: false,
		isCollapsed: false,
		isWorking: false,
		maxRows: 5,
		bodyColumns: 120,
		scroll: { offset: 0, bodyRows: 5 },
		view: {},
	},
} as const;

async function row($: Engine) {
	const ui = await $.ui.mount(band);
	const text = (await ui.find({ type: "Text", text: /./ }))?.text;
	await ui.unmount();
	return text;
}

const config =
	'[segments]\nstatus = false\nquota = false\ncredits = false\ncompactions = false\nrepo = false\nworktree = false\ncost = "false"\n';
const start = {
	cwd: "/workspace",
	surface: "terminal",
	isInteractive: true,
} as const;
const end = {
	reason: "prompt_input_exit",
	sessionId: "test",
	resume: { id: "test" },
} as const;

function session(on: On) {
	const clock = mock.clock(on);
	const calls: string[] = [];
	const state = {
		config,
		model: "claude-opus-5-5",
		usage: {
			startedAt: 0,
			context: { window: 1000000, percent: 37 },
			rateLimits: [],
			cost: { usd: 1.25 },
		} as SessionUsage,
		throws: false,
		delay: 0,
	};
	on("session.start", (_, e) => ({ cwd: e.cwd }));
	on("session.end", (_, e) => ({ sessionId: e.sessionId }));
	on("session.cwd", () => ({ value: "/workspace" }));
	on("session.model", () => ({ value: state.model }));
	on("session.info", () => ({
		value: { transcriptPath: "/workspace/session.jsonl", platform: "linux" },
	}));
	on("session.usage", async () => {
		calls.push("usage");
		const value = state.usage;
		if (state.delay) await clock.sleep(state.delay);
		if (state.throws) throw new Error("private diagnostic");
		return { value };
	});
	on("env.get", () => ({ value: "/home/test-user" }));
	on("fs.stat", (_, e) => {
		calls.push(e.path);
		return {
			value: {
				kind: "file",
				size: state.config.length,
				mtimeMs: 0,
				isLink: false,
			},
		};
	});
	on("fs.read", (_, e) => {
		calls.push(e.path);
		return {
			value: {
				as: "bytes",
				base64: btoa(
					Array.from(new TextEncoder().encode(state.config), (byte) =>
						String.fromCharCode(byte),
					).join(""),
				),
			},
		};
	});
	on("process.run", () => {
		throw new Error("native renderer must not launch a subprocess");
	});
	on("http.fetch", () => {
		throw new Error("disabled segments must not request network data");
	});
	return { clock, calls, state };
}

test("renders the shared core directly without an executable", async ($, on) => {
	const { calls } = session(on);
	await $.session.start(start);
	expect(await row($)).toBe("🤖 claude-opus-5-5 | 🧠 37%");
	expect(calls).toContain("/home/test-user/.ailinerc.toml");
});

test("refreshes while idle and keeps refreshing after clear and resume", async ($, on) => {
	const { clock, calls, state } = session(on);
	await $.session.start(start);
	await clock.advance(5000);
	expect(calls.filter((call) => call === "usage").length).toBe(2);
	state.usage.context.percent = 50;
	await $.session.end({ ...end, reason: "clear" });
	await clock.advance(5000);
	expect(await row($)).toBe("🤖 claude-opus-5-5 | 🧠 50%");
	await $.session.end({ ...end, reason: "resume" });
	await clock.advance(5000);
	expect(calls.filter((call) => call === "usage").length).toBe(4);
});

test("noninteractive sessions do not read config or session usage", async ($, on) => {
	const { clock, calls } = session(on);
	await $.session.start({ ...start, surface: null, isInteractive: false });
	await clock.advance(5000);
	expect(calls).toEqual([]);
	expect(await row($)).toBeUndefined();
});

test(
	"accepts an explicit config path without shell parsing",
	{ options: { config: "/config path/ailine.toml" } },
	async ($, on) => {
		const { calls } = session(on);
		await $.session.start(start);
		await row($);
		expect(calls).toContain("/config path/ailine.toml");
		expect(calls).not.toContain("/home/test-user/.ailinerc.toml");
	},
);

test("omits absent cost even when config explicitly enables the segment", async ($, on) => {
	const { state } = session(on);
	state.config = config.replace('cost = "false"', 'cost = "true"');
	state.usage = {
		startedAt: 0,
		context: { window: 1000000 },
		rateLimits: [
			{ kind: "five_hour", percentUsed: 20 },
			{ kind: "seven_day", percentUsed: 30, resetsAt: "invalid" },
		],
	};
	await $.session.start(start);
	expect(await row($)).toBe("🤖 claude-opus-5-5");
});

test("available cost and config theme are rendered without ANSI escapes", async ($, on) => {
	const { state } = session(on);
	state.config = `theme = "text"\n${config.replace('cost = "false"', 'cost = "true"')}`;
	await $.session.start(start);
	expect(await row($)).toBe("claude-opus-5-5 | $1.25 | 37%");
});

test("recovers from a failed session query without exposing diagnostics", async ($, on) => {
	const { clock, state } = session(on);
	state.throws = true;
	await $.session.start(start);
	expect(await row($)).toBe(
		"ailine: unavailable (check config and permissions)",
	);
	state.throws = false;
	await clock.advance(5000);
	expect(await row($)).toBe("🤖 claude-opus-5-5 | 🧠 37%");
});

test("stops its timer and clears its row at exit", async ($, on) => {
	const { clock, calls } = session(on);
	await $.session.start(start);
	await $.session.end(end);
	await clock.advance(10000);
	expect(calls.filter((call) => call === "usage").length).toBe(1);
	expect(await row($)).toBeUndefined();
});

test("does not overlap slow refreshes or restore a row after exit", async ($, on) => {
	const { clock, calls, state } = session(on);
	await $.session.start(start);
	state.delay = 8000;
	await clock.advance(10000);
	expect(calls.filter((call) => call === "usage").length).toBe(2);
	await $.session.end(end);
	await clock.advance(8000);
	expect(calls.filter((call) => call === "usage").length).toBe(2);
	expect(await row($)).toBeUndefined();
});

test("a refresh from a cleared session is replaced by one for the new session", async ($, on) => {
	const { clock, state } = session(on);
	await $.session.start(start);
	state.delay = 8000;
	await clock.advance(5000);
	await $.session.end({ ...end, reason: "clear" });
	state.usage = {
		startedAt: 10000,
		context: { window: 1000000, percent: 70 },
		rateLimits: [],
	};
	state.delay = 0;
	await $.session.start(start);
	expect(await row($)).toBeUndefined();
	await clock.advance(8000);
	expect(await row($)).toBe("🤖 claude-opus-5-5 | 🧠 70%");
});

test("a superseded session start cannot install a second refresh timer", async ($, on) => {
	const { clock, calls, state } = session(on);
	state.delay = 8000;
	const starting = $.session.start(start);
	await clock.advance(1000);
	await $.session.end({ ...end, reason: "clear" });
	state.delay = 0;
	await $.session.start(start);
	await clock.advance(8000);
	await starting;
	expect(calls.filter((call) => call === "usage").length).toBe(2);
	await clock.advance(5000);
	expect(calls.filter((call) => call === "usage").length).toBe(3);
});

test("session start does not wait for a slow first refresh", async ($, on) => {
	const { clock, calls, state } = session(on);
	state.delay = 8000;
	await $.session.start(start);
	expect(await row($)).toBeUndefined();
	await clock.advance(8000);
	expect(await row($)).toBe("🤖 claude-opus-5-5 | 🧠 37%");
	expect(calls.filter((call) => call === "usage").length).toBe(1);
});

test("survey suppression and native Text work on terminal, desktop and mobile", async ($, on) => {
	session(on);
	await $.session.start(start);
	for (const surface of ["terminal", "desktop", "mobile"] as const) {
		const ui = await $.ui.mount({ ...band, surface });
		expect(
			(await ui.find({ type: "Text", text: "🤖 claude-opus-5-5 | 🧠 37%" }))
				?.text,
		).toBe("🤖 claude-opus-5-5 | 🧠 37%");
		await ui.unmount();
	}
	const survey = await $.ui.mount({
		...band,
		props: { ...band.props, hasSurvey: true },
	});
	expect(
		await survey.find({ type: "Text", text: /claude-opus/ }),
	).toBeUndefined();
	await survey.unmount();
});
