import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

// cn merges class lists the way the component primitives expect: later
// utilities win over earlier ones of the same kind.
export function cn(...inputs: ClassValue[]): string {
	return twMerge(clsx(inputs));
}
