package natstransport

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	AssignmentAPIVersion = "torque.dev/nats-assignment/v1"
	AssignmentKind       = "CommandAssignment"

	AssignmentEnvelopeAPIVersion = "torque.dev/nats-assignment-envelope/v1"
	AssignmentEnvelopeKind       = "SignedCommandAssignment"
	AssignmentSignatureAlgorithm = "ed25519"
)

// CommandAssignment is the NATS request payload shared by the stack transport
// client and the agent worker.
type CommandAssignment struct {
	APIVersion         string `json:"apiVersion"`
	Kind               string `json:"kind"`
	AssignmentID       string `json:"assignmentId,omitempty"`
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
	AssignmentID       string
	TargetID           string
	ExpectedAgentID    string
	RequiredCapability string
	NodeKind           string
	RunID              string
	NodeID             string
	PlanDigest         string
}

type CommandAssignmentEnvelope struct {
	APIVersion   string                     `json:"apiVersion"`
	Kind         string                     `json:"kind"`
	Assignment   CommandAssignment          `json:"assignment"`
	Issuer       string                     `json:"issuer,omitempty"`
	Tenant       string                     `json:"tenant,omitempty"`
	IssuedAt     string                     `json:"issuedAt"`
	ExpiresAt    string                     `json:"expiresAt"`
	PolicyDigest string                     `json:"policyDigest,omitempty"`
	Signature    CommandAssignmentSignature `json:"signature"`
}

type CommandAssignmentSignature struct {
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"publicKey,omitempty"`
	Signature string `json:"signature"`
}

type CommandAssignmentEnvelopeOptions struct {
	Issuer       string
	Tenant       string
	PolicyDigest string
	IssuedAt     time.Time
	ExpiresAt    time.Time
	TTL          time.Duration
	PrivateKey   ed25519.PrivateKey
}

type CommandAssignmentVerifyOptions struct {
	RequireSignature     bool
	TrustedPublicKey     ed25519.PublicKey
	ExpectedTenant       string
	ExpectedPolicyDigest string
	ExpectedTarget       string
	ExpectedTargetID     string
	Now                  time.Time
}

type CommandAssignmentVerification struct {
	EnvelopePresent bool
	Verified        bool
	Issuer          string
	Tenant          string
	PolicyDigest    string
	IssuedAt        string
	ExpiresAt       string
	Algorithm       string
	PublicKeyDigest string
}

func NewCommandAssignment(operation string, target string, command string, sentAt time.Time) CommandAssignment {
	return NewCommandAssignmentWithMetadata(operation, target, command, sentAt, CommandAssignmentMetadata{})
}

func NewCommandAssignmentWithMetadata(operation string, target string, command string, sentAt time.Time, metadata CommandAssignmentMetadata) CommandAssignment {
	assignment := CommandAssignment{
		APIVersion:         AssignmentAPIVersion,
		Kind:               AssignmentKind,
		AssignmentID:       strings.TrimSpace(metadata.AssignmentID),
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
	if assignment.AssignmentID == "" {
		assignment.AssignmentID = DeriveAssignmentID(assignment)
	}
	return assignment
}

func ParseCommandAssignment(raw []byte) (CommandAssignment, error) {
	assignment, _, err := ParseCommandAssignmentMessage(raw)
	return assignment, err
}

func ParseCommandAssignmentMessage(raw []byte) (CommandAssignment, *CommandAssignmentEnvelope, error) {
	var probe struct {
		APIVersion string           `json:"apiVersion"`
		Kind       string           `json:"kind"`
		Assignment *json.RawMessage `json:"assignment,omitempty"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return CommandAssignment{}, nil, fmt.Errorf("parse command assignment: %w", err)
	}
	if strings.TrimSpace(probe.Kind) == AssignmentEnvelopeKind || probe.Assignment != nil {
		envelope, err := ParseCommandAssignmentEnvelope(raw)
		if err != nil {
			return CommandAssignment{}, nil, err
		}
		return envelope.Assignment, &envelope, nil
	}
	assignment, err := parseCommandAssignmentRaw(raw)
	return assignment, nil, err
}

func parseCommandAssignmentRaw(raw []byte) (CommandAssignment, error) {
	var assignment CommandAssignment
	if err := json.Unmarshal(raw, &assignment); err != nil {
		return CommandAssignment{}, fmt.Errorf("parse command assignment: %w", err)
	}
	return normalizeCommandAssignment(assignment)
}

func normalizeCommandAssignment(assignment CommandAssignment) (CommandAssignment, error) {
	assignment.APIVersion = strings.TrimSpace(assignment.APIVersion)
	assignment.Kind = strings.TrimSpace(assignment.Kind)
	assignment.AssignmentID = strings.TrimSpace(assignment.AssignmentID)
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
	if assignment.AssignmentID == "" {
		assignment.AssignmentID = DeriveAssignmentID(assignment)
	}
	return assignment, nil
}

func ParseCommandAssignmentEnvelope(raw []byte) (CommandAssignmentEnvelope, error) {
	var envelope CommandAssignmentEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return CommandAssignmentEnvelope{}, fmt.Errorf("parse command assignment envelope: %w", err)
	}
	envelope.APIVersion = strings.TrimSpace(envelope.APIVersion)
	envelope.Kind = strings.TrimSpace(envelope.Kind)
	envelope.Issuer = strings.TrimSpace(envelope.Issuer)
	envelope.Tenant = strings.TrimSpace(envelope.Tenant)
	envelope.IssuedAt = strings.TrimSpace(envelope.IssuedAt)
	envelope.ExpiresAt = strings.TrimSpace(envelope.ExpiresAt)
	envelope.PolicyDigest = strings.TrimSpace(envelope.PolicyDigest)
	envelope.Signature.Algorithm = strings.TrimSpace(envelope.Signature.Algorithm)
	envelope.Signature.PublicKey = strings.TrimSpace(envelope.Signature.PublicKey)
	envelope.Signature.Signature = strings.TrimSpace(envelope.Signature.Signature)
	if envelope.APIVersion != "" && envelope.APIVersion != AssignmentEnvelopeAPIVersion {
		return CommandAssignmentEnvelope{}, fmt.Errorf("unsupported assignment envelope apiVersion %q", envelope.APIVersion)
	}
	if envelope.Kind != AssignmentEnvelopeKind {
		return CommandAssignmentEnvelope{}, fmt.Errorf("unsupported assignment envelope kind %q", envelope.Kind)
	}
	assignment, err := normalizeCommandAssignment(envelope.Assignment)
	if err != nil {
		return CommandAssignmentEnvelope{}, err
	}
	envelope.Assignment = assignment
	return envelope, nil
}

func SignCommandAssignmentEnvelope(assignment CommandAssignment, opts CommandAssignmentEnvelopeOptions) (CommandAssignmentEnvelope, error) {
	if len(opts.PrivateKey) != ed25519.PrivateKeySize {
		return CommandAssignmentEnvelope{}, fmt.Errorf("ed25519 private key is required")
	}
	assignment, err := normalizeCommandAssignment(assignment)
	if err != nil {
		return CommandAssignmentEnvelope{}, err
	}
	issuedAt := opts.IssuedAt
	if issuedAt.IsZero() {
		issuedAt = time.Now().UTC()
	}
	expiresAt := opts.ExpiresAt
	if expiresAt.IsZero() {
		ttl := opts.TTL
		if ttl <= 0 {
			ttl = 5 * time.Minute
		}
		expiresAt = issuedAt.Add(ttl)
	}
	envelope := CommandAssignmentEnvelope{
		APIVersion:   AssignmentEnvelopeAPIVersion,
		Kind:         AssignmentEnvelopeKind,
		Assignment:   assignment,
		Issuer:       strings.TrimSpace(opts.Issuer),
		Tenant:       NormalizeTenant(opts.Tenant),
		IssuedAt:     issuedAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt:    expiresAt.UTC().Format(time.RFC3339Nano),
		PolicyDigest: strings.TrimSpace(opts.PolicyDigest),
		Signature: CommandAssignmentSignature{
			Algorithm: AssignmentSignatureAlgorithm,
			PublicKey: base64.StdEncoding.EncodeToString(opts.PrivateKey.Public().(ed25519.PublicKey)),
		},
	}
	_, sum, err := envelopeSigningBytes(envelope)
	if err != nil {
		return CommandAssignmentEnvelope{}, err
	}
	envelope.Signature.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(opts.PrivateKey, sum[:]))
	return envelope, nil
}

func VerifyCommandAssignmentMessage(raw []byte, opts CommandAssignmentVerifyOptions) (CommandAssignment, CommandAssignmentVerification, error) {
	assignment, envelope, err := ParseCommandAssignmentMessage(raw)
	if err != nil {
		return CommandAssignment{}, CommandAssignmentVerification{}, err
	}
	if envelope == nil {
		verification := CommandAssignmentVerification{}
		if opts.RequireSignature {
			return assignment, verification, fmt.Errorf("signed assignment envelope is required")
		}
		return assignment, verification, nil
	}
	verification := CommandAssignmentVerification{
		EnvelopePresent: true,
		Issuer:          envelope.Issuer,
		Tenant:          envelope.Tenant,
		PolicyDigest:    envelope.PolicyDigest,
		IssuedAt:        envelope.IssuedAt,
		ExpiresAt:       envelope.ExpiresAt,
		Algorithm:       envelope.Signature.Algorithm,
	}
	if strings.TrimSpace(envelope.Signature.Algorithm) != AssignmentSignatureAlgorithm {
		return assignment, verification, fmt.Errorf("unsupported assignment signature algorithm %q", envelope.Signature.Algorithm)
	}
	if strings.TrimSpace(envelope.Issuer) == "" {
		return assignment, verification, fmt.Errorf("assignment envelope issuer is required")
	}
	if strings.TrimSpace(envelope.Signature.Signature) == "" {
		return assignment, verification, fmt.Errorf("assignment envelope signature is required")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, envelope.ExpiresAt)
	if err != nil {
		return assignment, verification, fmt.Errorf("parse assignment expiresAt: %w", err)
	}
	if strings.TrimSpace(envelope.IssuedAt) != "" {
		if _, err := time.Parse(time.RFC3339Nano, envelope.IssuedAt); err != nil {
			return assignment, verification, fmt.Errorf("parse assignment issuedAt: %w", err)
		}
	}
	pub := append(ed25519.PublicKey(nil), opts.TrustedPublicKey...)
	if len(pub) == 0 {
		if !opts.RequireSignature {
			return assignment, verification, nil
		}
		return assignment, verification, fmt.Errorf("trusted assignment issuer key is required")
	}
	if len(pub) != ed25519.PublicKeySize {
		return assignment, verification, fmt.Errorf("trusted assignment issuer key has length %d (want %d)", len(pub), ed25519.PublicKeySize)
	}
	verification.PublicKeyDigest = "sha256:" + sha256Hex(pub)
	if embedded := strings.TrimSpace(envelope.Signature.PublicKey); embedded != "" {
		embeddedPub, err := base64.StdEncoding.DecodeString(embedded)
		if err != nil {
			return assignment, verification, fmt.Errorf("decode assignment publicKey: %w", err)
		}
		if !strings.EqualFold(sha256Hex(embeddedPub), sha256Hex(pub)) {
			return assignment, verification, fmt.Errorf("assignment publicKey does not match trusted issuer key")
		}
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envelope.Signature.Signature))
	if err != nil {
		return assignment, verification, fmt.Errorf("decode assignment signature: %w", err)
	}
	_, sum, err := envelopeSigningBytes(*envelope)
	if err != nil {
		return assignment, verification, err
	}
	if !ed25519.Verify(pub, sum[:], sig) {
		return assignment, verification, fmt.Errorf("invalid assignment signature")
	}
	verification.Verified = true
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !now.Before(expiresAt) {
		return assignment, verification, fmt.Errorf("assignment envelope expired at %s", envelope.ExpiresAt)
	}
	if expected := NormalizeTenant(opts.ExpectedTenant); strings.TrimSpace(opts.ExpectedTenant) != "" && envelope.Tenant != expected {
		return assignment, verification, fmt.Errorf("assignment envelope tenant %q does not match expected tenant %q", envelope.Tenant, expected)
	}
	if expected := strings.TrimSpace(opts.ExpectedPolicyDigest); expected != "" && strings.TrimSpace(envelope.PolicyDigest) != expected {
		return assignment, verification, fmt.Errorf("assignment envelope policyDigest %q does not match expected policyDigest %q", envelope.PolicyDigest, expected)
	}
	if expected := NormalizeTarget(opts.ExpectedTarget); strings.TrimSpace(opts.ExpectedTarget) != "" && assignment.Target != expected {
		return assignment, verification, fmt.Errorf("assignment target %q does not match expected target %q", assignment.Target, expected)
	}
	if expected := strings.TrimSpace(opts.ExpectedTargetID); expected != "" && strings.TrimSpace(assignment.TargetID) != expected {
		return assignment, verification, fmt.Errorf("assignment targetId %q does not match expected targetId %q", assignment.TargetID, expected)
	}
	return assignment, verification, nil
}

func CommandAssignmentVerificationMetadata(verification CommandAssignmentVerification) map[string]string {
	metadata := map[string]string{}
	if verification.EnvelopePresent {
		metadata["assignmentEnvelope"] = "true"
	}
	if verification.Verified {
		metadata["signatureVerified"] = "true"
	} else if verification.EnvelopePresent {
		metadata["signatureVerified"] = "false"
	}
	add := func(key, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			metadata[key] = value
		}
	}
	add("assignmentIssuer", verification.Issuer)
	add("assignmentEnvelopeTenant", verification.Tenant)
	add("policyDigest", verification.PolicyDigest)
	add("assignmentIssuedAt", verification.IssuedAt)
	add("assignmentExpiresAt", verification.ExpiresAt)
	add("signatureAlgorithm", verification.Algorithm)
	add("trustedIssuerKeyDigest", verification.PublicKeyDigest)
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func LoadEd25519PublicKeyFile(path string) (ed25519.PublicKey, error) {
	raw, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, err
	}
	var key struct {
		Type      string `json:"type"`
		PublicKey string `json:"publicKey"`
	}
	if err := json.Unmarshal(raw, &key); err == nil && strings.TrimSpace(key.PublicKey) != "" {
		if strings.TrimSpace(key.Type) != "" && strings.TrimSpace(key.Type) != "ed25519" {
			return nil, fmt.Errorf("unsupported key type %q", key.Type)
		}
		return decodeEd25519PublicKey(strings.TrimSpace(key.PublicKey))
	}
	return decodeEd25519PublicKey(strings.TrimSpace(string(raw)))
}

func envelopeSigningBytes(envelope CommandAssignmentEnvelope) ([]byte, [32]byte, error) {
	envelope.Signature.Signature = ""
	raw, err := json.Marshal(envelope)
	if err != nil {
		return nil, [32]byte{}, err
	}
	sum := sha256.Sum256(raw)
	return raw, sum, nil
}

func decodeEd25519PublicKey(value string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key has length %d (want %d)", len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func DeriveAssignmentID(assignment CommandAssignment) string {
	commandSum := sha256.Sum256([]byte(assignment.Command))
	key := struct {
		Operation          string `json:"operation"`
		Target             string `json:"target"`
		TargetID           string `json:"targetId,omitempty"`
		ExpectedAgentID    string `json:"expectedAgentId,omitempty"`
		RequiredCapability string `json:"requiredCapability,omitempty"`
		NodeKind           string `json:"nodeKind,omitempty"`
		RunID              string `json:"runId,omitempty"`
		NodeID             string `json:"nodeId,omitempty"`
		PlanDigest         string `json:"planDigest,omitempty"`
		CommandDigest      string `json:"commandDigest"`
	}{
		Operation:          strings.TrimSpace(assignment.Operation),
		Target:             NormalizeTarget(assignment.Target),
		TargetID:           strings.TrimSpace(assignment.TargetID),
		ExpectedAgentID:    strings.TrimSpace(assignment.ExpectedAgentID),
		RequiredCapability: strings.TrimSpace(assignment.RequiredCapability),
		NodeKind:           strings.TrimSpace(assignment.NodeKind),
		RunID:              strings.TrimSpace(assignment.RunID),
		NodeID:             strings.TrimSpace(assignment.NodeID),
		PlanDigest:         strings.TrimSpace(assignment.PlanDigest),
		CommandDigest:      "sha256:" + hex.EncodeToString(commandSum[:]),
	}
	raw, _ := json.Marshal(key)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
