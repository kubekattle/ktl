package natstransport

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	AssignmentAPIVersion = "torque.dev/nats-assignment/v1"
	AssignmentKind       = "CommandAssignment"
)

// CommandAssignment is the NATS request payload shared by the stack transport
// client and the agent worker.
type CommandAssignment struct {
	APIVersion         string `json:"apiVersion"`
	Kind               string `json:"kind"`
	Operation          string `json:"operation"`
	Target             string `json:"target"`
	Command            string `json:"command,omitempty"`
	TargetID           string `json:"targetId,omitempty"`
	ExpectedAgentID    string `json:"expectedAgentId,omitempty"`
	RequiredCapability string `json:"requiredCapability,omitempty"`
	NodeKind           string `json:"nodeKind,omitempty"`
	RunID              string `json:"runId,omitempty"`
	NodeID             string `json:"nodeId,omitempty"`
	PlanDigest         string `json:"planDigest,omitempty"`
	SentAt             string `json:"sentAt"`
}

type CommandAssignmentMetadata struct {
	TargetID           string
	ExpectedAgentID    string
	RequiredCapability string
	NodeKind           string
	RunID              string
	NodeID             string
	PlanDigest         string
}

func NewCommandAssignment(operation string, target string, command string, sentAt time.Time) CommandAssignment {
	return NewCommandAssignmentWithMetadata(operation, target, command, sentAt, CommandAssignmentMetadata{})
}

func NewCommandAssignmentWithMetadata(operation string, target string, command string, sentAt time.Time, metadata CommandAssignmentMetadata) CommandAssignment {
	return CommandAssignment{
		APIVersion:         AssignmentAPIVersion,
		Kind:               AssignmentKind,
		Operation:          strings.TrimSpace(operation),
		Target:             NormalizeTarget(target),
		Command:            command,
		TargetID:           strings.TrimSpace(metadata.TargetID),
		ExpectedAgentID:    strings.TrimSpace(metadata.ExpectedAgentID),
		RequiredCapability: strings.TrimSpace(metadata.RequiredCapability),
		NodeKind:           strings.TrimSpace(metadata.NodeKind),
		RunID:              strings.TrimSpace(metadata.RunID),
		NodeID:             strings.TrimSpace(metadata.NodeID),
		PlanDigest:         strings.TrimSpace(metadata.PlanDigest),
		SentAt:             sentAt.UTC().Format(time.RFC3339Nano),
	}
}

func ParseCommandAssignment(raw []byte) (CommandAssignment, error) {
	var assignment CommandAssignment
	if err := json.Unmarshal(raw, &assignment); err != nil {
		return CommandAssignment{}, fmt.Errorf("parse command assignment: %w", err)
	}
	assignment.APIVersion = strings.TrimSpace(assignment.APIVersion)
	assignment.Kind = strings.TrimSpace(assignment.Kind)
	assignment.Operation = strings.TrimSpace(assignment.Operation)
	assignment.Target = NormalizeTarget(assignment.Target)
	assignment.TargetID = strings.TrimSpace(assignment.TargetID)
	assignment.ExpectedAgentID = strings.TrimSpace(assignment.ExpectedAgentID)
	assignment.RequiredCapability = strings.TrimSpace(assignment.RequiredCapability)
	assignment.NodeKind = strings.TrimSpace(assignment.NodeKind)
	assignment.RunID = strings.TrimSpace(assignment.RunID)
	assignment.NodeID = strings.TrimSpace(assignment.NodeID)
	assignment.PlanDigest = strings.TrimSpace(assignment.PlanDigest)
	if assignment.APIVersion != "" && assignment.APIVersion != AssignmentAPIVersion {
		return CommandAssignment{}, fmt.Errorf("unsupported assignment apiVersion %q", assignment.APIVersion)
	}
	if assignment.Kind != AssignmentKind {
		return CommandAssignment{}, fmt.Errorf("unsupported assignment kind %q", assignment.Kind)
	}
	if assignment.Operation == "" {
		return CommandAssignment{}, fmt.Errorf("assignment operation is required")
	}
	if assignment.Target == "" {
		return CommandAssignment{}, fmt.Errorf("assignment target is required")
	}
	if strings.ContainsAny(assignment.Target, " \t\r\n") {
		return CommandAssignment{}, fmt.Errorf("assignment target must not contain whitespace")
	}
	return assignment, nil
}
