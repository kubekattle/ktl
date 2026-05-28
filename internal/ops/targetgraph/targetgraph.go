package targetgraph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	APIVersion = "torque.dev/v1alpha1"
	Kind       = "TargetGraph"
)

// TargetGraph is the typed inventory model for Torque ops automation.
type TargetGraph struct {
	APIVersion        string             `json:"apiVersion" yaml:"apiVersion"`
	Kind              string             `json:"kind" yaml:"kind"`
	Metadata          Metadata           `json:"metadata" yaml:"metadata"`
	Targets           []Target           `json:"targets" yaml:"targets"`
	Groups            []Group            `json:"groups,omitempty" yaml:"groups,omitempty"`
	Transports        []Transport        `json:"transports,omitempty" yaml:"transports,omitempty"`
	PrivilegeProfiles []PrivilegeProfile `json:"privilegeProfiles,omitempty" yaml:"privilegeProfiles,omitempty"`
	Variables         []VariableLayer    `json:"variables,omitempty" yaml:"variables,omitempty"`
}

type Metadata struct {
	Name   string            `json:"name" yaml:"name"`
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

type Target struct {
	ID                  string            `json:"id" yaml:"id"`
	Type                string            `json:"type" yaml:"type"`
	TransportRef        string            `json:"transportRef,omitempty" yaml:"transportRef,omitempty"`
	DurableTransportRef string            `json:"durableTransportRef,omitempty" yaml:"durableTransportRef,omitempty"`
	Labels              map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Groups              []string          `json:"groups,omitempty" yaml:"groups,omitempty"`
	Variables           []VariableLayer   `json:"variables,omitempty" yaml:"variables,omitempty"`
	PrivilegeProfile    string            `json:"privilegeProfile,omitempty" yaml:"privilegeProfile,omitempty"`
	Facts               FactPolicy        `json:"facts,omitempty" yaml:"facts,omitempty"`
	LockScope           string            `json:"lockScope,omitempty" yaml:"lockScope,omitempty"`
	AllowedCapabilities []string          `json:"allowedCapabilities,omitempty" yaml:"allowedCapabilities,omitempty"`
}

type Group struct {
	ID        string            `json:"id" yaml:"id"`
	Selector  map[string]string `json:"selector,omitempty" yaml:"selector,omitempty"`
	Targets   []string          `json:"targets,omitempty" yaml:"targets,omitempty"`
	Variables []VariableLayer   `json:"variables,omitempty" yaml:"variables,omitempty"`
}

type Transport struct {
	ID            string         `json:"id" yaml:"id"`
	Kind          string         `json:"kind" yaml:"kind"`
	Host          string         `json:"host,omitempty" yaml:"host,omitempty"`
	User          string         `json:"user,omitempty" yaml:"user,omitempty"`
	KeyRef        string         `json:"keyRef,omitempty" yaml:"keyRef,omitempty"`
	KubeconfigRef string         `json:"kubeconfigRef,omitempty" yaml:"kubeconfigRef,omitempty"`
	DSNRef        string         `json:"dsnRef,omitempty" yaml:"dsnRef,omitempty"`
	URL           string         `json:"url,omitempty" yaml:"url,omitempty"`
	Config        map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

type PrivilegeProfile struct {
	ID       string         `json:"id" yaml:"id"`
	Kind     string         `json:"kind" yaml:"kind"`
	Commands []string       `json:"commands,omitempty" yaml:"commands,omitempty"`
	Config   map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

type VariableLayer struct {
	ID     string         `json:"id" yaml:"id"`
	Values map[string]any `json:"values,omitempty" yaml:"values,omitempty"`
}

type FactPolicy struct {
	TTL       string `json:"ttl,omitempty" yaml:"ttl,omitempty"`
	Discovery string `json:"discovery,omitempty" yaml:"discovery,omitempty"`
}

// Summary is a compact, evidence-friendly view of a loaded TargetGraph.
type Summary struct {
	APIVersion            string         `json:"apiVersion"`
	Kind                  string         `json:"kind"`
	Name                  string         `json:"name"`
	TargetCount           int            `json:"targetCount"`
	GroupCount            int            `json:"groupCount"`
	TransportCount        int            `json:"transportCount"`
	PrivilegeProfileCount int            `json:"privilegeProfileCount"`
	VariableLayerCount    int            `json:"variableLayerCount"`
	TargetTypes           map[string]int `json:"targetTypes"`
	TransportKinds        map[string]int `json:"transportKinds"`
	GroupIDs              []string       `json:"groupIds"`
	TargetIDs             []string       `json:"targetIds"`
	SecretReferenceCount  int            `json:"secretReferenceCount"`
	HostReachabilityRefs  []string       `json:"hostReachabilityRefs"`
}

func LoadFile(path string) (*TargetGraph, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	graph, err := Load(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return graph, nil
}

func Load(r io.Reader) (*TargetGraph, error) {
	decoder := yaml.NewDecoder(r)
	decoder.KnownFields(true)
	var graph TargetGraph
	if err := decoder.Decode(&graph); err != nil {
		return nil, err
	}
	if err := graph.Validate(); err != nil {
		return nil, err
	}
	return &graph, nil
}

func (g TargetGraph) Validate() error {
	var errs []string
	if strings.TrimSpace(g.APIVersion) == "" {
		errs = append(errs, "apiVersion is required")
	} else if g.APIVersion != APIVersion {
		errs = append(errs, fmt.Sprintf("apiVersion must be %q", APIVersion))
	}
	if strings.TrimSpace(g.Kind) == "" {
		errs = append(errs, "kind is required")
	} else if g.Kind != Kind {
		errs = append(errs, fmt.Sprintf("kind must be %q", Kind))
	}
	if strings.TrimSpace(g.Metadata.Name) == "" {
		errs = append(errs, "metadata.name is required")
	}
	if len(g.Targets) == 0 {
		errs = append(errs, "at least one target is required")
	}

	targetIDs := map[string]struct{}{}
	for i, target := range g.Targets {
		path := fmt.Sprintf("targets[%d]", i)
		if strings.TrimSpace(target.ID) == "" {
			errs = append(errs, path+".id is required")
		} else if _, exists := targetIDs[target.ID]; exists {
			errs = append(errs, path+".id duplicates "+target.ID)
		} else {
			targetIDs[target.ID] = struct{}{}
		}
		if strings.TrimSpace(target.Type) == "" {
			errs = append(errs, path+".type is required")
		}
		if target.Facts.TTL != "" {
			if _, err := time.ParseDuration(target.Facts.TTL); err != nil {
				errs = append(errs, fmt.Sprintf("%s.facts.ttl must be a duration: %v", path, err))
			}
		}
		errs = append(errs, validateLabels(path+".labels", target.Labels)...)
		errs = append(errs, validateVariableLayers(path+".variables", target.Variables)...)
	}

	groupIDs := map[string]struct{}{}
	for i, group := range g.Groups {
		path := fmt.Sprintf("groups[%d]", i)
		if strings.TrimSpace(group.ID) == "" {
			errs = append(errs, path+".id is required")
		} else if _, exists := groupIDs[group.ID]; exists {
			errs = append(errs, path+".id duplicates "+group.ID)
		} else {
			groupIDs[group.ID] = struct{}{}
		}
		if len(group.Selector) == 0 && len(group.Targets) == 0 {
			errs = append(errs, path+" must declare selector or targets")
		}
		errs = append(errs, validateLabels(path+".selector", group.Selector)...)
		errs = append(errs, validateVariableLayers(path+".variables", group.Variables)...)
		for _, targetRef := range group.Targets {
			if _, exists := targetIDs[targetRef]; !exists {
				errs = append(errs, fmt.Sprintf("%s.targets references unknown target %q", path, targetRef))
			}
		}
	}

	transportIDs := map[string]struct{}{}
	for i, transport := range g.Transports {
		path := fmt.Sprintf("transports[%d]", i)
		if strings.TrimSpace(transport.ID) == "" {
			errs = append(errs, path+".id is required")
		} else if _, exists := transportIDs[transport.ID]; exists {
			errs = append(errs, path+".id duplicates "+transport.ID)
		} else {
			transportIDs[transport.ID] = struct{}{}
		}
		if strings.TrimSpace(transport.Kind) == "" {
			errs = append(errs, path+".kind is required")
		}
	}

	profileIDs := map[string]struct{}{}
	for i, profile := range g.PrivilegeProfiles {
		path := fmt.Sprintf("privilegeProfiles[%d]", i)
		if strings.TrimSpace(profile.ID) == "" {
			errs = append(errs, path+".id is required")
		} else if _, exists := profileIDs[profile.ID]; exists {
			errs = append(errs, path+".id duplicates "+profile.ID)
		} else {
			profileIDs[profile.ID] = struct{}{}
		}
		if strings.TrimSpace(profile.Kind) == "" {
			errs = append(errs, path+".kind is required")
		}
	}
	errs = append(errs, validateVariableLayers("variables", g.Variables)...)

	for i, target := range g.Targets {
		path := fmt.Sprintf("targets[%d]", i)
		if target.Type == "host" {
			if strings.TrimSpace(target.TransportRef) == "" && strings.TrimSpace(target.DurableTransportRef) == "" {
				errs = append(errs, path+".transportRef or "+path+".durableTransportRef is required for host targets")
			} else {
				if strings.TrimSpace(target.TransportRef) != "" && !isExternalRef(target.TransportRef) {
					if _, exists := transportIDs[target.TransportRef]; !exists {
						errs = append(errs, fmt.Sprintf("%s.transportRef references unknown transport %q", path, target.TransportRef))
					}
				}
				if strings.TrimSpace(target.DurableTransportRef) != "" && !isExternalRef(target.DurableTransportRef) {
					if _, exists := transportIDs[target.DurableTransportRef]; !exists {
						errs = append(errs, fmt.Sprintf("%s.durableTransportRef references unknown transport %q", path, target.DurableTransportRef))
					}
				}
			}
		}
		if target.PrivilegeProfile != "" {
			if _, exists := profileIDs[target.PrivilegeProfile]; !exists {
				errs = append(errs, fmt.Sprintf("%s.privilegeProfile references unknown profile %q", path, target.PrivilegeProfile))
			}
		}
		for _, groupRef := range target.Groups {
			if _, exists := groupIDs[groupRef]; !exists {
				errs = append(errs, fmt.Sprintf("%s.groups references unknown group %q", path, groupRef))
			}
		}
	}

	if len(errs) > 0 {
		return ValidationError{Errors: errs}
	}
	return nil
}

func (g TargetGraph) Summary() Summary {
	s := Summary{
		APIVersion:            g.APIVersion,
		Kind:                  g.Kind,
		Name:                  g.Metadata.Name,
		TargetCount:           len(g.Targets),
		GroupCount:            len(g.Groups),
		TransportCount:        len(g.Transports),
		PrivilegeProfileCount: len(g.PrivilegeProfiles),
		VariableLayerCount:    len(g.Variables),
		TargetTypes:           map[string]int{},
		TransportKinds:        map[string]int{},
	}
	for _, target := range g.Targets {
		s.TargetIDs = append(s.TargetIDs, target.ID)
		s.TargetTypes[target.Type]++
		s.VariableLayerCount += len(target.Variables)
		if target.Type == "host" && target.TransportRef != "" {
			s.HostReachabilityRefs = append(s.HostReachabilityRefs, target.ID)
		}
	}
	for _, group := range g.Groups {
		s.GroupIDs = append(s.GroupIDs, group.ID)
		s.VariableLayerCount += len(group.Variables)
	}
	for _, transport := range g.Transports {
		s.TransportKinds[transport.Kind]++
	}
	sort.Strings(s.TargetIDs)
	sort.Strings(s.GroupIDs)
	sort.Strings(s.HostReachabilityRefs)
	s.SecretReferenceCount = countSecrets(g)
	return s
}

func (s Summary) JSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

type ValidationError struct {
	Errors []string
}

func (e ValidationError) Error() string {
	return "invalid TargetGraph: " + strings.Join(e.Errors, "; ")
}

func validateLabels(path string, labels map[string]string) []string {
	var errs []string
	for key, value := range labels {
		if strings.TrimSpace(key) == "" {
			errs = append(errs, path+" contains an empty key")
		}
		if strings.TrimSpace(value) == "" {
			errs = append(errs, path+"."+key+" has an empty value")
		}
	}
	return errs
}

func validateVariableLayers(path string, layers []VariableLayer) []string {
	var errs []string
	ids := map[string]struct{}{}
	for i, layer := range layers {
		layerPath := fmt.Sprintf("%s[%d]", path, i)
		if strings.TrimSpace(layer.ID) == "" {
			errs = append(errs, layerPath+".id is required")
		} else if _, exists := ids[layer.ID]; exists {
			errs = append(errs, layerPath+".id duplicates "+layer.ID)
		} else {
			ids[layer.ID] = struct{}{}
		}
		if len(layer.Values) == 0 {
			errs = append(errs, layerPath+".values must not be empty")
		}
	}
	return errs
}

func isExternalRef(ref string) bool {
	return strings.Contains(ref, "://")
}

func countSecrets(value any) int {
	switch typed := value.(type) {
	case TargetGraph:
		return countSecrets(typed.Metadata) +
			countSecrets(typed.Targets) +
			countSecrets(typed.Groups) +
			countSecrets(typed.Transports) +
			countSecrets(typed.PrivilegeProfiles) +
			countSecrets(typed.Variables)
	case Metadata:
		return countSecrets(typed.Labels)
	case Target:
		count := countSecrets(typed.TransportRef) + countSecrets(typed.DurableTransportRef) + countSecrets(typed.Labels) + countSecrets(typed.Variables) + countSecrets(typed.Facts)
		count += countSecrets(typed.PrivilegeProfile) + countSecrets(typed.LockScope) + countSecrets(typed.AllowedCapabilities)
		return count
	case Group:
		return countSecrets(typed.Selector) + countSecrets(typed.Targets) + countSecrets(typed.Variables)
	case Transport:
		return countSecrets(typed.Host) + countSecrets(typed.User) + countSecrets(typed.KeyRef) +
			countSecrets(typed.KubeconfigRef) + countSecrets(typed.DSNRef) + countSecrets(typed.URL) + countSecrets(typed.Config)
	case PrivilegeProfile:
		return countSecrets(typed.Commands) + countSecrets(typed.Config)
	case VariableLayer:
		return countSecrets(typed.Values)
	case FactPolicy:
		return countSecrets(typed.TTL) + countSecrets(typed.Discovery)
	case []Target:
		count := 0
		for _, item := range typed {
			count += countSecrets(item)
		}
		return count
	case []Group:
		count := 0
		for _, item := range typed {
			count += countSecrets(item)
		}
		return count
	case []Transport:
		count := 0
		for _, item := range typed {
			count += countSecrets(item)
		}
		return count
	case []PrivilegeProfile:
		count := 0
		for _, item := range typed {
			count += countSecrets(item)
		}
		return count
	case []VariableLayer:
		count := 0
		for _, item := range typed {
			count += countSecrets(item)
		}
		return count
	case []string:
		count := 0
		for _, item := range typed {
			count += countSecrets(item)
		}
		return count
	case map[string]string:
		count := 0
		for key, item := range typed {
			count += countSecrets(key) + countSecrets(item)
		}
		return count
	case map[string]any:
		count := 0
		for key, item := range typed {
			count += countSecrets(key) + countSecrets(item)
		}
		return count
	case string:
		if strings.HasPrefix(typed, "secret://") {
			return 1
		}
	}
	return 0
}
