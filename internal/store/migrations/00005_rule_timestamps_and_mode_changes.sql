-- Rule timestamps, authored-object versions, recorded mode changes, and
-- the provisioning-token listing hint (M3.3).
--
-- rules gains created_at and updated_at, deferred from the read model by
-- maintainer ruling: a rule's instants are its own, not its ruleset's.
-- Existing rows are backfilled from the owning ruleset, which is the only
-- honest value the schema holds for them; from now on the authoring writes
-- maintain them, carrying a rule's created_at across an edit of its ruleset
-- and advancing updated_at only when the rule itself changed.
--
-- rulesets, rules, services, and address_groups gain version, the integer
-- a conditional write names. It starts at 1 and every write of the object
-- advances it by one inside the statement that writes it, so it is
-- monotonic whatever the clock does. Callers hold it as an opaque token:
-- the token is the stored integer's decimal form, compared byte-exact
-- against that form inside the conditional statement and never parsed on
-- the way. A rule's version is carried across an edit of its ruleset and
-- advances only when the rule itself changed, like its updated_at.
--
-- mode_changes records each bulk enforcement-mode change as the intent the
-- operator submitted and the set the control plane resolved it to inside
-- the transaction that applied it (ADR-0007 as amended: a mutation that
-- fans out returns a recorded intent, never a job). Nothing here is
-- orchestration state; convergence is observed on the workloads
-- themselves. mode_change_workloads names the resolved set with each
-- workload's mode before the change, so "these N workloads" is auditable
-- after the fact. It carries no foreign key to workloads: an audit row
-- outlives the workload it names.
--
-- provisioning_tokens gains token_prefix, the listing hint: the token's
-- fixed prefix and the first characters of its random body, kept so a
-- listing can tell tokens apart, as operator tokens already do (ADR-0016
-- as amended, ADR-0021). It is written at mint from now on. Tokens minted
-- before it hold only their digest, from which no hint can be recovered,
-- so their rows stay NULL and are listed with no hint; nothing is
-- fabricated for them.
--
-- region_id is reserved on the new top-level table (ADR-0012).

-- +goose Up
ALTER TABLE rules
    ADD COLUMN created_at timestamptz,
    ADD COLUMN updated_at timestamptz,
    ADD COLUMN version    bigint NOT NULL DEFAULT 1;

ALTER TABLE rulesets ADD COLUMN version bigint NOT NULL DEFAULT 1;
ALTER TABLE services ADD COLUMN version bigint NOT NULL DEFAULT 1;
ALTER TABLE address_groups ADD COLUMN version bigint NOT NULL DEFAULT 1;

ALTER TABLE provisioning_tokens ADD COLUMN token_prefix text;

UPDATE rules
SET created_at = rulesets.created_at, updated_at = rulesets.updated_at
FROM rulesets
WHERE rulesets.id = rules.ruleset_id;

ALTER TABLE rules
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN created_at SET DEFAULT now(),
    ALTER COLUMN updated_at SET NOT NULL,
    ALTER COLUMN updated_at SET DEFAULT now();

CREATE TABLE mode_changes (
    id                    uuid        PRIMARY KEY,
    region_id             text        NOT NULL DEFAULT 'default',
    created_at            timestamptz NOT NULL DEFAULT now(),
    target_mode           integer     NOT NULL,
    -- The selector submitted, as a JSON object of key to values; NULL when
    -- the change named workloads by id.
    selector              jsonb,
    expected_match_count  integer     NOT NULL,
    matched               integer     NOT NULL,
    desired_updated       integer     NOT NULL
);

CREATE TABLE mode_change_workloads (
    mode_change_id  uuid    NOT NULL REFERENCES mode_changes (id) ON DELETE CASCADE,
    workload_id     uuid    NOT NULL,
    previous_mode   integer NOT NULL,
    PRIMARY KEY (mode_change_id, workload_id)
);

-- +goose Down
DROP TABLE mode_change_workloads;
DROP TABLE mode_changes;
ALTER TABLE provisioning_tokens DROP COLUMN token_prefix;
ALTER TABLE address_groups DROP COLUMN version;
ALTER TABLE services DROP COLUMN version;
ALTER TABLE rulesets DROP COLUMN version;
ALTER TABLE rules
    DROP COLUMN version,
    DROP COLUMN updated_at,
    DROP COLUMN created_at;
