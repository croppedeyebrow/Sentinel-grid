package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/application"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/domain"
	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/platform/contracts"
)

var (
	generatedAt = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	windowStart = time.Date(2026, 11, 21, 11, 0, 0, 0, time.UTC)
	windowEnd   = time.Date(2026, 11, 21, 13, 0, 0, 0, time.UTC)
)

type stubService struct {
	view    application.MissionView
	err     error
	lastCmd application.CreateMission
	lastKey string
	calls   []string
	version int
	reason  domain.AbortReason
}

func (s *stubService) List(context.Context) ([]application.MissionView, error) {
	s.calls = append(s.calls, "list")
	if s.err != nil {
		return nil, s.err
	}
	return []application.MissionView{s.view}, nil
}

func (s *stubService) Get(context.Context, string) (application.MissionView, error) {
	s.calls = append(s.calls, "get")
	return s.view, s.err
}

func (s *stubService) Create(_ context.Context, command application.CreateMission, key string) (application.MissionView, error) {
	s.calls = append(s.calls, "create")
	s.lastCmd, s.lastKey = command, key
	return s.view, s.err
}

func (s *stubService) AddCheckpoint(context.Context, string, application.NewCheckpoint) (application.MissionView, error) {
	s.calls = append(s.calls, "addCheckpoint")
	return s.view, s.err
}

func (s *stubService) RemoveCheckpoint(context.Context, string, string) (application.MissionView, error) {
	s.calls = append(s.calls, "removeCheckpoint")
	return s.view, s.err
}

func (s *stubService) Assign(context.Context, string, string) (application.MissionView, error) {
	s.calls = append(s.calls, "assign")
	return s.view, s.err
}

func (s *stubService) Unassign(context.Context, string, string) (application.MissionView, error) {
	s.calls = append(s.calls, "unassign")
	return s.view, s.err
}

func (s *stubService) Transition(
	_ context.Context,
	_ string,
	transition domain.Transition,
	expectedVersion int,
	reason domain.AbortReason,
) (application.MissionView, error) {
	s.calls = append(s.calls, string(transition))
	s.version, s.reason = expectedVersion, reason
	return s.view, s.err
}

func activeView() application.MissionView {
	credited := windowStart.Add(12 * time.Minute)
	approved := windowStart.Add(-10 * time.Minute)
	mission := domain.Mission{
		ID:            "50000000-0000-0000-0000-000000000001",
		Name:          "하천변 정기 순찰 (오전)",
		Version:       2,
		Status:        domain.StatusActive,
		PatrolAreaID:  "20000000-0000-0000-0000-000000000001",
		PatrolRouteID: "40000000-0000-0000-0000-000000000001",
		PolicyVersion: 1,
		Window:        domain.Window{Start: windowStart, End: windowEnd},
		Checkpoints: []domain.Checkpoint{
			{ID: "cp-1", Sequence: 1, Name: "CP-1", Position: domain.Position{Latitude: 37.5665, Longitude: 126.978}, RadiusMeters: 40},
			{ID: "cp-2", Sequence: 2, Name: "CP-2", Position: domain.Position{Latitude: 37.57, Longitude: 126.985}, RadiusMeters: 40},
		},
		Assignments: []domain.Assignment{
			{UnitID: "unit-1", Callsign: "SG-01", Window: domain.Window{Start: windowStart, End: windowEnd}},
		},
		CreatedAt:  windowStart.Add(-time.Hour),
		ApprovedAt: &approved,
		StartedAt:  &windowStart,
	}

	judgements := []domain.CheckpointJudgement{
		{CheckpointID: "cp-1", Sequence: 1, Name: "CP-1", State: domain.CheckpointVisited, CreditedAt: &credited},
		{CheckpointID: "cp-2", Sequence: 2, Name: "CP-2", State: domain.CheckpointPlanned},
	}

	return application.MissionView{
		Mission:    mission,
		AreaName:   "Primary Patrol Area",
		RouteName:  "Riverside Loop",
		Judgements: judgements,
		Coverage:   domain.Summarize(judgements),
	}
}

func handlerFor(service MissionService) http.Handler {
	mux := http.NewServeMux()
	handler := NewHandler(service, nil)
	handler.now = func() time.Time { return generatedAt }
	handler.Register(mux)
	return mux
}

func request(t *testing.T, handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	request := httptest.NewRequest(method, path, reader)
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestListMatchesThePublishedContract(t *testing.T) {
	response := request(t, handlerFor(&stubService{view: activeView()}), http.MethodGet, "/api/v1/missions", "", nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	schema, err := contracts.Compile("patrol-mission-list", "patrol-mission")
	if err != nil {
		t.Fatalf("compile contract: %v", err)
	}
	instance, err := jsonOf(response.Body.Bytes())
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if err := schema.Validate(instance); err != nil {
		t.Fatalf("response violates the published contract: %v", err)
	}
}

func TestGetMatchesThePublishedContract(t *testing.T) {
	response := request(t, handlerFor(&stubService{view: activeView()}), http.MethodGet,
		"/api/v1/missions/50000000-0000-0000-0000-000000000001", "", nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	schema, err := contracts.Compile("patrol-mission")
	if err != nil {
		t.Fatalf("compile contract: %v", err)
	}
	instance, err := jsonOf(response.Body.Bytes())
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if err := schema.Validate(instance); err != nil {
		t.Fatalf("response violates the published contract: %v", err)
	}
}

func TestResponseCarriesTheJudgementNotTheStoredPlanAlone(t *testing.T) {
	response := request(t, handlerFor(&stubService{view: activeView()}), http.MethodGet,
		"/api/v1/missions/50000000-0000-0000-0000-000000000001", "", nil)

	var payload struct {
		Status             string   `json:"status"`
		AllowedTransitions []string `json:"allowedTransitions"`
		Checkpoints        []struct {
			State              string  `json:"state"`
			CreditedObservedAt *string `json:"creditedObservedAt"`
		} `json:"checkpoints"`
		Coverage struct {
			Visited int `json:"visited"`
			Missed  int `json:"missed"`
			Planned int `json:"planned"`
			Total   int `json:"total"`
		} `json:"coverage"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.Status != "ACTIVE" {
		t.Errorf("status = %s, want ACTIVE", payload.Status)
	}
	if payload.Checkpoints[0].State != "VISITED" || payload.Checkpoints[0].CreditedObservedAt == nil {
		t.Error("a visited checkpoint must carry the observation that credited it")
	}
	if payload.Checkpoints[1].State != "PLANNED" || payload.Checkpoints[1].CreditedObservedAt != nil {
		t.Error("an unjudged checkpoint carries no credit")
	}
	if payload.Coverage.Visited != 1 || payload.Coverage.Planned != 1 || payload.Coverage.Missed != 0 {
		t.Errorf("coverage = %+v, want one visited and one planned", payload.Coverage)
	}
	// A running mission offers exactly the transitions the aggregate allows.
	if len(payload.AllowedTransitions) != 2 {
		t.Errorf("allowedTransitions = %v, want complete and abort", payload.AllowedTransitions)
	}
}

func TestCreatePassesTheIdempotencyKeyThrough(t *testing.T) {
	service := &stubService{view: activeView()}
	body := `{"name":"오후 순찰","patrolAreaId":"area-1",
	          "window":{"start":"2026-11-21T13:00:00Z","end":"2026-11-21T15:00:00Z"},
	          "checkpoints":[{"name":"CP-1","position":{"latitude":37.5665,"longitude":126.978},"radiusMeters":40}],
	          "assignments":[{"unitId":"unit-1"}]}`

	response := request(t, handlerFor(service), http.MethodPost, "/api/v1/missions", body,
		map[string]string{"Idempotency-Key": "key-1"})

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", response.Code, response.Body.String())
	}
	if service.lastKey != "key-1" {
		t.Errorf("idempotency key = %q, want key-1", service.lastKey)
	}
	if len(service.lastCmd.Checkpoints) != 1 || service.lastCmd.Checkpoints[0].RadiusMeters != 40 {
		t.Errorf("checkpoints = %+v, want the body's checkpoint", service.lastCmd.Checkpoints)
	}
	if len(service.lastCmd.UnitIDs) != 1 || service.lastCmd.UnitIDs[0] != "unit-1" {
		t.Errorf("unitIds = %v, want [unit-1]", service.lastCmd.UnitIDs)
	}
}

func TestBadRequests(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"empty body", http.MethodPost, "/api/v1/missions", ""},
		{"undeclared field", http.MethodPost, "/api/v1/missions", `{"name":"x","patrolAreaId":"a","window":{"start":"2026-11-21T13:00:00Z","end":"2026-11-21T15:00:00Z"},"checkpoints":[],"coverageRatio":0.4}`},
		{"missing window", http.MethodPost, "/api/v1/missions", `{"name":"x","patrolAreaId":"a","checkpoints":[]}`},
		{"checkpoint without a radius", http.MethodPost, "/api/v1/missions/m1/checkpoints", `{"name":"CP","position":{"latitude":37.5,"longitude":126.9}}`},
		{"assignment without a unit", http.MethodPost, "/api/v1/missions/m1/assignments", `{"unitId":"  "}`},
		{"transition without the version", http.MethodPost, "/api/v1/missions/m1/approve", `{}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := &stubService{view: activeView()}
			response := request(t, handlerFor(service), testCase.method, testCase.path, testCase.body, nil)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
			}
			assertErrorCode(t, response.Body.Bytes(), "INVALID_REQUEST")
			if len(service.calls) != 0 {
				t.Errorf("service was called %v, want the request refused before it", service.calls)
			}
		})
	}
}

func TestTransitionCarriesVersionAndReason(t *testing.T) {
	service := &stubService{view: activeView()}

	response := request(t, handlerFor(service), http.MethodPost,
		"/api/v1/missions/m1/abort", `{"version":2,"reason":"BATTERY"}`, nil)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if service.version != 2 || service.reason != domain.AbortBattery {
		t.Errorf("version=%d reason=%s, want 2 and BATTERY", service.version, service.reason)
	}
}

func TestRefusalsMapToThePublishedCodes(t *testing.T) {
	expectedVersion, currentVersion := 2, 5

	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantDetail func(*testing.T, map[string]any)
	}{
		{
			name:       "assignment conflict",
			err:        application.AssignmentConflictError{Callsigns: []string{"SG-01"}},
			wantStatus: http.StatusConflict,
			wantCode:   "ASSIGNMENT_CONFLICT",
			wantDetail: func(t *testing.T, details map[string]any) {
				callsigns, ok := details["callsigns"].([]any)
				if !ok || len(callsigns) != 1 || callsigns[0] != "SG-01" {
					t.Errorf("details = %v, want the conflicting callsign", details)
				}
			},
		},
		{
			name:       "stale version",
			err:        domain.StaleVersionError{Expected: expectedVersion, Current: currentVersion},
			wantStatus: http.StatusConflict,
			wantCode:   "STALE_VERSION",
			wantDetail: func(t *testing.T, details map[string]any) {
				if details["expectedVersion"] != float64(expectedVersion) ||
					details["currentVersion"] != float64(currentVersion) {
					t.Errorf("details = %v, want both versions", details)
				}
			},
		},
		{
			name:       "transition not allowed",
			err:        domain.TransitionNotAllowedError{Transition: domain.TransitionStart, Status: domain.StatusDraft},
			wantStatus: http.StatusConflict,
			wantCode:   "TRANSITION_NOT_ALLOWED",
			wantDetail: func(t *testing.T, details map[string]any) {
				if details["status"] != "DRAFT" || details["transition"] != "start" {
					t.Errorf("details = %v, want the state and the transition", details)
				}
			},
		},
		{
			name:       "checkpoint outside the area",
			err:        domain.CheckpointOutsideError{Name: "CP-구역밖"},
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   "CHECKPOINT_OUTSIDE_AREA",
			wantDetail: func(t *testing.T, details map[string]any) {
				if details["checkpointName"] != "CP-구역밖" {
					t.Errorf("details = %v, want the checkpoint name", details)
				}
			},
		},
		{
			name:       "plan already frozen",
			err:        domain.NotDraftError{Status: domain.StatusReady},
			wantStatus: http.StatusConflict,
			wantCode:   "NOT_DRAFT",
			wantDetail: func(t *testing.T, details map[string]any) {
				if details["status"] != "READY" {
					t.Errorf("details = %v, want the state", details)
				}
			},
		},
		{name: "mission not found", err: application.ErrMissionNotFound, wantStatus: http.StatusNotFound, wantCode: "MISSION_NOT_FOUND"},
		{name: "checkpoint not found", err: application.ErrCheckpointNotFound, wantStatus: http.StatusNotFound, wantCode: "CHECKPOINT_NOT_FOUND"},
		{name: "area not found", err: application.ErrAreaNotFound, wantStatus: http.StatusNotFound, wantCode: "PATROL_AREA_NOT_FOUND"},
		{name: "unit not found", err: application.ErrUnitNotFound, wantStatus: http.StatusNotFound, wantCode: "UNIT_NOT_FOUND"},
		{name: "no checkpoints", err: domain.ErrNoCheckpoints, wantStatus: http.StatusUnprocessableEntity, wantCode: "NO_CHECKPOINTS"},
		{name: "no assignment", err: domain.ErrNoAssignment, wantStatus: http.StatusUnprocessableEntity, wantCode: "NO_ASSIGNMENT"},
		{name: "invalid window", err: domain.ErrInvalidWindow, wantStatus: http.StatusUnprocessableEntity, wantCode: "INVALID_WINDOW"},
		{name: "abort reason required", err: domain.ErrAbortReasonRequired, wantStatus: http.StatusUnprocessableEntity, wantCode: "ABORT_REASON_REQUIRED"},
		{name: "unknown abort reason", err: errors.New("x: " + domain.ErrUnknownAbortReason.Error()), wantStatus: http.StatusServiceUnavailable, wantCode: "PROJECTION_UNAVAILABLE"},
		{name: "not provisioned", err: application.ErrNotProvisioned, wantStatus: http.StatusServiceUnavailable, wantCode: "NOT_PROVISIONED"},
		{name: "store unreachable", err: errors.New("connection refused"), wantStatus: http.StatusServiceUnavailable, wantCode: "PROJECTION_UNAVAILABLE"},
	}

	schema, err := contracts.Compile("api-error")
	if err != nil {
		t.Fatalf("compile contract: %v", err)
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			handler := handlerFor(&stubService{err: testCase.err})

			response := request(t, handler, http.MethodGet, "/api/v1/missions/m1", "", nil)

			if response.Code != testCase.wantStatus {
				t.Errorf("status = %d, want %d", response.Code, testCase.wantStatus)
			}
			assertErrorCode(t, response.Body.Bytes(), testCase.wantCode)

			instance, err := jsonOf(response.Body.Bytes())
			if err != nil {
				t.Fatalf("parse response: %v", err)
			}
			if err := schema.Validate(instance); err != nil {
				t.Fatalf("refusal violates the published contract: %v", err)
			}

			if testCase.wantDetail != nil {
				var payload struct {
					Details map[string]any `json:"details"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				testCase.wantDetail(t, payload.Details)
			}
		})
	}
}

func assertErrorCode(t *testing.T, body []byte, want string) {
	t.Helper()

	var payload struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Code != want {
		t.Errorf("code = %s, want %s", payload.Code, want)
	}
	if payload.Error == "" {
		t.Error("a refusal must carry a sentence for the operator")
	}
}

func jsonOf(body []byte) (any, error) {
	var instance any
	if err := json.Unmarshal(body, &instance); err != nil {
		return nil, err
	}
	return instance, nil
}
