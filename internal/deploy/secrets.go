package deploy

import "github.com/kubekattle/ktl/internal/secretstore"

// SecretOptions configures deploy-time secret resolution.
type SecretOptions struct {
	Resolver  *secretstore.Resolver
	AuditSink func(secretstore.AuditReport)
	Validate  bool
}

// SecretRef represents a resolved secret reference for reporting/UI purposes.
type SecretRef struct {
	Provider  string `json:"provider"`
	Path      string `json:"path,omitempty"`
	Reference string `json:"reference,omitempty"`
	Masked    bool   `json:"masked,omitempty"`
}
