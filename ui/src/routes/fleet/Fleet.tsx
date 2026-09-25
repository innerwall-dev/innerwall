import { useCallback, useState } from "react";
import { listProvisioningTokens } from "@/api/fleet";
import { TabList, UnderlineTab } from "@/components/Tabs";
import { useResource } from "@/lib/resource";
import { Tokens } from "./Tokens";
import { WorkloadList } from "./WorkloadList";

// Fleet is the workloads section: the fleet and the provisioning tokens
// that enroll it. A tab shows its count only when the count is exact:
// the token listing is complete, and an unfiltered fleet read that comes
// back empty is a fleet of none. The fleet's total has no read, so a
// fleet with workloads shows no number.
export function Fleet({ tab }: { tab: "fleet" | "tokens" }) {
	const [fresh, setFresh] = useState(false);
	const onFresh = useCallback((f: boolean) => setFresh(f), []);
	const { resource: tokens, reload } = useResource(
		() => listProvisioningTokens(),
		[],
	);
	return (
		<div className="flex min-h-0 flex-1 flex-col">
			<TabList>
				<UnderlineTab
					to="/workloads"
					label="Workloads"
					count={fresh ? 0 : undefined}
					end
				/>
				<UnderlineTab
					to="/workloads/tokens"
					label="Provisioning tokens"
					count={
						tokens.status === "ready" ? tokens.data.tokens.length : undefined
					}
				/>
			</TabList>
			{tab === "fleet" ? (
				<WorkloadList onFresh={onFresh} />
			) : (
				<Tokens tokens={tokens} reload={reload} />
			)}
		</div>
	);
}
