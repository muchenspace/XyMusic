package contracts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testReference struct {
	file string
	test string
}

func TestEveryLegacyAPIHasSuccessfulHTTPTestEvidence(t *testing.T) {
	manifest := loadManifest(t)
	root := findBackendGoRoot(t)
	files := make(map[string]string)
	covered := make(map[string]testReference, len(manifest.APIs))
	for _, api := range manifest.APIs {
		key := routeKey(api.Method, api.Path)
		reference, ok := successfulHTTPTest(api)
		if !ok {
			t.Errorf("%s has no successful HTTP test evidence", key)
			continue
		}
		assertTestReferenceExists(t, root, files, reference)
		covered[key] = reference
		t.Logf("%s | success=%s#%s", key, reference.file, reference.test)
	}
	if len(covered) != manifest.CountingRules.ExpectedAPIEndpoints {
		t.Fatalf("successful HTTP evidence covers %d endpoints, want %d", len(covered), manifest.CountingRules.ExpectedAPIEndpoints)
	}
}

func TestEveryIdempotentAPIHasReplayBoundaryTestEvidence(t *testing.T) {
	manifest := loadManifest(t)
	root := findBackendGoRoot(t)
	files := make(map[string]string)
	covered := 0
	for _, api := range manifest.APIs {
		if api.Idempotency != "required" {
			continue
		}
		key := routeKey(api.Method, api.Path)
		reference, ok := idempotencyTest(api)
		if !ok {
			t.Errorf("%s has no idempotency replay/boundary test evidence", key)
			continue
		}
		assertTestReferenceExists(t, root, files, reference)
		covered++
		t.Logf("%s | idempotency=%s#%s", key, reference.file, reference.test)
	}
	if covered != 58 {
		t.Fatalf("idempotency evidence covers %d endpoints, want 58", covered)
	}
}

func successfulHTTPTest(api apiContract) (testReference, bool) {
	switch api.Scope {
	case "health":
		return testReference{"internal/platform/httpserver/engine_test.go", "TestHealthEndpointsAndResponseHardening"}, true
	case "setup":
		return testReference{"internal/modules/setup/routes_test.go", "TestRegisterRoutesPreservesNineEndpointContract"}, true
	case "identity":
		return testReference{"internal/modules/identity/routes_test.go", "TestIdentityRoutesPreserveHTTPContract"}, true
	case "profile":
		return testReference{"internal/modules/profile/routes_test.go", "TestProfileRoutesPreserveFourEndpointContract"}, true
	case "catalog":
		return testReference{"internal/modules/catalog/routes_test.go", "TestRoutesRegisterAllNineCatalogEndpointsAndAuthenticate"}, true
	case "playback":
		return testReference{"internal/modules/playback/routes_test.go", "TestCreatePlaybackGrantRouteReturnsServiceGrant"}, true
	case "library":
		return testReference{"internal/modules/library/routes_test.go", "TestRoutesRegisterAllFiveLibraryEndpoints"}, true
	case "playlist":
		return testReference{"internal/modules/playlist/routes_test.go", "TestRoutesExposeEightEndpointsWithIdempotencyAndIgnoreUnknownFields"}, true
	case "admin-auth":
		return adminAuthSuccessTest(api.Path)
	case "admin-management":
		return testReference{"internal/modules/adminmanagement/routes_test.go", "TestRoutesExposeNineEndpointsAndPreserveMutationContracts"}, true
	case "admin-catalog-query":
		return testReference{"internal/modules/admincatalog/routes_test.go", "TestRoutesExposeSevenAdminCatalogQueries"}, true
	case "admin-metadata":
		return testReference{"internal/modules/adminmetadata/routes_test.go", "TestRoutesExposeElevenMetadataEndpoints"}, true
	case "admin-tag-scraping":
		return testReference{"internal/modules/admintagscraping/routes_test.go", "TestRoutesExposeAllFifteenTagScrapingAPIs"}, true
	case "admin-operations":
		return adminOperationsSuccessTest(api.Path)
	case "admin-catalog-mutation":
		if api.Method == "GET" && api.Path == "/api/v1/admin/tracks/batch/delete-permanently/:jobId" {
			return testReference{
				"internal/modules/adminmutation/routes_test.go",
				"TestPermanentDeleteBatchStatusUsesAdminAuthenticationWithoutIdempotency",
			}, true
		}
		return testReference{"internal/modules/adminmutation/routes_test.go", "TestAdminMutationRoutesExecuteReplayAndRejectPayloadConflicts"}, true
	case "admin-library-source":
		return testReference{"internal/modules/adminsources/routes_test.go", "TestRoutesExposeAllThirteenLibrarySourceAPIs"}, true
	case "admin-media":
		return testReference{"internal/modules/adminmedia/routes_test.go", "TestRoutesExposeFiveAdminMediaEndpointsAndContracts"}, true
	default:
		return testReference{}, false
	}
}

func adminAuthSuccessTest(path string) (testReference, bool) {
	const file = "internal/modules/adminauth/routes_http_test.go"
	tests := map[string]string{
		"/api/v1/admin/auth/login":   "TestAdminAuthLoginRouteReturnsAdminSession",
		"/api/v1/admin/auth/session": "TestAdminAuthSessionRouteReturnsAuthenticatedUser",
		"/api/v1/admin/auth/refresh": "TestAdminAuthRefreshRouteReplaysAndRejectsPayloadConflict",
		"/api/v1/admin/auth/logout":  "TestAdminAuthLogoutRouteClearsSession",
	}
	test, ok := tests[path]
	return testReference{file, test}, ok
}

func adminOperationsSuccessTest(path string) (testReference, bool) {
	switch {
	case strings.HasPrefix(path, "/api/v1/admin/jobs"):
		return testReference{"internal/modules/adminjobs/routes_test.go", "TestRoutesExposeJobQueriesAndIdempotentMutations"}, true
	case path == "/api/v1/admin/audit":
		return testReference{"internal/modules/adminaudit/routes_test.go", "TestAuditQueryIgnoresUnknownFieldsAndUsesLastRepeatedValue"}, true
	case path == "/api/v1/admin/settings" || strings.HasPrefix(path, "/api/v1/admin/settings/") || path == "/api/v1/admin/system":
		return testReference{"internal/modules/adminsettings/routes_test.go", "TestRoutesExposeSevenSystemSettingsEndpoints"}, true
	default:
		return testReference{}, false
	}
}

func idempotencyTest(api apiContract) (testReference, bool) {
	switch api.Scope {
	case "identity":
		return testReference{"internal/modules/identity/identity_test.go", "TestAdminRefreshIdempotencyReplaysAndRejectsChangedTokenPayload"}, true
	case "profile":
		return testReference{"internal/modules/profile/routes_test.go", "TestProfileRoutesPreserveFourEndpointContract"}, true
	case "library":
		return testReference{"internal/modules/library/service_test.go", "TestRecordPlaybackReturnsIdempotentReplayWithoutRepeatingSideEffect"}, true
	case "playlist":
		return testReference{"internal/modules/playlist/routes_test.go", "TestRoutesExposeEightEndpointsWithIdempotencyAndIgnoreUnknownFields"}, true
	case "admin-auth":
		return testReference{"internal/modules/adminauth/routes_http_test.go", "TestAdminAuthRefreshRouteReplaysAndRejectsPayloadConflict"}, true
	case "admin-management":
		return testReference{"internal/modules/adminmanagement/routes_test.go", "TestRoutesExposeNineEndpointsAndPreserveMutationContracts"}, true
	case "admin-metadata":
		return testReference{"internal/modules/adminmetadata/routes_test.go", "TestRoutesExposeElevenMetadataEndpoints"}, true
	case "admin-tag-scraping":
		return testReference{"internal/modules/admintagscraping/routes_test.go", "TestRoutesExposeAllFifteenTagScrapingAPIs"}, true
	case "admin-operations":
		switch {
		case strings.HasPrefix(api.Path, "/api/v1/admin/jobs"):
			return testReference{"internal/modules/adminjobs/routes_test.go", "TestRoutesExposeJobQueriesAndIdempotentMutations"}, true
		case api.Path == "/api/v1/admin/settings":
			return testReference{"internal/modules/adminsettings/routes_test.go", "TestRoutesExposeSevenSystemSettingsEndpoints"}, true
		default:
			return testReference{}, false
		}
	case "admin-catalog-mutation":
		return testReference{"internal/modules/adminmutation/routes_test.go", "TestAdminMutationRoutesExecuteReplayAndRejectPayloadConflicts"}, true
	case "admin-library-source":
		return testReference{"internal/modules/adminsources/routes_test.go", "TestRoutesExposeAllThirteenLibrarySourceAPIs"}, true
	case "admin-media":
		return testReference{"internal/modules/adminmedia/routes_test.go", "TestRoutesExposeFiveAdminMediaEndpointsAndContracts"}, true
	default:
		return testReference{}, false
	}
}

func assertTestReferenceExists(t *testing.T, root string, files map[string]string, reference testReference) {
	t.Helper()
	content, ok := files[reference.file]
	if !ok {
		payload, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(reference.file)))
		if err != nil {
			t.Fatalf("read test evidence file %s: %v", reference.file, err)
		}
		content = string(payload)
		files[reference.file] = content
	}
	if !strings.Contains(content, "func "+reference.test+"(") {
		t.Fatalf("test evidence %s#%s does not exist", reference.file, reference.test)
	}
}
