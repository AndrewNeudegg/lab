package assistant

import (
	"testing"
	"time"
)

func TestDefaultCatalogueDistillsAssistantCapabilities(t *testing.T) {
	catalogue := DefaultCatalogue(time.Date(2026, 4, 30, 21, 0, 0, 0, time.UTC))

	if catalogue.Name != "Assistant" {
		t.Fatalf("name = %q, want Assistant", catalogue.Name)
	}
	if len(catalogue.Activities) < 6 {
		t.Fatalf("activities = %d, want life-improving activity set", len(catalogue.Activities))
	}
	if len(catalogue.Capabilities) < 8 {
		t.Fatalf("capabilities = %d, want broad assistant catalogue", len(catalogue.Capabilities))
	}
	if len(catalogue.UXPatterns) < 8 {
		t.Fatalf("ux patterns = %d, want agentic UI guidance", len(catalogue.UXPatterns))
	}

	var foundMemory, foundApproval, foundResearch bool
	for _, capability := range catalogue.Capabilities {
		if capability.WorkflowTemplate.Name == "" || len(capability.WorkflowTemplate.Steps) == 0 {
			t.Fatalf("capability %s missing workflow template: %#v", capability.ID, capability.WorkflowTemplate)
		}
		switch capability.ID {
		case "memory-context":
			foundMemory = true
		case "review-approve":
			foundApproval = true
		case "research-prepare":
			foundResearch = true
		}
	}
	if !foundMemory || !foundApproval || !foundResearch {
		t.Fatalf("capabilities missing memory=%v approval=%v research=%v", foundMemory, foundApproval, foundResearch)
	}
}

func TestFilterCatalogueUsesAreaAndSearch(t *testing.T) {
	catalogue := DefaultCatalogue(time.Date(2026, 4, 30, 21, 0, 0, 0, time.UTC))

	filtered := FilterCatalogue(catalogue, Query{Area: "research", Search: "sources"})

	if len(filtered.Capabilities) != 1 {
		t.Fatalf("capabilities = %d, want one research source capability", len(filtered.Capabilities))
	}
	if filtered.Capabilities[0].ID != "research-prepare" {
		t.Fatalf("capability = %q, want research-prepare", filtered.Capabilities[0].ID)
	}
	if len(filtered.Activities) != 1 || filtered.Activities[0].ID != "prepare-decision" {
		t.Fatalf("activities = %#v, want research decision activity", filtered.Activities)
	}
	if len(filtered.Filters.Areas) == 0 || filtered.Filters.Areas[0].Value != "all" {
		t.Fatalf("filters = %#v, want stable area filters", filtered.Filters.Areas)
	}
}

func TestFilterCatalogueMatchesActivityThroughCapability(t *testing.T) {
	catalogue := DefaultCatalogue(time.Date(2026, 4, 30, 21, 0, 0, 0, time.UTC))

	filtered := FilterCatalogue(catalogue, Query{Search: "approval"})

	if len(filtered.Capabilities) == 0 {
		t.Fatal("expected approval-related capabilities")
	}
	foundRoutine := false
	for _, activity := range filtered.Activities {
		if activity.ID == "run-routine" {
			foundRoutine = true
		}
	}
	if !foundRoutine {
		t.Fatalf("activities = %#v, want run-routine through review-approve capability", filtered.Activities)
	}
}

func TestFilterCatalogueMatchesCapabilityThroughActivity(t *testing.T) {
	catalogue := DefaultCatalogue(time.Date(2026, 4, 30, 21, 0, 0, 0, time.UTC))

	filtered := FilterCatalogue(catalogue, Query{Search: "shopping"})

	if len(filtered.Activities) != 1 || filtered.Activities[0].ID != "prepare-decision" {
		t.Fatalf("activities = %#v, want shopping decision activity", filtered.Activities)
	}
	foundResearch := false
	for _, capability := range filtered.Capabilities {
		if capability.ID == "research-prepare" {
			foundResearch = true
		}
	}
	if !foundResearch {
		t.Fatalf("capabilities = %#v, want research capability referenced by matching activity", filtered.Capabilities)
	}
}
