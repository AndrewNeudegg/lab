import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';

const pageSource = readFileSync(new URL('./+page.svelte', import.meta.url), 'utf8');

describe('assistant page composition', () => {
	  test('keeps run selection URL-addressable like other record pages', () => {
    expect(pageSource).toContain('assistantRunURL');
    expect(pageSource).toContain('assistantRunsURL');
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
	    expect(pageSource).toContain('<option value="auto">Auto route</option>');
	    expect(pageSource).toContain('<option value="remote">Remote project</option>');
	    expect(pageSource).toContain('<option value="local">Local homelabd</option>');
	    expect(pageSource).toContain('<dt>Target</dt>');
	    expect(pageSource).toContain('{targetLabel(selectedGoal.target)}');
	    expect(pageSource).toContain('{targetLabel(action.target)}');
	  });
	});
