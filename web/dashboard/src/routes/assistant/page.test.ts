import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';

const pageSource = readFileSync(new URL('./+page.svelte', import.meta.url), 'utf8');

describe('assistant page composition', () => {
	  test('keeps run selection URL-addressable like other record pages', () => {
    expect(pageSource).toContain('assistantRunURL');
    expect(pageSource).toContain('assistantRunsURL');
    expect(pageSource).toContain('runDetails[selectedRunId] || selectedRunSummary');
    expect(pageSource).toContain('const run = await client.getAssistantRun(runId)');
    expect(pageSource).toContain('const currentRunRouteId = () =>');
    expect(pageSource).toContain('const currentRunRouteView = (): AssistantRunView =>');
    expect(pageSource).toContain(
      'const navigateToRun = (runId: string, replaceState = false, view: AssistantRunView = runView) =>'
    );
    expect(pageSource).toContain("void goto(next, { keepFocus: true, noScroll: true, replaceState })");
    expect(pageSource).toContain(
      'const navigateToRunOverview = (replaceState = true, view: AssistantRunView = runView) =>'
    );
    expect(pageSource).toContain('const next = assistantRunsURL(view)');
    expect(pageSource).toContain('href={assistantRunURL(run.id, assistantRunView(run))}');
    expect(pageSource).toContain('on:click={() => void updateSelectedRunArchive(!selectedRun.archived)}');
	    expect(pageSource).toContain('on:click={() => setRunView(space.id)}');
	  });

	  test('surfaces Goal execution targets clearly', () => {
	    expect(pageSource).toContain("type GoalTargetMode = 'auto' | 'local' | 'remote'");
	    expect(pageSource).toContain('client.listWorkspaces()');
	    expect(pageSource).toContain('target: goalTargetFromForm()');
	    expect(pageSource).toContain('target: goalTargetFromEditForm()');
	    expect(pageSource).toContain('<option value="auto">Auto route</option>');
	    expect(pageSource).toContain('<option value="remote">Remote project</option>');
	    expect(pageSource).toContain('<option value="local">Local homelabd</option>');
	    expect(pageSource).toContain('<dt>Target</dt>');
	    expect(pageSource).toContain('{targetLabel(selectedGoal.target)}');
	    expect(pageSource).toContain('{targetLabel(action.target)}');
	  });

	  test('lets operators edit existing Goal text and task limits', () => {
	    expect(pageSource).toContain('aria-label="Edit Assistant Goal"');
	    expect(pageSource).toContain('client.updateAssistantGoal(selectedGoalId');
	    expect(pageSource).toContain('Autopilot task limit');
	    expect(pageSource).toContain('placeholder="-1 = unlimited"');
	    expect(pageSource).not.toContain('GOAL_AUTOPILOT_TASK_BUDGET_MAX');
	  });

	  test('surfaces canonical Goal blocker traces in lists, detail, and run history', () => {
	    expect(pageSource).toContain('type AssistantGoalBlockerTrace');
	    expect(pageSource).toContain('selectedGoalBlockerTrace');
	    expect(pageSource).toContain('const currentGoalRouteId = () =>');
	    expect(pageSource).toContain('const assistantGoalURL = (goalId: string)');
	    expect(pageSource).toContain('const navigateToGoal = (goalId: string, replaceState = false) =>');
	    expect(pageSource).toContain('goalBlockerTraceForRun');
	    expect(pageSource).toContain('aria-label="Goal blocker trace"');
	    expect(pageSource).toContain('Open blocking task');
	    expect(pageSource).toContain('Resume Autopilot');
	    expect(pageSource).toContain('Newer Goal blocker exists');
	    expect(pageSource).toContain('newer blocker');
	  });
	});
