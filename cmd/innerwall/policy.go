package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/innerwall-dev/innerwall/internal/compiler"
	"github.com/innerwall-dev/innerwall/internal/identity"
	"github.com/innerwall-dev/innerwall/internal/policy"
	"github.com/innerwall-dev/innerwall/internal/store"
)

// authoring opens the store and builds the authoring surface over it. Every
// mutation made through it is admitted, persisted, and rendered; the render
// announces changed workloads to the running control plane over the
// database, so this process never talks to the gateway directly.
type authoring struct {
	st  *store.Store
	svc *policy.Authoring
	eng *compiler.Engine
}

func openAuthoring(ctx context.Context, flagURL string) (*authoring, error) {
	st, err := openStore(ctx, flagURL)
	if err != nil {
		return nil, err
	}
	eng := &compiler.Engine{Store: st}
	return &authoring{st: st, svc: &policy.Authoring{Store: st, Renderer: renderAdapter{eng}}, eng: eng}, nil
}

func (a *authoring) close() { a.st.Close() }

// renderAdapter narrows the engine to the authoring service's interface.
type renderAdapter struct{ eng *compiler.Engine }

func (r renderAdapter) Render(ctx context.Context) error {
	rep, err := r.eng.Render(ctx)
	if err != nil {
		return err
	}
	if len(rep.Changed) > 0 {
		fmt.Fprintf(os.Stderr, "rendered %d workloads; %d changed\n", rep.Workloads, len(rep.Changed))
	}
	return nil
}

// names loads the name index for document conversion.
func (a *authoring) names(ctx context.Context) (policy.Names, error) {
	services, err := a.st.ListServices(ctx)
	if err != nil {
		return policy.Names{}, err
	}
	groups, err := a.st.ListAddressGroups(ctx)
	if err != nil {
		return policy.Names{}, err
	}
	return policy.NewNames(services, groups), nil
}

// stringFlags collects a repeatable string flag.
type stringFlags []string

func (s *stringFlags) String() string     { return strings.Join(*s, ",") }
func (s *stringFlags) Set(v string) error { *s = append(*s, v); return nil }

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func usageError(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

// --- service ---------------------------------------------------------------

func runService(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError("usage: innerwall service create|update|delete|get|list [flags]")
	}
	switch args[0] {
	case "create", "update":
		return runServiceWrite(ctx, args[0], args[1:])
	case "delete":
		return runServiceDelete(ctx, args[1:])
	case "get":
		return runServiceGet(ctx, args[1:])
	case "list":
		return runServiceList(ctx, args[1:])
	default:
		return usageError("unknown service command %q (create|update|delete|get|list)", args[0])
	}
}

func runServiceWrite(ctx context.Context, verb string, args []string) error {
	fs := flag.NewFlagSet("innerwall service "+verb, flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	name := fs.String("name", "", "service name")
	var entries stringFlags
	fs.Var(&entries, "entry", "protocol and ports, e.g. tcp:5432 or tcp:6000-6010,7000 or icmp (repeatable; replaces all entries on update)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	svc := &policy.Service{Name: *name}
	for _, spec := range entries {
		e, err := policy.ParseEntrySpec(spec)
		if err != nil {
			return err
		}
		svc.Entries = append(svc.Entries, e)
	}
	a, err := openAuthoring(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer a.close()
	if verb == "create" {
		if err := a.svc.CreateService(ctx, svc); err != nil {
			return err
		}
		fmt.Printf("service %s (%s) created\n", svc.ID, svc.Name)
		return nil
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall service update <id|name> [flags]")
	}
	existing, err := a.lookupService(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	svc.ID = existing.ID
	if svc.Name == "" {
		svc.Name = existing.Name
	}
	if len(svc.Entries) == 0 {
		svc.Entries = existing.Entries
	}
	if err := a.svc.UpdateService(ctx, svc); err != nil {
		return err
	}
	fmt.Printf("service %s (%s) updated\n", svc.ID, svc.Name)
	return nil
}

func (a *authoring) lookupService(ctx context.Context, ref string) (*policy.Service, error) {
	names, err := a.names(ctx)
	if err != nil {
		return nil, err
	}
	id, ok := policy.ResolveID(ref, names.ServiceByName)
	if !ok {
		return nil, fmt.Errorf("%w: %q", policy.ErrServiceUnknown, ref)
	}
	return a.st.GetService(ctx, id)
}

func runServiceDelete(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall service delete", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall service delete <id|name>")
	}
	a, err := openAuthoring(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer a.close()
	svc, err := a.lookupService(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if err := a.svc.DeleteService(ctx, svc.ID); err != nil {
		return err
	}
	fmt.Printf("service %s (%s) deleted\n", svc.ID, svc.Name)
	return nil
}

func runServiceGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall service get", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall service get <id|name>")
	}
	a, err := openAuthoring(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer a.close()
	svc, err := a.lookupService(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	return printJSON(serviceDoc(svc))
}

type serviceJSON struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Entries []string `json:"entries"`
}

func serviceDoc(s *policy.Service) serviceJSON {
	d := serviceJSON{ID: s.ID.String(), Name: s.Name, Entries: []string{}}
	for _, e := range s.Entries {
		d.Entries = append(d.Entries, policy.FormatEntrySpec(e))
	}
	return d
}

func runServiceList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall service list", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	services, err := st.ListServices(ctx)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tNAME\tENTRIES")
	for i := range services {
		d := serviceDoc(&services[i])
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", d.ID, d.Name, strings.Join(d.Entries, " "))
	}
	return w.Flush()
}

// --- address-group -----------------------------------------------------------

func runAddressGroup(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError("usage: innerwall address-group create|update|delete|get|list [flags]")
	}
	switch args[0] {
	case "create", "update":
		return runAddressGroupWrite(ctx, args[0], args[1:])
	case "delete":
		return runAddressGroupDelete(ctx, args[1:])
	case "get":
		return runAddressGroupGet(ctx, args[1:])
	case "list":
		return runAddressGroupList(ctx, args[1:])
	default:
		return usageError("unknown address-group command %q (create|update|delete|get|list)", args[0])
	}
}

func runAddressGroupWrite(ctx context.Context, verb string, args []string) error {
	fs := flag.NewFlagSet("innerwall address-group "+verb, flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	name := fs.String("name", "", "address group name")
	var cidrs stringFlags
	fs.Var(&cidrs, "cidr", "member CIDR (repeatable; replaces all members on update)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	g := &policy.AddressGroup{Name: *name, CIDRs: cidrs}
	a, err := openAuthoring(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer a.close()
	if verb == "create" {
		if err := a.svc.CreateAddressGroup(ctx, g); err != nil {
			return err
		}
		fmt.Printf("address group %s (%s) created\n", g.ID, g.Name)
		return nil
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall address-group update <id|name> [flags]")
	}
	existing, err := a.lookupAddressGroup(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	g.ID = existing.ID
	if g.Name == "" {
		g.Name = existing.Name
	}
	if len(g.CIDRs) == 0 {
		g.CIDRs = existing.CIDRs
	}
	if err := a.svc.UpdateAddressGroup(ctx, g); err != nil {
		return err
	}
	fmt.Printf("address group %s (%s) updated\n", g.ID, g.Name)
	return nil
}

func (a *authoring) lookupAddressGroup(ctx context.Context, ref string) (*policy.AddressGroup, error) {
	names, err := a.names(ctx)
	if err != nil {
		return nil, err
	}
	id, ok := policy.ResolveID(ref, names.AddressGroupByName)
	if !ok {
		return nil, fmt.Errorf("%w: %q", policy.ErrAddressGroupUnknown, ref)
	}
	return a.st.GetAddressGroup(ctx, id)
}

func runAddressGroupDelete(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall address-group delete", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall address-group delete <id|name>")
	}
	a, err := openAuthoring(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer a.close()
	g, err := a.lookupAddressGroup(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if err := a.svc.DeleteAddressGroup(ctx, g.ID); err != nil {
		return err
	}
	fmt.Printf("address group %s (%s) deleted\n", g.ID, g.Name)
	return nil
}

type addressGroupJSON struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	CIDRs []string `json:"cidrs"`
}

func runAddressGroupGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall address-group get", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall address-group get <id|name>")
	}
	a, err := openAuthoring(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer a.close()
	g, err := a.lookupAddressGroup(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	return printJSON(addressGroupJSON{ID: g.ID.String(), Name: g.Name, CIDRs: g.CIDRs})
}

func runAddressGroupList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall address-group list", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	groups, err := st.ListAddressGroups(ctx)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tNAME\tCIDRS")
	for _, g := range groups {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", g.ID, g.Name, strings.Join(g.CIDRs, " "))
	}
	return w.Flush()
}

// --- ruleset -----------------------------------------------------------------

func runRuleset(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError("usage: innerwall ruleset create|update|delete|get|list [flags]")
	}
	switch args[0] {
	case "create", "update":
		return runRulesetWrite(ctx, args[0], args[1:])
	case "delete":
		return runRulesetDelete(ctx, args[1:])
	case "get":
		return runRulesetGet(ctx, args[1:])
	case "list":
		return runRulesetList(ctx, args[1:])
	default:
		return usageError("unknown ruleset command %q (create|update|delete|get|list)", args[0])
	}
}

func readDocument(path string) ([]byte, error) {
	if path == "" {
		return nil, policy.ErrNoDocument
	}
	if path == "-" {
		return readAll(os.Stdin)
	}
	return os.ReadFile(path) //nolint:gosec // operator-supplied path
}

func readAll(f *os.File) ([]byte, error) {
	var out []byte
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			if errors.Is(err, os.ErrClosed) || err.Error() == "EOF" {
				return out, nil
			}
			return nil, err
		}
	}
}

func runRulesetWrite(ctx context.Context, verb string, args []string) error {
	fs := flag.NewFlagSet("innerwall ruleset "+verb, flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	file := fs.String("f", "", "ruleset document (JSON); - for stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	data, err := readDocument(*file)
	if err != nil {
		return err
	}
	a, err := openAuthoring(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer a.close()
	names, err := a.names(ctx)
	if err != nil {
		return err
	}
	rs, err := policy.DecodeRuleset(data, names)
	if err != nil {
		return err
	}
	if verb == "create" {
		if err := a.svc.CreateRuleset(ctx, rs); err != nil {
			return err
		}
		fmt.Printf("ruleset %s (%s) created with %d rules\n", rs.ID, rs.Name, len(rs.Rules))
		return nil
	}
	if fs.NArg() == 1 {
		existing, err := a.lookupRuleset(ctx, fs.Arg(0))
		if err != nil {
			return err
		}
		rs.ID = existing.ID
	}
	if rs.ID == uuid.Nil {
		return usageError("usage: innerwall ruleset update <id|name> -f <document>, or set \"id\" in the document")
	}
	if err := a.svc.UpdateRuleset(ctx, rs); err != nil {
		return err
	}
	fmt.Printf("ruleset %s (%s) updated with %d rules\n", rs.ID, rs.Name, len(rs.Rules))
	return nil
}

func (a *authoring) lookupRuleset(ctx context.Context, ref string) (*policy.Ruleset, error) {
	if id, err := uuid.Parse(ref); err == nil {
		return a.st.GetRuleset(ctx, id)
	}
	all, err := a.st.ListRulesets(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == ref {
			return &all[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %q", policy.ErrRulesetUnknown, ref)
}

func runRulesetDelete(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall ruleset delete", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall ruleset delete <id|name>")
	}
	a, err := openAuthoring(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer a.close()
	rs, err := a.lookupRuleset(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	if err := a.svc.DeleteRuleset(ctx, rs.ID); err != nil {
		return err
	}
	fmt.Printf("ruleset %s (%s) deleted\n", rs.ID, rs.Name)
	return nil
}

func runRulesetGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall ruleset get", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall ruleset get <id|name>")
	}
	a, err := openAuthoring(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer a.close()
	rs, err := a.lookupRuleset(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	names, err := a.names(ctx)
	if err != nil {
		return err
	}
	return printJSON(policy.RulesetToDoc(rs, names))
}

func runRulesetList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall ruleset list", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	rulesets, err := st.ListRulesets(ctx)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tNAME\tENABLED\tRULES\tSCOPE")
	for i := range rulesets {
		rs := &rulesets[i]
		_, _ = fmt.Fprintf(w, "%s\t%s\t%t\t%d\t%s\n", rs.ID, rs.Name, rs.Enabled, len(rs.Rules), selectorString(rs.Scope))
	}
	return w.Flush()
}

func selectorString(s policy.Selector) string {
	parts := make([]string, 0, len(s))
	for _, k := range sortedSelectorKeys(s) {
		parts = append(parts, k+"="+strings.Join(s[k], "|"))
	}
	return strings.Join(parts, ",")
}

func sortedSelectorKeys(s policy.Selector) []string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// --- policy ------------------------------------------------------------------

func runPolicy(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError("usage: innerwall policy render|show [flags]")
	}
	switch args[0] {
	case "render":
		return runPolicyRender(ctx, args[1:])
	case "show":
		return runPolicyShow(ctx, args[1:])
	default:
		return usageError("unknown policy command %q (render|show)", args[0])
	}
}

// runPolicyRender forces a render. Every mutation already renders; this
// exists for recovery after a render failed and for inspection.
func runPolicyRender(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall policy render", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	a, err := openAuthoring(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer a.close()
	rep, err := a.eng.Render(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("rendered %d workloads; %d changed\n", rep.Workloads, len(rep.Changed))
	for _, c := range rep.Changed {
		fmt.Printf("  %s -> version %d (%d changes)\n", c.ID, c.Version, c.Changes)
	}
	return nil
}

func runPolicyShow(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("innerwall policy show", flag.ContinueOnError)
	dbURL := fs.String("database-url", "", "Postgres connection string (default $"+envDatabaseURL+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError("usage: innerwall policy show <workload-id>")
	}
	id, err := identity.ParseWorkloadID(fs.Arg(0))
	if err != nil {
		return err
	}
	st, err := openStore(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	p, err := st.GetWorkloadPolicy(ctx, id)
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("no rendered policy for %s", id)
	}
	out, err := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(p)
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
