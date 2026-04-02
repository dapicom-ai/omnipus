# Design System Alignment Review

**Spec reviewed**: Implementation code in `src/` vs. `docs/brand/brand-guidelines.md` and `docs/BRD/Omnipus_BRD_AppendixC_UI_Spec.md`
**Review date**: 2026-04-01
**Verdict**: REVISE

## Executive Summary

The implementation is strongly aligned with the design system on foundational choices: color tokens, typography, icon library, dark-first design, sidebar architecture, and component structure all match the specifications. However, there are deviations in avatar color palette, icon set coverage, settings navigation pattern, and responsive breakpoint definitions that need correction. 9 findings total: 0 critical, 4 major, 3 minor, 2 observations.

| Severity | Count |
|----------|-------|
| CRITICAL | 0 |
| MAJOR | 4 |
| MINOR | 3 |
| OBSERVATION | 2 |
| **Total** | **9** |

---

## Findings

### MAJOR Findings

#### [MAJ-001] Avatar color palette deviates from spec

- **Lens**: Inconsistency
- **Affected section**: `src/lib/constants.ts` lines 14-17 vs. Appendix C section C.3.3
- **Description**: The spec defines 7 named avatar colors: Green (`#22C55E`), Blue (`#3B82F6`), Purple (`#A855F7`), Yellow (`#EAB308`), Orange (`#F97316`), Red (`#EF4444`), Gray (`#6B7280`). The implementation uses a different set of 8 colors: `#D4AF37` (Forge Gold), `#10B981` (Emerald -- not the spec's Green), `#3B82F6` (Blue -- matches), `#8B5CF6` (different purple), `#EF4444` (Red -- matches), `#F97316` (Orange -- matches), `#EC4899` (Pink -- not in spec), `#06B6D4` (Cyan -- not in spec). The spec's Green, Purple, Yellow, and Gray are missing. Forge Gold, Pink, and Cyan are additions not specified.
- **Impact**: Agent avatars will render with non-standard colors. Core agent defaults (General Assistant = Green `#22C55E`, Researcher = Purple `#A855F7`, Content Creator = Yellow `#EAB308`) will not have their designated colors available.
- **Recommendation**: Replace `AVATAR_COLORS` in `src/lib/constants.ts` with the 7 spec colors: `['#22C55E', '#3B82F6', '#A855F7', '#EAB308', '#F97316', '#EF4444', '#6B7280']`. If Forge Gold is desired as an 8th option, append it, but ensure the spec's 7 are present.

---

#### [MAJ-002] Settings uses tab navigation instead of spec's card-based navigation

- **Lens**: Inconsistency
- **Affected section**: `src/routes/_app/settings.tsx` vs. Appendix C section C.6.5
- **Description**: The spec states: "Six sections, each a navigation card on the settings main page. Click -> section detail page. Back button returns." The implementation uses a horizontal tab strip (`<Tabs>`) with 7 inline tab panels. This is a different navigation pattern. The spec intends a card-grid landing page where each settings section is a card the user clicks to enter a detail view.
- **Impact**: Users see all settings sections as tabs rather than the intended card-based drill-down pattern. On phone screens, the 7-tab horizontal list will overflow and require scrolling, which the spec did not intend. The spec explicitly separates the sections for progressive disclosure.
- **Recommendation**: Replace the tab-based layout with a card grid for the settings landing page. Each card navigates to a separate detail route (e.g., `/settings/providers`, `/settings/security`). Use TanStack Router for subroutes. Tabs can remain as a progressive enhancement for desktop if desired, but the primary pattern should match the spec.

---

#### [MAJ-003] IconRenderer covers only 20 icons; spec requires ~60 for icon picker

- **Lens**: Incompleteness
- **Affected section**: `src/components/shared/IconRenderer.tsx` vs. Appendix C section C.3.3
- **Description**: The spec defines an icon picker with "~60 icons available for selection" grouped into 9 categories: Agents & AI, Work, Development, Creative, Communication, Research, Security, Nature, Abstract. The current `IconRenderer` maps only 20 icon names. Missing icons include: Microscope (Researcher default), PencilSimple (Content Creator default), and many others specified for agent profiles.
- **Impact**: Users creating custom agents will have a severely limited icon selection. The core agent defaults reference icons (Microscope, PencilSimple) that are not in the renderer's map, meaning they will fall back to the default Robot icon instead of rendering correctly.
- **Recommendation**: Expand `ICON_MAP` to include all ~60 icons from the spec's 9 categories. At minimum, immediately add `microscope: Microscope` and `pencil-simple: PencilSimple` to support core agent defaults.

---

#### [MAJ-004] Responsive breakpoints deviate from spec

- **Lens**: Inconsistency
- **Affected section**: `src/components/layout/Sidebar.tsx` (line 19: `PHONE_BREAKPOINT = 768`) and Tailwind usage vs. Appendix C section C.9.1
- **Description**: The spec defines three breakpoints: Desktop >1024px, Tablet 640-1024px, Phone <640px. The sidebar uses 768px as the phone breakpoint (matching Tailwind's `md`), and the sidebar pin toggle uses the `md:` prefix (768px). However, the spec says Phone is <640px and Tablet is 640-1024px. At 640-768px, the implementation treats the device as phone-width (unpins sidebar, hides pin toggle) while the spec considers this tablet territory where pinning should still be available. Additionally, the `sm` breakpoint (640px) in Tailwind should be the spec's phone boundary, not `md` (768px).
- **Impact**: Users on small tablets or large phones (640-768px width) lose the pin sidebar option that the spec grants to the tablet breakpoint. The entire responsive tier system is shifted 128px wider than specified.
- **Recommendation**: Adjust `PHONE_BREAKPOINT` to 640 and change the pin toggle visibility from `hidden md:flex` to `hidden sm:flex` (Tailwind `sm` = 640px, matching the spec's phone/tablet boundary). Review all `sm:` and `md:` prefix usage across components to ensure alignment with the spec's three-tier breakpoint system.

---

### MINOR Findings

#### [MIN-001] Sidebar nav item "Skills & Tools" uses PuzzlePiece instead of spec's Puzzle

- **Lens**: Inconsistency
- **Affected section**: `src/components/layout/Sidebar.tsx` line 8 vs. Appendix C section C.6.1
- **Description**: The spec lists the Skills & Tools icon as `Puzzle`. The import and usage is `PuzzlePiece`. Phosphor Icons has both `Puzzle` and `PuzzlePiece` -- they are different icons. The spec specifically says `Puzzle`.
- **Recommendation**: Change the import from `PuzzlePiece` to `Puzzle` from `@phosphor-icons/react` to match the spec.

---

#### [MIN-002] SessionBar uses Timer icon for "Sessions" instead of spec's layout

- **Lens**: Inconsistency
- **Affected section**: `src/components/chat/SessionBar.tsx` vs. Appendix C section C.6.1.1
- **Description**: The spec shows the session bar as: `< Sessions | Agent: Work | Model: opus | heartbeat | cost | tokens`. The implementation has the "Sessions" button at the far right with a Timer icon, whereas the spec has `< Sessions` as a left-aligned text link that opens the session hierarchy slide-over. The spec does not show a Timer icon for sessions.
- **Recommendation**: Move the Sessions element to the left side of the session bar, style it as `< Sessions` text (left-pointing caret + text), and remove the Timer icon to match the spec wireframe.

---

#### [MIN-003] Warning badge uses XCircle icon for non-error context

- **Lens**: Incorrectness
- **Affected section**: `src/routes/onboarding.tsx` line 413
- **Description**: The fallback provider warning on the onboarding screen uses `XCircle` (an error/failure icon) in a warning-styled container (yellow border, `--color-warning`). The semantic mismatch between a warning context and an error icon could confuse users. The spec's emoji-to-icon mapping table maps `Warning` to the `Warning` Phosphor icon for caution contexts.
- **Recommendation**: Replace `<XCircle>` with `<Warning>` from `@phosphor-icons/react` for this warning-context message.

---

### Observations

#### [OBS-001] Destructive variant badge missing from spec's semantic badge system

- **Lens**: Overcomplexity
- **Affected section**: `src/components/ui/badge.tsx`
- **Suggestion**: The badge component includes 7 variants (default, secondary, outline, success, error, warning, muted). The spec's brand guidelines define only 5 semantic colors (primary, secondary, accent, success, error). The `warning` and `muted` variants are reasonable extensions. However, the `destructive` variant referenced in `AgentCard.tsx` for error-status agents does not exist in the badge component (it would fall through to default). Consider either adding an explicit `destructive` alias for `error` or auditing call sites.

---

#### [OBS-002] Body line-height set correctly but H1 spec not implemented

- **Lens**: Incompleteness
- **Affected section**: `src/styles/globals.css` line 69 vs. brand-guidelines.md section 4
- **Suggestion**: The CSS sets body `line-height: 1.6` matching the spec's Body line-height. However, the spec defines H1 as `48px / Bold (700) / 1.1` and Caption as `12px / Medium (500) / 1.4`. There are no CSS rules or Tailwind utilities enforcing these specific line-heights for headlines and captions. Components use inline Tailwind classes like `text-4xl` and `text-5xl` (which are not 48px) with default Tailwind line-heights. Consider adding utility classes or CSS custom properties for the spec's type scale (`--type-h1`, `--type-body`, `--type-caption`).

---

## Structural Integrity

### Document Completeness Assessment (Generic Markdown)

**Scope clarity**: The design system is well-defined across two documents (brand guidelines and Appendix C). The scope is clear.

**Actors identified**: All actors (users, agents, system) are identified in the spec.

**Success criteria**: The spec provides concrete values (hex codes, font names, pixel sizes, icon names) that serve as testable success criteria.

**Failure modes**: Not applicable -- this is a design system review, not a feature spec.

**Implementation detail**: Sufficient for implementation. Token values, component patterns, and responsive rules are explicit.

**Assumptions & constraints**: The constraint that all three variants share `@omnipus/ui` is explicit. The dark-first assumption is stated.

---

## Test Coverage Assessment

### Missing Test Categories

| Category | Gap Description | Affected Scenarios |
|----------|----------------|-------------------|
| Visual regression | No visual tests verifying design token application | All components |
| Responsive breakpoints | No tests validating the three-tier responsive behavior | Sidebar pin, SessionBar adaptation |
| Color contrast | No accessibility contrast ratio verification for the dark palette | All text on surface combinations |

### Dataset Gaps

| Dataset | Missing Boundary Type | Recommendation |
|---------|----------------------|----------------|
| Avatar colors | Spec vs. implementation mismatch | Add a test asserting `AVATAR_COLORS` matches spec values |
| Icon coverage | Missing core agent icons | Add a test asserting all core agent default icons exist in `ICON_MAP` |

---

## STRIDE Threat Summary

Not applicable -- this is a design system alignment review, not a security-facing specification.

---

## Unasked Questions

1. **Light mode**: The spec mentions "Light mode is a secondary consideration -- invert primary/secondary while maintaining accent and semantic colors." Has light mode been considered in the current token architecture, or will it require a refactor later? The current `@theme` block has no light-mode overrides.

2. **Emoji-to-icon translator**: The spec defines an ~80-mapping emoji-to-icon translator (C.3.2). Is this implemented anywhere? It is not visible in the reviewed files. If deferred, is that intentional?

3. **Agent avatar sizes**: The spec defines 5 avatar sizes (24px, 32px, 48px, 64px for different contexts). The implementation uses `w-10 h-10` (40px) in AgentCard and `w-5 h-5` (20px) in SessionBar. Neither matches the spec sizes exactly. Is this intentional?

4. **Sidebar width on phone**: The spec says phone sidebar should be "Full-width overlay with dimmed backdrop." The implementation uses a fixed 256px width on all sizes with no backdrop dim. Is the phone behavior intentionally simplified?

---

## Verdict Rationale

The codebase demonstrates strong alignment with the design system's foundational decisions: the "Sovereign Deep" color palette is correctly tokenized, Phosphor Icons are used consistently (no Lucide contamination), the three brand fonts are imported and applied, dark-first surfaces are layered correctly (surface-0 through surface-3), and the overall component architecture follows shadcn/ui patterns with proper Radix primitives.

However, four major deviations need correction. The avatar color palette (MAJ-001) directly affects core agent visual identity. The settings navigation pattern (MAJ-002) is a structural UX difference from the spec. The limited icon set (MAJ-003) blocks the icon picker feature and causes core agent icons to fall back incorrectly. The responsive breakpoint shift (MAJ-004) misclassifies tablet-width devices as phones.

### Recommended Next Actions

- [ ] Fix avatar color palette to match spec's 7 named colors (MAJ-001)
- [ ] Decide on settings navigation approach: keep tabs or implement spec's card-grid pattern (MAJ-002)
- [ ] Expand IconRenderer to cover core agent defaults at minimum (MAJ-003)
- [ ] Align responsive breakpoints with spec's 640px phone boundary (MAJ-004)
- [ ] Replace PuzzlePiece with Puzzle in sidebar (MIN-001)
- [ ] Audit SessionBar layout against spec wireframe (MIN-002)
