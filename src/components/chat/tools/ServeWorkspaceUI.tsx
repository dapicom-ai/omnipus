/**
 * ServeWorkspaceUI — replay-only alias for the pre-unification `serve_workspace`
 * tool name.
 *
 * Kept so chat transcripts saved before the web_serve tool unification render
 * correctly. New sessions use WebServeUI (registered as `web_serve`). Do not
 * use this component for new features — add them to WebServeUI.tsx instead.
 *
 * Spec: FR-008 / FR-010 / FR-011 / FR-012 / FR-015 / FR-019.
 */

import { makeWebServeUI } from './WebServeUI'

export const ServeWorkspaceUI = makeWebServeUI('serve_workspace')
