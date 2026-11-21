package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/patrolmission/domain"
)

var (
	now         = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	windowStart = time.Date(2026, 11, 21, 11, 30, 0, 0, time.UTC)
	windowEnd   = time.Date(2026, 11, 21, 13, 30, 0, 0, time.UTC)
)

func testWindow() domain.Window { return domain.Window{Start: windowStart, End: windowEnd} }

func testArea() domain.Area {
	return domain.Area{
		ID: "area-1",
		Boundary: []domain.Position{
			{Latitude: 37.558, Longitude: 126.965},
			{Latitude: 37.558, Longitude: 126.991},
			{Latitude: 37.575, Longitude: 126.991},
			{Latitude: 37.575, Longitude: 126.965},
			{Latitude: 37.558, Longitude: 126.965},
		},
	}
}

var checkpointPosition = domain.Position{Latitude: 37.5665, Longitude: 126.978}

// fakeRepository records what the service asked it to persist, so the tests
// can assert on the optimistic locking contract as well as the outcome.
type fakeRepository struct {
	site         Site
	area         domain.Area
	areaName     string
	routeName    string
	callsigns    map[string]string
	missions     map[string]domain.Mission
	byKey        map[string]string
	commitments  []domain.Commitment
	observations []domain.Observation

	created         []domain.Mission
	createdKeys     []string
	saved           []domain.Mission
	savedExpected   []int
	callsignsAsked  [][]string
	observationsFor []string
	areaErr         error
	saveErr         error
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		site:      Site{ID: "site-1", ReceiptAllowance: 45 * time.Second, PolicyVersion: 3},
		area:      testArea(),
		areaName:  "Primary Patrol Area",
		routeName: "Riverside Loop",
		callsigns: map[string]string{"unit-1": "SG-01", "unit-2": "SG-02"},
		missions:  map[string]domain.Mission{},
		byKey:     map[string]string{},
	}
}

func (f *fakeRepository) Site(context.Context) (Site, error) { return f.site, nil }

func (f *fakeRepository) Area(_ context.Context, areaID string) (domain.Area, string, error) {
	if f.areaErr != nil {
		return domain.Area{}, "", f.areaErr
	}
	if areaID != f.area.ID {
		return domain.Area{}, "", ErrAreaNotFound
	}
	return f.area, f.areaName, nil
}

func (f *fakeRepository) RouteName(context.Context, string) (string, error) {
	return f.routeName, nil
}

func (f *fakeRepository) Callsigns(_ context.Context, unitIDs []string) (map[string]string, error) {
	f.callsignsAsked = append(f.callsignsAsked, unitIDs)
	resolved := make(map[string]string, len(unitIDs))
	for _, unitID := range unitIDs {
		callsign, found := f.callsigns[unitID]
		if !found {
			return nil, ErrUnitNotFound
		}
		resolved[unitID] = callsign
	}
	return resolved, nil
}

func (f *fakeRepository) Missions(context.Context) ([]domain.Mission, error) {
	missions := make([]domain.Mission, 0, len(f.missions))
	for _, mission := range f.missions {
		missions = append(missions, mission)
	}
	return missions, nil
}

func (f *fakeRepository) Mission(_ context.Context, missionID string) (domain.Mission, error) {
	mission, found := f.missions[missionID]
	if !found {
		return domain.Mission{}, ErrMissionNotFound
	}
	return mission, nil
}

func (f *fakeRepository) MissionByIdempotencyKey(ctx context.Context, key string) (domain.Mission, error) {
	missionID, found := f.byKey[key]
	if !found {
		return domain.Mission{}, ErrMissionNotFound
	}
	return f.Mission(ctx, missionID)
}

func (f *fakeRepository) Commitments(context.Context, domain.Window) ([]domain.Commitment, error) {
	return f.commitments, nil
}

func (f *fakeRepository) Observations(_ context.Context, mission domain.Mission) ([]domain.Observation, error) {
	f.observationsFor = append(f.observationsFor, mission.ID)
	return f.observations, nil
}

func (f *fakeRepository) Create(_ context.Context, _ string, mission domain.Mission, key string) error {
	f.created = append(f.created, mission)
	f.createdKeys = append(f.createdKeys, key)
	f.missions[mission.ID] = mission
	f.byKey[key] = mission.ID
	return nil
}

func (f *fakeRepository) Save(_ context.Context, mission domain.Mission, expectedVersion int) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, mission)
	f.savedExpected = append(f.savedExpected, expectedVersion)
	f.missions[mission.ID] = mission
	return nil
}

func testService(repository Repository) *Service {
	counter := 0
	return NewService(
		repository,
		func() (string, error) {
			counter++
			return "id-" + string(rune('0'+counter)), nil
		},
		WithClock(func() time.Time { return now }),
	)
}

func createCommand() CreateMission {
	return CreateMission{
		Name:         "  하천변 정기 순찰  ",
		PatrolAreaID: "area-1",
		Window:       testWindow(),
		Checkpoints: []NewCheckpoint{
			{Name: "CP-1", Position: checkpointPosition, RadiusMeters: 40},
			{Name: "CP-2", Position: domain.Position{Latitude: 37.57, Longitude: 126.98}, RadiusMeters: 40},
		},
		UnitIDs: []string{"unit-1"},
	}
}

func TestCreateOpensADraft(t *testing.T) {
	repository := newFakeRepository()

	view, err := testService(repository).Create(context.Background(), createCommand(), "key-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mission := view.Mission
	if mission.Status != domain.StatusDraft {
		t.Errorf("status = %s, want DRAFT", mission.Status)
	}
	if mission.Name != "하천변 정기 순찰" {
		t.Errorf("name = %q, want it trimmed", mission.Name)
	}
	if len(mission.Checkpoints) != 2 {
		t.Fatalf("checkpoints = %d, want 2", len(mission.Checkpoints))
	}
	for index, checkpoint := range mission.Checkpoints {
		if checkpoint.Sequence != index+1 {
			t.Errorf("checkpoint %d sequence = %d, want %d", index, checkpoint.Sequence, index+1)
		}
	}
	if len(mission.Assignments) != 1 || mission.Assignments[0].Callsign != "SG-01" {
		t.Errorf("assignments = %+v, want SG-01 resolved from the unit id", mission.Assignments)
	}
	if len(repository.created) != 1 || repository.createdKeys[0] != "key-1" {
		t.Error("the mission and its idempotency key must be stored together")
	}
	// A draft has no verdicts yet.
	if view.Coverage.Planned != 2 || view.Coverage.Total != 2 {
		t.Errorf("coverage = %+v, want two planned checkpoints", view.Coverage)
	}
}

func TestCreateIsIdempotent(t *testing.T) {
	repository := newFakeRepository()
	service := testService(repository)

	first, err := service.Create(context.Background(), createCommand(), "key-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := service.Create(context.Background(), createCommand(), "key-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first.Mission.ID != second.Mission.ID {
		t.Errorf("ids %s and %s differ, want the same mission returned", first.Mission.ID, second.Mission.ID)
	}
	if len(repository.created) != 1 {
		t.Errorf("created %d missions, want 1", len(repository.created))
	}
}

func TestCreateRefusals(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		mutate  func(*CreateMission)
		prepare func(*fakeRepository)
		wantErr error
	}{
		{
			name:    "without an idempotency key",
			key:     "  ",
			mutate:  func(*CreateMission) {},
			wantErr: ErrIdempotencyKeyRequired,
		},
		{
			name:    "with a window that ends before it starts",
			key:     "key-1",
			mutate:  func(c *CreateMission) { c.Window = domain.Window{Start: windowEnd, End: windowStart} },
			wantErr: domain.ErrInvalidWindow,
		},
		{
			name:    "with an unknown patrol area",
			key:     "key-1",
			mutate:  func(c *CreateMission) { c.PatrolAreaID = "area-missing" },
			wantErr: ErrAreaNotFound,
		},
		{
			name:    "with an unknown unit",
			key:     "key-1",
			mutate:  func(c *CreateMission) { c.UnitIDs = []string{"unit-missing"} },
			wantErr: ErrUnitNotFound,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := newFakeRepository()
			if testCase.prepare != nil {
				testCase.prepare(repository)
			}
			command := createCommand()
			testCase.mutate(&command)

			_, err := testService(repository).Create(context.Background(), command, testCase.key)

			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("err = %v, want %v", err, testCase.wantErr)
			}
			if len(repository.created) != 0 {
				t.Error("a refused create must not store a mission")
			}
		})
	}
}

// approvableDraft stores a draft that satisfies every approval precondition.
func approvableDraft(repository *fakeRepository) domain.Mission {
	mission := domain.Mission{
		ID:           "mission-1",
		Name:         "하천변 정기 순찰",
		Status:       domain.StatusDraft,
		PatrolAreaID: "area-1",
		Window:       testWindow(),
		Checkpoints: []domain.Checkpoint{
			{ID: "cp-1", Sequence: 1, Name: "CP-1", Position: checkpointPosition, RadiusMeters: 40},
		},
		Assignments: []domain.Assignment{
			{UnitID: "unit-1", Callsign: "SG-01", Window: testWindow()},
		},
		CreatedAt: now.Add(-time.Hour),
	}
	repository.missions[mission.ID] = mission
	return mission
}

func TestApproveFreezesThePlanAndLocksOnVersion(t *testing.T) {
	repository := newFakeRepository()
	mission := approvableDraft(repository)

	view, err := testService(repository).Transition(
		context.Background(), mission.ID, domain.TransitionApprove, mission.Version, "",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if view.Mission.Status != domain.StatusReady {
		t.Errorf("status = %s, want READY", view.Mission.Status)
	}
	if len(repository.savedExpected) != 1 || repository.savedExpected[0] != mission.Version {
		t.Errorf("saved with expected version %v, want %d", repository.savedExpected, mission.Version)
	}
	if repository.saved[0].Version != mission.Version+1 {
		t.Errorf("stored version = %d, want %d", repository.saved[0].Version, mission.Version+1)
	}
	if view.Mission.PolicyVersion != repository.site.PolicyVersion {
		t.Errorf("policyVersion = %d, want the site revision %d pinned", view.Mission.PolicyVersion, repository.site.PolicyVersion)
	}
}

func TestApproveRefusesAUnitCommittedElsewhere(t *testing.T) {
	repository := newFakeRepository()
	mission := approvableDraft(repository)
	repository.commitments = []domain.Commitment{
		{MissionID: "other", UnitID: "unit-1", Callsign: "SG-01", Window: testWindow()},
	}

	_, err := testService(repository).Transition(
		context.Background(), mission.ID, domain.TransitionApprove, mission.Version, "",
	)

	var conflict AssignmentConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want AssignmentConflictError", err)
	}
	if len(conflict.Callsigns) != 1 || conflict.Callsigns[0] != "SG-01" {
		t.Errorf("callsigns = %v, want [SG-01]", conflict.Callsigns)
	}
	if len(repository.saved) != 0 {
		t.Error("a refused approval must not be stored")
	}
}

func TestTransitionPassesTheDomainRefusalsThrough(t *testing.T) {
	cases := []struct {
		name       string
		transition domain.Transition
		reason     domain.AbortReason
		version    int
		wantErr    error
	}{
		{"start before approval", domain.TransitionStart, "", 0, domain.ErrTransitionNotAllowed},
		{"stale version", domain.TransitionApprove, "", 7, domain.ErrStaleVersion},
		{"abort without a reason", domain.TransitionAbort, "", 0, domain.ErrAbortReasonRequired},
		{"abort with an unknown reason", domain.TransitionAbort, "BECAUSE", 0, domain.ErrUnknownAbortReason},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := newFakeRepository()
			mission := approvableDraft(repository)

			_, err := testService(repository).Transition(
				context.Background(), mission.ID, testCase.transition, testCase.version, testCase.reason,
			)

			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("err = %v, want %v", err, testCase.wantErr)
			}
			if len(repository.saved) != 0 {
				t.Error("a refused transition must not be stored")
			}
		})
	}
}

func TestGetReportsAMissingMission(t *testing.T) {
	_, err := testService(newFakeRepository()).Get(context.Background(), "mission-missing")

	if !errors.Is(err, ErrMissionNotFound) {
		t.Fatalf("err = %v, want ErrMissionNotFound", err)
	}
}

func TestCoverageIsJudgedFromObservations(t *testing.T) {
	observed := windowStart.Add(10 * time.Minute)
	nearby := domain.Position{Latitude: checkpointPosition.Latitude + 0.0001, Longitude: checkpointPosition.Longitude}

	cases := []struct {
		name        string
		status      domain.Status
		observation domain.Observation
		wantState   domain.CheckpointState
	}{
		{
			name:   "a promptly received observation credits the pass",
			status: domain.StatusActive,
			observation: domain.Observation{
				Position: &nearby, ObservedAt: &observed, ReceivedAt: &observed,
			},
			wantState: domain.CheckpointVisited,
		},
		{
			name:   "an observation received too late cannot credit it",
			status: domain.StatusActive,
			observation: domain.Observation{
				Position:   &nearby,
				ObservedAt: &observed,
				ReceivedAt: timePointer(observed.Add(10 * time.Minute)),
			},
			wantState: domain.CheckpointPlanned,
		},
		{
			name:   "a finished mission with usable observations reports the gap",
			status: domain.StatusCompleted,
			observation: domain.Observation{
				Position:   timePositionPointer(domain.Position{Latitude: 37.5, Longitude: 126.9}),
				ObservedAt: &observed,
				ReceivedAt: &observed,
			},
			wantState: domain.CheckpointMissed,
		},
		{
			name:   "a finished mission without usable observations stays undecided",
			status: domain.StatusCompleted,
			observation: domain.Observation{
				Position:   &nearby,
				ObservedAt: &observed,
				ReceivedAt: timePointer(observed.Add(10 * time.Minute)),
			},
			wantState: domain.CheckpointUnknown,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := newFakeRepository()
			mission := approvableDraft(repository)
			mission.Status = testCase.status
			repository.missions[mission.ID] = mission
			repository.observations = []domain.Observation{testCase.observation}

			view, err := testService(repository).Get(context.Background(), mission.ID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(view.Judgements) != 1 {
				t.Fatalf("judgements = %d, want 1", len(view.Judgements))
			}
			if view.Judgements[0].State != testCase.wantState {
				t.Errorf("state = %s, want %s", view.Judgements[0].State, testCase.wantState)
			}
			if view.AreaName != "Primary Patrol Area" {
				t.Errorf("areaName = %q, want the area name resolved", view.AreaName)
			}
		})
	}
}

func timePointer(value time.Time) *time.Time { return &value }

func timePositionPointer(value domain.Position) *domain.Position { return &value }
