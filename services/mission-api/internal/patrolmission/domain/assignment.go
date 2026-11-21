package domain

// Assignment binds one patrol unit to a mission for an operating window.
type Assignment struct {
	UnitID   string
	Callsign string
	Window   Window
}

// Commitment is an assignment a unit already holds on another mission. The
// application loads these, because no single mission can see them.
type Commitment struct {
	MissionID string
	UnitID    string
	Callsign  string
	Window    Window
}

// ConflictingCallsigns returns the units of this mission that are already
// committed to an overlapping window elsewhere. One unit cannot hold two
// overlapping assignments, and the refusal names the units so the planner can
// act on it.
func ConflictingCallsigns(mission Mission, committed []Commitment) []string {
	conflicts := make([]string, 0)
	seen := make(map[string]bool, len(mission.Assignments))

	for _, assignment := range mission.Assignments {
		for _, commitment := range committed {
			if commitment.MissionID == mission.ID {
				continue
			}
			if commitment.UnitID != assignment.UnitID {
				continue
			}
			if !assignment.Window.Overlaps(commitment.Window) {
				continue
			}
			if seen[assignment.Callsign] {
				continue
			}
			seen[assignment.Callsign] = true
			conflicts = append(conflicts, assignment.Callsign)
		}
	}

	return conflicts
}
