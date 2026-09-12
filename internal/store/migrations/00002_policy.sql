-- Policy authoring, rendered state, and workload status (ADR-0018).
--
-- Three concerns share this migration because one render reads all of them
-- in a single transaction:
--
--   * The AUTHORED model: services, address groups, and rulesets with their
--     rules. It mirrors the authored layer of the wire contract (ADR-0015).
--     Rules carry a direction column even though v1 admits inbound only
--     (ADR-0010); enabling outbound is a validation change, not a migration.
--   * The RENDERED model: one row per workload holding the serialized
--     WorkloadPolicy the agent receives and its per-workload monotonic
--     version. Versions live here and nowhere else, so they survive any
--     control-plane restart (ADR-0019).
--   * Workload STATUS: enforcement mode, host facts and the addresses
--     derived from them (the renderer resolves peers to these), listening
--     services, agent info, last-seen, and the convergence state the sync
--     stream reports.
--
-- region_id is reserved on every new top-level table (ADR-0012).

-- +goose Up

-- --- authored model ---------------------------------------------------------

CREATE TABLE services (
    id          uuid        PRIMARY KEY,
    region_id   text        NOT NULL DEFAULT 'default',
    name        text        NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- One row per (protocol, port range). A NULL range means every port of the
-- protocol, which is how a protocol without ports (ICMP) is expressed.
CREATE TABLE service_entries (
    service_id  uuid    NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    ordinal     integer NOT NULL,
    protocol    integer NOT NULL,
    port_start  integer,
    port_end    integer,
    PRIMARY KEY (service_id, ordinal),
    CHECK ((port_start IS NULL) = (port_end IS NULL)),
    CHECK (port_start IS NULL OR (port_start >= 0 AND port_start <= port_end AND port_end <= 65535))
);

CREATE TABLE address_groups (
    id          uuid        PRIMARY KEY,
    region_id   text        NOT NULL DEFAULT 'default',
    name        text        NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE address_group_cidrs (
    address_group_id  uuid NOT NULL REFERENCES address_groups (id) ON DELETE CASCADE,
    cidr              text NOT NULL,
    PRIMARY KEY (address_group_id, cidr)
);

CREATE TABLE rulesets (
    id           uuid        PRIMARY KEY,
    region_id    text        NOT NULL DEFAULT 'default',
    name         text        NOT NULL UNIQUE,
    description  text        NOT NULL DEFAULT '',
    enabled      boolean     NOT NULL DEFAULT true,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- The scope selector: requirements are ANDed across rows, values ORed within
-- a row. A ruleset with no rows has an empty selector, which admission
-- rejects because an empty selector matches nothing.
CREATE TABLE ruleset_scope_matches (
    ruleset_id  uuid   NOT NULL REFERENCES rulesets (id) ON DELETE CASCADE,
    key         text   NOT NULL,
    "values"    text[] NOT NULL,
    PRIMARY KEY (ruleset_id, key)
);

CREATE TABLE rules (
    id           uuid    PRIMARY KEY,
    ruleset_id   uuid    NOT NULL REFERENCES rulesets (id) ON DELETE CASCADE,
    ordinal      integer NOT NULL,
    direction    integer NOT NULL,
    enabled      boolean NOT NULL DEFAULT true,
    description  text    NOT NULL DEFAULT '',
    UNIQUE (ruleset_id, ordinal)
);

-- The remote end of a rule. kind selects which column carries the peer:
-- 1 = workload selector (rows in rule_peer_matches), 2 = address group,
-- 3 = literal CIDR.
CREATE TABLE rule_peers (
    rule_id           uuid    NOT NULL REFERENCES rules (id) ON DELETE CASCADE,
    ordinal           integer NOT NULL,
    kind              integer NOT NULL,
    address_group_id  uuid    REFERENCES address_groups (id) ON DELETE RESTRICT,
    cidr              text,
    PRIMARY KEY (rule_id, ordinal),
    CHECK (
        (kind = 1 AND address_group_id IS NULL AND cidr IS NULL) OR
        (kind = 2 AND address_group_id IS NOT NULL AND cidr IS NULL) OR
        (kind = 3 AND address_group_id IS NULL AND cidr IS NOT NULL)
    )
);

CREATE INDEX rule_peers_address_group_id_idx ON rule_peers (address_group_id);

CREATE TABLE rule_peer_matches (
    rule_id       uuid   NOT NULL,
    peer_ordinal  integer NOT NULL,
    key           text   NOT NULL,
    "values"      text[] NOT NULL,
    PRIMARY KEY (rule_id, peer_ordinal, key),
    FOREIGN KEY (rule_id, peer_ordinal) REFERENCES rule_peers (rule_id, ordinal) ON DELETE CASCADE
);

-- Services a rule permits, by reference to a named service definition. A
-- referenced service cannot be deleted while a rule names it.
CREATE TABLE rule_service_refs (
    rule_id     uuid NOT NULL REFERENCES rules (id) ON DELETE CASCADE,
    service_id  uuid NOT NULL REFERENCES services (id) ON DELETE RESTRICT,
    PRIMARY KEY (rule_id, service_id)
);

CREATE INDEX rule_service_refs_service_id_idx ON rule_service_refs (service_id);

-- Services a rule permits, written inline. Same shape as service_entries.
CREATE TABLE rule_service_entries (
    rule_id     uuid    NOT NULL REFERENCES rules (id) ON DELETE CASCADE,
    ordinal     integer NOT NULL,
    protocol    integer NOT NULL,
    port_start  integer,
    port_end    integer,
    PRIMARY KEY (rule_id, ordinal),
    CHECK ((port_start IS NULL) = (port_end IS NULL)),
    CHECK (port_start IS NULL OR (port_start >= 0 AND port_start <= port_end AND port_end <= 65535))
);

-- --- rendered model ----------------------------------------------------------

-- The current rendered policy of each workload: the serialized WorkloadPolicy
-- message and its version. A version advances only when the rendered bytes
-- change; the row is replaced, never appended, because the agent contract
-- has no use for history (reconnects snapshot, ADR-0015).
CREATE TABLE workload_policies (
    workload_id  uuid        PRIMARY KEY REFERENCES workloads (id) ON DELETE CASCADE,
    version      bigint      NOT NULL,
    policy       bytea       NOT NULL,
    rendered_at  timestamptz NOT NULL DEFAULT now()
);

-- --- workload status ---------------------------------------------------------

ALTER TABLE workloads
    ADD COLUMN mode                     integer     NOT NULL DEFAULT 1,
    ADD COLUMN facts                    bytea,
    ADD COLUMN agent_version            text        NOT NULL DEFAULT '',
    ADD COLUMN agent_capabilities       text[]      NOT NULL DEFAULT '{}',
    ADD COLUMN last_seen_at             timestamptz,
    ADD COLUMN sync_state               integer     NOT NULL DEFAULT 4,
    ADD COLUMN applied_policy_version   bigint      NOT NULL DEFAULT 0,
    ADD COLUMN sync_error               text        NOT NULL DEFAULT '',
    ADD COLUMN dropped_flow_records     bigint      NOT NULL DEFAULT 0;

-- Host addresses derived from the reported interfaces, one per row, as bare
-- addresses. The renderer turns them into host routes. Rewritten whenever
-- facts change; a change here is a render trigger.
CREATE TABLE workload_addresses (
    workload_id  uuid NOT NULL REFERENCES workloads (id) ON DELETE CASCADE,
    address      text NOT NULL,
    PRIMARY KEY (workload_id, address)
);

CREATE TABLE workload_listening_services (
    workload_id   uuid    NOT NULL REFERENCES workloads (id) ON DELETE CASCADE,
    protocol      integer NOT NULL,
    port          integer NOT NULL,
    process_name  text    NOT NULL DEFAULT '',
    process_path  text    NOT NULL DEFAULT '',
    PRIMARY KEY (workload_id, protocol, port)
);

-- +goose Down
DROP TABLE workload_listening_services;
DROP TABLE workload_addresses;
ALTER TABLE workloads
    DROP COLUMN dropped_flow_records,
    DROP COLUMN sync_error,
    DROP COLUMN applied_policy_version,
    DROP COLUMN sync_state,
    DROP COLUMN last_seen_at,
    DROP COLUMN agent_capabilities,
    DROP COLUMN agent_version,
    DROP COLUMN facts,
    DROP COLUMN mode;
DROP TABLE workload_policies;
DROP TABLE rule_service_entries;
DROP TABLE rule_service_refs;
DROP TABLE rule_peer_matches;
DROP TABLE rule_peers;
DROP TABLE rules;
DROP TABLE ruleset_scope_matches;
DROP TABLE rulesets;
DROP TABLE address_group_cidrs;
DROP TABLE address_groups;
DROP TABLE service_entries;
DROP TABLE services;
