package worker

var phaseReflectionMap = map[string][]string{
	"setup": {
		"What is the project's current state? What surprised you?",
		"What files or patterns will be most relevant to this task?",
	},
	"triage": {
		"What type of task is this? What makes it complex or simple?",
		"What could go wrong with this task?",
	},
	"discovery": {
		"What existing code can be reused? What needs to be written from scratch?",
		"What patterns does this codebase follow that you must respect?",
	},
	"skill_discovery": {
		"What domain knowledge is required? What did you learn about the tech stack?",
	},
	"complexity": {
		"What are the biggest risks? What unknowns remain?",
		"Is the estimated complexity accurate? Should it be adjusted?",
	},
	"brainstorming": {
		"What approaches did you consider? Why did you pick this one?",
		"What trade-offs are you accepting with the chosen approach?",
	},
	"architecture": {
		"What trade-offs did you make in the design?",
		"What alternatives did you reject and why?",
		"What would break if requirements change?",
	},
	"implementation": {
		"What was the hardest part to implement?",
		"What shortcuts did you take? Are any of them risky?",
		"What would you do differently if you restarted?",
	},
	"design_verification": {
		"Does the implementation match the architecture plan?",
		"What deviations exist and are they justified?",
	},
	"domain_compliance": {
		"What project-specific rules did you check?",
		"What compliance issues did you find?",
	},
	"code_quality": {
		"What did you find that the implementer missed?",
		"What patterns could be improved without changing behavior?",
	},
	"test_planning": {
		"What edge cases are hardest to test?",
		"What should be tested but cannot easily be automated?",
	},
	"testing": {
		"Which tests were hardest to write? Why?",
		"What code paths remain untested?",
	},
	"coverage_verification": {
		"Are there modified code paths without test coverage?",
		"Is the coverage meaningful or just line-count padding?",
	},
	"test_quality": {
		"Are assertions testing behavior or implementation details?",
		"Would these tests catch a real regression?",
	},
	"completion": {
		"What would you tell the next developer working on this codebase?",
		"What technical debt was introduced? What should be cleaned up later?",
		"Is the task fully done or are there loose ends?",
	},
}

// PhaseReflections returns mandatory exit questions for a phase.
func PhaseReflections(phaseID int, phaseName string) []string {
	if qs, ok := phaseReflectionMap[phaseName]; ok {
		return qs
	}
	return []string{
		"What was the hardest part of this phase?",
		"What would you do differently if you restarted?",
		"What context would help the next phase?",
	}
}
