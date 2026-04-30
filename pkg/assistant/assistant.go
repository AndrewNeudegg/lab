package assistant

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	workflowstore "github.com/andrewneudegg/lab/pkg/workflow"
)

const (
	AutonomyObserve = "observe"
	AutonomyPlan    = "plan"
	AutonomyConfirm = "confirm"
	AutonomyAuto    = "automatic"
)

type Query struct {
	Search string
	Area   string
}

type Catalogue struct {
	Name            string           `json:"name"`
	Summary         string           `json:"summary"`
	UpdatedAt       time.Time        `json:"updated_at"`
	Principles      []Principle      `json:"principles"`
	Activities      []Activity       `json:"activities"`
	Capabilities    []Capability     `json:"capabilities"`
	UXPatterns      []UXPattern      `json:"ux_patterns"`
	ResearchSources []ResearchSource `json:"research_sources"`
	Filters         Filters          `json:"filters"`
}

type Filters struct {
	Areas []FilterOption `json:"areas"`
}

type FilterOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type Principle struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type Activity struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Area          string   `json:"area"`
	Cadence       string   `json:"cadence"`
	Description   string   `json:"description"`
	Outcome       string   `json:"outcome"`
	CapabilityIDs []string `json:"capability_ids"`
	SearchTerms   []string `json:"search_terms,omitempty"`
}

type Capability struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Area             string           `json:"area"`
	Summary          string           `json:"summary"`
	Promise          string           `json:"promise"`
	Cadence          string           `json:"cadence"`
	Autonomy         string           `json:"autonomy"`
	Inputs           []string         `json:"inputs"`
	Outputs          []string         `json:"outputs"`
	Surfaces         []ActionLink     `json:"surfaces"`
	UXPatternIDs     []string         `json:"ux_pattern_ids"`
	Safeguards       []string         `json:"safeguards"`
	WorkflowTemplate WorkflowTemplate `json:"workflow_template"`
	SearchTerms      []string         `json:"search_terms,omitempty"`
}

type ActionLink struct {
	Label   string `json:"label"`
	Href    string `json:"href"`
	Surface string `json:"surface"`
}

type WorkflowTemplate struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Goal        string               `json:"goal"`
	Steps       []workflowstore.Step `json:"steps"`
}

type UXPattern struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Summary        string `json:"summary"`
	AppliesTo      string `json:"applies_to"`
	Implementation string `json:"implementation"`
}

type ResearchSource struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Summary string `json:"summary"`
}

func DefaultCatalogue(now time.Time) Catalogue {
	catalogue := Catalogue{
		Name:      "Assistant",
		Summary:   "A life-improving operating layer that turns context into briefings, plans, durable workflows, memory, reviews, and safe action.",
		UpdatedAt: now.UTC(),
		Principles: []Principle{
			{
				Name:    "Context first",
				Summary: "Ground help in the operator's current task queue, workflows, docs, health, recent conversations, and explicit memories before inventing new work.",
			},
			{
				Name:    "Proactive, but interruptible",
				Summary: "Surface timely briefs and opportunities, then make every recommendation easy to dismiss, correct, pause, or convert into a task.",
			},
			{
				Name:    "Plan before action",
				Summary: "Any state-changing or high-stakes action starts as an inspectable plan with clear scope, inputs, expected outputs, and approval needs.",
			},
			{
				Name:    "Receipts over mystery",
				Summary: "Every Assistant action should leave a trace: source context, rationale, status, result, and next recovery path.",
			},
		},
		Activities: []Activity{
			{
				ID:            "start-day",
				Name:          "Start my day",
				Area:          "focus",
				Cadence:       "Daily",
				Description:   "Summarise schedule, task pressure, unread decisions, overnight system health, and one realistic focus plan.",
				Outcome:       "A short morning brief with priorities, blockers, and suggested focus blocks.",
				CapabilityIDs: []string{"brief-prioritise", "monitor-notify", "plan-schedule"},
				SearchTerms:   []string{"brief", "daily", "morning", "priorities", "calendar", "focus"},
			},
			{
				ID:            "capture-remember",
				Name:          "Capture and remember",
				Area:          "memory",
				Cadence:       "Any time",
				Description:   "Turn stray notes, preferences, facts, decisions, and recurring constraints into searchable memory with visible scope.",
				Outcome:       "Future answers use durable context without hiding where it came from.",
				CapabilityIDs: []string{"memory-context", "review-approve"},
				SearchTerms:   []string{"memory", "preference", "remember", "decision", "context"},
			},
			{
				ID:            "prepare-decision",
				Name:          "Research a decision",
				Area:          "research",
				Cadence:       "On demand",
				Description:   "Investigate options, compare trade-offs, cite sources, and reduce the result to a recommended next action.",
				Outcome:       "A sourced decision brief with risks, alternatives, and follow-up work encoded.",
				CapabilityIDs: []string{"research-prepare", "brief-prioritise"},
				SearchTerms:   []string{"research", "compare", "decision", "sources", "shopping", "travel"},
			},
			{
				ID:            "run-routine",
				Name:          "Run a routine",
				Area:          "execution",
				Cadence:       "Scheduled or triggered",
				Description:   "Convert recurring admin, maintenance, planning, and reporting into workflows that can run with review gates.",
				Outcome:       "Repeatable workflows handle boring work while preserving operator approval for risky steps.",
				CapabilityIDs: []string{"workflow-execution", "monitor-notify", "review-approve"},
				SearchTerms:   []string{"routine", "scheduled", "workflow", "automation", "recurring"},
			},
			{
				ID:            "prepare-communicate",
				Name:          "Prepare communication",
				Area:          "communication",
				Cadence:       "On demand",
				Description:   "Draft replies, summaries, meeting notes, status updates, and follow-ups from the right context.",
				Outcome:       "Human-reviewed messages that save time without sending surprises.",
				CapabilityIDs: []string{"communicate-compose", "memory-context", "review-approve"},
				SearchTerms:   []string{"email", "message", "meeting", "notes", "summary", "follow up"},
			},
			{
				ID:            "watch-systems",
				Name:          "Watch what matters",
				Area:          "systems",
				Cadence:       "Continuous",
				Description:   "Monitor tasks, health, approvals, docs drift, and external signals, then notify only when the operator can act.",
				Outcome:       "Fewer surprise failures and fewer noisy notifications.",
				CapabilityIDs: []string{"monitor-notify", "brief-prioritise", "review-approve"},
				SearchTerms:   []string{"monitor", "health", "alerts", "approval", "queue", "systems"},
			},
		},
		Capabilities: []Capability{
			{
				ID:       "brief-prioritise",
				Name:     "Brief and prioritise",
				Area:     "focus",
				Summary:  "Produce daily, weekly, and situation-specific briefs from task state, calendar-like commitments, docs, and health signals.",
				Promise:  "Tell me what matters now, what can wait, and what needs a decision.",
				Cadence:  "Daily, weekly, and on return after time away",
				Autonomy: AutonomyObserve,
				Inputs:   []string{"Tasks", "Workflows", "Approvals", "Events", "Health", "Operator notes"},
				Outputs:  []string{"Priority list", "Blockers", "Suggested focus blocks", "Follow-up prompts"},
				Surfaces: []ActionLink{
					{Label: "Open Chat", Href: "/chat", Surface: "chat"},
					{Label: "Inspect Tasks", Href: "/tasks", Surface: "tasks"},
				},
				UXPatternIDs: []string{"mission-control", "context-cards", "explainable-rationale"},
				Safeguards:   []string{"Show source counts", "Mark stale context", "Keep recommendations dismissible"},
				WorkflowTemplate: WorkflowTemplate{
					Name:        "Assistant daily brief",
					Description: "A reviewable morning brief compiled from homelabd state.",
					Goal:        "Create a concise daily brief with priorities, risks, approvals, and a focus plan.",
					Steps: []workflowstore.Step{
						llmStep("Collect context", "Read tasks, workflows, approvals, health, and recent events. Identify urgent decisions and stale work."),
						llmStep("Write brief", "Summarise the day as priorities, blockers, suggested focus blocks, and safe next actions."),
					},
				},
				SearchTerms: []string{"brief", "priority", "daily", "weekly", "focus", "pulse"},
			},
			{
				ID:       "memory-context",
				Name:     "Memory and context",
				Area:     "memory",
				Summary:  "Maintain explicit preferences, important facts, project context, recurring constraints, and durable decision notes.",
				Promise:  "Stop making me repeat stable context while keeping every memory inspectable and removable.",
				Cadence:  "Continuous, with review on changes",
				Autonomy: AutonomyConfirm,
				Inputs:   []string{"Chat", "Docs", "Task results", "Operator corrections", "Uploaded references"},
				Outputs:  []string{"Saved memory", "Project context", "Decision log", "Context boundaries"},
				Surfaces: []ActionLink{
					{Label: "Open Docs", Href: "/docs", Surface: "docs"},
					{Label: "Open Chat", Href: "/chat", Surface: "chat"},
				},
				UXPatternIDs: []string{"editable-memory", "context-cards", "action-audit"},
				Safeguards:   []string{"Require confirmation before saving sensitive memory", "Show memory scope", "Allow delete and correction"},
				WorkflowTemplate: WorkflowTemplate{
					Name:        "Assistant memory review",
					Description: "A recurring check that turns useful context into explicit memory proposals.",
					Goal:        "Review recent decisions and corrections, then propose memories with source links and delete paths.",
					Steps: []workflowstore.Step{
						llmStep("Find durable facts", "Identify preferences, recurring constraints, decisions, and facts worth remembering."),
						waitStep("Human memory approval", "operator approves proposed memory changes", 86400),
					},
				},
				SearchTerms: []string{"memory", "context", "project", "preference", "remember", "decision"},
			},
			{
				ID:       "plan-schedule",
				Name:     "Plan and schedule",
				Area:     "planning",
				Summary:  "Break goals into tasks, reminders, routines, and reviewable schedules that respect time, energy, and existing commitments.",
				Promise:  "Turn intent into a realistic plan instead of another vague note.",
				Cadence:  "On demand, timed, and recurring",
				Autonomy: AutonomyPlan,
				Inputs:   []string{"Goal", "Existing tasks", "Deadlines", "Operator constraints", "Known routines"},
				Outputs:  []string{"Task plan", "Calendar proposal", "Reminder", "Workflow draft"},
				Surfaces: []ActionLink{
					{Label: "Create Task", Href: "/tasks", Surface: "tasks"},
					{Label: "Build Workflow", Href: "/workflows", Surface: "workflows"},
				},
				UXPatternIDs: []string{"intent-preview", "autonomy-dial", "taskboard-outcomes"},
				Safeguards:   []string{"Never schedule externally without confirmation", "Show conflicts before committing", "Keep edit path visible"},
				WorkflowTemplate: WorkflowTemplate{
					Name:        "Assistant goal planner",
					Description: "Convert a high-level goal into tasks, waits, and approval gates.",
					Goal:        "Draft a realistic task plan with dependencies, risks, and suggested scheduling windows.",
					Steps: []workflowstore.Step{
						llmStep("Decompose goal", "Break the goal into concrete tasks, dependencies, and decision points."),
						waitStep("Plan review", "operator confirms scope and timing", 43200),
					},
				},
				SearchTerms: []string{"plan", "schedule", "calendar", "reminder", "routine", "tasks"},
			},
			{
				ID:       "research-prepare",
				Name:     "Research and prepare",
				Area:     "research",
				Summary:  "Run sourced research for decisions, meetings, purchases, travel, learning, and technical investigations.",
				Promise:  "Give me a brief that is current, cited, comparable, and ready to act on.",
				Cadence:  "On demand or before planned events",
				Autonomy: AutonomyPlan,
				Inputs:   []string{"Question", "Web sources", "Docs", "Files", "Prior decisions"},
				Outputs:  []string{"Sourced brief", "Comparison", "Recommendation", "Open questions", "Task proposal"},
				Surfaces: []ActionLink{
					{Label: "Open Chat", Href: "/chat", Surface: "chat"},
					{Label: "Encode Workflow", Href: "/workflows", Surface: "workflows"},
				},
				UXPatternIDs: []string{"source-tray", "confidence-signals", "context-cards"},
				Safeguards:   []string{"Show sources and recency", "Separate facts from inference", "Escalate high-stakes topics to operator review"},
				WorkflowTemplate: WorkflowTemplate{
					Name:        "Assistant research brief",
					Description: "A research workflow that gathers sources before making a recommendation.",
					Goal:        "Research the question, compare options, cite sources, and recommend next action.",
					Steps: []workflowstore.Step{
						toolStep("Search current sources", "internet.search", map[string]string{"query": "assistant research topic"}),
						llmStep("Synthesize decision brief", "Compare options, cite source recency, state confidence, and recommend a next step."),
					},
				},
				SearchTerms: []string{"research", "prepare", "decision", "meeting", "compare", "sources"},
			},
			{
				ID:       "workflow-execution",
				Name:     "Workflow execution",
				Area:     "execution",
				Summary:  "Turn repeatable work into durable, inspectable workflows with progress, pauses, approval gates, and recovery.",
				Promise:  "Handle multi-step work without hiding what happened or bypassing control.",
				Cadence:  "On demand, event-triggered, or scheduled",
				Autonomy: AutonomyConfirm,
				Inputs:   []string{"Workflow goal", "Tools", "Approvals", "Health gates", "Task context"},
				Outputs:  []string{"Workflow run", "Step receipts", "Approval request", "Recovery path"},
				Surfaces: []ActionLink{
					{Label: "Open Workflows", Href: "/workflows", Surface: "workflows"},
					{Label: "Review Approvals", Href: "/tasks", Surface: "tasks"},
				},
				UXPatternIDs: []string{"taskboard-outcomes", "interrupt-steer", "action-audit"},
				Safeguards:   []string{"Pause for risky tool calls", "Expose current step", "Allow cancellation and retry"},
				WorkflowTemplate: WorkflowTemplate{
					Name:        "Assistant supervised routine",
					Description: "A durable workflow template with explicit plan, tool step, and approval wait.",
					Goal:        "Run a recurring assistant routine with receipts and human approval before side effects.",
					Steps: []workflowstore.Step{
						llmStep("Plan routine", "List the intended actions and the risk of each step before tools run."),
						waitStep("Operator approval", "operator approves side effects", 21600),
						toolStep("Run approved tool", "task.create", map[string]string{"goal": "Follow up from approved Assistant routine"}),
					},
				},
				SearchTerms: []string{"workflow", "agent", "execute", "approval", "automation", "routine"},
			},
			{
				ID:       "communicate-compose",
				Name:     "Communicate and compose",
				Area:     "communication",
				Summary:  "Draft, summarise, rewrite, and follow up on emails, messages, notes, and status reports using the right context.",
				Promise:  "Make communication faster while preserving my final say before anything leaves the system.",
				Cadence:  "On demand and after important events",
				Autonomy: AutonomyPlan,
				Inputs:   []string{"Message goal", "Audience", "Tone", "Relevant docs", "Task outcome"},
				Outputs:  []string{"Draft", "Summary", "Follow-up", "Status update"},
				Surfaces: []ActionLink{
					{Label: "Open Chat", Href: "/chat", Surface: "chat"},
					{Label: "Capture Follow-up", Href: "/tasks", Surface: "tasks"},
				},
				UXPatternIDs: []string{"intent-preview", "editable-output", "action-audit"},
				Safeguards:   []string{"Draft before send", "Show audience and source context", "Require confirmation for external messages"},
				WorkflowTemplate: WorkflowTemplate{
					Name:        "Assistant follow-up draft",
					Description: "Draft a message after a task or meeting, then wait for review.",
					Goal:        "Draft a concise follow-up from context and hold it for operator approval.",
					Steps: []workflowstore.Step{
						llmStep("Draft message", "Write a concise follow-up with audience, tone, source facts, and proposed next action."),
						waitStep("Human send decision", "operator reviews and sends externally", 86400),
					},
				},
				SearchTerms: []string{"draft", "message", "email", "notes", "summary", "follow-up"},
			},
			{
				ID:       "monitor-notify",
				Name:     "Monitor and notify",
				Area:     "systems",
				Summary:  "Watch tasks, services, approvals, stale decisions, and external signals, then notify when useful action is possible.",
				Promise:  "Catch important changes without turning the assistant into noise.",
				Cadence:  "Continuous and scheduled",
				Autonomy: AutonomyObserve,
				Inputs:   []string{"Healthd", "Task events", "Approvals", "Workflow runs", "Operator watch rules"},
				Outputs:  []string{"Alert", "Digest", "Escalation", "Suggested recovery"},
				Surfaces: []ActionLink{
					{Label: "Open Health", Href: "/healthd", Surface: "health"},
					{Label: "Open Tasks", Href: "/tasks", Surface: "tasks"},
				},
				UXPatternIDs: []string{"mission-control", "notification-budget", "explainable-rationale"},
				Safeguards:   []string{"Bundle low-value alerts into digests", "Explain why now", "Respect quiet hours and severity"},
				WorkflowTemplate: WorkflowTemplate{
					Name:        "Assistant watch report",
					Description: "A recurring watch that turns operational changes into actionable digests.",
					Goal:        "Review health, tasks, approvals, and workflow runs; notify only on actionable changes.",
					Steps: []workflowstore.Step{
						llmStep("Inspect signals", "Review health, task, workflow, and approval state for actionable changes."),
						llmStep("Create digest", "Bundle low-value observations and highlight only urgent decisions."),
					},
				},
				SearchTerms: []string{"monitor", "notify", "health", "watch", "alert", "digest"},
			},
			{
				ID:       "review-approve",
				Name:     "Review and approve",
				Area:     "execution",
				Summary:  "Collect sensitive actions, memory changes, sends, deletes, purchases, and workflow side effects into one approval path.",
				Promise:  "Keep automation useful without letting it cross a boundary quietly.",
				Cadence:  "Whenever risk crosses the configured autonomy level",
				Autonomy: AutonomyConfirm,
				Inputs:   []string{"Approval requests", "Proposed tool args", "Risk level", "Rationale", "Undo path"},
				Outputs:  []string{"Approve", "Deny", "Edit", "Retry", "Audit entry"},
				Surfaces: []ActionLink{
					{Label: "Review Tasks", Href: "/tasks", Surface: "tasks"},
					{Label: "Open Workflows", Href: "/workflows", Surface: "workflows"},
				},
				UXPatternIDs: []string{"intent-preview", "autonomy-dial", "action-audit"},
				Safeguards:   []string{"Show exact args before approval", "Support edit before approve", "Record every decision"},
				WorkflowTemplate: WorkflowTemplate{
					Name:        "Assistant approval sweep",
					Description: "A review pass over pending assistant decisions.",
					Goal:        "Summarise pending approvals with rationale, risk, exact proposed args, and recommended decision.",
					Steps: []workflowstore.Step{
						llmStep("Group approvals", "Cluster pending approvals by risk, system, and urgency."),
						waitStep("Operator decision", "operator approves, edits, or denies each risky action", 86400),
					},
				},
				SearchTerms: []string{"approve", "review", "consent", "risk", "edit", "audit"},
			},
		},
		UXPatterns: []UXPattern{
			{
				ID:             "mission-control",
				Name:           "Mission control",
				Summary:        "Show active outcomes, queued decisions, recent changes, and confidence in one scan-friendly surface.",
				AppliesTo:      "Briefs, monitoring, workflows, and daily focus",
				Implementation: "Use count badges, status text, freshness timestamps, and links to the source object.",
			},
			{
				ID:             "taskboard-outcomes",
				Name:           "Taskboard plus outcomes",
				Summary:        "Represent agent work as visible units with state, owner, next action, and result.",
				AppliesTo:      "Workflow execution and planning",
				Implementation: "Prefer list-detail rows and detail records over chat-only progress.",
			},
			{
				ID:             "intent-preview",
				Name:           "Intent preview",
				Summary:        "Pause before meaningful action and show what will happen, why, and how to change course.",
				AppliesTo:      "Scheduling, sending, purchases, deletes, memory writes, and tool calls",
				Implementation: "Show sequential steps with Proceed, Edit, and Handle myself actions.",
			},
			{
				ID:             "autonomy-dial",
				Name:           "Autonomy dial",
				Summary:        "Let the operator tune each capability from observe, to plan, to confirm, to pre-approved automatic action.",
				AppliesTo:      "Any capability that can change state",
				Implementation: "Store autonomy per task type and make the current level visible near actions.",
			},
			{
				ID:             "context-cards",
				Name:           "Context cards",
				Summary:        "Expose the sources, memories, files, and task records used by a recommendation.",
				AppliesTo:      "Research, briefs, memory, communication, and planning",
				Implementation: "Show source chips with timestamps, permissions, and stale-context warnings.",
			},
			{
				ID:             "source-tray",
				Name:           "Source tray",
				Summary:        "Keep citations and retrieved material close to the answer so claims can be checked quickly.",
				AppliesTo:      "Research and decision briefs",
				Implementation: "Separate sourced facts from model inference and include source recency.",
			},
			{
				ID:             "confidence-signals",
				Name:           "Confidence signals",
				Summary:        "Show uncertainty, missing data, and when the assistant needs a human or specialist.",
				AppliesTo:      "High-stakes or ambiguous work",
				Implementation: "Use concise status labels, open questions, and escalation prompts rather than fake certainty.",
			},
			{
				ID:             "explainable-rationale",
				Name:           "Explainable rationale",
				Summary:        "Say why the assistant made or proposed a decision using user-facing language.",
				AppliesTo:      "Autonomous or proactive suggestions",
				Implementation: "Use because-statements tied to preferences, prior choices, and current context.",
			},
			{
				ID:             "editable-memory",
				Name:           "Editable memory",
				Summary:        "Make remembered facts visible, scoped, correctable, and deletable.",
				AppliesTo:      "Preferences, projects, long-running efforts, and sensitive context",
				Implementation: "Require confirmation for new memories and keep delete/correction paths in the same surface.",
			},
			{
				ID:             "editable-output",
				Name:           "Editable output",
				Summary:        "Treat drafts as collaborative artefacts, not final answers.",
				AppliesTo:      "Messages, notes, status reports, and plans",
				Implementation: "Support rewrite, inline edit, regenerate with constraints, and save as context.",
			},
			{
				ID:             "interrupt-steer",
				Name:           "Interrupt and steer",
				Summary:        "Let the operator halt running work, redirect it, or take over without losing context.",
				AppliesTo:      "Long-running workflows and tool execution",
				Implementation: "Expose current step, cancel, retry, and resume controls with the latest trace.",
			},
			{
				ID:             "notification-budget",
				Name:           "Notification budget",
				Summary:        "Reserve interruptions for decisions or timely opportunities, and digest the rest.",
				AppliesTo:      "Monitoring, reminders, daily briefs, and scheduled tasks",
				Implementation: "Attach severity, quiet-hours, and why-now explanations to notification rules.",
			},
			{
				ID:             "action-audit",
				Name:           "Action audit",
				Summary:        "Record proposed action, exact inputs, human decision, result, and recovery path.",
				AppliesTo:      "All agentic systems",
				Implementation: "Make receipts searchable from the originating task, workflow, or memory item.",
			},
		},
		ResearchSources: []ResearchSource{
			{
				Title:   "Google Gemini assistant capabilities",
				URL:     "https://gemini.google/assistant/?hl=en",
				Summary: "Natural language, multimodal help, events/reminders/lists, Gmail/Drive, screen context, messages, routines, and task completion.",
			},
			{
				Title:   "Microsoft Copilot personal assistant features",
				URL:     "https://www.microsoft.com/en-us/microsoft-copilot/for-individuals/features",
				Summary: "Daily help, planning, screen-aware Vision, connectors, file uploads, daily briefings, and privacy controls.",
			},
			{
				Title:   "OpenAI ChatGPT Tasks and Pulse",
				URL:     "https://help.openai.com/en/articles/10291617-tasks-in-chatgpt",
				Summary: "Automated prompts, one-off and recurring triggers, proactive delivery, daily research summaries, and task management.",
			},
			{
				Title:   "OpenAI ChatGPT Projects",
				URL:     "https://help.openai.com/en/articles/10169521-projects-in-chatgpt",
				Summary: "Long-running workspaces, files, instructions, project memory, repeatable workflows, and connected app context.",
			},
			{
				Title:   "Microsoft human-AI interaction guidelines",
				URL:     "https://www.microsoft.com/en-us/research/articles/guidelines-for-human-ai-interaction-eighteen-best-practices-for-human-centered-ai-design/",
				Summary: "Set expectations, show context, support dismissal and correction, explain behaviour, and manage change over time.",
			},
			{
				Title:   "Smashing Magazine agentic AI UX patterns",
				URL:     "https://www.smashingmagazine.com/2026/02/designing-agentic-ai-practical-ux-patterns/",
				Summary: "Intent previews, autonomy controls, explainable rationale, confidence signals, action audit, undo, and escalation.",
			},
		},
	}
	catalogue.Filters = buildFilters(catalogue.Capabilities)
	return catalogue
}

func FilterCatalogue(catalogue Catalogue, query Query) Catalogue {
	search := strings.ToLower(strings.TrimSpace(query.Search))
	area := strings.ToLower(strings.TrimSpace(query.Area))
	if area == "all" {
		area = ""
	}
	if search == "" && area == "" {
		catalogue.Filters = buildFilters(catalogue.Capabilities)
		return catalogue
	}

	activityCapabilityIDs := map[string]bool{}
	if search != "" {
		for _, activity := range catalogue.Activities {
			if area != "" && activity.Area != area {
				continue
			}
			if !matchesActivity(activity, search) {
				continue
			}
			for _, id := range activity.CapabilityIDs {
				activityCapabilityIDs[id] = true
			}
		}
	}

	capabilities := make([]Capability, 0, len(catalogue.Capabilities))
	directCapabilityIDs := map[string]bool{}
	for _, capability := range catalogue.Capabilities {
		if area != "" && capability.Area != area {
			continue
		}
		matchedCapability := search == "" || matchesCapability(capability, search)
		if search != "" && !matchedCapability && !activityCapabilityIDs[capability.ID] {
			continue
		}
		capabilities = append(capabilities, capability)
		if matchedCapability {
			directCapabilityIDs[capability.ID] = true
		}
	}

	activities := make([]Activity, 0, len(catalogue.Activities))
	for _, activity := range catalogue.Activities {
		if area != "" && activity.Area != area {
			continue
		}
		if search != "" && !matchesActivity(activity, search) && !activityReferences(activity, directCapabilityIDs) {
			continue
		}
		activities = append(activities, activity)
	}

	catalogue.Capabilities = capabilities
	catalogue.Activities = activities
	catalogue.Filters = buildFilters(DefaultCatalogue(catalogue.UpdatedAt).Capabilities)
	return catalogue
}

func llmStep(name, prompt string) workflowstore.Step {
	return workflowstore.Step{Name: name, Kind: workflowstore.StepKindLLM, Prompt: prompt}
}

func waitStep(name, condition string, timeoutSeconds int) workflowstore.Step {
	return workflowstore.Step{
		Name:           name,
		Kind:           workflowstore.StepKindWait,
		Condition:      condition,
		TimeoutSeconds: timeoutSeconds,
	}
}

func toolStep(name, tool string, args any) workflowstore.Step {
	raw, err := json.Marshal(args)
	if err != nil {
		raw = []byte(`{}`)
	}
	return workflowstore.Step{
		Name: name,
		Kind: workflowstore.StepKindTool,
		Tool: tool,
		Args: raw,
	}
}

func buildFilters(capabilities []Capability) Filters {
	labels := map[string]string{
		"communication": "Communication",
		"execution":     "Execution",
		"focus":         "Daily focus",
		"memory":        "Memory",
		"planning":      "Planning",
		"research":      "Research",
		"systems":       "Systems",
	}
	counts := map[string]int{}
	for _, capability := range capabilities {
		counts[capability.Area]++
	}
	areas := []FilterOption{{Value: "all", Label: "All areas", Count: len(capabilities)}}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		label := labels[key]
		if label == "" {
			label = strings.Title(strings.ReplaceAll(key, "-", " "))
		}
		areas = append(areas, FilterOption{Value: key, Label: label, Count: counts[key]})
	}
	return Filters{Areas: areas}
}

func activityReferences(activity Activity, capabilityIDs map[string]bool) bool {
	for _, id := range activity.CapabilityIDs {
		if capabilityIDs[id] {
			return true
		}
	}
	return false
}

func matchesCapability(capability Capability, search string) bool {
	return containsFold(search, capability.ID, capability.Name, capability.Area, capability.Summary, capability.Promise, capability.Cadence, capability.Autonomy) ||
		containsFold(search, capability.Inputs...) ||
		containsFold(search, capability.Outputs...) ||
		containsFold(search, capability.Safeguards...) ||
		containsFold(search, capability.SearchTerms...)
}

func matchesActivity(activity Activity, search string) bool {
	return containsFold(search, activity.ID, activity.Name, activity.Area, activity.Cadence, activity.Description, activity.Outcome) ||
		containsFold(search, activity.SearchTerms...)
}

func containsFold(search string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), search) {
			return true
		}
	}
	return false
}
