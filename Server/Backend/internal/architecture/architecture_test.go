// Package architecture contains source-level guard tests that lock the
// dependency direction of the backend module graph. The tests only read and
// parse .go files; they never touch the database or the network.
//
// Rules are table-driven. Every rule lists its legal exceptions (whitelist)
// next to the rule with an explicit reason so future changes stay intentional.
package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Source loading
// ---------------------------------------------------------------------------

type sourceFile struct {
	rel     string // slash-separated path relative to the backend module root
	area    string // shared | platform | modules | app | cmd | control | ...
	module  string // module directory name when area == "modules"
	pkg     string // Go package name
	isTest  bool
	imports []string
	file    *ast.File
}

// backendRoot resolves the backend module root (the directory containing
// go.mod) from the architecture package directory.
func backendRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve backend root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("architecture tests must run from the backend module: %v", err)
	}
	return root
}

// loadSources parses every .go file under internal/ and cmd/ once. Parsing is
// syntax-only, so platform-specific files are included regardless of GOOS.
func loadSources(t *testing.T) []*sourceFile {
	t.Helper()
	root := backendRoot(t)
	fset := token.NewFileSet()
	var files []*sourceFile
	for _, base := range []string{"internal", "cmd"} {
		walkRoot := filepath.Join(root, base)
		err := filepath.WalkDir(walkRoot, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") {
				return nil
			}
			parsed, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if parseErr != nil {
				return fmt.Errorf("parse %s: %w", path, parseErr)
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			area, module := classify(rel)
			file := &sourceFile{
				rel:    rel,
				area:   area,
				module: module,
				pkg:    parsed.Name.Name,
				isTest: strings.HasSuffix(name, "_test.go"),
				file:   parsed,
			}
			for _, imported := range parsed.Imports {
				file.imports = append(file.imports, strings.Trim(imported.Path.Value, `"`))
			}
			files = append(files, file)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return files
}

func classify(rel string) (area, module string) {
	switch {
	case strings.HasPrefix(rel, "cmd/"):
		return "cmd", ""
	case strings.HasPrefix(rel, "internal/shared/"):
		return "shared", ""
	case strings.HasPrefix(rel, "internal/platform/"):
		return "platform", ""
	case strings.HasPrefix(rel, "internal/modules/"):
		rest := strings.TrimPrefix(rel, "internal/modules/")
		return "modules", strings.SplitN(rest, "/", 2)[0]
	case rel == "internal/app" || strings.HasPrefix(rel, "internal/app/"):
		return "app", ""
	case strings.HasPrefix(rel, "internal/control/"):
		return "control", ""
	case strings.HasPrefix(rel, "internal/config/"):
		return "config", ""
	case strings.HasPrefix(rel, "internal/workers/"):
		return "workers", ""
	case strings.HasPrefix(rel, "internal/testsupport/"):
		return "testsupport", ""
	case strings.HasPrefix(rel, "internal/architecture/"):
		return "architecture", ""
	default:
		return "other", ""
	}
}

// matchesBanned reports whether an import path matches a banned entry. An
// entry matches its exact path and every subpackage path (for example
// "github.com/jackc/pgx" matches "github.com/jackc/pgx/v5/pgxpool"). Entries
// ending in "/" are treated as plain prefixes.
func matchesBanned(importPath string, banned []string) bool {
	for _, entry := range banned {
		trimmed := strings.TrimSuffix(entry, "/")
		if importPath == trimmed || strings.HasPrefix(importPath, trimmed+"/") {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Rule 1: import direction between areas
// ---------------------------------------------------------------------------

type importRule struct {
	name    string
	reason  string
	applies func(file *sourceFile) bool
	banned  []string
	// allowed lists explicit whitelisted files with the reason they are
	// exempt. Keys are slash-separated paths relative to the backend root.
	allowed map[string]string
}

var importDirectionRules = []importRule{
	{
		name:   "shared must not depend on platform, modules, app or cmd",
		reason: "shared is the bottom layer; every other layer may import it",
		applies: func(file *sourceFile) bool {
			return file.area == "shared"
		},
		banned: []string{
			"xymusic/server/internal/platform/",
			"xymusic/server/internal/modules/",
			"xymusic/server/internal/app",
			"xymusic/server/cmd/",
		},
		allowed: map[string]string{},
	},
	{
		name:   "platform must not depend on modules, app or cmd",
		reason: "platform provides infrastructure and may only depend on shared and config",
		applies: func(file *sourceFile) bool {
			return file.area == "platform"
		},
		banned: []string{
			"xymusic/server/internal/modules/",
			"xymusic/server/internal/app",
			"xymusic/server/cmd/",
		},
		allowed: map[string]string{},
	},
	{
		name:   "modules must not depend on app or cmd",
		reason: "internal/app is the composition root; modules receive ports, never the root",
		applies: func(file *sourceFile) bool {
			return file.area == "modules"
		},
		banned: []string{
			"xymusic/server/internal/app",
			"xymusic/server/cmd/",
		},
		allowed: map[string]string{},
	},
}

func (rule importRule) check(t *testing.T, files []*sourceFile) {
	t.Helper()
	t.Run(rule.name, func(t *testing.T) {
		for _, file := range files {
			if file.isTest || !rule.applies(file) {
				continue
			}
			if _, exempt := rule.allowed[file.rel]; exempt {
				continue
			}
			for _, imported := range file.imports {
				if matchesBanned(imported, rule.banned) {
					t.Errorf("%s: imports %q; rule %q: %s", file.rel, imported, rule.name, rule.reason)
				}
			}
		}
	})
}

func TestImportDirectionRules(t *testing.T) {
	files := loadSources(t)
	for _, rule := range importDirectionRules {
		rule.check(t, files)
	}
}

// ---------------------------------------------------------------------------
// Rule 2: file-role import bans inside modules
// ---------------------------------------------------------------------------

type fileRoleRule struct {
	name    string
	reason  string
	match   func(file *sourceFile) bool
	banned  []string
	allowed map[string]string
}

var moduleFileRoleRules = []fileRoleRule{
	{
		name:   "ports.go must not import pgx",
		reason: "ports describe module contracts and must stay driver-agnostic",
		match: func(file *sourceFile) bool {
			return file.area == "modules" && filepath.Base(file.rel) == "ports.go"
		},
		banned:  []string{"github.com/jackc/pgx"},
		allowed: map[string]string{},
	},
	{
		name:   "service.go must not import gin, platform database, security or localmedia",
		reason: "application services depend on module ports, not transports or platform implementations",
		match: func(file *sourceFile) bool {
			return file.area == "modules" && filepath.Base(file.rel) == "service.go"
		},
		banned: []string{
			"github.com/gin-gonic/gin",
			"xymusic/server/internal/platform/database",
			"xymusic/server/internal/platform/security",
			"xymusic/server/internal/platform/localmedia",
		},
		allowed: map[string]string{},
	},
	{
		name:   "routes.go must not import pgx",
		reason: "HTTP transports must not open database connections directly",
		match: func(file *sourceFile) bool {
			base := filepath.Base(file.rel)
			return file.area == "modules" && (base == "routes.go" || strings.HasPrefix(base, "routes_") || strings.HasSuffix(base, "_routes.go"))
		},
		banned:  []string{"github.com/jackc/pgx"},
		allowed: map[string]string{},
	},
	{
		name:   "repository*.go must not import gin",
		reason: "persistence adapters must not depend on the HTTP transport",
		match: func(file *sourceFile) bool {
			base := filepath.Base(file.rel)
			return file.area == "modules" && strings.HasPrefix(base, "repository") && strings.HasSuffix(base, ".go")
		},
		banned:  []string{"github.com/gin-gonic/gin"},
		allowed: map[string]string{},
	},
}

func (rule fileRoleRule) check(t *testing.T, files []*sourceFile) {
	t.Helper()
	t.Run(rule.name, func(t *testing.T) {
		for _, file := range files {
			if file.isTest || !rule.match(file) {
				continue
			}
			if _, exempt := rule.allowed[file.rel]; exempt {
				continue
			}
			for _, imported := range file.imports {
				if matchesBanned(imported, rule.banned) {
					t.Errorf("%s: imports %q; rule %q: %s", file.rel, imported, rule.name, rule.reason)
				}
			}
		}
	})
}

func TestModuleFileRoleRules(t *testing.T) {
	files := loadSources(t)
	for _, rule := range moduleFileRoleRules {
		rule.check(t, files)
	}
}

// ---------------------------------------------------------------------------
// Rule 3: cross-module Service references
// ---------------------------------------------------------------------------

// moduleServiceConstructors are the constructors that build a module's
// concrete application service.
var moduleServiceConstructors = map[string]bool{
	"NewService":                   true,
	"NewServiceWithOptions":        true,
	"NewBatchService":              true,
	"NewArtistArtworkBatchService": true,
}

// moduleServiceReferenceWhitelist exempts a file from the cross-module Service
// reference rule. Keys are slash-separated paths relative to the backend root;
// values must explain why the reference is legitimate.
var moduleServiceReferenceWhitelist = map[string]string{}

// cmdModuleImportWhitelist lists module packages that cmd may import without
// constructing services. cmd/xymusic only needs the setup runtime source
// constants; all assembly stays in internal/app and internal/control.
var cmdModuleImportWhitelist = map[string]string{
	"xymusic/server/internal/modules/setup": "cmd/xymusic references setup.RuntimeSource* constants only; it never constructs the setup service",
}

// inspectServiceReferences visits every selector expression whose qualifier
// resolves to an imported module package (for example catalog.NewService or
// identity.Service). Import aliases are resolved through the file's import
// declarations so aliased module imports are still detected.
func inspectServiceReferences(file *sourceFile, visit func(sel *ast.SelectorExpr, modulePkg string)) {
	qualifiers := make(map[string]string) // local qualifier -> module package name
	for _, spec := range file.file.Imports {
		path := strings.Trim(spec.Path.Value, `"`)
		if !strings.HasPrefix(path, "xymusic/server/internal/modules/") {
			continue
		}
		moduleDir := strings.SplitN(strings.TrimPrefix(path, "xymusic/server/internal/modules/"), "/", 2)[0]
		qualifier := moduleDir
		if spec.Name != nil {
			qualifier = spec.Name.Name
		}
		qualifiers[qualifier] = moduleDir
	}
	if len(qualifiers) == 0 {
		return
	}
	ast.Inspect(file.file, func(node ast.Node) bool {
		sel, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		modulePkg, isModule := qualifiers[ident.Name]
		if !isModule {
			return true
		}
		// A module must never import itself through the module graph; that
		// would be a self-reference, not a cross-module one.
		if modulePkg == file.module {
			return true
		}
		visit(sel, modulePkg)
		return true
	})
}

func TestOnlyCompositionRootReferencesModuleServices(t *testing.T) {
	files := loadSources(t)
	for _, file := range files {
		if file.isTest {
			continue
		}
		// internal/app is the composition root and cmd is the process entry
		// point; both may reference module services. Every other area must go
		// through module ports.
		if file.area == "app" || file.area == "cmd" {
			continue
		}
		if _, exempt := moduleServiceReferenceWhitelist[file.rel]; exempt {
			continue
		}
		inspectServiceReferences(file, func(sel *ast.SelectorExpr, modulePkg string) {
			switch {
			case sel.Sel.Name == "Service":
				t.Errorf(
					"%s: references %s.Service; only internal/app and cmd may depend on concrete module services (modules must depend on ports)",
					file.rel, modulePkg,
				)
			case moduleServiceConstructors[sel.Sel.Name]:
				t.Errorf(
					"%s: calls %s.%s; only internal/app and cmd may construct module services",
					file.rel, modulePkg, sel.Sel.Name,
				)
			}
		})
	}
}

func TestCmdDoesNotConstructModuleServices(t *testing.T) {
	files := loadSources(t)
	for _, file := range files {
		if file.area != "cmd" || file.isTest {
			continue
		}
		// cmd may only import module packages for constants and types that are
		// explicitly whitelisted; service construction always stays in
		// internal/app or internal/control.
		for _, imported := range file.imports {
			if !strings.HasPrefix(imported, "xymusic/server/internal/modules/") {
				continue
			}
			if _, allowed := cmdModuleImportWhitelist[imported]; !allowed {
				t.Errorf(
					"%s: imports %q; cmd must assemble through internal/app and internal/control (add an explicit whitelist entry with a reason if the import is required)",
					file.rel, imported,
				)
			}
		}
		inspectServiceReferences(file, func(sel *ast.SelectorExpr, modulePkg string) {
			switch {
			case sel.Sel.Name == "Service":
				t.Errorf(
					"%s: references %s.Service; cmd must not depend on concrete module services",
					file.rel, modulePkg,
				)
			case moduleServiceConstructors[sel.Sel.Name]:
				t.Errorf(
					"%s: calls %s.%s; cmd must assemble services through internal/app, not construct them directly",
					file.rel, modulePkg, sel.Sel.Name,
				)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Scanner self-check
// ---------------------------------------------------------------------------

// TestBannedImportMatcher documents the exact matching semantics of the guard
// engine so a future refactor cannot silently weaken every rule.
func TestBannedImportMatcher(t *testing.T) {
	tests := []struct {
		importPath string
		banned     []string
		want       bool
	}{
		{"github.com/jackc/pgx", []string{"github.com/jackc/pgx"}, true},
		{"github.com/jackc/pgx/v5", []string{"github.com/jackc/pgx"}, true},
		{"github.com/jackc/pgx/v5/pgxpool", []string{"github.com/jackc/pgx"}, true},
		{"github.com/jackc/pgxfoo", []string{"github.com/jackc/pgx"}, false},
		{"xymusic/server/internal/platform/database", []string{"xymusic/server/internal/platform/"}, true},
		{"xymusic/server/internal/platform", []string{"xymusic/server/internal/platform"}, true},
		{"xymusic/server/internal/platformx", []string{"xymusic/server/internal/platform"}, false},
		{"github.com/gin-gonic/gin", []string{"github.com/gin-gonic/gin"}, true},
		{"fmt", []string{"github.com/gin-gonic/gin"}, false},
	}
	for _, test := range tests {
		if got := matchesBanned(test.importPath, test.banned); got != test.want {
			t.Errorf("matchesBanned(%q, %v) = %v, want %v", test.importPath, test.banned, got, test.want)
		}
	}
}

// TestWhitelistEntriesAreStillNeeded fails when an exemption is no longer
// required, so stale exceptions cannot hide future regressions.
func TestWhitelistEntriesAreStillNeeded(t *testing.T) {
	files := loadSources(t)
	byRel := make(map[string]*sourceFile, len(files))
	for _, file := range files {
		byRel[file.rel] = file
	}
	for _, rule := range importDirectionRules {
		for rel, reason := range rule.allowed {
			file, exists := byRel[rel]
			if !exists {
				t.Errorf("rule %q whitelists missing file %q (reason: %s)", rule.name, rel, reason)
				continue
			}
			violated := false
			for _, imported := range file.imports {
				if matchesBanned(imported, rule.banned) {
					violated = true
					break
				}
			}
			if !violated {
				t.Errorf("rule %q whitelists %q but it no longer violates the rule; remove the exemption", rule.name, rel)
			}
		}
	}
	for _, rule := range moduleFileRoleRules {
		for rel, reason := range rule.allowed {
			file, exists := byRel[rel]
			if !exists {
				t.Errorf("rule %q whitelists missing file %q (reason: %s)", rule.name, rel, reason)
				continue
			}
			violated := false
			for _, imported := range file.imports {
				if matchesBanned(imported, rule.banned) {
					violated = true
					break
				}
			}
			if !violated {
				t.Errorf("rule %q whitelists %q but it no longer violates the rule; remove the exemption", rule.name, rel)
			}
		}
	}
	for rel, reason := range moduleServiceReferenceWhitelist {
		file, exists := byRel[rel]
		if !exists {
			t.Errorf("service-reference whitelist names missing file %q (reason: %s)", rel, reason)
			continue
		}
		referenced := false
		inspectServiceReferences(file, func(sel *ast.SelectorExpr, _ string) {
			if sel.Sel.Name == "Service" || moduleServiceConstructors[sel.Sel.Name] {
				referenced = true
			}
		})
		if !referenced {
			t.Errorf("service-reference whitelist names %q but it no longer references a module service; remove the exemption", rel)
		}
	}
	for imported, reason := range cmdModuleImportWhitelist {
		found := false
		for _, file := range files {
			if file.area != "cmd" {
				continue
			}
			for _, candidate := range file.imports {
				if candidate == imported {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("cmd module-import whitelist names %q but no cmd file imports it (reason: %s)", imported, reason)
		}
	}
}

// TestSourceScannerFindsBackendPackages guards against a silently broken scan
// (for example a wrong working directory) that would make every rule pass
// without checking anything.
func TestSourceScannerFindsBackendPackages(t *testing.T) {
	files := loadSources(t)
	areas := map[string]int{}
	modules := map[string]bool{}
	for _, file := range files {
		if file.isTest {
			continue
		}
		areas[file.area]++
		if file.area == "modules" {
			modules[file.module] = true
			// Module directory names and package names must agree; the
			// cross-module rule keys off the import path and reports the
			// package qualifier, so a mismatch would misreport violations.
			if file.pkg != file.module {
				t.Errorf("module %q contains package %q; module directory and package names must match", file.module, file.pkg)
			}
		}
	}
	for _, required := range []string{"shared", "platform", "modules", "app", "cmd"} {
		if areas[required] == 0 {
			t.Fatalf("scanner found no non-test files in area %q; areas=%v", required, areas)
		}
	}
	if len(modules) < 10 {
		t.Fatalf("scanner found only %d modules; modules=%v", len(modules), modules)
	}
}
