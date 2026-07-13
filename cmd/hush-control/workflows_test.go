package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Deploy API":        "deploy-api",
		"  git pull  ":      "git-pull",
		"restart!!!nginx":   "restart-nginx",
		"":                  "",
		"---":               "",
		"Café Über 2000":    "caf-ber-2000",
		"already-a-slug-99": "already-a-slug-99",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewWorkflowIDUniqueAndSlugged(t *testing.T) {
	a := newWorkflowID("Deploy API")
	b := newWorkflowID("Deploy API")
	if a == b {
		t.Fatalf("ids collided: %q", a)
	}
	if !strings.HasPrefix(a, "deploy-api-") {
		t.Errorf("id %q missing slug prefix", a)
	}
	if unnamed := newWorkflowID("!!!"); unnamed == "" || strings.Contains(unnamed, "-") {
		t.Errorf("empty-slug id should be a bare suffix, got %q", unnamed)
	}
}

func TestValidateWorkflow(t *testing.T) {
	inFleet := func(host string) bool { return host == "citadel" || host == "nas" }

	if _, err := validateWorkflow("  ", []Step{{Host: "citadel", Cmd: "uptime"}}, inFleet); err == nil {
		t.Error("blank name should be rejected")
	}
	if _, err := validateWorkflow("x", nil, inFleet); err == nil {
		t.Error("zero steps should be rejected")
	}
	if _, err := validateWorkflow("x", []Step{{Host: "citadel", Cmd: ""}}, inFleet); err == nil {
		t.Error("blank command should be rejected")
	}
	if _, err := validateWorkflow("x", []Step{{Host: "ghost", Cmd: "ls"}}, inFleet); err == nil {
		t.Error("unknown host should be rejected")
	}

	wf, err := validateWorkflow("Deploy API", []Step{
		{Host: "citadel", Cmd: "  git pull  "},
		{Host: "nas", Cmd: "systemctl restart api"},
	}, inFleet)
	if err != nil {
		t.Fatalf("valid workflow rejected: %v", err)
	}
	if wf.ID == "" || wf.CreatedAt == "" {
		t.Errorf("id/createdAt not filled: %+v", wf)
	}
	if wf.Steps[0].Cmd != "git pull" {
		t.Errorf("command not trimmed: %q", wf.Steps[0].Cmd)
	}
}

func TestWorkflowStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workflows.json")
	s := newWorkflowStore(path)
	if got := s.Snapshot(); len(got) != 0 {
		t.Fatalf("fresh store not empty: %d", len(got))
	}

	saved, err := s.Add(Workflow{ID: "w1", Name: "one", Steps: []Step{{Host: "box", Cmd: "echo hi"}}})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if saved.ID != "w1" {
		t.Fatalf("add returned %+v", saved)
	}

	// A fresh store over the same path must see the persisted blueprint.
	reloaded := newWorkflowStore(path)
	if got := reloaded.Snapshot(); len(got) != 1 || got[0].ID != "w1" {
		t.Fatalf("reload = %+v", got)
	}
	if _, ok := reloaded.find("w1"); !ok {
		t.Error("find(w1) missed a persisted workflow")
	}

	removed, err := reloaded.Delete("w1")
	if err != nil || !removed {
		t.Fatalf("delete = %v, %v", removed, err)
	}
	if removed, _ := reloaded.Delete("w1"); removed {
		t.Error("second delete should report nothing removed")
	}
}

func TestLoadWorkflowsCorruptStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workflows.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadWorkflows(path); len(got) != 0 {
		t.Errorf("corrupt file should start empty, got %d", len(got))
	}
}

// fakeAgent stands in for hush-agent's /exec, replaying a scripted SSE run.
func fakeAgent(t *testing.T, frames ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exec" {
			t.Errorf("agent hit %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, f := range frames {
			io.WriteString(w, "data: "+f+"\n\n")
		}
	}))
}

func decodeWorkflowStream(t *testing.T, body string) []workflowEvent {
	t.Helper()
	var evs []workflowEvent
	for _, frame := range strings.Split(body, "\n\n") {
		line := strings.TrimSpace(frame)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev workflowEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(line[len("data:"):])), &ev); err != nil {
			t.Fatalf("bad frame %q: %v", line, err)
		}
		evs = append(evs, ev)
	}
	return evs
}

func TestRunWorkflowAllStepsSucceed(t *testing.T) {
	agent := fakeAgent(t,
		`{"kind":"start","pid":7}`,
		`{"kind":"out","stream":"stdout","data":"hello\n"}`,
		`{"kind":"exit","code":0,"ms":12}`,
	)
	defer agent.Close()

	wf := Workflow{Name: "two-step", Steps: []Step{
		{Host: "box", Cmd: "echo hello"},
		{Host: "box", Cmd: "true"},
	}}
	resolve := func(host string) (Agent, bool) { return Agent{Name: host, Addr: agent.URL}, true }

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/x/run", nil)
	runWorkflow(req.Context(), rec, agent.Client(), resolve, wf)

	evs := decodeWorkflowStream(t, rec.Body.String())
	var steps, exits int
	var done *workflowEvent
	for i := range evs {
		switch evs[i].Kind {
		case "step":
			steps++
		case "stepExit":
			exits++
		case "done":
			done = &evs[i]
		}
	}
	if steps != 2 || exits != 2 {
		t.Errorf("steps=%d exits=%d, want 2/2", steps, exits)
	}
	if done == nil || !done.OK || done.Ran != 2 {
		t.Errorf("done frame = %+v, want ok run of 2", done)
	}
	if !strings.Contains(rec.Body.String(), `"data":"hello\n"`) {
		t.Error("step output not relayed")
	}
}

func TestRunWorkflowStopsAtFailingStep(t *testing.T) {
	agent := fakeAgent(t, `{"kind":"exit","code":1,"ms":3}`)
	defer agent.Close()

	wf := Workflow{Name: "fails", Steps: []Step{
		{Host: "box", Cmd: "false"},
		{Host: "box", Cmd: "echo unreached"},
	}}
	resolve := func(host string) (Agent, bool) { return Agent{Name: host, Addr: agent.URL}, true }

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/x/run", nil)
	runWorkflow(req.Context(), rec, agent.Client(), resolve, wf)

	evs := decodeWorkflowStream(t, rec.Body.String())
	steps := 0
	var done *workflowEvent
	for i := range evs {
		switch evs[i].Kind {
		case "step":
			steps++
		case "done":
			done = &evs[i]
		}
	}
	if steps != 1 {
		t.Errorf("ran %d steps, want fail-fast after 1", steps)
	}
	if done == nil || done.OK || done.FailedStep == nil || *done.FailedStep != 0 {
		t.Errorf("done frame = %+v, want failure at step 0", done)
	}
}

func TestRunWorkflowUnknownHostStops(t *testing.T) {
	wf := Workflow{Name: "ghost", Steps: []Step{{Host: "gone", Cmd: "ls"}}}
	resolve := func(string) (Agent, bool) { return Agent{}, false }

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/x/run", nil)
	runWorkflow(req.Context(), rec, http.DefaultClient, resolve, wf)

	body := rec.Body.String()
	if !strings.Contains(body, "not in the fleet") {
		t.Errorf("missing not-in-fleet error: %q", body)
	}
	evs := decodeWorkflowStream(t, body)
	last := evs[len(evs)-1]
	if last.Kind != "done" || last.OK {
		t.Errorf("last frame = %+v, want failed done", last)
	}
}

func TestWorkflowsHTTPCreateListRun(t *testing.T) {
	agent := fakeAgent(t, `{"kind":"exit","code":0}`)
	defer agent.Close()

	dir := t.TempDir()
	store := newAgentStore(filepath.Join(dir, "fleet.json"), []Agent{{Name: "box", Addr: agent.URL}})
	mux := buildMux(store, muxDiscoverer(store), "")

	// Create.
	body := `{"name":"Ping","steps":[{"host":"box","cmd":"echo ok"}]}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflows", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}
	var created Workflow
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("created workflow has no id")
	}

	// Reject an unknown host.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflows", strings.NewReader(`{"name":"x","steps":[{"host":"ghost","cmd":"ls"}]}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown-host create = %d, want 400", rec.Code)
	}

	// List.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workflows", nil))
	var list []Workflow
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}

	// Run streams SSE.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflows/"+created.ID+"/run", nil))
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("run content-type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"kind":"done"`) {
		t.Errorf("run missing done frame: %q", rec.Body.String())
	}

	// Delete, then confirm it's gone.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/workflows/"+created.ID, nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflows/"+created.ID+"/run", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("run after delete = %d, want 404", rec.Code)
	}
}
