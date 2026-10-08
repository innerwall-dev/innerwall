import { Slot } from "@radix-ui/react-slot";
import * as React from "react";
import { Icon } from "@/components/Icon";
import {
	Sheet,
	SheetContent,
	SheetDescription,
	SheetHeader,
	SheetTitle,
} from "@/components/ui/sheet";
import {
	Tooltip,
	TooltipContent,
	TooltipProvider,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import { useIsMobile } from "@/hooks/use-mobile";
import { cn } from "@/lib/utils";

// From shadcn/ui (shadcn@3.5.0, new-york-v4, MIT): the sidebar's
// structure and behavior (collapse to an icon rail, the collapse state
// kept in its cookie, the sheet below the mobile breakpoint, the ⌘B /
// Ctrl+B shortcut, tooltips on collapsed entries), trimmed to the parts
// the console uses. Every color and dimension is a design token: the
// widths are the package's sidebar-width (inherited from tokens.css, so
// it is never set here) and sidebar-width-collapsed, the rail sits on
// sidebar-bg behind a sidebar-border hairline, and entries are
// sidebar-item tall at radius-md, sidebar-item-hover on hover and
// sidebar-item-active when current.

export const SIDEBAR_COOKIE_NAME = "sidebar_state";
const SIDEBAR_COOKIE_MAX_AGE = 60 * 60 * 24 * 7;
const SIDEBAR_KEYBOARD_SHORTCUT = "b";

type SidebarContextProps = {
	state: "expanded" | "collapsed";
	open: boolean;
	setOpen: (open: boolean) => void;
	openMobile: boolean;
	setOpenMobile: (open: boolean) => void;
	isMobile: boolean;
	toggleSidebar: () => void;
};

const SidebarContext = React.createContext<SidebarContextProps | null>(null);

function useSidebar() {
	const context = React.useContext(SidebarContext);
	if (!context) {
		throw new Error("useSidebar must be used within a SidebarProvider.");
	}

	return context;
}

// sidebarCookieOpen reads the collapse state the provider last wrote,
// or null when there is none.
function sidebarCookieOpen(): boolean | null {
	const m = document.cookie.match(
		new RegExp(`(?:^|; )${SIDEBAR_COOKIE_NAME}=(true|false)`),
	);
	return m ? m[1] === "true" : null;
}

function SidebarProvider({
	defaultOpen = true,
	open: openProp,
	onOpenChange: setOpenProp,
	className,
	style,
	children,
	...props
}: React.ComponentProps<"div"> & {
	defaultOpen?: boolean;
	open?: boolean;
	onOpenChange?: (open: boolean) => void;
}) {
	const isMobile = useIsMobile();
	const [openMobile, setOpenMobile] = React.useState(false);

	// This is the internal state of the sidebar.
	// We use openProp and setOpenProp for control from outside the component.
	const [_open, _setOpen] = React.useState(
		() => sidebarCookieOpen() ?? defaultOpen,
	);
	const open = openProp ?? _open;
	const setOpen = React.useCallback(
		(value: boolean | ((value: boolean) => boolean)) => {
			const openState = typeof value === "function" ? value(open) : value;
			if (setOpenProp) {
				setOpenProp(openState);
			} else {
				_setOpen(openState);
			}

			// This sets the cookie to keep the sidebar state.
			// biome-ignore lint/suspicious/noDocumentCookie: the component's own persistence, read back by sidebarCookieOpen
			document.cookie = `${SIDEBAR_COOKIE_NAME}=${openState}; path=/; max-age=${SIDEBAR_COOKIE_MAX_AGE}; samesite=strict`;
		},
		[setOpenProp, open],
	);

	// Helper to toggle the sidebar.
	const toggleSidebar = React.useCallback(() => {
		return isMobile ? setOpenMobile((open) => !open) : setOpen((open) => !open);
	}, [isMobile, setOpen]);

	// Adds a keyboard shortcut to toggle the sidebar.
	React.useEffect(() => {
		const handleKeyDown = (event: KeyboardEvent) => {
			if (
				event.key === SIDEBAR_KEYBOARD_SHORTCUT &&
				(event.metaKey || event.ctrlKey)
			) {
				event.preventDefault();
				toggleSidebar();
			}
		};

		window.addEventListener("keydown", handleKeyDown);
		return () => window.removeEventListener("keydown", handleKeyDown);
	}, [toggleSidebar]);

	// We add a state so that we can do data-state="expanded" or "collapsed".
	// This makes it easier to style the sidebar with Tailwind classes.
	const state = open ? "expanded" : "collapsed";

	const contextValue = React.useMemo<SidebarContextProps>(
		() => ({
			state,
			open,
			setOpen,
			isMobile,
			openMobile,
			setOpenMobile,
			toggleSidebar,
		}),
		[state, open, setOpen, isMobile, openMobile, toggleSidebar],
	);

	return (
		<SidebarContext.Provider value={contextValue}>
			<TooltipProvider delayDuration={0}>
				<div
					data-slot="sidebar-wrapper"
					style={
						{
							"--sidebar-width-icon": "var(--sidebar-width-collapsed)",
							...style,
						} as React.CSSProperties
					}
					className={cn("group/sidebar-wrapper flex w-full", className)}
					{...props}
				>
					{children}
				</div>
			</TooltipProvider>
		</SidebarContext.Provider>
	);
}

function Sidebar({
	side = "left",
	collapsible = "icon",
	className,
	children,
	...props
}: React.ComponentProps<"div"> & {
	side?: "left" | "right";
	collapsible?: "offcanvas" | "icon" | "none";
}) {
	const { isMobile, state, openMobile, setOpenMobile } = useSidebar();

	if (collapsible === "none") {
		return (
			<div
				data-slot="sidebar"
				className={cn(
					"flex h-full w-(--sidebar-width) flex-col bg-sidebar text-sidebar-foreground",
					className,
				)}
				{...props}
			>
				{children}
			</div>
		);
	}

	if (isMobile) {
		return (
			<Sheet open={openMobile} onOpenChange={setOpenMobile} {...props}>
				<SheetContent
					data-sidebar="sidebar"
					data-slot="sidebar"
					data-mobile="true"
					className="w-(--sidebar-width) gap-0 bg-sidebar p-0 text-sidebar-foreground [&>button]:hidden"
					side={side}
				>
					<SheetHeader className="sr-only">
						<SheetTitle>Sidebar</SheetTitle>
						<SheetDescription>Displays the mobile sidebar.</SheetDescription>
					</SheetHeader>
					<div className="flex h-full w-full flex-col">{children}</div>
				</SheetContent>
			</Sheet>
		);
	}

	return (
		<div
			className="group peer hidden text-sidebar-foreground md:block"
			data-state={state}
			data-collapsible={state === "collapsed" ? collapsible : ""}
			data-side={side}
			data-slot="sidebar"
		>
			{/* This is what handles the sidebar gap on desktop */}
			<div
				data-slot="sidebar-gap"
				className={cn(
					"relative w-(--sidebar-width) bg-transparent transition-[width] duration-200 ease-linear",
					"group-data-[collapsible=offcanvas]:w-0",
					"group-data-[side=right]:rotate-180",
					"group-data-[collapsible=icon]:w-(--sidebar-width-icon)",
				)}
			/>
			<div
				data-slot="sidebar-container"
				className={cn(
					"fixed inset-y-0 z-10 hidden h-svh w-(--sidebar-width) border-sidebar-border transition-[left,right,width] duration-200 ease-linear md:flex",
					side === "left"
						? "left-0 group-data-[collapsible=offcanvas]:left-[calc(var(--sidebar-width)*-1)]"
						: "right-0 group-data-[collapsible=offcanvas]:right-[calc(var(--sidebar-width)*-1)]",
					"group-data-[collapsible=icon]:w-(--sidebar-width-icon) group-data-[side=left]:border-r group-data-[side=right]:border-l",
					className,
				)}
				{...props}
			>
				<div
					data-sidebar="sidebar"
					data-slot="sidebar-inner"
					className="flex h-full w-full flex-col bg-sidebar"
				>
					{children}
				</div>
			</div>
		</div>
	);
}

// SidebarTrigger is the collapse control. In the rail's footer it is an
// entry like the others, Collapse with panel-left-close or the icon
// alone with panel-left-open when collapsed; `compact` is the icon-only
// form the page header shows where the sidebar is a sheet.
function SidebarTrigger({
	className,
	onClick,
	compact = false,
	...props
}: React.ComponentProps<"button"> & { compact?: boolean }) {
	const { toggleSidebar, state, isMobile } = useSidebar();
	const collapsed = state === "collapsed" && !isMobile;
	// Where the sidebar is a sheet, the header's compact control opens it
	// and the footer's closes it.
	const label = isMobile
		? compact
			? "Open sidebar"
			: "Close sidebar"
		: collapsed
			? "Expand sidebar"
			: "Collapse sidebar";
	const icon = collapsed || compact ? "panel-left-open" : "panel-left-close";

	if (compact) {
		if (!isMobile) return null;
		return (
			<button
				type="button"
				data-sidebar="trigger"
				data-slot="sidebar-trigger"
				aria-label={label}
				className={cn(
					"inline-flex size-control-md shrink-0 cursor-pointer items-center justify-center rounded-md hover:bg-hover",
					className,
				)}
				onClick={(event) => {
					onClick?.(event);
					toggleSidebar();
				}}
				{...props}
			>
				<Icon name={icon} />
			</button>
		);
	}

	return (
		<SidebarMenuButton
			data-sidebar="trigger"
			data-slot="sidebar-trigger"
			aria-label={label}
			tooltip={label}
			className={className}
			onClick={(event) => {
				onClick?.(event);
				toggleSidebar();
			}}
			{...props}
		>
			<Icon name={icon} />
			<span>{isMobile ? "Close" : collapsed ? "Expand" : "Collapse"}</span>
		</SidebarMenuButton>
	);
}

function SidebarHeader({ className, ...props }: React.ComponentProps<"div">) {
	return (
		<div
			data-slot="sidebar-header"
			data-sidebar="header"
			className={cn("flex flex-col gap-2 px-2 pt-3", className)}
			{...props}
		/>
	);
}

function SidebarFooter({ className, ...props }: React.ComponentProps<"div">) {
	return (
		<div
			data-slot="sidebar-footer"
			data-sidebar="footer"
			className={cn("flex flex-col gap-0.5 p-2", className)}
			{...props}
		/>
	);
}

function SidebarContent({ className, ...props }: React.ComponentProps<"div">) {
	return (
		<div
			data-slot="sidebar-content"
			data-sidebar="content"
			className={cn(
				"flex min-h-0 flex-1 flex-col gap-2 overflow-auto group-data-[collapsible=icon]:overflow-hidden",
				className,
			)}
			{...props}
		/>
	);
}

function SidebarGroup({ className, ...props }: React.ComponentProps<"div">) {
	return (
		<div
			data-slot="sidebar-group"
			data-sidebar="group"
			className={cn("relative flex w-full min-w-0 flex-col p-2", className)}
			{...props}
		/>
	);
}

function SidebarMenu({ className, ...props }: React.ComponentProps<"ul">) {
	return (
		<ul
			data-slot="sidebar-menu"
			data-sidebar="menu"
			className={cn("flex w-full min-w-0 flex-col gap-0.5", className)}
			{...props}
		/>
	);
}

function SidebarMenuItem({ className, ...props }: React.ComponentProps<"li">) {
	return (
		<li
			data-slot="sidebar-menu-item"
			data-sidebar="menu-item"
			className={cn("group/menu-item relative", className)}
			{...props}
		/>
	);
}

// An entry: icon-gap between the 16px icon and its ui label, the label
// in text-secondary; the current entry on sidebar-item-active in
// ui-strong with its icon at icon-active (never blue: location is not
// selection). Collapsed, the entry is the rail's 40px hit area with the
// icon centered and the label clipped.
const sidebarMenuButtonClass =
	"peer/menu-button flex h-sidebar-item w-full cursor-pointer items-center gap-2 overflow-hidden rounded-md px-2 text-left type-ui text-secondary outline-hidden transition-[width,height,padding] hover:bg-sidebar-item-hover hover:text-primary focus-visible:focus-ring disabled:pointer-events-none disabled:opacity-disabled aria-disabled:pointer-events-none aria-disabled:opacity-disabled data-[active=true]:bg-sidebar-item-active data-[active=true]:font-medium data-[active=true]:text-primary data-[active=true]:[&_[data-slot=icon]]:text-icon-active data-[state=open]:bg-sidebar-item-hover group-has-data-[sidebar=menu-badge]/menu-item:pr-8 group-data-[collapsible=icon]:w-10! group-data-[collapsible=icon]:px-3! [&>span:last-child]:truncate";

function SidebarMenuButton({
	asChild = false,
	isActive = false,
	tooltip,
	className,
	...props
}: React.ComponentProps<"button"> & {
	asChild?: boolean;
	isActive?: boolean;
	tooltip?: string | React.ComponentProps<typeof TooltipContent>;
}) {
	const Comp = asChild ? Slot : "button";
	const { isMobile, state } = useSidebar();

	const button = (
		<Comp
			data-slot="sidebar-menu-button"
			data-sidebar="menu-button"
			data-active={isActive}
			className={cn(sidebarMenuButtonClass, className)}
			{...props}
		/>
	);

	if (!tooltip) {
		return button;
	}

	if (typeof tooltip === "string") {
		tooltip = {
			children: tooltip,
		};
	}

	return (
		<Tooltip>
			<TooltipTrigger asChild>{button}</TooltipTrigger>
			<TooltipContent
				side="right"
				align="center"
				hidden={state !== "collapsed" || isMobile}
				{...tooltip}
			/>
		</Tooltip>
	);
}

function SidebarMenuBadge({
	className,
	...props
}: React.ComponentProps<"div">) {
	return (
		<div
			data-slot="sidebar-menu-badge"
			data-sidebar="menu-badge"
			className={cn(
				"pointer-events-none absolute top-1.5 right-1 flex h-5 min-w-5 items-center justify-center rounded-md px-1 type-mono-xs text-tertiary select-none",
				"group-data-[collapsible=icon]:hidden",
				className,
			)}
			{...props}
		/>
	);
}

export {
	Sidebar,
	SidebarContent,
	SidebarFooter,
	SidebarGroup,
	SidebarHeader,
	SidebarMenu,
	SidebarMenuBadge,
	SidebarMenuButton,
	SidebarMenuItem,
	SidebarProvider,
	SidebarTrigger,
	useSidebar,
};
