# Labeling strategy

Policy is written between label sets, so the labels a workload carries decide which rules it holds and which peers it is. This guide covers the label grammar, where labels enter, and how to shape a label scheme around the tiers you protect. The model behind it is in [docs/policy.md](../policy.md); the grammar's decision is ADR-0022.

## The grammar

A label is `key=value`, in one grammar wherever a label enters:

- **Key:** 1 to 63 ASCII letters, digits, `.`, `_`, `-`, or `/`.
- **Value:** 1 to 63 letters, digits, `.`, `_`, or `-`.
- **Both** begin and end with a letter or digit.

A workload holds at most one value for each key. Because neither part can contain whitespace, `=`, or `|`, every label can be written back as the `key=value` text it was entered as, and text such as `app=web env=lab` is always two labels, never one.

The grammar is admitted at every entry point: a provisioning token's labels at mint, a label edit (console or `innerwall workload set-labels`), a ruleset's scope and its workload peers, a mode change's selector, and the `label` filters of the reads. Text outside it is refused with a finding that quotes the offending key or value, so whitespace in it shows. A label stored before the grammar existed and outside it is drawn quoted in the console; repair it in the workload's label editor. Enrollment refuses a provisioning token whose stored labels fall outside the grammar: mint a new token.

## Where labels come from

**At enrollment, from the token.** A provisioning token carries labels, and every workload it enrolls is assigned them:

```sh
innerwall token mint --name web-prod --label role=web --label env=prod
```

Minting one token per tier you protect means each host enrolls already labeled for its tier, and nothing about it is left to edit by hand afterwards.

**Later, by a label edit.** A label edit replaces the workload's whole label set. In the console, a workload's labels are edited in place on its page, as a write conditioned on the set you read, with a review of what changes before it is saved. On the command line:

```sh
innerwall workload set-labels --label role=web --label env=prod <workload-id>
```

> [!IMPORTANT]
> A label edit is a policy change. It moves the workload into or out of ruleset scopes, so the rules it holds change, and it moves the workload into or out of workload peers, so the addresses other workloads admit change. Both render and reach the agents at once, the same as a ruleset write.

## Labels that select what you protect

A rule names no destination: a ruleset's scope is the destination of every rule in it, and the intended shape is one ruleset per protected tier ([docs/policy.md](../policy.md#read-this-first-the-scope-is-the-destination)). So the first question a label scheme answers is: **what single requirement selects exactly one tier?**

A scheme that answers it carries one label per question policy asks:

| Key | Answers | Example values |
|---|---|---|
| `role` | What tier is this workload? The scope of that tier's ruleset, and the workload peer other tiers admit. | `web`, `api`, `db`, `bastion` |
| `env` | Which environment? Narrows a tier to one environment, and separates environments that must not reach each other. | `prod`, `staging` |
| `app` | Which application, where several share an estate? Narrows a tier to one application's instances. | `billing`, `search` |

With it, each protected tier is selected by one requirement or a small AND of them:

- `role=db` selects every database; `role=db env=prod` selects the production databases. Keys in a selector are ANDed.
- `role=db|cache` selects both tiers. Values listed for one key are ORed.
- A selector with no requirements matches nothing, so an empty scope protects no workload rather than all of them.

**Label what a workload is, not what it may reach.** A label such as `can-reach=db` describes an edge, and edges are what rules say. A workload labeled for what it is (`role=api`) can be named as a peer by any tier that admits it, and a new tier admits it without relabeling anything.

**Keep values stable.** An address never enters policy: a workload peer resolves to the current addresses of the workloads it matches and follows them as they change. A label that changes with the host (an address, a hostname that is rebuilt) brings back the churn labels exist to remove.

**Check a selector before you rely on it.** The policy editor's scope card shows how many workloads the scope matches as you edit it, resolved by the control plane's own match, before anything is saved; the dry run then lists every workload whose rendered policy would change. A tier label that matches more workloads than the tier holds shows up there first.

## Next

[Writing the first ruleset](first-ruleset.md) turns a tier label into a ruleset scope.
