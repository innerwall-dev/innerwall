// Package api serves the public REST/JSON façade consumed by the UI and by
// automation. The façade is generated from the protobuf definitions in proto/;
// nothing exists here that is not defined there first (ADR-0007). Design rules
// for estate-scale automation: bulk endpoints, cursor pagination everywhere,
// async jobs for anything that fans out.
package api
