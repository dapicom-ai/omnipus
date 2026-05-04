/**
 * RunInWorkspaceUI — replay-only alias for the pre-unification `run_in_workspace`
 * tool name.
 *
 * Kept so chat transcripts saved before the web_serve tool unification render
 * correctly. New sessions use WebServeUI (registered as `web_serve`). Do not
 * use this component for new features — add them to WebServeUI.tsx instead.
 *
 * Spec: FR-008a / CR-03 / FR-013 / FR-014.
 */

import { makeWebServeUI } from './WebServeUI'

export const RunInWorkspaceUI = makeWebServeUI('run_in_workspace')
