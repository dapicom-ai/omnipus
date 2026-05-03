/**
 * ServeWorkspaceUI — back-compat alias for the `serve_workspace` tool name.
 *
 * Historical chat transcripts reference `serve_workspace`. Keeping this
 * component registered ensures those transcripts replay correctly. All
 * logic now lives in WebServeUI.
 *
 * Spec: FR-008 / FR-010 / FR-011 / FR-012 / FR-015 / FR-019.
 */

import { makeWebServeUI } from './WebServeUI'

export const ServeWorkspaceUI = makeWebServeUI('serve_workspace')
