import { Link } from "react-router";
import { Centered, EmptyState } from "@/components/EmptyState";
import { Button } from "@/components/ui/button";

// Fresh-install state of the flow map (design screen 18).
export function FlowMap() {
	return (
		<Centered>
			<EmptyState
				title="No flows observed yet"
				width={480}
				actions={
					<Button asChild className="self-start">
						<Link to="/workloads">Open enrollment</Link>
					</Button>
				}
			>
				The map draws inbound traffic between label groups as agents report it.
				Enroll workloads and give them a few minutes in visibility mode.
			</EmptyState>
		</Centered>
	);
}
