import {
	Boxes,
	ChevronDown,
	ChevronsUpDown,
	CircleMinus,
	Clock,
	Copy,
	Eye,
	FlaskConical,
	Globe,
	Info,
	KeyRound,
	LogOut,
	type LucideIcon,
	Moon,
	Network,
	PanelLeftClose,
	PanelLeftOpen,
	Plus,
	ScrollText,
	Search,
	Server,
	Shield,
	Sun,
	User,
	X,
} from "lucide-react";
import { cn } from "@/lib/utils";

// The console's chrome icons: Lucide (ISC), bundled from the pinned
// lucide-react, for exactly the names the design package lists (its
// Icon spec) plus the two this console adds on the record, `info` for
// the policy editor's notes and `search` for the header's search field.
// Status never travels on these: flow decisions, workload health, and
// severity are StatusGlyph's.
const icons = {
	server: Server,
	network: Network,
	"flask-conical": FlaskConical,
	"scroll-text": ScrollText,
	"panel-left-close": PanelLeftClose,
	"panel-left-open": PanelLeftOpen,
	user: User,
	"chevrons-up-down": ChevronsUpDown,
	sun: Sun,
	moon: Moon,
	"log-out": LogOut,
	eye: Eye,
	shield: Shield,
	clock: Clock,
	"chevron-down": ChevronDown,
	x: X,
	copy: Copy,
	plus: Plus,
	"key-round": KeyRound,
	"circle-minus": CircleMinus,
	boxes: Boxes,
	globe: Globe,
	info: Info,
	search: Search,
} satisfies Record<string, LucideIcon>;

export type IconName = keyof typeof icons;

// Icon is one chrome icon at icon-size (16px), stroke 2 on Lucide's 24
// grid, in currentColor at icon-default unless the caller's class sets
// another icon color (icon-active in the current sidebar entry or a
// pressed segment; the label color inside a primary button). It is
// hidden from assistive technology unless it is a control's only
// content, when `label` names it.
export function Icon({
	name,
	label,
	className,
}: {
	name: IconName;
	label?: string;
	className?: string;
}) {
	const Glyph = icons[name];
	return (
		<Glyph
			data-slot="icon"
			data-icon={name}
			size={16}
			strokeWidth={2}
			className={cn("size-icon shrink-0 text-icon-default", className)}
			aria-hidden={label ? undefined : true}
			aria-label={label}
			role={label ? "img" : undefined}
			focusable="false"
		/>
	);
}
