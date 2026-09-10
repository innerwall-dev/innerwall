// Package compiler turns label-based rules plus registry state into per-agent
// compiled rulesets, each with a monotonic version. Only agents affected by a
// change are recompiled. A single compiler is active across replicas, held by
// a Postgres advisory lock (ADR-0017). Compilation is inbound-only in v1; the
// schema reserves direction (ADR-0010). Compiled bundles are signed from day
// one (ADR-0012).
package compiler
