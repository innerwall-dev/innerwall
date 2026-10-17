# Policy

How Innerwall policy is shaped, what a workload ends up holding, and how to author it without surprises. The decisions behind it are ADR-0010 (inbound only), ADR-0018 (rendering and versions), ADR-0020 (what the kernel holds), and ADR-0022 (the label grammar).

## Read this first: the scope is the destination

- **A rule has no destination selector.** A rule names its peers (who may connect) and its services (on what). That is all it names.
- **The ruleset's scope is the destination.** Every rule in a ruleset applies to every workload the ruleset's scope matches, and to no other.
- **One ruleset per protected tier is the intended shape.** To admit `tcp/5432` to the database tier only, write a ruleset whose scope is the database tier (`role=db`) and put the rule there. The same rule in a ruleset scoped to the whole environment (`env=prod`) admits `tcp/5432` on every workload in `env=prod`.
- **The dry run is the safety net.** Before anything is written, it lists every workload whose rendered policy would change and the rules it would gain or lose. A rule landing wider than intended shows up there as more workloads than the tier holds.

## The model

**Workloads carry labels.** A label is `key=value`, in the one grammar ADR-0022 defines: a key of 1 to 63 ASCII letters, digits, `.`, `_`, `-`, or `/`, a value of 1 to 63 letters, digits, `.`, `_`, or `-`, each beginning and ending with a letter or digit. Policy is written between label sets; addresses are an output of rendering, never an input.

**A selector matches workloads by label.** Every key it names must be present (AND), and the workload's value for that key must be one of the values listed (OR). `role=db env=prod` matches the production databases; `role=db|cache` matches both tiers. A selector with no requirements matches nothing, so an empty scope protects no workload rather than all of them.

**A ruleset is a scope and the rules that apply under it.** It has a name, a description, an enabled switch, a scope (a selector), and its rules. A disabled ruleset contributes nothing to any workload.

**A rule admits.** It has a direction, an enabled switch, a description, peers, and services:

- *Direction* is `inbound` and nothing else in this version (ADR-0010). A rule constrains what may reach the workloads in scope; nothing constrains what they send.
- *Peers* are the remote end, the sources a connection may come from: workloads by selector, a named address group, or a literal CIDR. A workload peer resolves to the current addresses of the workloads it matches, and follows them as they change.
- *Services* are the local ports: named service definitions, inline entries (`tcp` with ports or port ranges, `udp` likewise, `icmp`), or both.
- A disabled rule admits nothing while off and keeps its place in the ruleset.

There is no deny rule. A rule only admits; what decides the rest is the workload's mode.

**What a workload holds.** A workload's rendered policy is the union of the enabled rules of every enabled ruleset whose scope matches it, with peers resolved to addresses. Its version advances only when that rendered output changes (ADR-0018). Loopback and established connections are accepted before any rule. A new inbound connection the rules do not admit meets the mode's terminal verdict (ADR-0020):

| Mode | Terminal verdict | Recorded as |
|---|---|---|
| `visibility` | none: the rules are not installed and nothing is dropped | `observed` |
| `simulation` | accept, and record what enforcement would drop | `would_block` |
| `enforced` | drop | `blocked` |

A workload that no scope matches holds no rules. In visibility that changes nothing; in simulation every new inbound connection to it is `would_block`; enforced, every one is dropped. The simulation review reports exactly this before enforcement is turned on.

## Shaping rulesets

**One ruleset per protected tier.** For a three-tier application:

| Ruleset | Scope (the destination) | Rules (peers → services) |
|---|---|---|
| `web-inbound` | `role=web` | address group `corp` → `tcp/443` |
| `api-inbound` | `role=api` | `role=web` → `tcp/8080` |
| `db-inbound` | `role=db` | `role=api` → service `postgres` |

Each tier's ruleset says who may reach that tier and on what. Reading a ruleset answers one question, "what may connect to these workloads", and nothing in one tier's ruleset can open a port on another tier.

**The shape that goes wrong.** One ruleset scoped `env=prod` holding all three rules reads like a list of edges (corp → web, web → api, api → db), but a rule has no destination: every workload in `env=prod` gains all three rules. The database then admits `tcp/443` from `corp`, and the web servers admit `postgres` from the API tier. The dry run shows it: every `env=prod` workload listed as gaining every rule.

**Narrow with the scope.** To admit something to fewer workloads, the scope is what narrows: add a requirement (`role=db env=prod` rather than `role=db`), or move the rule into a ruleset whose scope is exactly the workloads it should reach. A tier that admits several peers on different services holds several rules in its one ruleset.

**Broad scopes are for admissions that really are estate-wide.** A ruleset scoped `env=prod` that admits `tcp/22` from `role=bastion` is the right shape when every production workload should accept SSH from the bastion: the destination really is every workload in scope. A workload in several scopes holds the union of their rules, so such a ruleset composes with each tier's own.

## Authoring

Policy is edited live: there is no draft and no estate-wide policy version. Every write is conditioned on the version its author read, and a write against a version that has moved is refused with the current one. The console's rule and scope saves say how many workloads they apply to, and apply at once. The console writes services and address groups from the policy rail's definitions lists. The command line writes the same objects (`innerwall ruleset create -f`, `ruleset update`, `service create`, `address-group create`; see [the command-line reference](reference/innerwall-cli.md#ruleset)) through the same domain functions, under the same admission. Writing a tier's first ruleset step by step, including the peer input's three forms, is in [writing the first ruleset](guides/first-ruleset.md).

**The dry run.** In the policy editor, **Dry run** submits every ruleset as read, with the unsaved edits in place, and shows each workload whose rendered policy would change: the rules it would gain, lose, or see changed. It never writes and never gates a save. Run it after editing a scope or a rule and before saving: if the list holds workloads outside the tier, the rule is in the wrong ruleset or the scope is too broad.

**Save ordering.** Saves are independent writes, so order matters when a scope edit and a rule edit are both unsaved: a rule saved on its own applies at once under the scope *as saved*, which is not the combination the dry run previewed. The edited row's save therefore writes the scope and the rule together, as one ruleset write conditioned on its version (**Save rule and scope**), which is exactly the set the dry run of the two previews. Saving the scope on its own stays offered and is safe.

**The rules table.** Each row of a ruleset's rules is one rule:

| Column | What it is |
|---|---|
| Enabled | The rule's enabled switch. It is not a destination: the scope above the table is the destination of every row. |
| Peers (may connect) | The sources the rule admits |
| Services (on) | The ports and protocols it admits them on |
| Description | The rule's description and short id |
| Simulation · last 24h | The connections the rule allowed over the last day across the workloads in scope, and on how many workloads it matched them |

The table's heading says the same thing in one line: inbound rules, who may connect to the scoped workloads, on what.

## From authoring to enforcement

Every workload starts in visibility. The path is: write a ruleset per tier, dry-run it, move the tier's workloads to simulation, read the simulation review (the real, recent connections that would have been blocked, beside any gaps in the evidence), and promote to enforcement when the review says it is safe, or with an explicit acknowledgment when it does not (ADR-0001). Reverting a change is another live edit. Each step has its guide: [labeling strategy](guides/labeling.md), [writing the first ruleset](guides/first-ruleset.md), [reading a simulation review](guides/simulation-review.md), and [promotion and enforcement](guides/promotion-and-enforcement.md).
