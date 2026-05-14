package commands

import "context"

func cancelCommand() Definition {
	return Definition{
		Name:        "cancel",
		Description: "Cancel the current turn",
		Usage:       "/cancel",
		// No Aliases per FR-5 — /stop, /abort, /kill and any other alias are
		// explicitly forbidden.
		Handler: func(ctx context.Context, req Request, rt *Runtime) error {
			if rt == nil {
				return req.Reply(unavailableMsg)
			}

			var sessionID string
			if rt.SessionID != nil {
				sessionID = rt.SessionID()
			}

			canceller := Canceller{
				UserID:  req.SenderID,
				Channel: req.Channel,
			}

			// CancelActiveTurn always returns nil (see runtime.go); the reply is
			// sent regardless of whether a turn was actually active — the handler
			// layer acknowledges receipt, and the audit path (turn_cancel_attempt
			// / turn_cancelled entries) is the responsibility of the gateway
			// cancel-handler wave.
			if err := rt.CancelActiveTurn(ctx, sessionID, canceller); err != nil {
				return req.Reply("Cancel request failed: " + err.Error())
			}
			return req.Reply("⏸ Cancelling...")
		},
	}
}
