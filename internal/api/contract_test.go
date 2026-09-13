package api_test

import (
	"os"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/innerwall-dev/innerwall/internal/api"
	"github.com/innerwall-dev/innerwall/internal/flowstore"
)

// contract is the hand-authored operator surface contract, as far as
// this test reads it.
type contract struct {
	Paths      map[string]map[string]any `yaml:"paths"`
	Components struct {
		Schemas map[string]struct {
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
		api.ProblemMatchCountMismatch, api.ProblemDuplicateName, api.ProblemInUse, api.ProblemAlreadyRevoked, api.ProblemNotFound, api.ProblemMethodNotAllowed, api.ProblemInternal,
	}
	sort.Strings(problemTypes)
	sort.Strings(code)
	if strings.Join(problemTypes, "\n") != strings.Join(code, "\n") {
		t.Fatalf("problem types documented:\n%s\nin code:\n%s", strings.Join(problemTypes, "\n"), strings.Join(code, "\n"))
	}
}
