package agent

// AgentMode defines a pre-configured set of permissions and behavior.
type AgentMode struct {
	Name        string
	Description string
	Permissions Ruleset
	MaxTurns    int
	ReadOnly    bool
}

// PlanMode creates a read-only agent that can analyze but not modify.
func PlanMode() AgentMode {
	return AgentMode{
		Name:        "plan",
		Description: "Read-only planning mode for analysis before implementation",
		ReadOnly:    true,
		MaxTurns:    20,
		Permissions: Ruleset{
			{Permission: "read", Pattern: "*", Action: ActionAllow},
			{Permission: "glob", Pattern: "*", Action: ActionAllow},
			{Permission: "grep", Pattern: "*", Action: ActionAllow},
			{Permission: "getsymbol", Pattern: "*", Action: ActionAllow},
			{Permission: "gettypedefinition", Pattern: "*", Action: ActionAllow},
			{Permission: "getdiff", Pattern: "*", Action: ActionAllow},
			{Permission: "getcommitlog", Pattern: "*", Action: ActionAllow},
			{Permission: "treesummary", Pattern: "*", Action: ActionAllow},
			{Permission: "todowrite", Pattern: "*", Action: ActionAllow},
			{Permission: "todoread", Pattern: "*", Action: ActionAllow},
			{Permission: "edit", Pattern: "*", Action: ActionDeny},
			{Permission: "bash", Pattern: "*", Action: ActionDeny},
			{Permission: "subagent", Pattern: "*", Action: ActionDeny},
		},
	}
}

// ExploreMode creates a fast, read-only codebase search agent.
func ExploreMode() AgentMode {
	return AgentMode{
		Name:        "explore",
		Description: "Fast codebase exploration — read-only search and navigation",
		ReadOnly:    true,
		MaxTurns:    10,
		Permissions: Ruleset{
			{Permission: "read", Pattern: "*", Action: ActionAllow},
			{Permission: "glob", Pattern: "*", Action: ActionAllow},
			{Permission: "grep", Pattern: "*", Action: ActionAllow},
			{Permission: "getsymbol", Pattern: "*", Action: ActionAllow},
			{Permission: "gettypedefinition", Pattern: "*", Action: ActionAllow},
			{Permission: "treesummary", Pattern: "*", Action: ActionAllow},
			{Permission: "edit", Pattern: "*", Action: ActionDeny},
			{Permission: "bash", Pattern: "*", Action: ActionDeny},
		},
	}
}

// BuildMode creates the default full-access agent.
func BuildMode() AgentMode {
	return AgentMode{
		Name:        "build",
		Description: "Full-access implementation mode",
		MaxTurns:    15,
		Permissions: Ruleset{
			{Permission: "*", Pattern: "*", Action: ActionAllow},
		},
	}
}

// modeRegistry maps mode name strings to their factory functions.
var modeRegistry = map[string]func() AgentMode{
	"plan":    PlanMode,
	"explore": ExploreMode,
	"build":   BuildMode,
}

// LookupMode returns the AgentMode for a given mode name, and a bool indicating
// whether the mode was found.
func LookupMode(name string) (AgentMode, bool) {
	fn, ok := modeRegistry[name]
	if !ok {
		return AgentMode{}, false
	}
	return fn(), true
}
