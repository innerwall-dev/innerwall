package api_test

import (
	"os"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/innerwall-dev/innerwall/internal/api"
	"github.com/innerwall-dev/innerwall/internal/flowstore"
	"github.com/innerwall-dev/innerwall/internal/readmodel"
)

// contract is the hand-authored operator surface contract, as far as
// this test reads it.
type contract struct {
	Paths      map[string]map[string]any `yaml:"paths"`
	Components struct {
		Schemas map[string]struct {
			Enum       []string `yaml:"enum"`
			Properties map[string]struct {
				Enum []string `yaml:"enum"`
			} `yaml:"properties"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

func loadContract(t *testing.T) *contract {
	t.Helper()
	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var c contract
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatalf("api/openapi.yaml does not parse: %v", err)
	}
	return &c
}

// TestContractCoversTheMountedRoutes checks the contract against what the
// surface mounts, in both directions: every mounted method and path is
// documented, and nothing is documented that is not mounted. The spec
// linter checks the document on its own; this is the one place the
// document and the implementation meet.
func TestContractCoversTheMountedRoutes(t *testing.T) {
	c := loadContract(t)
	documented := map[string]bool{}
	for path, item := range c.Paths {
		for key := range item {
			switch key {
			case "get", "put", "post", "delete", "patch":
				documented[strings.ToUpper(key)+" "+api.APIPrefix+path] = true
			}
		}
	}
	mounted := api.New(api.Deps{}).Routes()
	for _, route := range mounted {
		if !documented[route] {
			t.Errorf("mounted but not in api/openapi.yaml: %s", route)
		}
	}
	mountedSet := map[string]bool{}
	for _, route := range mounted {
		mountedSet[route] = true
	}
	docs := make([]string, 0, len(documented))
	for route := range documented {
		docs = append(docs, route)
	}
	sort.Strings(docs)
	for _, route := range docs {
		if !mountedSet[route] {
			t.Errorf("in api/openapi.yaml but not mounted: %s", route)
		}
	}
}

// TestContractClosedSets checks the enumerations the contract documents
// as closed against the code that defines them.
func TestContractClosedSets(t *testing.T) {
	c := loadContract(t)
	rollup := c.Paths["/flows/rollup"]["get"].(map[string]any)
	var groupBy []string
	for _, p := range rollup["parameters"].([]any) {
		pm := p.(map[string]any)
		if pm["name"] == "group_by" {
			for _, v := range pm["schema"].(map[string]any)["enum"].([]any) {
				groupBy = append(groupBy, v.(string))
			}
		}
	}
	want := make([]string, 0, len(flowstore.GroupBys))
	for _, g := range flowstore.GroupBys {
		want = append(want, string(g))
	}
	if strings.Join(groupBy, "|") != strings.Join(want, "|") {
		t.Fatalf("group_by enum = %v, code = %v", groupBy, want)
	}

	problemTypes := c.Components.Schemas["Problem"].Properties["type"].Enum
	code := []string{
		api.ProblemUnauthenticated, api.ProblemInvalidCredentials, api.ProblemNoPassword, api.ProblemCrossOrigin, api.ProblemTooManyAttempts,
		api.ProblemInvalidRequest, api.ProblemInvalidParameter, api.ProblemValidation, api.ProblemPreconditionRequired, api.ProblemPreconditionFailed,
		api.ProblemMatchCountMismatch, api.ProblemDuplicateName, api.ProblemInUse, api.ProblemAlreadyRevoked, api.ProblemAgentOffline, api.ProblemWorkloadRetired, api.ProblemNotFound, api.ProblemMethodNotAllowed, api.ProblemInternal,
	}
	sort.Strings(problemTypes)
	sort.Strings(code)
	if strings.Join(problemTypes, "\n") != strings.Join(code, "\n") {
		t.Fatalf("problem types documented:\n%s\nin code:\n%s", strings.Join(problemTypes, "\n"), strings.Join(code, "\n"))
	}

	lifecycle := c.Components.Schemas["Lifecycle"].Enum
	if want := []string{string(readmodel.LifecycleActive), string(readmodel.LifecycleRetired)}; strings.Join(lifecycle, "|") != strings.Join(want, "|") {
		t.Fatalf("Lifecycle enum = %v, code = %v", lifecycle, want)
	}

	// The credential's lifecycle is nested in the workload, below what
	// the typed contract reads.
	var nested struct {
		Components struct {
			Schemas struct {
				Workload struct {
					Properties struct {
						Health struct {
							Properties struct {
								Credential struct {
									Properties struct {
										Lifecycle struct {
											Enum []string `yaml:"enum"`
										} `yaml:"lifecycle"`
									} `yaml:"properties"`
								} `yaml:"credential"`
							} `yaml:"properties"`
						} `yaml:"health"`
					} `yaml:"properties"`
				} `yaml:"Workload"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &nested); err != nil {
		t.Fatal(err)
	}
	credLifecycle := nested.Components.Schemas.Workload.Properties.Health.Properties.Credential.Properties.Lifecycle.Enum
	if want := []string{string(readmodel.CredentialValid), string(readmodel.CredentialInGrace), string(readmodel.CredentialPastGrace)}; strings.Join(credLifecycle, "|") != strings.Join(want, "|") {
		t.Fatalf("credential lifecycle enum = %v, code = %v", credLifecycle, want)
	}
}

// TestContractCredentialBlock checks the workload read's credential block
// against what the surface encodes, in both directions: every key the
// contract requires of it, and of the presented credential nested in it,
// is the set the encoder writes. The issued and the presented credential
// share their key names, so a key landing on the wrong one shows here.
func TestContractCredentialBlock(t *testing.T) {
	type object struct {
		Required   []string             `yaml:"required"`
		Properties map[string]yaml.Node `yaml:"properties"`
	}
	var doc struct {
		Components struct {
			Schemas struct {
				Workload struct {
					Properties struct {
						Health struct {
							Properties struct {
								Credential struct {
									Required   []string `yaml:"required"`
									Properties struct {
										LastPresented object `yaml:"last_presented"`
									} `yaml:"properties"`
								} `yaml:"credential"`
							} `yaml:"properties"`
						} `yaml:"health"`
					} `yaml:"properties"`
				} `yaml:"Workload"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	cred := doc.Components.Schemas.Workload.Properties.Health.Properties.Credential
	presented := cred.Properties.LastPresented

	// web-1 in the read fixture has a presented credential.
	s := newReadSurface(t)
	_, body := s.get(t, "/api/v1/workloads/"+s.fx.web.String())
	encoded, _ := field(body, "health.credential").(map[string]any)
	encodedPresented, _ := encoded["last_presented"].(map[string]any)
	if encodedPresented == nil {
		t.Fatalf("fixture credential carries no presented credential: %v", encoded)
	}
	for what, pair := range map[string][2][]string{
		"credential":                {cred.Required, keys(encoded)},
		"credential.last_presented": {presented.Required, keys(encodedPresented)},
	} {
		documented, written := append([]string(nil), pair[0]...), pair[1]
		sort.Strings(documented)
		if strings.Join(documented, "|") != strings.Join(written, "|") {
			t.Errorf("%s required = %v, encoded = %v", what, documented, written)
		}
	}
	for _, k := range presented.Required {
		if _, ok := presented.Properties[k]; !ok {
			t.Errorf("credential.last_presented requires %q and does not describe it", k)
		}
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestContractDeclaresTheOriginGuard checks that every operation that is
// not a read declares the 403 the shared origin guard returns for a
// cross-site request: the guard sits in front of every unsafe method, so
// the refusal is part of each one's contract, not only of the prose.
func TestContractDeclaresTheOriginGuard(t *testing.T) {
	c := loadContract(t)
	for path, item := range c.Paths {
		for method, op := range item {
			switch method {
			case "post", "put", "delete", "patch":
			default:
				continue
			}
			responses, _ := op.(map[string]any)["responses"].(map[string]any)
			if _, ok := responses["403"]; !ok {
				t.Errorf("%s %s declares no 403 for the origin guard", strings.ToUpper(method), path)
			}
		}
	}
}
