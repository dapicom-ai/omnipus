package browser

import (
	"github.com/dapicom-ai/omnipus/pkg/security"
	"github.com/dapicom-ai/omnipus/pkg/tools"
)

// RegisterTools registers all 7 browser automation tools with the given registry.
// The BrowserManager is shared across all tools and manages the Chromium lifecycle.
// Returns an error if ssrf is nil (SSRF protection is mandatory per SEC-24).
//
// Tools registered:
//   - browser.navigate  — navigate to a URL (SSRF-checked)
//   - browser.click     — click an element by CSS selector
//   - browser.type      — type text into an input
//   - browser.screenshot — capture a full-page PNG screenshot
//   - browser.get_text  — extract inner text from an element
//   - browser.wait      — wait for an element to appear
//   - browser.evaluate  — execute JS (policy-gated, denied by default)
func RegisterTools(
	registry *tools.ToolRegistry,
	cfg BrowserConfig,
	ssrf *security.SSRFChecker,
) (*BrowserManager, error) {
	mgr, err := NewBrowserManager(cfg, ssrf)
	if err != nil {
		return nil, err
	}

	registry.Register(&NavigateTool{mgr: mgr})
	registry.Register(&ClickTool{mgr: mgr})
	registry.Register(&TypeTool{mgr: mgr})
	registry.Register(&ScreenshotTool{mgr: mgr})
	registry.Register(&GetTextTool{mgr: mgr})
	registry.Register(&WaitTool{mgr: mgr})
	registry.Register(&EvaluateTool{mgr: mgr})

	return mgr, nil
}
