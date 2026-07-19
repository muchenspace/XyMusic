package contracts_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

type legacyManifest struct {
	SchemaVersion        int                  `json:"schemaVersion"`
	ContractName         string               `json:"contractName"`
	AuthoritativeSources authoritativeSources `json:"authoritativeSources"`
	CountingRules        countingRules        `json:"countingRules"`
	Definitions          definitions          `json:"definitions"`
	APIs                 []apiContract        `json:"apis"`
	StaticEntries        []staticEntry        `json:"staticEntries"`
}

type authoritativeSources struct {
	LegacyBackendRoot    string        `json:"legacyBackendRoot"`
	APIBootstrapFile     string        `json:"apiBootstrapFile"`
	ControlBootstrapFile string        `json:"controlBootstrapFile"`
	RouteFiles           []routeSource `json:"routeFiles"`
}

type routeSource struct {
	File  string `json:"file"`
	Scope string `json:"scope"`
}

type countingRules struct {
	ExpectedAPIEndpoints int      `json:"expectedAPIEndpoints"`
	Includes             []string `json:"includes"`
	Excludes             []string `json:"excludes"`
}

type definitions struct {
	Auth        map[string]string `json:"auth"`
	Idempotency map[string]string `json:"idempotency"`
	BodyKind    map[string]string `json:"bodyKind"`
}

type apiContract struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Scope       string `json:"scope"`
	Auth        string `json:"auth"`
	Idempotency string `json:"idempotency"`
	BodyKind    string `json:"bodyKind"`
}

type staticEntry struct {
	Methods []string `json:"methods"`
	Path    string   `json:"path"`
	Kind    string   `json:"kind"`
	Target  string   `json:"target"`
}

var (
	apiPathPattern = regexp.MustCompile(`^/(?:[A-Za-z0-9._~-]+|:[A-Za-z][A-Za-z0-9]*)(?:/(?:[A-Za-z0-9._~-]+|:[A-Za-z][A-Za-z0-9]*))*$`)
	goGroupPattern = regexp.MustCompile(`(?m)^[\t ]*([A-Za-z_][A-Za-z0-9_]*)[\t ]*:=[\t ]*[A-Za-z_][A-Za-z0-9_]*\.Group\("([^"]*)"\)`)
	goRoutePattern = regexp.MustCompile(`(?m)^[\t ]*([A-Za-z_][A-Za-z0-9_]*)\.(GET|POST|PUT|PATCH|DELETE)\("([^"]*)"`)
)

func TestLegacyAPIManifest(t *testing.T) {
	manifest := loadManifest(t)

	if manifest.SchemaVersion != 1 {
		t.Fatalf("schemaVersion = %d, want 1", manifest.SchemaVersion)
	}
	if manifest.ContractName != "xymusic-legacy-http-api" {
		t.Fatalf("contractName = %q", manifest.ContractName)
	}
	if manifest.CountingRules.ExpectedAPIEndpoints != 135 {
		t.Fatalf("declared endpoint count = %d, want 135", manifest.CountingRules.ExpectedAPIEndpoints)
	}
	if got := len(manifest.APIs); got != manifest.CountingRules.ExpectedAPIEndpoints {
		t.Fatalf("manifest contains %d APIs, declared %d", got, manifest.CountingRules.ExpectedAPIEndpoints)
	}

	allowedMethods := stringSet("GET", "POST", "PUT", "PATCH", "DELETE")
	allowedScopes := map[string]struct{}{"health": {}}
	seenSources := make(map[string]struct{}, len(manifest.AuthoritativeSources.RouteFiles))
	for _, source := range manifest.AuthoritativeSources.RouteFiles {
		if source.File == "" || source.Scope == "" {
			t.Fatalf("route source must contain file and scope: %+v", source)
		}
		if _, duplicate := seenSources[source.File]; duplicate {
			t.Fatalf("duplicate authoritative route file %q", source.File)
		}
		seenSources[source.File] = struct{}{}
		allowedScopes[source.Scope] = struct{}{}
	}
	requireDefinitionKeys(t, "auth", manifest.Definitions.Auth,
		"none", "bearer", "refresh-token", "admin-session", "admin-refresh-cookie")
	requireDefinitionKeys(t, "idempotency", manifest.Definitions.Idempotency, "none", "required")
	requireDefinitionKeys(t, "bodyKind", manifest.Definitions.BodyKind, "none", "json", "json-optional", "binary")

	methodCounts := map[string]int{}
	authCounts := map[string]int{}
	idempotencyCounts := map[string]int{}
	bodyCounts := map[string]int{}
	seen := map[string]apiContract{}
	for index, api := range manifest.APIs {
		key := routeKey(api.Method, api.Path)
		if previous, duplicate := seen[key]; duplicate {
			t.Fatalf("duplicate API %s at index %d (first: %+v)", key, index, previous)
		}
		seen[key] = api

		if _, ok := allowedMethods[api.Method]; !ok {
			t.Errorf("%s has unsupported or non-uppercase method %q", key, api.Method)
		}
		if !apiPathPattern.MatchString(api.Path) || strings.HasSuffix(api.Path, "/") {
			t.Errorf("%s has invalid canonical path; use legacy :param names and no trailing slash", key)
		}
		if !strings.HasPrefix(api.Path, "/api/") && !strings.HasPrefix(api.Path, "/health/") {
			t.Errorf("%s is outside the counted API namespaces", key)
		}
		if _, ok := allowedScopes[api.Scope]; !ok {
			t.Errorf("%s has unknown scope %q", key, api.Scope)
		}
		if _, ok := manifest.Definitions.Auth[api.Auth]; !ok {
			t.Errorf("%s has unknown auth %q", key, api.Auth)
		}
		if _, ok := manifest.Definitions.Idempotency[api.Idempotency]; !ok {
			t.Errorf("%s has unknown idempotency %q", key, api.Idempotency)
		}
		if _, ok := manifest.Definitions.BodyKind[api.BodyKind]; !ok {
			t.Errorf("%s has unknown bodyKind %q", key, api.BodyKind)
		}
		if api.Method == "GET" && api.BodyKind != "none" {
			t.Errorf("%s is GET but bodyKind is %q", key, api.BodyKind)
		}
		if api.Method == "GET" && api.Idempotency != "none" {
			t.Errorf("%s is GET but requires an idempotency key", key)
		}

		methodCounts[api.Method]++
		authCounts[api.Auth]++
		idempotencyCounts[api.Idempotency]++
		bodyCounts[api.BodyKind]++
	}

	assertCounts(t, "method", methodCounts, map[string]int{
		"GET": 50, "POST": 64, "PUT": 4, "PATCH": 11, "DELETE": 6,
	})
	assertCounts(t, "auth", authCounts, map[string]int{
		"none": 14, "bearer": 29, "refresh-token": 1, "admin-session": 90, "admin-refresh-cookie": 1,
	})
	assertCounts(t, "idempotency", idempotencyCounts, map[string]int{
		"none": 77, "required": 58,
	})
	assertCounts(t, "bodyKind", bodyCounts, map[string]int{
		"none": 65, "json": 67, "json-optional": 2, "binary": 1,
	})

	keyContracts := []apiContract{
		{Method: "POST", Path: "/api/setup/complete", Scope: "setup", Auth: "none", Idempotency: "none", BodyKind: "json"},
		{Method: "POST", Path: "/api/v1/auth/refresh", Scope: "identity", Auth: "refresh-token", Idempotency: "required", BodyKind: "json"},
		{Method: "POST", Path: "/api/v1/admin/auth/refresh", Scope: "admin-auth", Auth: "admin-refresh-cookie", Idempotency: "required", BodyKind: "none"},
		{Method: "POST", Path: "/api/v1/admin/users/:id/sessions/:sessionId/revoke", Scope: "admin-management", Auth: "admin-session", Idempotency: "required", BodyKind: "json"},
		{Method: "DELETE", Path: "/api/v1/playlists/:id/tracks/:entryId", Scope: "playlist", Auth: "bearer", Idempotency: "required", BodyKind: "none"},
		{Method: "POST", Path: "/api/v1/admin/tracks/:id/restore", Scope: "admin-catalog-mutation", Auth: "admin-session", Idempotency: "required", BodyKind: "json"},
		{Method: "POST", Path: "/api/v1/admin/tracks/batch/delete-permanently", Scope: "admin-catalog-mutation", Auth: "admin-session", Idempotency: "required", BodyKind: "json"},
		{Method: "GET", Path: "/api/v1/admin/tracks/batch/delete-permanently/:jobId", Scope: "admin-catalog-mutation", Auth: "admin-session", Idempotency: "none", BodyKind: "none"},
		{Method: "POST", Path: "/api/v1/admin/tracks/:id/metadata/revisions/:revisionId/restore", Scope: "admin-metadata", Auth: "admin-session", Idempotency: "required", BodyKind: "json"},
		{Method: "GET", Path: "/api/v1/admin/sources/:id/scans/:scanId/events", Scope: "admin-library-source", Auth: "admin-session", Idempotency: "none", BodyKind: "none"},
		{Method: "PUT", Path: "/api/v1/admin/media/uploads/:id/content", Scope: "admin-media", Auth: "admin-session", Idempotency: "none", BodyKind: "binary"},
	}
	for _, want := range keyContracts {
		got, ok := seen[routeKey(want.Method, want.Path)]
		if !ok {
			t.Errorf("missing key contract %s", routeKey(want.Method, want.Path))
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("key contract %s = %+v, want %+v", routeKey(want.Method, want.Path), got, want)
		}
	}

	validateStaticEntries(t, manifest.StaticEntries)
}

func TestLegacyAPIManifestMatchesAuthoritativeRoutes(t *testing.T) {
	manifest := loadManifest(t)
	backendGoRoot := findBackendGoRoot(t)
	legacyRoot := filepath.Clean(filepath.Join(backendGoRoot, filepath.FromSlash(manifest.AuthoritativeSources.LegacyBackendRoot)))

	extracted := map[string]string{}
	bootstrapPath := filepath.Join(legacyRoot, filepath.FromSlash(manifest.AuthoritativeSources.APIBootstrapFile))
	addExtractedRoutes(t, extracted, bootstrapPath, "health")
	for _, source := range manifest.AuthoritativeSources.RouteFiles {
		sourcePath := filepath.Join(legacyRoot, filepath.FromSlash(source.File))
		addExtractedRoutes(t, extracted, sourcePath, source.Scope)
	}
	if got := len(extracted); got != 135 {
		t.Fatalf("authoritative Go route extraction found %d APIs, want 135", got)
	}

	manifestRoutes := make(map[string]string, len(manifest.APIs))
	for _, api := range manifest.APIs {
		manifestRoutes[routeKey(api.Method, api.Path)] = api.Scope
	}
	var differences []string
	for key, sourceScope := range extracted {
		manifestScope, ok := manifestRoutes[key]
		switch {
		case !ok:
			differences = append(differences, "missing from manifest: "+key)
		case manifestScope != sourceScope:
			differences = append(differences, fmt.Sprintf("scope mismatch for %s: manifest=%s source=%s", key, manifestScope, sourceScope))
		}
	}
	for key := range manifestRoutes {
		if _, ok := extracted[key]; !ok {
			differences = append(differences, "not present in legacy routes: "+key)
		}
	}
	if len(differences) != 0 {
		sort.Strings(differences)
		t.Fatalf("manifest differs from authoritative legacy routes:\n%s", strings.Join(differences, "\n"))
	}

	controlPath := filepath.Join(legacyRoot, filepath.FromSlash(manifest.AuthoritativeSources.ControlBootstrapFile))
	control, err := os.ReadFile(controlPath)
	if err != nil {
		t.Fatalf("read control bootstrap %s: %v", controlPath, err)
	}
	for _, marker := range []string{
		"setup.RegisterRoutes(engine, options.Setup)",
		"registerRuntimeForwarding(engine, options.Manager)",
		"engine.Any(\"/api/v1\", forward)",
		"engine.Any(\"/api/v1/*path\", forward)",
	} {
		if !strings.Contains(string(control), marker) {
			t.Errorf("control bootstrap is missing expected mount/static marker %q", marker)
		}
	}
}

func loadManifest(t *testing.T) legacyManifest {
	t.Helper()
	file, err := os.Open("legacy-api.json")
	if err != nil {
		t.Fatalf("open legacy-api.json: %v", err)
	}
	defer file.Close()

	var manifest legacyManifest
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("decode legacy-api.json: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("legacy-api.json must contain exactly one JSON value: %v", err)
	}
	return manifest
}

func findBackendGoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate BackendGo root containing go.mod")
		}
		dir = parent
	}
}

func addExtractedRoutes(t *testing.T, destination map[string]string, path, scope string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read authoritative route file %s: %v", path, err)
	}
	prefixes := make(map[string]string)
	for _, match := range goGroupPattern.FindAllSubmatch(content, -1) {
		prefixes[string(match[1])] = string(match[2])
	}
	matches := goRoutePattern.FindAllSubmatch(content, -1)
	if len(matches) == 0 {
		t.Fatalf("route file %s yielded no concrete routes", path)
	}
	for _, match := range matches {
		prefix := prefixes[string(match[1])]
		method := string(match[2])
		routePath := canonicalRoutePath(prefix + string(match[3]))
		key := routeKey(method, routePath)
		if previousScope, duplicate := destination[key]; duplicate {
			t.Fatalf("authoritative routes duplicate %s in scopes %s and %s", key, previousScope, scope)
		}
		destination[key] = scope
	}
}

func canonicalRoutePath(path string) string {
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

func validateStaticEntries(t *testing.T, entries []staticEntry) {
	t.Helper()
	if len(entries) != 3 {
		t.Errorf("static entry count = %d, want 3", len(entries))
	}
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if entry.Path == "" || entry.Kind == "" || entry.Target == "" || len(entry.Methods) == 0 {
			t.Errorf("incomplete static entry: %+v", entry)
		}
		for _, method := range entry.Methods {
			key := routeKey(method, entry.Path)
			if _, duplicate := seen[key]; duplicate {
				t.Errorf("duplicate static entry %s", key)
			}
			seen[key] = struct{}{}
		}
	}
	for _, key := range []string{"GET /", "GET /admin", "HEAD /admin", "GET /admin/*", "HEAD /admin/*"} {
		if _, ok := seen[key]; !ok {
			t.Errorf("missing static entry %s", key)
		}
	}
}

func requireDefinitionKeys(t *testing.T, name string, values map[string]string, keys ...string) {
	t.Helper()
	if len(values) != len(keys) {
		t.Fatalf("%s definitions = %v, want exactly %v", name, sortedKeys(values), keys)
	}
	for _, key := range keys {
		if strings.TrimSpace(values[key]) == "" {
			t.Fatalf("%s definition %q is missing or empty", name, key)
		}
	}
}

func assertCounts(t *testing.T, name string, got, want map[string]int) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s counts = %v, want %v", name, got, want)
	}
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func routeKey(method, path string) string {
	return method + " " + path
}
