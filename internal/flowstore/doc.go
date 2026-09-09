// Package flowstore defines the FlowStore interface and its Postgres
// implementation. All flow reads and writes go through the interface; no other
// package issues SQL against flow tables. The schema is column-shaped and
// time-partitioned so a columnar backend can be added behind the same
// interface when a real estate's scale demands it (ADR-0009).
package flowstore
