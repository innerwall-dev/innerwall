// Package readmodel is the operator read model: the pure reads over
// persisted state that the console's screens and the command line issue.
// Every function here is a read of what the registry, the rendered policy
// store, and the flow store already hold, shaped for the screen that asks;
// nothing here computes what a query could return, and nothing here
// writes. The operator surface's handlers and the command line call these
// functions and nothing else, which is what keeps the two transports from
// drifting (ADR-0007 as amended, ADR-0021).
//
// The flow reads go through the FlowStore interface (ADR-0009): a rollup
// is one of a closed set of named groupings, aggregated in the store and
// bounded (ADR-0007 as amended); peers come back as ingestion resolved
// them, never re-resolved at read time (ADR-0019); flow lists paginate by
// cursor keyed on window start and row id. Workload reads pair the
// registry's record with the latest rendered version so sync drift is one
// read, and derive nothing but the credential state, which is a function
// of the stored expiry, the stored renewal error, and the clock.
package readmodel
