// Package plan models the implementation plan (specs/<feature>/plan.md):
// layered groups of small, reviewable tasks. Parsing and validation arrive in M1.
package plan

// Plan is a parsed plan.md.
type Plan struct {
	Feature        string
	FunctionalHash string
	TechnicalHash  string
	Groups         []Group
}

// Group is a G# block, implemented and committed as one unit.
type Group struct {
	ID        string // G1, G2, ...
	Title     string
	Layer     string
	DependsOn []string
	Goal      string
	Tasks     []Task
}

// Task is a T#.# block.
type Task struct {
	ID          string // T1.1, ...
	Title       string
	Covers      []string // FR/NFR/AC IDs
	Files       []string
	DoneWhen    string
	Verify      string
	Description string
}

// Covered returns every requirement/criterion ID covered by at least one task.
func (p Plan) Covered() map[string][]string {
	out := map[string][]string{}
	for _, g := range p.Groups {
		for _, t := range g.Tasks {
			for _, id := range t.Covers {
				out[id] = append(out[id], t.ID)
			}
		}
	}
	return out
}
