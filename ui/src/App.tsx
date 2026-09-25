import { Navigate, Route, Routes } from "react-router";
import { SessionProvider } from "@/auth/SessionProvider";
import { FlowMap } from "@/routes/FlowMap";
import { Fleet } from "@/routes/fleet/Fleet";
import { Login } from "@/routes/Login";
import { Policy } from "@/routes/Policy";
import { SimulationReview } from "@/routes/SimulationReview";
import { WorkloadRoute } from "@/routes/workload/WorkloadDetail";
import { Shell } from "@/shell/Shell";
import { ThemeProvider } from "@/theme/ThemeProvider";

// The console's routes. Everything but the login screen sits inside the
// authenticated shell; the sections are in product order (ADR-0001) and
// each talks only to the public REST surface (ADR-0007).
export function App() {
	return (
		<ThemeProvider>
			<SessionProvider>
				<Routes>
					<Route path="/login" element={<Login />} />
					<Route element={<Shell />}>
						<Route index element={<Navigate to="/simulation" replace />} />
						<Route path="/simulation/*" element={<SimulationReview />} />
						<Route path="/map/*" element={<FlowMap />} />
						<Route path="/workloads" element={<Fleet tab="fleet" />} />
						<Route path="/workloads/tokens" element={<Fleet tab="tokens" />} />
						<Route path="/workloads/:id/*" element={<WorkloadRoute />} />
						<Route path="/policy/*" element={<Policy />} />
						<Route path="*" element={<Navigate to="/simulation" replace />} />
					</Route>
				</Routes>
			</SessionProvider>
		</ThemeProvider>
	);
}
