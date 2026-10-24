import {
	type ReactNode,
	useCallback,
	useId,
	useLayoutEffect,
	useRef,
	useState,
} from "react";
import { Link } from "react-router";
import type { EvidenceGaps, Workload } from "@/api/schema";
import {
	type GlyphStatus,
	StatusGlyph,
	tint,
	tone,
} from "@/components/StatusGlyph";
import { Button } from "@/components/ui/button";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { ago, count, offset } from "@/lib/format";
import type { Resource } from "@/lib/resource";
import { cn } from "@/lib/utils";
import {
	enforcementRemoved,
	reconnecting,
	retired,
	standing,
	syncNote,
	version,
} from "../fleet/describe";
import type { ScreenRange } from "./range";

// A workload's exceptional states: what changes how the data on its
// pages must be read (a removed table, a stale last-seen, a credential
// past its expiry, a failed apply, shifted timestamps, missing rows).
// While one is active it is a chip in the tab row on every page of the
// workload and, on the Overview, an alert card; the chip opens the same
// content in a popover. Status is never color alone: each carries its
// glyph and its word.
export interface ExceptionalState {
	key: "removed" | "offline" | "credential" | "degraded" | "skew" | "gap";
	glyph: GlyphStatus;
	critical: boolean;
	word: string;
	body: ReactNode;
	action?: ReactNode;
}

// exceptionalStates are the workload's active states in severity order:
// the critical ones first (a removed table before all), then the
// warnings. A retired workload has no live health to warn of: its
// removed table, offline stream, expired credential, and failed apply
// are what was last recorded and nothing to act on, so only the states
// that change how its history reads (shifted timestamps, missing rows)
// remain.
export function exceptionalStates(
	w: Workload,
	gaps: Resource<EvidenceGaps>,
	range: ScreenRange,
): ExceptionalState[] {
	const history = historyStates(w, gaps, range);
	return retired(w) ? history : liveStates(w).concat(history);
}

function liveStates(w: Workload): ExceptionalState[] {
	const states: ExceptionalState[] = [];
	const s = w.sync;
	if (enforcementRemoved(w)) {
		states.push({
			key: "removed",
			glyph: "error",
			critical: true,
			word: "Enforcement removed",
			body: (
				<span data-testid="enforcement-removed">
					The agent reports its firewall table gone from the host
					{w.health.owned_table.absent_since ? (
						<>
							{" "}
							since{" "}
							<time dateTime={w.health.owned_table.absent_since}>
								{ago(w.health.owned_table.absent_since)}
							</time>
						</>
					) : null}
					, so nothing here is enforced or observed, and the blind interval is
					recorded as an evidence gap. {version(s.applied_version)} stays
					acknowledged and on the host's disk: the agent reinstalls it on its
					next policy update or restart, and a resent snapshot is one.
				</span>
			),
		});
	}
	if (s.state === "offline") {
		states.push({
			key: "offline",
			glyph: "offline",
			critical: true,
			word: reconnecting(w) ? "Reconnecting" : "Offline",
			body: syncNote(w),
			action: <OfflineResend />,
		});
	}
	if (w.health.credential.state === "expired") {
		// The lead says the credential expired; the sentence says which
		// side of the renewal grace it is on, and past it, the way back.
		const cred = standing(w);
		states.push({
			key: "credential",
			glyph: "error",
			critical: true,
			word: "Credential expired",
			body: (
				<span data-testid="credential-expired">
					{cred.text}. {cred.note}
					{w.health.credential.last_error ? (
						<span className="block text-secondary">
							{w.health.credential.last_error}
						</span>
					) : null}
				</span>
			),
			action: cred.reenroll ? (
				<Button variant="secondary" size="sm" asChild>
					<Link to="/workloads/tokens">Mint a provisioning token</Link>
				</Button>
			) : undefined,
		});
	}
	if (s.state === "degraded" && !enforcementRemoved(w)) {
		states.push({
			key: "degraded",
			glyph: "degraded",
			critical: false,
			word: "Degraded",
			body: (
				<>
					{s.applied_version > 0
						? `Host stays on ${version(s.applied_version)} (its last good policy).`
						: "Host keeps the policy it had before; it has applied none from this control plane."}{" "}
					The control plane resends a full snapshot on the next attempt.
				</>
			),
		});
	}
	return states;
}

function historyStates(
	w: Workload,
	gaps: Resource<EvidenceGaps>,
	range: ScreenRange,
): ExceptionalState[] {
	const states: ExceptionalState[] = [];
	const skew = clockSkew(w, range);
	if (skew) {
		states.push({
			key: "skew",
			glyph: "alert",
			critical: false,
			word: "Clock skew",
			body: skew,
		});
	}
	if (gaps.status === "ready" && gaps.data.gaps.length > 0) {
		const more = gaps.data.truncated || gaps.data.gaps.length > gapsShown;
		states.push({
			key: "gap",
			glyph: "alert",
			critical: false,
			word: "Evidence gap",
			body: (
				<>
					<span className="type-mono-sm">
						{count(gaps.data.gaps.length)}
						{more ? "+" : ""}
					</span>{" "}
					in {range.text}. Intervals the agent knows it lost evidence in; the
					flows shown in them are incomplete.
				</>
			),
		});
	}
	return states;
}

// gapsShown is how many evidence gaps the Overview lists; past it the
// count says there are more.
export const gapsShown = 3;

// clockSkew says the agent's clock disagreed with the control plane's:
// its windows are stamped with its own clock, so the times on its pages
// are off by about the offset. It holds while the skew is current, and
// after the clock came back for as long as the last skewed report falls
// inside the screen's range, since the windows stamped by the wrong
// clock are still in it.
function clockSkew(w: Workload, range: ScreenRange): ReactNode | null {
	const skew = w.health.clock_skew;
	if (!skew) return null;
	if (!skew.current && Date.parse(skew.observed_at) < Date.parse(range.from)) {
		return null;
	}
	const by = offset(skew.offset_seconds * 1000);
	const way = skew.offset_seconds < 0 ? "behind" : "ahead of";
	return (
		<span data-testid="clock-skew">
			{skew.current ? (
				<>
					This agent's clock is {by} {way} the control plane's, as of its latest
					flow report {ago(skew.observed_at)}. It stamps its windows with its
					own clock, so the times below are off by about that much. Re-sync the
					host's clock, for example with{" "}
					<code className="type-mono-sm">
						systemctl restart systemd-timesyncd
					</code>
					.
				</>
			) : (
				<>
					This agent's clock was {by} {way} the control plane's in a flow report{" "}
					{ago(skew.observed_at)}; its reports since are within tolerance.
					Windows it reported until then were stamped with the wrong clock, and
					their times below are off by up to that much.
				</>
			)}
		</span>
	);
}

// OfflineResend is the offline state's action. The control plane
// directs a reconnect only to an agent it records as online and refuses
// an offline one, so the action is held disabled with the reason beside
// it rather than offered to fail.
function OfflineResend() {
	const reason = useId();
	return (
		<span className="flex flex-col items-end gap-1">
			<Button
				variant="secondary"
				size="sm"
				aria-disabled="true"
				aria-describedby={reason}
				title={offlineRefusal}
				onClick={(e) => e.preventDefault()}
			>
				Resend snapshot
			</Button>
			<span
				id={reason}
				className="max-w-[220px] text-right type-caption text-tertiary"
			>
				{offlineRefusal}
			</span>
		</span>
	);
}

const offlineRefusal =
	"Refused while the agent is offline; it is sent a fresh snapshot whenever it next connects.";

// Lead is a state's glyph and word in its tone, at 500.
function Lead({ s, className }: { s: ExceptionalState; className?: string }) {
	return (
		<span
			className={cn(
				"inline-flex items-center gap-1.5 font-medium",
				tone[s.glyph],
				className,
			)}
		>
			<StatusGlyph status={s.glyph} size="md" />
			<span>{s.word}</span>
		</span>
	);
}

// StateDetail is one state as a popover shows it: the lead, the
// sentence in text-primary, and the action where there is one.
function StateDetail({ s }: { s: ExceptionalState }) {
	return (
		<div className="flex flex-col gap-1.5 type-header-register font-normal">
			<Lead s={s} />
			<div className="text-primary">{s.body}</div>
			{s.action ? <div className="flex justify-end">{s.action}</div> : null}
		</div>
	);
}

// The chips' gap, and the room an overflow chip ("+N") needs.
const chipGap = 6;
const overflowWidth = 44;

// StateChips are the active states, right-aligned in the tab row in
// severity order: each a 22px tinted chip of glyph and word that opens
// its popover on click or Enter (one open at a time; Escape closes it).
// When they cannot all fit beside the tabs, the rest collapse into one
// "+N" chip in the highest tone it hides, whose popover lists them.
export function StateChips({ states }: { states: ExceptionalState[] }) {
	const [open, setOpen] = useState<string | null>(null);
	// Opening one chip's popover replaces the open one. The one it
	// replaces reports its own dismissal after, so a close clears only
	// the popover still open.
	const toggle = (key: string, o: boolean) =>
		setOpen((cur) => (o ? key : cur === key ? null : cur));
	const box = useRef<HTMLDivElement>(null);
	const measure = useRef<HTMLDivElement>(null);
	const [fit, setFit] = useState(states.length);

	const refit = useCallback(() => {
		const room = box.current?.clientWidth ?? 0;
		const widths = Array.from(measure.current?.children ?? []).map(
			(c) => (c as HTMLElement).offsetWidth,
		);
		// Unmeasured (no layout): every chip is shown.
		if (room === 0 || widths.every((x) => x === 0)) {
			setFit(states.length);
			return;
		}
		const total = (k: number) =>
			widths.slice(0, k).reduce((a, x) => a + x, 0) + chipGap * (k - 1);
		if (total(states.length) <= room) {
			setFit(states.length);
			return;
		}
		let k = states.length - 1;
		while (k > 0 && total(k) + chipGap + overflowWidth > room) k -= 1;
		setFit(k);
	}, [states.length]);

	useLayoutEffect(() => {
		refit();
		const el = box.current;
		if (!el || typeof ResizeObserver === "undefined") return;
		const ro = new ResizeObserver(refit);
		ro.observe(el);
		return () => ro.disconnect();
	}, [refit]);

	if (states.length === 0) return null;
	const shown = states.slice(0, fit);
	const hidden = states.slice(fit);
	const chip = (s: { glyph: GlyphStatus }) =>
		cn(
			"inline-flex h-chip shrink-0 cursor-pointer items-center gap-1.5 whitespace-nowrap rounded-inner border px-2 type-control hover:shadow-[inset_0_0_0_1px_var(--border-control-hover)] aria-expanded:focus-ring",
			tint[s.glyph],
			tone[s.glyph],
		);
	return (
		<div
			ref={box}
			className="relative flex min-w-0 flex-1 items-center justify-end gap-1.5"
			data-testid="state-chips"
		>
			<div
				ref={measure}
				aria-hidden="true"
				className="pointer-events-none invisible absolute top-0 right-0 flex gap-1.5"
			>
				{states.map((s) => (
					<span key={s.key} className={chip(s)}>
						<StatusGlyph status={s.glyph} size="md" />
						{s.word}
					</span>
				))}
			</div>
			{shown.map((s) => (
				<Popover
					key={s.key}
					open={open === s.key}
					onOpenChange={(o) => toggle(s.key, o)}
				>
					<PopoverTrigger asChild>
						<button type="button" className={chip(s)}>
							<StatusGlyph status={s.glyph} size="md" />
							{s.word}
						</button>
					</PopoverTrigger>
					<PopoverContent
						align="end"
						sideOffset={8}
						className="w-popover"
						aria-label={s.word}
					>
						<StateDetail s={s} />
					</PopoverContent>
				</Popover>
			))}
			{hidden.length > 0 ? (
				<Popover open={open === "more"} onOpenChange={(o) => toggle("more", o)}>
					<PopoverTrigger asChild>
						<button
							type="button"
							className={chip(hidden[0] as ExceptionalState)}
							aria-label={`${hidden.length} more: ${hidden.map((s) => s.word).join(", ")}`}
						>
							+{hidden.length}
						</button>
					</PopoverTrigger>
					<PopoverContent
						align="end"
						sideOffset={8}
						className="flex w-popover flex-col divide-y divide-hairline p-0"
						aria-label={`${hidden.length} more`}
					>
						{hidden.map((s) => (
							<div key={s.key} className="p-3">
								<StateDetail s={s} />
							</div>
						))}
					</PopoverContent>
				</Popover>
			) : null}
		</div>
	);
}

// AlertCards are the active states inline at the top of the Overview,
// one card per state in severity order: the tint and the status
// hairline, the glyph and lead word in the tone, the sentence in
// text-primary, and the action on the right.
export function AlertCards({ states }: { states: ExceptionalState[] }) {
	if (states.length === 0) return null;
	return (
		<div className="flex flex-col gap-2">
			{states.map((s) => (
				<section
					key={s.key}
					aria-label={s.word}
					data-testid={`alert-${s.key}`}
					className={cn(
						"flex items-start gap-3 rounded-panel border px-3.5 py-2.5 type-dense",
						tint[s.glyph],
					)}
				>
					<div className="flex min-w-0 flex-1 flex-col gap-0.5">
						<Lead s={s} />
						<div className="text-primary">{s.body}</div>
					</div>
					{s.action ? <div className="shrink-0">{s.action}</div> : null}
				</section>
			))}
		</div>
	);
}
