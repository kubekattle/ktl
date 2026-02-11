package compose

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadComposeProjectFromTestdata(t *testing.T) {
	composePath := filepath.Join("..", "..", "testdata", "build", "compose", "docker-compose.yml")
	project, err := LoadComposeProject([]string{composePath}, "testproj", nil)
	if err != nil {
		t.Fatalf("LoadComposeProject returned error: %v", err)
	}
	if project.Name != "testproj" {
		t.Fatalf("expected project name testproj, got %s", project.Name)
	}
	if len(project.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(project.Services))
	}
}

func TestLoadComposeProjectDefaultsProjectName(t *testing.T) {
	composePath := filepath.Join("..", "..", "testdata", "build", "compose", "docker-compose.yml")
	project, err := LoadComposeProject([]string{composePath}, "", nil)
	if err != nil {
		t.Fatalf("LoadComposeProject returned error: %v", err)
	}
	if project == nil || project.Name == "" {
		t.Fatalf("expected project name to be set")
	}
}

func TestCollectBuildableServices(t *testing.T) {
	composePath := filepath.Join("..", "..", "testdata", "build", "compose", "docker-compose.yml")
	project, err := LoadComposeProject([]string{composePath}, "compose-tests", nil)
	if err != nil {
		t.Fatalf("LoadComposeProject: %v", err)
	}
	services, skipped, err := CollectBuildableServices(project, nil)
	if err != nil {
		t.Fatalf("CollectBuildableServices: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("expected no skipped services, got %v", skipped)
	}
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}
	if _, ok := services["api"]; !ok {
		t.Fatalf("api service missing from result")
	}
}

func TestComposeDependencyGraph(t *testing.T) {
	composePath := filepath.Join("..", "..", "testdata", "build", "compose", "docker-compose.yml")
	project, err := LoadComposeProject([]string{composePath}, "compose-tests", nil)
	if err != nil {
		t.Fatalf("LoadComposeProject: %v", err)
	}
	services, _, err := CollectBuildableServices(project, nil)
	if err != nil {
		t.Fatalf("CollectBuildableServices: %v", err)
	}
	dependents, pending := ComposeDependencyGraph(services)
	if len(pending) != 2 {
		t.Fatalf("expected pending entries for 2 services, got %d", len(pending))
	}
	for svc, count := range pending {
		if count != 0 {
			t.Fatalf("expected no dependencies for %s, got %d", svc, count)
		}
	}
	if len(dependents) != 0 {
		t.Fatalf("expected no dependents, got %v", dependents)
	}
}

func TestServiceTags(t *testing.T) {
	composePath := filepath.Join("..", "..", "testdata", "build", "compose", "docker-compose.yml")
	project, err := LoadComposeProject([]string{composePath}, "compose-tests", nil)
	if err != nil {
		t.Fatalf("LoadComposeProject: %v", err)
	}
	services, _, err := CollectBuildableServices(project, nil)
	if err != nil {
		t.Fatalf("CollectBuildableServices: %v", err)
	}
	tags := ServiceTags(project, "api", services["api"])
	expected := []string{"ktl-test/api:dev"}
	if !reflect.DeepEqual(tags, expected) {
		t.Fatalf("expected %v, got %v", expected, tags)
	}
}

func TestCollectBuildableServices_UnknownServiceIncludesAvailable(t *testing.T) {
	composePath := filepath.Join("..", "..", "testdata", "build", "compose", "docker-compose.yml")
	project, err := LoadComposeProject([]string{composePath}, "compose-tests", nil)
	if err != nil {
		t.Fatalf("LoadComposeProject: %v", err)
	}

	_, _, err = CollectBuildableServices(project, []string{"does-not-exist"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "unknown compose service") || !strings.Contains(err.Error(), "available: api, worker") {
		t.Fatalf("unexpected error: %v", err)
	}
}
