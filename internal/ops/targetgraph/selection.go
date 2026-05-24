package targetgraph

import (
	"fmt"
	"sort"
)

type SelectionRequest struct {
	Selector map[string]string `json:"selector,omitempty"`
	Groups   []string          `json:"groups,omitempty"`
	Limit    int               `json:"limit,omitempty"`
}

type SelectionResult struct {
	Selector           map[string]string   `json:"selector,omitempty"`
	Groups             []string            `json:"groups,omitempty"`
	MatchedTargetIDs   []string            `json:"matchedTargetIds"`
	OmittedTargetIDs   []string            `json:"omittedTargetIds,omitempty"`
	GroupExpanded      map[string][]string `json:"groupExpanded,omitempty"`
	SelectorMatchedIDs []string            `json:"selectorMatchedIds,omitempty"`
	Conflicts          []SelectionConflict `json:"conflicts,omitempty"`
	Limit              int                 `json:"limit,omitempty"`
	Limited            bool                `json:"limited"`
	BeforeLimitCount   int                 `json:"beforeLimitCount"`
	AfterLimitCount    int                 `json:"afterLimitCount"`
	ConflictCount      int                 `json:"conflictCount"`
}

type SelectionConflict struct {
	Kind     string `json:"kind"`
	GroupID  string `json:"groupId,omitempty"`
	TargetID string `json:"targetId,omitempty"`
	Reason   string `json:"reason"`
}

func (g TargetGraph) ResolveSelection(req SelectionRequest) (SelectionResult, error) {
	result := SelectionResult{
		Selector:      copyStringMap(req.Selector),
		Groups:        sortedStrings(req.Groups),
		GroupExpanded: map[string][]string{},
		Limit:         req.Limit,
	}
	if req.Limit < 0 {
		return result, fmt.Errorf("selection limit must be >= 0")
	}

	targetsByID := g.targetsByID()
	candidates := map[string]Target{}
	explicitGroupTarget := map[string]string{}
	if len(req.Groups) > 0 {
		for _, groupID := range req.Groups {
			group, ok := g.groupByID(groupID)
			if !ok {
				return result, fmt.Errorf("unknown group %q", groupID)
			}
			for _, targetID := range group.Targets {
				if _, exists := explicitGroupTarget[targetID]; !exists {
					explicitGroupTarget[targetID] = groupID
				}
			}
			expanded, err := g.ExpandGroup(groupID)
			if err != nil {
				return result, err
			}
			result.GroupExpanded[groupID] = expanded
			for _, targetID := range expanded {
				candidates[targetID] = targetsByID[targetID]
			}
		}
	} else {
		for _, target := range g.Targets {
			candidates[target.ID] = target
		}
	}

	if len(req.Selector) > 0 {
		for _, target := range g.Targets {
			if LabelsMatch(target.Labels, req.Selector) {
				result.SelectorMatchedIDs = append(result.SelectorMatchedIDs, target.ID)
			}
		}
		sort.Strings(result.SelectorMatchedIDs)
	}

	matched := make([]string, 0, len(candidates))
	for targetID, target := range candidates {
		if LabelsMatch(target.Labels, req.Selector) {
			matched = append(matched, targetID)
			continue
		}
		if groupID, ok := explicitGroupTarget[targetID]; ok && len(req.Selector) > 0 {
			result.Conflicts = append(result.Conflicts, SelectionConflict{
				Kind:     "group-selector",
				GroupID:  groupID,
				TargetID: targetID,
				Reason:   "group member does not satisfy selector",
			})
		}
	}
	sort.Strings(matched)
	sort.Slice(result.Conflicts, func(i, j int) bool {
		if result.Conflicts[i].TargetID == result.Conflicts[j].TargetID {
			return result.Conflicts[i].Kind < result.Conflicts[j].Kind
		}
		return result.Conflicts[i].TargetID < result.Conflicts[j].TargetID
	})

	result.BeforeLimitCount = len(matched)
	if req.Limit > 0 && len(matched) > req.Limit {
		result.MatchedTargetIDs = append([]string(nil), matched[:req.Limit]...)
		result.OmittedTargetIDs = append([]string(nil), matched[req.Limit:]...)
		result.Limited = true
	} else {
		result.MatchedTargetIDs = matched
	}
	result.AfterLimitCount = len(result.MatchedTargetIDs)
	result.ConflictCount = len(result.Conflicts)
	if len(result.GroupExpanded) == 0 {
		result.GroupExpanded = nil
	}
	return result, nil
}

func (g TargetGraph) ExpandGroup(groupID string) ([]string, error) {
	group, ok := g.groupByID(groupID)
	if !ok {
		return nil, fmt.Errorf("unknown group %q", groupID)
	}
	seen := map[string]struct{}{}
	for _, targetID := range group.Targets {
		seen[targetID] = struct{}{}
	}
	if len(group.Selector) > 0 {
		for _, target := range g.Targets {
			if LabelsMatch(target.Labels, group.Selector) {
				seen[target.ID] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for targetID := range seen {
		out = append(out, targetID)
	}
	sort.Strings(out)
	return out, nil
}

func (g TargetGraph) SelectTargets(selector map[string]string) []string {
	var out []string
	for _, target := range g.Targets {
		if LabelsMatch(target.Labels, selector) {
			out = append(out, target.ID)
		}
	}
	sort.Strings(out)
	return out
}

func LabelsMatch(labels map[string]string, selector map[string]string) bool {
	for key, want := range selector {
		if labels[key] != want {
			return false
		}
	}
	return true
}

func (g TargetGraph) targetsByID() map[string]Target {
	out := make(map[string]Target, len(g.Targets))
	for _, target := range g.Targets {
		out[target.ID] = target
	}
	return out
}

func (g TargetGraph) groupByID(groupID string) (Group, bool) {
	for _, group := range g.Groups {
		if group.ID == groupID {
			return group, true
		}
	}
	return Group{}, false
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
