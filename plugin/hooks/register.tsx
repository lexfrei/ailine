import type {
	EngineInterface,
	Register,
	SessionUsage,
	Timer,
} from "claude-code";
import { type CompactionCache, emptyCompactionCache } from "../core/compaction";
import { loadConfigText } from "../core/config";
import { defaultConfigPath } from "../core/config-file";
import { emptyInput, type StatusInput } from "../core/input";
import { renderStatusline, type RenderRuntime } from "../core/render";
import { decodeUtf8 } from "../core/text";
import { stripFormatting } from "../core/wrap";
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

type State = {
	configPath: string;
	active: boolean;
	busy: boolean;
	generation: number;
	text: string | undefined;
	pool: HttpPool;
	compactions: CompactionCache;
};

function sessionInput(
	model: string,
	cwd: string,
	transcriptPath: string,
	usage: SessionUsage,
): StatusInput {
	const input = emptyInput();
	input.model = { id: model, displayName: model };
	input.cwd = cwd;
	input.transcriptPath = transcriptPath;
	if (
		usage.context.percent !== undefined &&
		Number.isFinite(usage.context.percent)
	)
		input.contextPct = usage.context.percent;
	if (usage.cost !== undefined && Number.isFinite(usage.cost.usd))
		input.costUsd = usage.cost.usd;
	for (const window of usage.rateLimits) {
		if (!Number.isFinite(window.percentUsed)) continue;
		const reset = Date.parse(window.resetsAt ?? "");
		const value = {
			usedPercentage: window.percentUsed,
			resetsAtMs: Number.isFinite(reset) ? reset : undefined,
		};
		if (window.kind === "five_hour") input.rateLimits.fiveHour = value;
		else if (window.kind === "seven_day") input.rateLimits.sevenDay = value;
	}
	return input;
}

async function refresh($: EngineInterface, state: State): Promise<void> {
	if (!state.active || state.busy) return;
	state.busy = true;
	const generation = state.generation;
	try {
		const [model, cwd, info, usage, nowMs] = await Promise.all([
			$.session.model(),
			$.session.cwd(),
			$.session.info(),
			$.session.usage(),
			$.clock.now(),
		]);
		const runtime: RenderRuntime = {
			files: {
				stat: (path) => hostStat($, path),
				read: (path, offset, length) => hostRead($, path, offset, length),
			},
			cache: {
				read: (key, ttlMs, now) => hostCacheRead($, key, ttlMs, now),
				write: (key, value) => hostCacheWrite($, key, value),
			},
			httpGet: (url, headers, timeoutMs) =>
				hostHttpGet($, state.pool, url, headers, timeoutMs),
			now: () => nowMs,
			getToken: () => getHostToken($),
			hashToken: hostHashToken,
			compactionCache: state.compactions,
		};
		const path =
			state.configPath ||
			(await defaultConfigPath(runtime.files, (await $.env.get("HOME")) ?? ""));
		const bytes = path === "" ? undefined : await runtime.files.read(path);
		const { config } = loadConfigText(
			bytes === undefined ? undefined : decodeUtf8(bytes),
		);
		if (usage.cost === undefined || !Number.isFinite(usage.cost.usd))
			config.segments.cost = "false";
		const text = await renderStatusline(
			sessionInput(model, cwd, info.transcriptPath, usage),
			config,
			runtime,
			{ maxWidth: 0, hyperlinks: false },
		);
		if (state.active && generation === state.generation)
			state.text = stripFormatting(text).trim() || undefined;
	} catch {
		if (state.active && generation === state.generation)
			state.text = "ailine: unavailable (check config and permissions)";
	} finally {
		state.busy = false;
		if (state.active && generation === state.generation)
			$.ui.invalidate("ui.render");
		else if (state.active) void refresh($, state);
	}
}

export const register: Register = (on, options) => {
	const state: State = {
		configPath: typeof options.config === "string" ? options.config : "",
		active: false,
		busy: false,
		generation: 0,
		text: undefined,
		pool: new Map(),
		compactions: emptyCompactionCache(),
	};
	let timer: Timer | undefined;

	on("session.start", async ($, e, next) => {
		const result = await next(e);
		timer?.cancel();
		state.generation++;
		state.text = undefined;
		state.active = e.isInteractive;
		if (state.active) {
			void refresh($, state);
			timer = $.clock.every(5000, () => {
				void refresh($, state);
			});
		}
		return result;
	});

	on("ui.render", { component: "AbovePrompt" }, ($, e, next) => {
		if (!state.text || e.props.hasSurvey) return next(e);
		const { Text } = $.ui.resolve(e);
		return <Text>{state.text}</Text>;
	});

	on("session.end", ($, e, next) => {
		state.generation++;
		state.text = undefined;
		if (e.reason !== "clear" && e.reason !== "resume") {
			state.active = false;
			timer?.cancel();
		}
		$.ui.invalidate("ui.render");
		return next(e);
	});
};
