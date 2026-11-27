package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/twinklehawk/vikunja-client-go"
)

// fakeResponse is a canned response returned by [fakeVikunja].
type fakeResponse struct {
	status int
	body   any
}

// recordedRequest is a request received by [fakeVikunja].
type recordedRequest struct {
	method string
	path   string
	query  string
	body   string
}

// fakeVikunja is a fake Vikunja API server. Requests are matched against routes keyed by
// "<METHOD> <path>" and optionally "<METHOD> <path>?<query>"; the query form takes precedence.
// Unmatched requests get a 404.
type fakeVikunja struct {
	t      *testing.T
	routes map[string]fakeResponse

	mu       sync.Mutex
	requests []recordedRequest
}

func newFakeVikunja(t *testing.T, routes map[string]fakeResponse) (*fakeVikunja, *vikunja.Client) {
	t.Helper()
	f := &fakeVikunja{t: t, routes: routes}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	client := vikunja.NewClient(
		vikunja.WithBaseURL(server.URL),
		vikunja.WithHTTPClient(server.Client()),
	)
	return f, client
}

func (f *fakeVikunja) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Errorf("reading request body: %v", err)
	}
	f.mu.Lock()
	f.requests = append(f.requests, recordedRequest{
		method: r.Method,
		path:   r.URL.Path,
		query:  r.URL.RawQuery,
		body:   strings.TrimSpace(string(body)),
	})
	f.mu.Unlock()

	resp, ok := f.routes[r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery]
	if !ok {
		resp, ok = f.routes[r.Method+" "+r.URL.Path]
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	status := resp.status
	if status == 0 {
		status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if resp.body != nil {
		if err := json.NewEncoder(w).Encode(resp.body); err != nil {
			f.t.Errorf("encoding response: %v", err)
		}
	}
}

// writes returns the non-GET requests received, which are the ones that modify data.
func (f *fakeVikunja) writes() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var writes []recordedRequest
	for _, r := range f.requests {
		if r.method != http.MethodGet {
			writes = append(writes, r)
		}
	}
	return writes
}

func assertWrites(t *testing.T, f *fakeVikunja, want []recordedRequest) {
	t.Helper()
	got := f.writes()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	// query is irrelevant for writes
	for i := range got {
		got[i].query = ""
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("write requests = %+v, want %+v", got, want)
	}
}

func onePage[T any](items ...T) map[string]any {
	return map[string]any{"items": items, "page": 1, "total_pages": 1}
}

func TestCanHandleJob(t *testing.T) {
	handlers := map[string]jobHandler{
		"CreateTask":      &createTaskHandler{},
		"ApplyTodayLabel": &applyTodayLabelHandler{},
	}
	jobTypes := []string{"CreateTask", "ApplyTodayLabel", "createtask", "", "Unknown"}

	for handlerType, h := range handlers {
		for _, jobType := range jobTypes {
			got := h.canHandleJob(&JobConfig{JobType: jobType})
			want := jobType == handlerType
			if got != want {
				t.Errorf("%T.canHandleJob(%q) = %v, want %v", h, jobType, got, want)
			}
		}
	}
}

func TestParseCreateTaskConfig(t *testing.T) {
	tests := []struct {
		name    string
		params  map[string]string
		want    createTaskConfig
		wantErr bool
	}{
		{
			name: "all parameters",
			params: map[string]string{
				"title":       "Water plants",
				"project":     "Home",
				"description": "All of them",
				"labels":      "Chores,Today",
			},
			want: createTaskConfig{
				title:       "Water plants",
				project:     "Home",
				description: "All of them",
				labels:      []string{"Chores", "Today"},
			},
		},
		{
			name:   "required parameters only",
			params: map[string]string{"title": "Water plants", "project": "Home"},
			want: createTaskConfig{
				title:   "Water plants",
				project: "Home",
			},
		},
		{
			name: "labels are trimmed, deduplicated and empty ones dropped",
			params: map[string]string{
				"title":   "Water plants",
				"project": "Home",
				"labels":  " Chores , Today,,Chores, ,Garden ",
			},
			want: createTaskConfig{
				title:   "Water plants",
				project: "Home",
				labels:  []string{"Chores", "Today", "Garden"},
			},
		},
		{
			name: "labels with only separators and whitespace",
			params: map[string]string{
				"title":   "Water plants",
				"project": "Home",
				"labels":  " , ,",
			},
			want: createTaskConfig{
				title:   "Water plants",
				project: "Home",
			},
		},
		{
			name:    "missing title",
			params:  map[string]string{"project": "Home"},
			wantErr: true,
		},
		{
			name:    "missing project",
			params:  map[string]string{"title": "Water plants"},
			wantErr: true,
		},
		{
			name:    "nil parameters",
			params:  nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCreateTaskConfig(&JobConfig{Parameters: tt.params})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseCreateTaskConfig() expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCreateTaskConfig() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseCreateTaskConfig() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func createTaskRoutes() map[string]fakeResponse {
	return map[string]fakeResponse{
		"GET /api/v2/projects": {body: onePage(
			vikunja.Project{ID: 3, Title: "Home Office"},
			vikunja.Project{ID: 7, Title: "Home"},
		)},
		"POST /api/v2/projects/7/tasks": {
			status: http.StatusCreated,
			body:   vikunja.Task{ID: 42, Title: "Water plants", ProjectID: 7},
		},
		"GET /api/v2/labels?page=1&q=Chores": {
			body: onePage(vikunja.Label{ID: 11, Title: "Chores"}),
		},
		"GET /api/v2/labels?page=1&q=Today": {
			body: onePage(vikunja.Label{ID: 12, Title: "Today"}),
		},
		"POST /api/v2/tasks/42/labels": {status: http.StatusCreated},
	}
}

func TestCreateTaskHandleJob(t *testing.T) {
	f, client := newFakeVikunja(t, createTaskRoutes())
	h := &createTaskHandler{client: client}

	err := h.handleJob(context.Background(), &JobConfig{
		Name:    "water plants",
		JobType: "CreateTask",
		Parameters: map[string]string{
			"title":       "Water plants",
			"project":     "Home",
			"description": "All of them",
			"labels":      "Chores, Today,,Chores",
		},
	})
	if err != nil {
		t.Fatalf("handleJob() unexpected error: %v", err)
	}

	assertWrites(t, f, []recordedRequest{
		{
			method: http.MethodPost,
			path:   "/api/v2/projects/7/tasks",
			body:   `{"title":"Water plants","project_id":7,"description":"All of them","bucket_id":0,"buckets":null,"labels":null}`,
		},
		{method: http.MethodPost, path: "/api/v2/tasks/42/labels", body: `{"label_id":11}`},
		{method: http.MethodPost, path: "/api/v2/tasks/42/labels", body: `{"label_id":12}`},
	})
}

func TestCreateTaskHandleJobNoLabels(t *testing.T) {
	f, client := newFakeVikunja(t, createTaskRoutes())
	h := &createTaskHandler{client: client}

	err := h.handleJob(context.Background(), &JobConfig{
		Parameters: map[string]string{"title": "Water plants", "project": "Home"},
	})
	if err != nil {
		t.Fatalf("handleJob() unexpected error: %v", err)
	}

	writes := f.writes()
	if len(writes) != 1 || writes[0].path != "/api/v2/projects/7/tasks" {
		t.Errorf("write requests = %+v, want only the task creation", writes)
	}
}

func TestCreateTaskHandleJobErrors(t *testing.T) {
	validParams := map[string]string{
		"title":   "Water plants",
		"project": "Home",
		"labels":  "Chores",
	}
	tests := []struct {
		name         string
		params       map[string]string
		override     map[string]fakeResponse
		wantInErr    string
		wantNoWrites bool
	}{
		{
			name:         "invalid parameters",
			params:       map[string]string{"title": "Water plants"},
			wantInErr:    "cannot be empty",
			wantNoWrites: true,
		},
		{
			name:   "project lookup fails",
			params: validParams,
			override: map[string]fakeResponse{
				"GET /api/v2/projects": {status: http.StatusInternalServerError},
			},
			wantInErr:    "finding project",
			wantNoWrites: true,
		},
		{
			name:   "project not found",
			params: validParams,
			override: map[string]fakeResponse{
				"GET /api/v2/projects": {body: onePage(vikunja.Project{ID: 3, Title: "Work"})},
			},
			wantInErr:    "finding project",
			wantNoWrites: true,
		},
		{
			name:   "task creation fails",
			params: validParams,
			override: map[string]fakeResponse{
				"POST /api/v2/projects/7/tasks": {status: http.StatusForbidden},
			},
			wantInErr: "creating task",
		},
		{
			name:   "created task has no ID",
			params: validParams,
			override: map[string]fakeResponse{
				"POST /api/v2/projects/7/tasks": {
					status: http.StatusCreated,
					body:   vikunja.Task{Title: "Water plants"},
				},
			},
			wantInErr: "does not have ID",
		},
		{
			name:   "label not found",
			params: validParams,
			override: map[string]fakeResponse{
				"GET /api/v2/labels?page=1&q=Chores": {body: onePage[vikunja.Label]()},
			},
			wantInErr:    "finding label 'Chores'",
			wantNoWrites: true,
		},
		{
			name:   "adding label fails",
			params: validParams,
			override: map[string]fakeResponse{
				"POST /api/v2/tasks/42/labels": {status: http.StatusBadRequest},
			},
			wantInErr: "adding label 'Chores'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes := createTaskRoutes()
			for k, v := range tt.override {
				routes[k] = v
			}
			f, client := newFakeVikunja(t, routes)
			h := &createTaskHandler{client: client}

			err := h.handleJob(context.Background(), &JobConfig{Parameters: tt.params})
			if err == nil {
				t.Fatal("handleJob() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantInErr) {
				t.Errorf("handleJob() error = %q, want it to contain %q", err, tt.wantInErr)
			}
			if tt.wantNoWrites {
				assertWrites(t, f, nil)
			}
		})
	}
}

const (
	inboxID       = 5
	priorityView  = 20
	otherView     = 21
	nowBucket     = 30
	laterBucket   = 31
	todayLabel    = 12
	otherLabel    = 13
	tasksPagePath = "/api/v2/projects/5/views/20/tasks"
)

func applyTodayLabelRoutes(tasks ...vikunja.Task) map[string]fakeResponse {
	return map[string]fakeResponse{
		"GET /api/v2/projects": {body: onePage(vikunja.Project{ID: inboxID, Title: "Inbox"})},
		"GET /api/v2/projects/5/views": {body: onePage(
			vikunja.ProjectView{ID: 19, Title: "List"},
			vikunja.ProjectView{ID: priorityView, Title: "Priority"},
		)},
		"GET /api/v2/projects/5/views/20/buckets": {body: onePage(
			vikunja.Bucket{ID: laterBucket, Title: "Later"},
			vikunja.Bucket{ID: nowBucket, Title: "Now"},
		)},
		"GET /api/v2/labels": {
			body: onePage(vikunja.Label{ID: todayLabel, Title: "Today"}),
		},
		"GET " + tasksPagePath:             {body: onePage(tasks...)},
		"POST /api/v2/tasks/1/labels":      {status: http.StatusCreated},
		"POST /api/v2/tasks/2/labels":      {status: http.StatusCreated},
		"POST /api/v2/tasks/3/labels":      {status: http.StatusCreated},
		"POST /api/v2/tasks/4/labels":      {status: http.StatusCreated},
		"POST /api/v2/tasks/5/labels":      {status: http.StatusCreated},
		"DELETE /api/v2/tasks/1/labels/12": {},
		"DELETE /api/v2/tasks/2/labels/12": {},
		"DELETE /api/v2/tasks/3/labels/12": {},
		"DELETE /api/v2/tasks/4/labels/12": {},
		"DELETE /api/v2/tasks/5/labels/12": {},
	}
}

func TestApplyTodayLabelHandleJob(t *testing.T) {
	inNow := []vikunja.Bucket{{ID: nowBucket, ProjectViewID: priorityView}}
	inLater := []vikunja.Bucket{{ID: laterBucket, ProjectViewID: priorityView}}
	// same bucket ID as Now, but in a different view
	inOtherView := []vikunja.Bucket{{ID: nowBucket, ProjectViewID: otherView}}
	today := []vikunja.Label{{ID: todayLabel, Title: "Today"}}
	other := []vikunja.Label{{ID: otherLabel, Title: "Other"}}

	f, client := newFakeVikunja(t, applyTodayLabelRoutes(
		vikunja.Task{ID: 1, Title: "in Now, no label", Buckets: inNow, Labels: other},
		vikunja.Task{ID: 2, Title: "in Now, has label", Buckets: inNow, Labels: today},
		vikunja.Task{ID: 3, Title: "in Later, has label", Buckets: inLater, Labels: today},
		vikunja.Task{ID: 4, Title: "in Later, no label", Buckets: inLater},
		vikunja.Task{
			ID:      5,
			Title:   "Now in other view, has label",
			Buckets: inOtherView,
			Labels:  today,
		},
	))
	h := &applyTodayLabelHandler{client: client}

	if err := h.handleJob(
		context.Background(),
		&JobConfig{JobType: "ApplyTodayLabel"},
	); err != nil {
		t.Fatalf("handleJob() unexpected error: %v", err)
	}

	assertWrites(t, f, []recordedRequest{
		{method: http.MethodPost, path: "/api/v2/tasks/1/labels", body: `{"label_id":12}`},
		{method: http.MethodDelete, path: "/api/v2/tasks/3/labels/12"},
		{method: http.MethodDelete, path: "/api/v2/tasks/5/labels/12"},
	})
}

func TestApplyTodayLabelHandleJobRequestsBuckets(t *testing.T) {
	f, client := newFakeVikunja(t, applyTodayLabelRoutes())
	h := &applyTodayLabelHandler{client: client}

	if err := h.handleJob(context.Background(), &JobConfig{}); err != nil {
		t.Fatalf("handleJob() unexpected error: %v", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		if r.path == tasksPagePath {
			if !strings.Contains(r.query, "expand=buckets") {
				t.Errorf("task list query = %q, want it to expand buckets", r.query)
			}
			return
		}
	}
	t.Error("tasks in the Priority view were never listed")
}

func TestApplyTodayLabelHandleJobPaginates(t *testing.T) {
	inNow := []vikunja.Bucket{{ID: nowBucket, ProjectViewID: priorityView}}
	routes := applyTodayLabelRoutes()
	delete(routes, "GET "+tasksPagePath)
	routes["GET "+tasksPagePath+"?expand=buckets&page=1"] = fakeResponse{body: map[string]any{
		"items":       []vikunja.Task{{ID: 1, Buckets: inNow}},
		"page":        1,
		"total_pages": 2,
	}}
	routes["GET "+tasksPagePath+"?expand=buckets&page=2"] = fakeResponse{body: map[string]any{
		"items":       []vikunja.Task{{ID: 2, Buckets: inNow}},
		"page":        2,
		"total_pages": 2,
	}}
	f, client := newFakeVikunja(t, routes)
	h := &applyTodayLabelHandler{client: client}

	if err := h.handleJob(context.Background(), &JobConfig{}); err != nil {
		t.Fatalf("handleJob() unexpected error: %v", err)
	}

	assertWrites(t, f, []recordedRequest{
		{method: http.MethodPost, path: "/api/v2/tasks/1/labels", body: `{"label_id":12}`},
		{method: http.MethodPost, path: "/api/v2/tasks/2/labels", body: `{"label_id":12}`},
	})
}

func TestApplyTodayLabelHandleJobErrors(t *testing.T) {
	inNow := []vikunja.Bucket{{ID: nowBucket, ProjectViewID: priorityView}}
	today := []vikunja.Label{{ID: todayLabel}}
	tests := []struct {
		name      string
		tasks     []vikunja.Task
		override  map[string]fakeResponse
		wantInErr string
	}{
		{
			name: "inbox project not found",
			override: map[string]fakeResponse{
				"GET /api/v2/projects": {body: onePage(vikunja.Project{ID: 1, Title: "Work"})},
			},
			wantInErr: "finding project",
		},
		{
			name: "priority view not found",
			override: map[string]fakeResponse{
				"GET /api/v2/projects/5/views": {
					body: onePage(vikunja.ProjectView{ID: 19, Title: "List"}),
				},
			},
			wantInErr: "finding project view",
		},
		{
			name: "now bucket not found",
			override: map[string]fakeResponse{
				"GET /api/v2/projects/5/views/20/buckets": {status: http.StatusInternalServerError},
			},
			wantInErr: "finding bucket in view",
		},
		{
			name: "today label not found",
			override: map[string]fakeResponse{
				"GET /api/v2/labels": {body: onePage[vikunja.Label]()},
			},
			wantInErr: "finding label",
		},
		{
			name: "listing tasks fails",
			override: map[string]fakeResponse{
				"GET " + tasksPagePath: {status: http.StatusInternalServerError},
			},
			wantInErr: "iterating through tasks",
		},
		{
			name:  "adding label fails",
			tasks: []vikunja.Task{{ID: 1, Buckets: inNow}},
			override: map[string]fakeResponse{
				"POST /api/v2/tasks/1/labels": {status: http.StatusBadRequest},
			},
			wantInErr: "adding task label",
		},
		{
			name:  "removing label fails",
			tasks: []vikunja.Task{{ID: 1, Labels: today}},
			override: map[string]fakeResponse{
				"DELETE /api/v2/tasks/1/labels/12": {status: http.StatusInternalServerError},
			},
			wantInErr: "removing task label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes := applyTodayLabelRoutes(tt.tasks...)
			for k, v := range tt.override {
				routes[k] = v
			}
			_, client := newFakeVikunja(t, routes)
			h := &applyTodayLabelHandler{client: client}

			err := h.handleJob(context.Background(), &JobConfig{})
			if err == nil {
				t.Fatal("handleJob() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantInErr) {
				t.Errorf("handleJob() error = %q, want it to contain %q", err, tt.wantInErr)
			}
		})
	}
}
