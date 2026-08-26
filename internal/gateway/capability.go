package gateway

import (
	"fmt"
	"sort"

	"github.com/yosephbernandus/baton/internal/role"
	"github.com/yosephbernandus/baton/internal/transport"
)

// CheckRoleCapabilities reports roles whose tool boundary the runtime they are
// assigned to cannot enforce.
//
// Baton's roles are only as real as the transport under them. A reviewer that
// declares read-only tools but runs on a transport with no way to withhold
// editing is a reviewer in prompt text alone — the run still looks successful,
// and the boundary quietly did nothing. Reporting the gap before the run is the
// difference between an enforced constraint and a documented intention.
//
// caps looks up what a runtime can do. Runtimes it has no answer for are
// skipped rather than assumed to be incapable.
func CheckRoleCapabilities(
	roleRuntimes map[string]string,
	caps func(runtimeName string) (transport.Caps, bool),
) []Finding {
	if caps == nil || len(roleRuntimes) == 0 {
		return nil
	}

	// Iterate in a stable order so the report reads the same across runs.
	roles := make([]string, 0, len(roleRuntimes))
	for r := range roleRuntimes {
		roles = append(roles, r)
	}
	sort.Strings(roles)

	var findings []Finding
	for _, roleName := range roles {
		runtimeName := roleRuntimes[roleName]
		if runtimeName == "" {
			continue
		}

		tools := role.AllowedTools(roleName)
		if len(tools) == 0 {
			// The role asks for no boundary, so there is nothing to enforce.
			continue
		}

		c, ok := caps(runtimeName)
		if !ok {
			continue
		}

		if !c.Probed {
			findings = append(findings, Finding{
				Check:    "role_capability",
				Severity: SeverityWarn,
				Message: fmt.Sprintf(
					"role %q on runtime %q: capabilities not established, so its tool boundary is unverified until the run connects",
					roleName, runtimeName),
			})
			continue
		}

		switch c.ToolRestriction {
		case transport.RestrictPerTool:
			// The boundary can be enforced exactly.

		case transport.RestrictCoarse:
			// A coarse mechanism can withhold editing but cannot name tools. It
			// covers a read-only role and does nothing for one that permits
			// some writes but not others.
			if allowsEditing(tools) {
				findings = append(findings, Finding{
					Check:    "role_capability",
					Severity: SeverityWarn,
					Message: fmt.Sprintf(
						"role %q on runtime %q: only coarse tool restriction is available, so the boundary (%v) is enforced only where the agent asks permission",
						roleName, runtimeName, tools),
				})
			}

		default:
			// Deliberately a warning, not an error, even though this is the
			// case that loses the most. Gateway errors block a run, and an
			// unenforced boundary does not stop the work — it weakens a
			// guarantee the config implied. Blocking here would also fail every
			// existing strict-mode pipeline on upgrade, since no runtime in the
			// shipped example config declares a restriction flag.
			findings = append(findings, Finding{
				Check:    "role_capability",
				Severity: SeverityWarn,
				Message: fmt.Sprintf(
					"role %q on runtime %q: runtime cannot restrict tools, so the boundary (%v) is prompt guidance only",
					roleName, runtimeName, tools),
			})
		}
	}
	return findings
}

// allowsEditing reports whether a tool list includes anything that can change
// files.
func allowsEditing(tools []string) bool {
	for _, t := range tools {
		switch t {
		case "Edit", "Write", "MultiEdit", "NotebookEdit":
			return true
		}
	}
	return false
}
