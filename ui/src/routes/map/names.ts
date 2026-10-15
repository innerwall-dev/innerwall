import type { PeerRef } from "@/api/schema";

// Names the map shares with screens outside it. This module imports no
// library, so a screen that loads eagerly can name a peer without pulling
// the map's chunk (the graph library and the layout) into its own.

// peerName is a pair's source as a row names it.
export function peerName(p: PeerRef): string {
	switch (p.kind) {
		case "workload":
			return p.name ?? p.workload_id ?? "workload";
		case "group":
			return p.name ?? "address group";
		case "address":
			return p.address ?? "address";
		default:
			return `unrecognized · ${p.address ?? ""}`;
	}
}
