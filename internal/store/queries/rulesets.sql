-- Rulesets and their rules: the authored policy model (ADR-0018). A ruleset
-- is written and replaced as a unit; its child rows are deleted and
-- reinserted inside one transaction on update.

-- name: CreateRuleset :exec
INSERT INTO rulesets (id, name, description, enabled, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $5);

-- name: UpdateRuleset :execrows
UPDATE rulesets
SET name = $2, description = $3, enabled = $4, updated_at = $5
WHERE id = $1;

-- name: DeleteRuleset :execrows
DELETE FROM rulesets
WHERE id = $1;

-- name: GetRuleset :one
SELECT * FROM rulesets
WHERE id = $1;

-- name: ListRulesets :many
SELECT * FROM rulesets
ORDER BY name, id;

-- name: AddRulesetScopeMatch :exec
INSERT INTO ruleset_scope_matches (ruleset_id, key, "values")
VALUES ($1, $2, $3);

-- name: DeleteRulesetScopeMatches :exec
DELETE FROM ruleset_scope_matches
WHERE ruleset_id = $1;

-- name: ListAllRulesetScopeMatches :many
SELECT * FROM ruleset_scope_matches
ORDER BY ruleset_id, key;

-- name: AddRule :exec
INSERT INTO rules (id, ruleset_id, ordinal, direction, enabled, description)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: DeleteRules :exec
DELETE FROM rules
WHERE ruleset_id = $1;

-- name: ListAllRules :many
SELECT * FROM rules
ORDER BY ruleset_id, ordinal;

-- name: AddRulePeer :exec
INSERT INTO rule_peers (rule_id, ordinal, kind, address_group_id, cidr)
VALUES ($1, $2, $3, $4, $5);

-- name: ListAllRulePeers :many
SELECT * FROM rule_peers
ORDER BY rule_id, ordinal;

-- name: AddRulePeerMatch :exec
INSERT INTO rule_peer_matches (rule_id, peer_ordinal, key, "values")
VALUES ($1, $2, $3, $4);

-- name: ListAllRulePeerMatches :many
SELECT * FROM rule_peer_matches
ORDER BY rule_id, peer_ordinal, key;

-- name: AddRuleServiceRef :exec
INSERT INTO rule_service_refs (rule_id, service_id)
VALUES ($1, $2);

-- name: ListAllRuleServiceRefs :many
SELECT * FROM rule_service_refs
ORDER BY rule_id, service_id;

-- name: AddRuleServiceEntry :exec
INSERT INTO rule_service_entries (rule_id, ordinal, protocol, port_start, port_end)
VALUES ($1, $2, $3, $4, $5);

-- name: ListAllRuleServiceEntries :many
SELECT * FROM rule_service_entries
ORDER BY rule_id, ordinal;
