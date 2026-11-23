export type Theme = "emoji" | "text";
export type Mode = "auto" | "true" | "false";

export type QuotaWindow = {
	utilization: number;
	resetsAtMs: number;
	totalMinutes: number;
	remainingMinutes: number;
};

export type ScopedWindow = {
	name: string;
	id: string;
	window: QuotaWindow | undefined;
};

export type ExtraUsage = {
	monthlyLimit: number;
	usedCredits: number;
};

export type ExhaustedWindow = {
	name: string;
	minutes: number;
};

export type QuotaData = {
	fiveHour?: QuotaWindow | undefined;
	sevenDay?: QuotaWindow | undefined;
	sevenDayOpus?: QuotaWindow | undefined;
	sevenDaySonnet?: QuotaWindow | undefined;
	sevenDayCowork?: QuotaWindow | undefined;
	sevenDayOAuthApps?: QuotaWindow | undefined;
	scoped?: ScopedWindow[] | undefined;
	extra?: ExtraUsage | undefined;
	errorType?: string | undefined;
};
