package service_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/project"
	"github.com/maxgfr/feelc/internal/registry"
	"github.com/maxgfr/feelc/internal/service"
)

// writableProject sets up a 2-module project (a, b) in a temp dir with editing enabled, and returns the
// wired handler.
func writableProject(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("feelc.project.json", `{"name":"tmp","modules":[{"name":"a","path":"a.rules"},{"name":"b","path":"b.rules"}]}`)
	write("a.rules", "model \"a\" {}\ninput age : number in [0..120]\ndecision adult : boolean = age >= 18\n")
	write("b.rules", "model \"b\" {}\ninput age : number in [0..120]\ndecision senior : boolean = age >= 65\n")
	ws, err := project.OpenWorkspace(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	srv := service.New(registry.New(), nil, nil)
	srv.SetWorkspace(ws)
	srv.SetProject(ws.Current())
	return srv.Handler()
}

// TestModuleETagIfMatch: GET emits an ETag; a stale If-Match PUT 412s; the current ETag succeeds and a
// fresh ETag is echoed; no If-Match keeps last-writer-wins.
func TestModuleETagIfMatch(t *testing.T) {
	h := writableProject(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/modules/a/source", nil))
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("GET module source: missing ETag header")
	}

	edit := "model \"a\" {}\ninput age : number in [0..120]\ndecision adult : boolean = age >= 21\n"

	// Stale If-Match → 412.
	req := httptest.NewRequest("PUT", "/v1/modules/a/source", strings.NewReader(edit))
	req.Header.Set("If-Match", `"deadbeef"`)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale If-Match: got %d, want 412", rec.Code)
	}

	// Current ETag → 200 + a new ETag echoed.
	req = httptest.NewRequest("PUT", "/v1/modules/a/source", strings.NewReader(edit))
	req.Header.Set("If-Match", etag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("matching If-Match: got %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if ne := rec.Header().Get("ETag"); ne == "" || ne == etag {
		t.Errorf("expected a fresh ETag after edit, got %q (old %q)", ne, etag)
	}

	// No If-Match → last-writer-wins still works.
	req = httptest.NewRequest("PUT", "/v1/modules/a/source", strings.NewReader(edit))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("no If-Match: got %d, want 200", rec.Code)
	}
}

// TestCreateIfNoneMatch: If-None-Match:* on an existing name 412s; a fresh name succeeds.
func TestCreateIfNoneMatch(t *testing.T) {
	h := writableProject(t)
	src := "model \"x\" {}\ninput x : boolean\ndecision y : boolean = x\n"
	body := func(name string) string {
		b, _ := json.Marshal(map[string]string{"name": name, "source": src})
		return string(b)
	}
	req := httptest.NewRequest("POST", "/v1/modules", strings.NewReader(body("a")))
	req.Header.Set("If-None-Match", "*")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("create existing w/ If-None-Match:*: got %d, want 412", rec.Code)
	}

	req = httptest.NewRequest("POST", "/v1/modules", strings.NewReader(body("c")))
	req.Header.Set("If-None-Match", "*")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create fresh module: got %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestModulesPagination: absent params return all modules + total; ?limit/?offset window the sorted list.
func TestModulesPagination(t *testing.T) {
	h := writableProject(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/modules", nil))
	var all struct {
		Total   int `json:"total"`
		Modules []struct {
			Name string `json:"name"`
		} `json:"modules"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&all); err != nil {
		t.Fatal(err)
	}
	if all.Total != 2 || len(all.Modules) != 2 {
		t.Fatalf("no params: total=%d modules=%d, want 2/2", all.Total, len(all.Modules))
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/modules?limit=1&offset=0", nil))
	var pg struct {
		Total   int `json:"total"`
		Limit   int `json:"limit"`
		Offset  int `json:"offset"`
		Modules []struct {
			Name string `json:"name"`
		} `json:"modules"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&pg); err != nil {
		t.Fatal(err)
	}
	if pg.Total != 2 || len(pg.Modules) != 1 || pg.Limit != 1 {
		t.Fatalf("limit=1: total=%d modules=%d limit=%d, want 2/1/1", pg.Total, len(pg.Modules), pg.Limit)
	}
	if pg.Modules[0].Name != "a" {
		t.Errorf("first page module = %q, want a (sorted by name)", pg.Modules[0].Name)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/modules?offset=999", nil))
	var beyond struct {
		Total   int               `json:"total"`
		Modules []json.RawMessage `json:"modules"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&beyond); err != nil {
		t.Fatal(err)
	}
	if beyond.Total != 2 || len(beyond.Modules) != 0 {
		t.Fatalf("offset=999: total=%d modules=%d, want 2/0", beyond.Total, len(beyond.Modules))
	}

	// Hostile params must not 500 (slice-bounds / overflow): a near-MaxInt limit, negative values.
	for _, qs := range []string{"limit=9223372036854775807", "limit=-1", "offset=-5", "limit=99999999999999999999", "limit=abc&offset=xyz", "limit=0"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/modules?"+qs, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET /v1/modules?%s: got %d, want 200 (no panic/500 on hostile params)", qs, rec.Code)
		}
	}
}

// TestRollbackEndpoint: single-file mode — 409 until ≥2 versions, then 200 republishing the previous one.
func TestRollbackEndpoint(t *testing.T) {
	reg := registry.New()
	h := service.New(reg, nil, nil).Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/admin/rollback", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("rollback with no history: got %d, want 409", rec.Code)
	}

	reg.Store(&ir.CompiledModel{Name: "m"}, "ha")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/admin/rollback", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("rollback with one version: got %d, want 409", rec.Code)
	}

	reg.Store(&ir.CompiledModel{Name: "m"}, "hb")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/admin/rollback", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("rollback: got %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Hash       string `json:"hash"`
		RolledBack bool   `json:"rolledBack"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Hash != "ha" || !body.RolledBack {
		t.Errorf("rollback body = %+v, want hash=ha rolledBack=true", body)
	}
	if cur := reg.Current(); cur == nil || cur.Hash != "ha" {
		t.Errorf("registry current after rollback = %v, want hash ha", cur)
	}
}

// TestRollbackBlockedInProjectMode confirms the split-brain guard: rollback 404s when a project is served.
func TestRollbackBlockedInProjectMode(t *testing.T) {
	h := writableProject(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/admin/rollback", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("rollback in project mode: got %d, want 404", rec.Code)
	}
}
