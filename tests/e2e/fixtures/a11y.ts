import { type Page, expect } from '@playwright/test';
import { AxeBuilder } from '@axe-core/playwright';
import { type ImpactValue } from 'axe-core';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import { z } from 'zod';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const BASELINE_PATH = path.join(__dirname, 'a11y-baseline.json');

const BaselineEntrySchema = z.object({
  rule: z.string(),
  selector: z.string(),
  reason: z.string(),
  ticket: z.string().optional(),
});

const BaselineSchema = z.object({
  violations: z.array(BaselineEntrySchema),
});

type BaselineEntry = z.infer<typeof BaselineEntrySchema>;

interface A11yOptions {
  include?: string[];
  exclude?: string[];
}

function loadBaseline(): BaselineEntry[] {
  if (!fs.existsSync(BASELINE_PATH)) {
    return [];
  }
  const raw = fs.readFileSync(BASELINE_PATH, 'utf-8');
  const parsed = JSON.parse(raw);
  const result = BaselineSchema.safeParse(parsed);
  if (!result.success) {
    throw new Error(
      `a11y baseline at ${BASELINE_PATH} failed validation:\n${result.error.toString()}\n` +
        'Fix or delete the baseline file — a corrupted baseline is never silently ignored.',
    );
  }
  return result.data.violations;
}

async function expectA11yClean(page: Page, opts?: A11yOptions): Promise<void> {
  const baseline = loadBaseline();
  const baselineKeys = new Set(baseline.map((e) => `${e.rule}::${e.selector}`));

  let builder = new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']);

  if (opts?.include && opts.include.length > 0) {
    builder = builder.include(opts.include);
  }
  if (opts?.exclude && opts.exclude.length > 0) {
    builder = builder.exclude(opts.exclude);
  }

  const results = await builder.analyze();

  const SERIOUS_IMPACTS: ImpactValue[] = ['serious', 'critical'];

  const actionableViolations = results.violations.filter((v) => {
    if (!SERIOUS_IMPACTS.includes(v.impact as ImpactValue)) return false;
    return v.nodes.some((node) => {
      const selector = node.target.join(', ');
      return !baselineKeys.has(`${v.id}::${selector}`);
    });
  });

  expect(
    actionableViolations,
    `Axe found ${actionableViolations.length} serious/critical accessibility violation(s):\n${actionableViolations
      .map((v) => `  [${v.impact}] ${v.id}: ${v.description}`)
      .join('\n')}`,
  ).toHaveLength(0);
}

export { expectA11yClean };
