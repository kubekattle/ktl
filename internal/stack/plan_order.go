package stack

import "fmt"

// ComputeExecutionOrder returns a deterministic topological order for the plan.
// It uses the same ready-set priority as the runtime scheduler.
func ComputeExecutionOrder(p *Plan, command string) ([]string, error) {
	if p == nil {
		return nil, fmt.Errorf("plan is nil")
	}
	run := wrapRunNodes(p.Nodes)
	s := newScheduler(run, command)
	var out []string
	for {
		n := s.NextReady()
		if n == nil {
			break
		}
		out = append(out, n.ID)
		s.MarkSucceeded(n.ID)
	}
	if len(out) != len(p.Nodes) {
		return nil, fmt.Errorf("unable to compute order: graph is not fully schedulable (cycle or cross-cluster name collision)")
	}
	s.FinalizeBlocked()
	snap := s.Snapshot()
	for id, st := range snap.Status {
		if st == "blocked" {
			return nil, fmt.Errorf("unable to compute order: %s is blocked", id)
		}
	}
	return out, nil
}
