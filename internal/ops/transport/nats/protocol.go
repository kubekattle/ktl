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
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Operation  string `json:"operation"`
	Target     string `json:"target"`
	Command    string `json:"command,omitempty"`
	SentAt     string `json:"sentAt"`
}

func NewCommandAssignment(operation string, target string, command string, sentAt time.Time) CommandAssignment {
	return CommandAssignment{
		APIVersion: AssignmentAPIVersion,
		Kind:       AssignmentKind,
		Operation:  strings.TrimSpace(operation),
		Target:     NormalizeTarget(target),
		Command:    command,
		SentAt:     sentAt.UTC().Format(time.RFC3339Nano),
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
