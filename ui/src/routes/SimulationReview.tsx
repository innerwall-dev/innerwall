import { Link } from "react-router";
import { EmptyState } from "@/components/EmptyState";
import { Button } from "@/components/ui/button";

// Fresh-install state of simulation review (design screen 17). The
// populated screen arrives with the read screens.
export function SimulationReview() {
	return (
		<EmptyState
			title="Nothing to review yet"
			steps={[
				{
					lead: "Enroll workloads",
					rest: "mint a token in Enrollment, run the installer.",
				},
				{
					lead: "Let flows accumulate",
					rest: "workloads start in visibility; the Flow map fills in.",
				},
				{
					lead: "Author a ruleset and switch its scope to simulation",
					rest: "then this screen tells you if it is safe to enforce.",
				},
			]}
			actions={
				<>
					<Button asChild>
						<Link to="/workloads/tokens">Open enrollment</Link>
					</Button>
					<Button variant="secondary" asChild>
						<Link to="/policy">Create a ruleset</Link>
					</Button>
				</>
			}
		>
			Simulation review compares observed traffic against a ruleset's rendered
			policy and shows what enforcement would break. It needs workloads, flows,
			and at least one ruleset.
		</EmptyState>
	);
}
