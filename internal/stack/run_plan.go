package stack

import "fmt"

func PlanFromRunPlan(rp *RunPlan) (*Plan, error) {
	if rp == nil {
		return nil, fmt.Errorf("nil run plan")
	}
	p := &Plan{
		StackRoot: rp.StackRoot,
		StackName: rp.StackName,
		Profile:   rp.Profile,
		Nodes:     rp.Nodes,
		ByID:      map[string]*ResolvedRelease{},
		ByCluster: map[string][]*ResolvedRelease{},
	}
	for _, n := range p.Nodes {
		p.ByID[n.ID] = n
		p.ByCluster[n.Cluster.Name] = append(p.ByCluster[n.Cluster.Name], n)
	}
	if err := assignExecutionGroups(p); err != nil {
		return nil, err
	}
	return p, nil
}
