/**
 * RunInWorkspaceUI — back-compat alias for the `run_in_workspace` tool name.
 *
 * Historical chat transcripts reference `run_in_workspace`. Keeping this
 * component registered ensures those transcripts replay correctly. All
 * logic now lives in WebServeUI.
 *
 * Spec: FR-008a / CR-03 / FR-013 / FR-014.
 */

import { makeWebServeUI } from './WebServeUI'

export const RunInWorkspaceUI = makeWebServeUI('run_in_workspace')
