// Generated from api/openapi.yaml by scripts/generate-api.mjs. Do not edit.

export interface paths {
    "/address-groups": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Every address group. */
        get: operations["listAddressGroups"];
        put?: never;
        /**
         * Create an address group.
         * @description CIDRs are persisted in canonical masked form, without duplicates.
         */
        post: operations["createAddressGroup"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/address-groups/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        /** One address group. */
        get: operations["getAddressGroup"];
        /** Replace an address group's name and members. */
        put: operations["putAddressGroup"];
        post?: never;
        /** Delete an address group no rule references. */
        delete: operations["deleteAddressGroup"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/flows": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * One page of a workload's stored flow windows.
         * @description There is no unbounded form; `workload` is required.
         */
        get: operations["getFlows"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/flows/rollup": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * A grouped rollup of stored flow windows.
         * @description One of a closed set of named groupings over the windows in a range,
         *     aggregated in the store and bounded: at most `limit` groups in the
         *     requested order, with the totals of the whole and `truncated` set when
         *     more groups exist. `effective_from` and `effective_to` are the bounds
         *     of the windows actually covered, null when none were. Peers are what
         *     ingestion resolved and stored; only the current display name of a
         *     stored identity is added.
         */
        get: operations["getFlowsRollup"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/me": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** The authenticated operator and the site label. */
        get: operations["getMe"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/mode-changes": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Change the enforcement mode of a set of workloads.
         * @description Exactly one of `selector` or `workload_ids` selects the set;
         *     `expected_match_count` is required and must equal what the selection
         *     resolves to on the control plane, otherwise the change is refused as
         *     `409 match-count-mismatch` with both numbers (preview the selector
         *     first). In one transaction the control plane resolves the selection
         *     through the renderer's own scope match, records the intent as the set
         *     it resolved to, flips the desired mode of exactly that set by id, and
         *     renders, so a label change committed meanwhile cannot move the set.
         *     The response acknowledges the recorded intent and says nothing about
         *     convergence, which is observed on the workload reads as applied
         *     version against latest.
         */
        post: operations["createModeChange"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/operator-tokens": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Every operator token, prefix and metadata only. */
        get: operations["listOperatorTokens"];
        put?: never;
        /**
         * Mint an operator token for automation.
         * @description The secret is in this response and nowhere else, ever. It is presented as a bearer credential and resolves to the same principal a session does.
         */
        post: operations["mintOperatorToken"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/operator-tokens/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        /**
         * Revoke an operator token.
         * @description Revocation, not deletion; the token stays listed as revoked and no longer authenticates.
         */
        delete: operations["revokeOperatorToken"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/policies/render-dryrun": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Render a hypothetical policy set and report what would change, persisting nothing.
         * @description The request carries the complete set of rulesets as they would be
         *     authored (services and address groups are the persisted ones,
         *     referenced by id or name) and, optionally, the `state_version` the
         *     author read before editing. The control plane admits the set exactly
         *     as a write would, renders it against persisted state read as one
         *     consistent snapshot, diffs every workload's result against its
         *     persisted rendered policy through the same delta implementation the
         *     sync stream uses, and discards everything. No lock is held and no
         *     version moves. `stale` is set when the request named a state version
         *     other than the one computed against.
         */
        post: operations["renderDryRun"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/provisioning-tokens": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Every provisioning token, prefix and metadata only.
         * @description A secret is never listed; its digest and a listing prefix are stored. A token minted before the prefix was kept lists a null prefix.
         */
        get: operations["listProvisioningTokens"];
        put?: never;
        /**
         * Mint a provisioning token.
         * @description The secret is in this response and nowhere else, ever. A token assigns its labels to every workload it enrolls and may enroll any number while valid.
         */
        post: operations["mintProvisioningToken"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/provisioning-tokens/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        /**
         * Revoke a provisioning token.
         * @description Revocation, not deletion; the token stays listed as revoked. Future enrollments with it are refused; workloads it already enrolled are untouched.
         */
        delete: operations["revokeProvisioningToken"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/rulesets": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Every ruleset, and the version of the state a policy editor authors against.
         * @description `state_version` is a digest of everything a render reads: each
         *     workload's labels, addresses, and mode, and each authored object's
         *     last write. A dry run reports the version it computed against, so an
         *     editor that started from this value can tell when the state moved.
         */
        get: operations["listRulesets"];
        put?: never;
        /**
         * Create a ruleset with its rules.
         * @description The whole ruleset is admitted before anything is persisted, and every workload is rendered after.
         */
        post: operations["createRuleset"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/rulesets/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        /** One ruleset with its rules. */
        get: operations["getRuleset"];
        /**
         * Replace a ruleset and all of its rules.
         * @description A rule that keeps its id keeps its `created_at`, and its `updated_at` and version move only when the rule itself changed; a rule without an id is new.
         */
        put: operations["putRuleset"];
        post?: never;
        /** Delete a ruleset and its rules. */
        delete: operations["deleteRuleset"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/rulesets/{id}/rules": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Add one rule to a ruleset.
         * @description The ruleset is written as a unit, conditioned on the version it was read at inside the control plane, so a concurrent edit is refused rather than overwritten. Findings are relative to the rule.
         */
        post: operations["createRule"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/rulesets/{id}/rules/{rule_id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
                /** @description The rule's id. */
                rule_id: string;
            };
            cookie?: never;
        };
        /** One rule. */
        get: operations["getRule"];
        /** Replace one rule, conditioned on the rule's own version. */
        put: operations["putRule"];
        post?: never;
        /** Remove one rule, conditioned on the rule's own version. */
        delete: operations["deleteRule"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/selectors/preview": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * What a selector resolves to now.
         * @description Read-only in effect; a POST because the selector is a structured body. The resolution is the same one a mode change uses, so a preview and the change it precedes cannot differ.
         */
        post: operations["previewSelector"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/services": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** Every named service. */
        get: operations["listServices"];
        put?: never;
        /** Create a named service. */
        post: operations["createService"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/services/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        /** One service. */
        get: operations["getService"];
        /** Replace a service's name and entries. */
        put: operations["putService"];
        post?: never;
        /** Delete a service no rule references. */
        delete: operations["deleteService"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/session": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Log in with the operator password.
         * @description The one endpoint reachable without a credential. On success a session
         *     cookie is set (unreadable by scripts, TLS only, not sent cross-site)
         *     with a fixed seven-day lifetime, and the operator is returned. A
         *     control plane with no password refuses with `403 no-password` and
         *     never offers to set one. Attempts are throttled per source address;
         *     beyond the limit the answer is `429 too-many-attempts` with
         *     `Retry-After`.
         */
        post: operations["createSession"];
        /**
         * Log out.
         * @description Revokes the session the request authenticated with, if it was a session, and clears the cookie either way.
         */
        delete: operations["deleteSession"];
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/workloads": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * One page of the fleet in fleet order.
         * @description Fleet order is the order the fleet screen shows: the workloads needing
         *     attention first (degraded, offline, pending, synced), most recently
         *     seen first within a state.
         */
        get: operations["listWorkloads"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/workloads/{id}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The workload's id. */
                id: components["parameters"]["workloadID"];
            };
            cookie?: never;
        };
        /** One workload, in the shape the list uses. */
        get: operations["getWorkload"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/workloads/{id}/labels": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The workload's id. */
                id: components["parameters"]["workloadID"];
            };
            cookie?: never;
        };
        /** A workload's labels, as the resource a label edit is conditioned on. */
        get: operations["getWorkloadLabels"];
        /**
         * Replace a workload's labels.
         * @description A label change moves the workload in and out of selectors, so the change renders; affected workloads' versions advance.
         */
        put: operations["putWorkloadLabels"];
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/workloads/{id}/rendered-policy": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The workload's id. */
                id: components["parameters"]["workloadID"];
            };
            cookie?: never;
        };
        /**
         * The persisted rendered policy of one workload.
         * @description Configuration only, no counts; the console pairs it with a rollup. A workload with no persisted policy yet is reported at version zero with no rules.
         */
        get: operations["getRenderedPolicy"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/workloads/{id}/resend-snapshot": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The workload's id. */
                id: components["parameters"]["workloadID"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Direct a workload's agent to reconnect and receive a fresh snapshot.
         * @description Fires a reconnect directive toward the replica holding the workload's
         *     sync stream; the agent reconnects and its new stream begins with a
         *     snapshot. Delivery is best-effort by design and nothing waits for it:
         *     the response carries `last_snapshot_sent_at` as it stood when the
         *     directive was fired (null when no snapshot has been recorded), and
         *     the outcome is observed on a later read of the workload as
         *     `sync.last_snapshot_sent_at` advancing. The control plane writes that
         *     instant whenever it sends a snapshot, whatever the cause; this
         *     endpoint never writes it. A workload whose agent is offline, as the
         *     control plane last recorded it, is refused as `409 agent-offline`
         *     with the instant it was last seen. That judgment approximates
         *     whether a stream exists; it is not a presence check.
         */
        post: operations["resendSnapshot"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        /** @description A named set of CIDRs for peers that are not managed workloads. */
        AddressGroup: {
            cidrs: string[];
            created_at: components["schemas"]["Timestamp"];
            /** Format: uuid */
            id: string;
            name: string;
            updated_at: components["schemas"]["Timestamp"];
            version: components["schemas"]["Version"];
        };
        /** @description Summed counts of a group or of a whole rollup. */
        Counters: {
            byte_count: number;
            connection_count: number;
            /** @description Stored records. */
            flow_count: number;
        };
        /** @description What rendering the hypothetical set would change, per workload. */
        DryRunResult: {
            stale: boolean;
            state_version: components["schemas"]["StateVersion"];
            /** @description Only workloads whose rendered policy would differ. */
            workloads: {
                added: components["schemas"]["RenderedRuleDelta"][];
                changed: {
                    after: components["schemas"]["RenderedRuleDelta"];
                    before: components["schemas"]["RenderedRuleDelta"];
                }[];
                mode: {
                    from: components["schemas"]["Mode"];
                    to: components["schemas"]["Mode"];
                } | null;
                removed: components["schemas"]["RenderedRuleDelta"][];
                /** @description The persisted version the diff is against; zero when none is rendered yet. */
                version: number;
                workload: components["schemas"]["WorkloadRef"];
            }[];
        };
        /** @description One protocol with the ports it permits. No ports means every port; a protocol without ports (icmp) lists none. */
        Entry: {
            ports?: string[];
            protocol: components["schemas"]["Protocol"];
        };
        /** @description One admission failure of a refused write. */
        Finding: {
            message: string;
            /** @description Where in the request body, e.g. `rules[1].peers[0].cidr`. */
            path: string;
            /** @description The stable name of the admission rule broken, e.g. `cidr`, `selector-empty`, `direction-outbound`. */
            rule: string;
        };
        /** @description One stored flow record of one reporting window, as ingestion resolved it. */
        Flow: {
            byte_count: number;
            connection_count: number;
            /** @enum {string} */
            direction: "inbound" | "outbound";
            dst_address: string;
            first_seen: components["schemas"]["Timestamp"];
            id: number;
            last_seen: components["schemas"]["Timestamp"];
            peer: components["schemas"]["PeerRef"];
            process_name?: string;
            rule: components["schemas"]["RuleRef"];
            service: components["schemas"]["ServiceKey"];
            src_address: string;
            verdict: components["schemas"]["Verdict"];
            window_end: components["schemas"]["Timestamp"];
            window_start: components["schemas"]["Timestamp"];
        };
        /** @description One page of a workload's stored windows, newest first. */
        FlowsPage: {
            flows: components["schemas"]["Flow"][];
            from: components["schemas"]["Timestamp"];
            next_cursor: string | null;
            to: components["schemas"]["Timestamp"];
            workload: components["schemas"]["WorkloadRef"];
        };
        /** @description Labels as a key to value object. */
        LabelMap: {
            [key: string]: string;
        };
        /** @description A workload's labels with the version a label edit is conditioned on. */
        Labels: {
            labels: components["schemas"]["LabelMap"];
            version: components["schemas"]["Version"];
        };
        /** @description The operator as the console shows it, and the site label configured on the control plane. */
        Me: {
            display_name: string | null;
            site: string;
        };
        /**
         * @description An enforcement mode: observe only, evaluate without dropping, or enforce.
         * @enum {string}
         */
        Mode: "visibility" | "simulation" | "enforced";
        /** @description An acknowledgment of the recorded intent; never a statement of progress. */
        ModeChangeAck: {
            /** @description How many of them were not already in the target mode. */
            desired_updated: number;
            /** @description The size of the resolved set. */
            matched: number;
            /** Format: uuid */
            mode_change_id: string;
        };
        /** @description A bulk enforcement-mode change as the operator submits it. */
        ModeChangeRequest: {
            expected_match_count: number;
            selector?: components["schemas"]["Selector"];
            target_mode: components["schemas"]["Mode"];
            workload_ids?: components["schemas"]["WorkloadID"][];
        };
        /** @description An operator token as listed: its prefix and metadata; the secret is never stored. */
        OperatorToken: {
            created_at: components["schemas"]["Timestamp"];
            /** Format: date-time */
            expires_at: string | null;
            /** Format: uuid */
            id: string;
            /** Format: date-time */
            last_used_at: string | null;
            name: string;
            /** @description The prefix and a few leading characters, enough to tell tokens apart in a list. */
            prefix: string;
            /** Format: date-time */
            revoked_at: string | null;
            state: components["schemas"]["TokenState"];
        };
        /** @description The remote end of a rule; exactly one property is set. An address group may be given by id or, on the way in, by name; the control plane writes ids. */
        Peer: {
            address_group?: string;
            cidr?: string;
            workloads?: components["schemas"]["Selector"];
        };
        /** @description A resolved peer as ingestion stored it, with the current display name of the identity. */
        PeerRef: {
            address?: string;
            /** Format: uuid */
            address_group_id?: string;
            /** @enum {string} */
            kind: "unknown" | "workload" | "address_group";
            labels: components["schemas"]["LabelMap"];
            name?: string;
            workload_id?: components["schemas"]["WorkloadID"];
        };
        /** @description An inclusive port range; a single port has start equal to end. */
        PortRange: {
            end: number;
            start: number;
        };
        /** @description An error document (RFC 9457 shape) with a stable type and, for some conditions, an extension a client acts on. */
        Problem: {
            /** @description On a `precondition-failed` problem, the version the resource holds now. */
            current_version?: string;
            detail?: string;
            /** @description On a `validation` problem, one finding per field to fix. */
            errors?: components["schemas"]["Finding"][];
            /** @description On a `match-count-mismatch` problem, what the caller expected. */
            expected?: number;
            /**
             * Format: date-time
             * @description On an `agent-offline` problem, always present; when the agent was last heard from, null when it never was.
             */
            last_seen_at?: string | null;
            /** @description On a `match-count-mismatch` problem, what the selection resolved to. */
            matched?: number;
            status: number;
            title: string;
            /**
             * @description The stable identifier of the condition.
             * @enum {string}
             */
            type: "urn:innerwall:problem:unauthenticated" | "urn:innerwall:problem:invalid-credentials" | "urn:innerwall:problem:no-password" | "urn:innerwall:problem:cross-origin" | "urn:innerwall:problem:too-many-attempts" | "urn:innerwall:problem:invalid-request" | "urn:innerwall:problem:invalid-parameter" | "urn:innerwall:problem:validation" | "urn:innerwall:problem:precondition-required" | "urn:innerwall:problem:precondition-failed" | "urn:innerwall:problem:match-count-mismatch" | "urn:innerwall:problem:duplicate-name" | "urn:innerwall:problem:in-use" | "urn:innerwall:problem:already-revoked" | "urn:innerwall:problem:agent-offline" | "urn:innerwall:problem:not-found" | "urn:innerwall:problem:method-not-allowed" | "urn:innerwall:problem:internal";
        };
        /**
         * @description A transport protocol.
         * @enum {string}
         */
        Protocol: "tcp" | "udp" | "icmp";
        /** @description A provisioning token as listed: its prefix and metadata; the secret is never stored. */
        ProvisioningToken: {
            created_at: components["schemas"]["Timestamp"];
            expires_at: components["schemas"]["Timestamp"];
            /** Format: uuid */
            id: string;
            labels: components["schemas"]["LabelMap"];
            /** Format: date-time */
            last_used_at: string | null;
            name: string;
            /** @description The prefix and a few leading characters, enough to tell tokens apart in a list. Null for a token minted before the control plane kept this hint; none can be recovered from the stored digest. */
            prefix: string | null;
            /** Format: date-time */
            revoked_at: string | null;
            state: components["schemas"]["TokenState"];
            use_count: number;
        };
        /** @description A workload's persisted rendered policy as configuration. */
        RenderedPolicy: {
            mode: components["schemas"]["Mode"];
            /** Format: date-time */
            rendered_at: string | null;
            rules: {
                authored_rule_id?: string;
                /**
                 * Format: date-time
                 * @description The authored rule's own instant; null when the authored rule no longer exists.
                 */
                created_at: string | null;
                description: string;
                id: string;
                peer_cidrs: string[];
                /** @description Empty when the rule permits every port of the protocol. */
                ports: components["schemas"]["PortRange"][];
                protocol: components["schemas"]["Protocol"];
                /** @description The authored ruleset the rule came from; null when the authored rule no longer exists. */
                ruleset: {
                    /** Format: uuid */
                    id: string;
                    name: string;
                } | null;
                /**
                 * Format: date-time
                 * @description When the authored rule itself last changed; null when it no longer exists.
                 */
                updated_at: string | null;
                verdict: components["schemas"]["Verdict"];
            }[];
            /** @description What traffic no rule admits receives under the mode. */
            terminal_verdict: components["schemas"]["Verdict"];
            version: number;
            workload: components["schemas"]["WorkloadRef"];
        };
        /** @description One resolved rule as a dry run reports it. */
        RenderedRuleDelta: {
            /** @description The resolved rule id, `<authored rule id>/<protocol>`. */
            id: string;
            peer_cidrs: string[];
            ports: components["schemas"]["PortRange"][];
            protocol: components["schemas"]["Protocol"];
        };
        /** @description An acknowledgment that the reconnect directive was fired; never a statement that it arrived. */
        ResendSnapshotAck: {
            /**
             * Format: date-time
             * @description The workload's snapshot instant as it stood when the directive was fired; null when none has been recorded.
             */
            last_snapshot_sent_at: string | null;
        };
        /** @description A grouped, bounded rollup of stored windows over a range. */
        Rollup: {
            /** Format: date-time */
            effective_from: string | null;
            /** Format: date-time */
            effective_to: string | null;
            from: components["schemas"]["Timestamp"];
            /** @description The dimensions of the grouping, in order. */
            group_by: ("rule" | "peer" | "src" | "dst" | "service")[];
            /** @description How many groups exist in the range, whether or not they were returned. */
            group_count: number;
            groups: (components["schemas"]["Counters"] & {
                first_seen: components["schemas"]["Timestamp"];
                /** @description One property per dimension of the grouping. */
                keys: {
                    dst?: components["schemas"]["WorkloadRef"];
                    peer?: components["schemas"]["PeerRef"];
                    rule?: components["schemas"]["RuleRef"];
                    service?: components["schemas"]["ServiceKey"];
                    src?: components["schemas"]["PeerRef"];
                };
                last_seen: components["schemas"]["Timestamp"];
            })[];
            to: components["schemas"]["Timestamp"];
            totals: components["schemas"]["Counters"];
            truncated: boolean;
        };
        /** @description A rule as persisted, with its own version and instants. */
        Rule: components["schemas"]["RuleInput"] & {
            created_at: components["schemas"]["Timestamp"];
            updated_at: components["schemas"]["Timestamp"];
            version: components["schemas"]["Version"];
        };
        /** @description A rule as authored. Services may be given by id or, on the way in, by name; at least one of `services` or `entries` is required. Version and timestamps are ignored on the way in. */
        RuleInput: {
            description?: string;
            /**
             * @description Only inbound is admitted in this version (ADR-0010).
             * @enum {string}
             */
            direction: "inbound";
            /** @default true */
            enabled: boolean;
            entries?: components["schemas"]["Entry"][];
            /**
             * Format: uuid
             * @description Kept across an edit of the ruleset; a rule without one is new.
             */
            id?: string;
            peers: components["schemas"]["Peer"][];
            services?: string[];
        };
        /** @description A resolved rule as flow records name it; null for the group of records no rule admitted. */
        RuleRef: {
            authored_rule_id?: string;
            id: string;
            protocol?: components["schemas"]["Protocol"];
        } | null;
        /** @description A ruleset as persisted, with its version and instants and those of each rule. */
        Ruleset: components["schemas"]["RulesetInput"] & {
            created_at: components["schemas"]["Timestamp"];
            rules?: components["schemas"]["Rule"][];
            updated_at: components["schemas"]["Timestamp"];
            version: components["schemas"]["Version"];
        };
        /** @description A ruleset as authored; the ruleset is admitted and written as a unit with its rules. */
        RulesetInput: {
            description?: string;
            /** @default true */
            enabled: boolean;
            /**
             * Format: uuid
             * @description Ignored on create; the path names the ruleset on update.
             */
            id?: string;
            name: string;
            rules: components["schemas"]["RuleInput"][];
            scope: components["schemas"]["Selector"];
        };
        /** @description Label requirements ANDed across keys; a key's values are ORed. An empty selector matches nothing and is refused. */
        Selector: {
            [key: string]: string[];
        };
        /** @description A named, reusable set of protocol and port entries that rules reference. */
        Service: {
            created_at: components["schemas"]["Timestamp"];
            entries: components["schemas"]["Entry"][];
            /** Format: uuid */
            id: string;
            name: string;
            updated_at: components["schemas"]["Timestamp"];
            version: components["schemas"]["Version"];
        };
        /** @description A destination protocol and port; port is zero for a protocol without ports. */
        ServiceKey: {
            port: number;
            protocol: components["schemas"]["Protocol"];
        };
        /** @description The version of the state a render reads, derived and never stored. */
        StateVersion: string;
        /**
         * @description A workload's convergence with its rendered policy, as its stream reports it.
         * @enum {string}
         */
        SyncState: "synced" | "pending" | "degraded" | "offline";
        /**
         * Format: date-time
         * @description RFC 3339, UTC.
         */
        Timestamp: string;
        /**
         * @description A token's state now: valid, expired, revoked, or otherwise unusable.
         * @enum {string}
         */
        TokenState: "valid" | "expired" | "revoked" | "invalid";
        /**
         * @description The decision a flow received: observed (visibility), allowed, would_block (simulation), or blocked (enforced).
         * @enum {string}
         */
        Verdict: "observed" | "allowed" | "would_block" | "blocked";
        /** @description An opaque resource version, compared byte-exact; also the entity tag. */
        Version: string;
        /** @description A workload as the fleet list and the detail read share it. */
        Workload: {
            addresses: string[];
            agent: {
                capabilities: string[];
                version: string;
            };
            enrolled_at: components["schemas"]["Timestamp"];
            health: {
                credential: {
                    expires_at: components["schemas"]["Timestamp"];
                    last_error: string;
                    /** Format: date-time */
                    last_renewed_at: string | null;
                    /** @enum {string} */
                    state: "renews" | "renewal-failed" | "expired";
                };
                dropped_flow_records: number;
                /** Format: date-time */
                last_seen_at: string | null;
            };
            hostname: string;
            id: components["schemas"]["WorkloadID"];
            labels: components["schemas"]["LabelMap"];
            listening_services: {
                port: number;
                process_name: string;
                process_path: string;
                protocol: components["schemas"]["Protocol"];
            }[];
            mode: components["schemas"]["Mode"];
            os: {
                architecture?: string;
                family?: string;
                kernel_version?: string;
                name?: string;
                version?: string;
            } | null;
            /** @description Convergence with the rendered policy; `latest_version` ahead of `applied_version` is drift in flight. */
            sync: {
                applied_version: number;
                error: string;
                /**
                 * Format: date-time
                 * @description When the control plane last sent the workload a snapshot, whatever the cause; null when none has been recorded.
                 */
                last_snapshot_sent_at: string | null;
                /** Format: date-time */
                latest_rendered_at: string | null;
                latest_version: number;
                state: components["schemas"]["SyncState"];
            };
        };
        /**
         * Format: uuid
         * @description A workload identity, the UUID the control plane assigned at enrollment.
         */
        WorkloadID: string;
        /** @description A workload named by id, with its current hostname and labels. */
        WorkloadRef: {
            hostname: string;
            id: components["schemas"]["WorkloadID"];
            labels: components["schemas"]["LabelMap"];
        };
    };
    responses: {
        /** @description The token is already revoked (`already-revoked`). */
        AlreadyRevoked: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/problem+json": components["schemas"]["Problem"];
            };
        };
        /** @description Another object of the kind already has that name, or a rule with that id already exists (`duplicate-name`). */
        DuplicateName: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/problem+json": components["schemas"]["Problem"];
            };
        };
        /** @description A rule references the object, so it cannot be deleted (`in-use`). */
        InUse: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/problem+json": components["schemas"]["Problem"];
            };
        };
        /** @description A query or path parameter cannot be read (`invalid-parameter`); `detail` names it. */
        InvalidParameter: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/problem+json": components["schemas"]["Problem"];
            };
        };
        /** @description The body is not a JSON document of the expected shape (`invalid-request`). */
        InvalidRequest: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/problem+json": components["schemas"]["Problem"];
            };
        };
        /** @description No such resource (`not-found`). A malformed id is not found, since nothing has it. */
        NotFound: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/problem+json": components["schemas"]["Problem"];
            };
        };
        /** @description The resource no longer holds the version `If-Match` named (`precondition-failed`); `current_version` is what it holds now. */
        PreconditionFailed: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                /**
                 * @example {
                 *       "type": "urn:innerwall:problem:precondition-failed",
                 *       "title": "Precondition failed",
                 *       "status": 412,
                 *       "current_version": "1789300800007000000"
                 *     }
                 */
                "application/problem+json": components["schemas"]["Problem"];
            };
        };
        /** @description The write carries no `If-Match` (`precondition-required`). */
        PreconditionRequired: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/problem+json": components["schemas"]["Problem"];
            };
        };
        /** @description No valid credential was presented (`unauthenticated`). */
        Unauthenticated: {
            headers: {
                "WWW-Authenticate"?: string;
                [name: string]: unknown;
            };
            content: {
                "application/problem+json": components["schemas"]["Problem"];
            };
        };
        /** @description The body was read and refused by admission (`validation`); `errors` lists every finding. */
        Validation: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                /**
                 * @example {
                 *       "type": "urn:innerwall:problem:validation",
                 *       "title": "Validation failed",
                 *       "status": 400,
                 *       "detail": "the request was refused by admission; see errors",
                 *       "errors": [
                 *         {
                 *           "path": "rules[0].peers[1].cidr",
                 *           "rule": "cidr",
                 *           "message": "address is not a valid CIDR (nope)"
                 *         }
                 *       ]
                 *     }
                 */
                "application/problem+json": components["schemas"]["Problem"];
            };
        };
    };
    parameters: {
        /** @description The `next_cursor` of the previous page. */
        cursor: string;
        /** @description Restrict to records in this direction (only inbound is produced in this version). */
        direction: "inbound" | "outbound";
        /** @description Start of the range (RFC 3339). Defaults to a day before `to`. */
        from: string;
        /** @description The object's id. */
        id: string;
        /** @description The version the caller last read, as the resource's entity tag. Required on every update and delete. */
        ifMatch: string;
        /** @description A `key=value` requirement; repeat to AND keys, repeat a key to OR its values. Restricts to the workloads the selector currently matches. */
        label: string[];
        /** @description A destination service, `<protocol>/<port>` such as `tcp/5432`, or `icmp`. */
        service: string;
        /** @description End of the range (RFC 3339), exclusive. Defaults to now. */
        to: string;
        /** @description Restrict to records with this verdict. */
        verdict: components["schemas"]["Verdict"];
        /** @description The workload's id. */
        workloadID: components["schemas"]["WorkloadID"];
    };
    requestBodies: {
        /** @description An address group's name and member CIDRs. */
        AddressGroup: {
            content: {
                "application/json": {
                    cidrs: string[];
                    name: string;
                };
            };
        };
        /** @description One rule, in the document form the command line reads. */
        Rule: {
            content: {
                "application/json": components["schemas"]["RuleInput"];
            };
        };
        /** @description A ruleset with its rules, in the document form the command line reads. */
        Ruleset: {
            content: {
                "application/json": components["schemas"]["RulesetInput"];
            };
        };
        /** @description A service's name and entries. */
        Service: {
            content: {
                "application/json": {
                    entries: components["schemas"]["Entry"][];
                    name: string;
                };
            };
        };
    };
    headers: {
        /** @description The resource's version, quoted. Pass it back as `If-Match` on a write. */
        ETag: string;
        /** @description Where the created resource is. */
        Location: string;
    };
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    listAddressGroups: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The address groups. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": {
                        address_groups: components["schemas"]["AddressGroup"][];
                    };
                };
            };
            401: components["responses"]["Unauthenticated"];
        };
    };
    createAddressGroup: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: components["requestBodies"]["AddressGroup"];
        responses: {
            /** @description The address group as persisted. */
            201: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    Location: components["headers"]["Location"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AddressGroup"];
                };
            };
            400: components["responses"]["Validation"];
            401: components["responses"]["Unauthenticated"];
            409: components["responses"]["DuplicateName"];
        };
    };
    getAddressGroup: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The address group. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AddressGroup"];
                };
            };
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
        };
    };
    putAddressGroup: {
        parameters: {
            query?: never;
            header: {
                /** @description The version the caller last read, as the resource's entity tag. Required on every update and delete. */
                "If-Match": components["parameters"]["ifMatch"];
            };
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        requestBody: components["requestBodies"]["AddressGroup"];
        responses: {
            /** @description The address group as persisted. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AddressGroup"];
                };
            };
            400: components["responses"]["Validation"];
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["DuplicateName"];
            412: components["responses"]["PreconditionFailed"];
            428: components["responses"]["PreconditionRequired"];
        };
    };
    deleteAddressGroup: {
        parameters: {
            query?: never;
            header: {
                /** @description The version the caller last read, as the resource's entity tag. Required on every update and delete. */
                "If-Match": components["parameters"]["ifMatch"];
            };
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Deleted. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InUse"];
            412: components["responses"]["PreconditionFailed"];
            428: components["responses"]["PreconditionRequired"];
        };
    };
    getFlows: {
        parameters: {
            query: {
                /** @description The `next_cursor` of the previous page. */
                cursor?: components["parameters"]["cursor"];
                /** @description Restrict to records in this direction (only inbound is produced in this version). */
                direction?: components["parameters"]["direction"];
                /** @description Start of the range (RFC 3339). Defaults to a day before `to`. */
                from?: components["parameters"]["from"];
                /** @description Page size; the store's default and maximum apply. */
                limit?: number;
                /** @description Restrict to one stored peer key (a workload id, an address group id, or a bare address). */
                peer?: string;
                /** @description A destination service, `<protocol>/<port>` such as `tcp/5432`, or `icmp`. */
                service?: components["parameters"]["service"];
                /** @description End of the range (RFC 3339), exclusive. Defaults to now. */
                to?: components["parameters"]["to"];
                /** @description Restrict to records with this verdict. */
                verdict?: components["parameters"]["verdict"];
                /** @description The workload whose windows are listed. */
                workload: components["schemas"]["WorkloadID"];
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One page, newest window first. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["FlowsPage"];
                };
            };
            400: components["responses"]["InvalidParameter"];
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
        };
    };
    getFlowsRollup: {
        parameters: {
            query: {
                /** @description Restrict to records in this direction (only inbound is produced in this version). */
                direction?: components["parameters"]["direction"];
                /** @description Start of the range (RFC 3339). Defaults to a day before `to`. */
                from?: components["parameters"]["from"];
                /** @description The grouping. The set is closed; nothing else is accepted. */
                group_by: "rule" | "rule,peer" | "src,dst" | "dst,service";
                /** @description A `key=value` requirement; repeat to AND keys, repeat a key to OR its values. Restricts to the workloads the selector currently matches. */
                label?: components["parameters"]["label"];
                /** @description Maximum groups returned; the store's default and maximum apply. */
                limit?: number;
                /** @description The order groups are ranked in before the limit applies. */
                order?: "connections" | "recent";
                /** @description A destination service, `<protocol>/<port>` such as `tcp/5432`, or `icmp`. */
                service?: components["parameters"]["service"];
                /** @description End of the range (RFC 3339), exclusive. Defaults to now. */
                to?: components["parameters"]["to"];
                /** @description Restrict to records with this verdict. */
                verdict?: components["parameters"]["verdict"];
                /** @description Restrict to one workload. */
                workload?: components["schemas"]["WorkloadID"];
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The rollup. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Rollup"];
                };
            };
            400: components["responses"]["InvalidParameter"];
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
        };
    };
    getMe: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The operator. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Me"];
                };
            };
            401: components["responses"]["Unauthenticated"];
        };
    };
    createModeChange: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description The selection, the mode to reach, and the expected size of the selection. */
        requestBody: {
            content: {
                "application/json": components["schemas"]["ModeChangeRequest"];
            };
        };
        responses: {
            /** @description The recorded intent. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ModeChangeAck"];
                };
            };
            400: components["responses"]["Validation"];
            401: components["responses"]["Unauthenticated"];
            /** @description The selection resolved to a different number of workloads than expected (`match-count-mismatch`). */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    /**
                     * @example {
                     *       "type": "urn:innerwall:problem:match-count-mismatch",
                     *       "title": "Match count mismatch",
                     *       "status": 409,
                     *       "expected": 2,
                     *       "matched": 3
                     *     }
                     */
                    "application/problem+json": components["schemas"]["Problem"];
                };
            };
        };
    };
    listOperatorTokens: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The tokens, newest first. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": {
                        tokens: components["schemas"]["OperatorToken"][];
                    };
                };
            };
            401: components["responses"]["Unauthenticated"];
        };
    };
    mintOperatorToken: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description The token's name and its lifetime. */
        requestBody: {
            content: {
                "application/json": {
                    name?: string;
                    /** @description Lifetime; the token does not expire when zero or absent. */
                    ttl_seconds?: number;
                };
            };
        };
        responses: {
            /** @description The token, with its secret. */
            201: {
                headers: {
                    Location: components["headers"]["Location"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["OperatorToken"] & {
                        /** @description The secret, prefixed `iwo_`. Shown once. */
                        token: string;
                    };
                };
            };
            400: components["responses"]["InvalidRequest"];
            401: components["responses"]["Unauthenticated"];
        };
    };
    revokeOperatorToken: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Revoked. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["AlreadyRevoked"];
        };
    };
    renderDryRun: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description The complete hypothetical set of rulesets, and the state version the author read. */
        requestBody: {
            content: {
                "application/json": {
                    rulesets: components["schemas"]["RulesetInput"][];
                    state_version?: components["schemas"]["StateVersion"];
                };
            };
        };
        responses: {
            /** @description The per-workload deltas. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DryRunResult"];
                };
            };
            400: components["responses"]["Validation"];
            401: components["responses"]["Unauthenticated"];
        };
    };
    listProvisioningTokens: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The tokens, newest first. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": {
                        tokens: components["schemas"]["ProvisioningToken"][];
                    };
                };
            };
            401: components["responses"]["Unauthenticated"];
        };
    };
    mintProvisioningToken: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description The token's name, the labels it assigns, and its lifetime. */
        requestBody: {
            content: {
                "application/json": {
                    labels?: components["schemas"]["LabelMap"];
                    name?: string;
                    /** @description Lifetime; the default (30 days) when zero or absent. */
                    ttl_seconds?: number;
                };
            };
        };
        responses: {
            /** @description The token, with its secret. */
            201: {
                headers: {
                    Location: components["headers"]["Location"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ProvisioningToken"] & {
                        /** @description The secret, prefixed `iw_`. Shown once. */
                        token: string;
                    };
                };
            };
            400: components["responses"]["Validation"];
            401: components["responses"]["Unauthenticated"];
        };
    };
    revokeProvisioningToken: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Revoked. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["AlreadyRevoked"];
        };
    };
    listRulesets: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The rulesets. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": {
                        rulesets: components["schemas"]["Ruleset"][];
                        state_version: components["schemas"]["StateVersion"];
                    };
                };
            };
            401: components["responses"]["Unauthenticated"];
        };
    };
    createRuleset: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: components["requestBodies"]["Ruleset"];
        responses: {
            /** @description The ruleset as persisted. */
            201: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    Location: components["headers"]["Location"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Ruleset"];
                };
            };
            400: components["responses"]["Validation"];
            401: components["responses"]["Unauthenticated"];
            409: components["responses"]["DuplicateName"];
        };
    };
    getRuleset: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The ruleset. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Ruleset"];
                };
            };
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
        };
    };
    putRuleset: {
        parameters: {
            query?: never;
            header: {
                /** @description The version the caller last read, as the resource's entity tag. Required on every update and delete. */
                "If-Match": components["parameters"]["ifMatch"];
            };
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        requestBody: components["requestBodies"]["Ruleset"];
        responses: {
            /** @description The ruleset as persisted. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Ruleset"];
                };
            };
            400: components["responses"]["Validation"];
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["DuplicateName"];
            412: components["responses"]["PreconditionFailed"];
            428: components["responses"]["PreconditionRequired"];
        };
    };
    deleteRuleset: {
        parameters: {
            query?: never;
            header: {
                /** @description The version the caller last read, as the resource's entity tag. Required on every update and delete. */
                "If-Match": components["parameters"]["ifMatch"];
            };
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Deleted. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
            412: components["responses"]["PreconditionFailed"];
            428: components["responses"]["PreconditionRequired"];
        };
    };
    createRule: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        requestBody: components["requestBodies"]["Rule"];
        responses: {
            /** @description The rule as persisted. */
            201: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    Location: components["headers"]["Location"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Rule"];
                };
            };
            400: components["responses"]["Validation"];
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
            412: components["responses"]["PreconditionFailed"];
        };
    };
    getRule: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
                /** @description The rule's id. */
                rule_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The rule. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Rule"];
                };
            };
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
        };
    };
    putRule: {
        parameters: {
            query?: never;
            header: {
                /** @description The version the caller last read, as the resource's entity tag. Required on every update and delete. */
                "If-Match": components["parameters"]["ifMatch"];
            };
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
                /** @description The rule's id. */
                rule_id: string;
            };
            cookie?: never;
        };
        requestBody: components["requestBodies"]["Rule"];
        responses: {
            /** @description The rule as persisted. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Rule"];
                };
            };
            400: components["responses"]["Validation"];
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
            412: components["responses"]["PreconditionFailed"];
            428: components["responses"]["PreconditionRequired"];
        };
    };
    deleteRule: {
        parameters: {
            query?: never;
            header: {
                /** @description The version the caller last read, as the resource's entity tag. Required on every update and delete. */
                "If-Match": components["parameters"]["ifMatch"];
            };
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
                /** @description The rule's id. */
                rule_id: string;
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Deleted. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
            412: components["responses"]["PreconditionFailed"];
            428: components["responses"]["PreconditionRequired"];
        };
    };
    previewSelector: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description The selector to resolve. */
        requestBody: {
            content: {
                "application/json": {
                    selector: components["schemas"]["Selector"];
                };
            };
        };
        responses: {
            /** @description The matched workloads. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": {
                        count: number;
                        matched: components["schemas"]["WorkloadRef"][];
                    };
                };
            };
            400: components["responses"]["Validation"];
            401: components["responses"]["Unauthenticated"];
        };
    };
    listServices: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The services. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": {
                        services: components["schemas"]["Service"][];
                    };
                };
            };
            401: components["responses"]["Unauthenticated"];
        };
    };
    createService: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: components["requestBodies"]["Service"];
        responses: {
            /** @description The service as persisted. */
            201: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    Location: components["headers"]["Location"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Service"];
                };
            };
            400: components["responses"]["Validation"];
            401: components["responses"]["Unauthenticated"];
            409: components["responses"]["DuplicateName"];
        };
    };
    getService: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The service. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Service"];
                };
            };
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
        };
    };
    putService: {
        parameters: {
            query?: never;
            header: {
                /** @description The version the caller last read, as the resource's entity tag. Required on every update and delete. */
                "If-Match": components["parameters"]["ifMatch"];
            };
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        requestBody: components["requestBodies"]["Service"];
        responses: {
            /** @description The service as persisted. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Service"];
                };
            };
            400: components["responses"]["Validation"];
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["DuplicateName"];
            412: components["responses"]["PreconditionFailed"];
            428: components["responses"]["PreconditionRequired"];
        };
    };
    deleteService: {
        parameters: {
            query?: never;
            header: {
                /** @description The version the caller last read, as the resource's entity tag. Required on every update and delete. */
                "If-Match": components["parameters"]["ifMatch"];
            };
            path: {
                /** @description The object's id. */
                id: components["parameters"]["id"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Deleted. */
            204: {
                headers: {
                    [name: string]: unknown;
                };
                content?: never;
            };
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InUse"];
            412: components["responses"]["PreconditionFailed"];
            428: components["responses"]["PreconditionRequired"];
        };
    };
    createSession: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /** @description The operator password. */
        requestBody: {
            content: {
                "application/json": {
                    password: string;
                };
            };
        };
        responses: {
            /** @description Logged in; the cookie is set. */
            200: {
                headers: {
                    "Set-Cookie"?: string;
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Me"];
                };
            };
            400: components["responses"]["InvalidRequest"];
            /** @description The password is not correct (`invalid-credentials`). */
            401: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["Problem"];
                };
            };
            /** @description No password has been set (`no-password`), or the request was cross-origin (`cross-origin`). */
            403: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["Problem"];
                };
            };
            /** @description Too many attempts from this source in the window (`too-many-attempts`). */
            429: {
                headers: {
                    "Retry-After"?: number;
                    [name: string]: unknown;
                };
                content: {
                    "application/problem+json": components["schemas"]["Problem"];
                };
            };
        };
    };
    deleteSession: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Logged out. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": Record<string, never>;
                };
            };
            401: components["responses"]["Unauthenticated"];
        };
    };
    listWorkloads: {
        parameters: {
            query?: {
                /** @description The `next_cursor` of the previous page. */
                cursor?: components["parameters"]["cursor"];
                /** @description A `key=value` requirement; repeat to AND keys, repeat a key to OR its values. Restricts to the workloads the selector currently matches. */
                label?: components["parameters"]["label"];
                /** @description Page size; the read model's default and maximum apply. */
                limit?: number;
                /** @description Restrict to workloads in this enforcement mode. */
                mode?: components["schemas"]["Mode"];
                /** @description Restrict to workloads in this sync state. */
                sync_state?: components["schemas"]["SyncState"];
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One page of workloads. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": {
                        next_cursor: string | null;
                        workloads: components["schemas"]["Workload"][];
                    };
                };
            };
            400: components["responses"]["InvalidParameter"];
            401: components["responses"]["Unauthenticated"];
        };
    };
    getWorkload: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The workload's id. */
                id: components["parameters"]["workloadID"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The workload. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Workload"];
                };
            };
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
        };
    };
    getWorkloadLabels: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The workload's id. */
                id: components["parameters"]["workloadID"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The labels and their version. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Labels"];
                };
            };
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
        };
    };
    putWorkloadLabels: {
        parameters: {
            query?: never;
            header: {
                /** @description The version the caller last read, as the resource's entity tag. Required on every update and delete. */
                "If-Match": components["parameters"]["ifMatch"];
            };
            path: {
                /** @description The workload's id. */
                id: components["parameters"]["workloadID"];
            };
            cookie?: never;
        };
        /** @description The complete label set that replaces the current one. */
        requestBody: {
            content: {
                "application/json": {
                    labels: components["schemas"]["LabelMap"];
                };
            };
        };
        responses: {
            /** @description The labels as persisted, with their new version. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["Labels"];
                };
            };
            400: components["responses"]["Validation"];
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
            412: components["responses"]["PreconditionFailed"];
            428: components["responses"]["PreconditionRequired"];
        };
    };
    getRenderedPolicy: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The workload's id. */
                id: components["parameters"]["workloadID"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The rendered policy. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RenderedPolicy"];
                };
            };
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
        };
    };
    resendSnapshot: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The workload's id. */
                id: components["parameters"]["workloadID"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The directive was fired; the snapshot instant as it stood when it was. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ResendSnapshotAck"];
                };
            };
            401: components["responses"]["Unauthenticated"];
            404: components["responses"]["NotFound"];
            /** @description The workload's agent is offline as last recorded (`agent-offline`); nothing was sent. `last_seen_at` is when it was last heard from, null when never. */
            409: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    /**
                     * @example {
                     *       "type": "urn:innerwall:problem:agent-offline",
                     *       "title": "Agent offline",
                     *       "status": 409,
                     *       "last_seen_at": "2026-09-24T18:02:11Z"
                     *     }
                     */
                    "application/problem+json": components["schemas"]["Problem"];
                };
            };
        };
    };
}
