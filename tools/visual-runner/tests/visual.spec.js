const { test, expect } = require("@playwright/test");
const fs = require("fs");
const path = require("path");
const { PNG } = require("pngjs");
const pixelmatch = require("pixelmatch");

const targetsFile = process.env.TARGETS_FILE || "/app/ui-tests/targets.json";
const artifactsRoot = process.env.ARTIFACT_DIR || "/app/.artifacts/visual";
const baselineDir = path.join(artifactsRoot, "baseline");
const currentDir = path.join(artifactsRoot, "current");
const diffDir = path.join(artifactsRoot, "diff");
const threshold = Number(process.env.PIXEL_THRESHOLD || 100); // max differing pixels allowed

function loadTargets() {
  if (!fs.existsSync(targetsFile)) {
    throw new Error(`Missing targets file: ${targetsFile}`);
  }
  const cfg = JSON.parse(fs.readFileSync(targetsFile, "utf-8"));
  if (!cfg.baseURL || !cfg.routes || cfg.routes.length === 0) {
    throw new Error("targets.json must include baseURL and routes[]");
  }
  cfg.viewports =
    cfg.viewports ||
    [
      { width: 1280, height: 720, name: "desktop" },
      { width: 375, height: 667, name: "mobile" },
    ];
  return cfg;
}

function ensureDirs() {
  [artifactsRoot, baselineDir, currentDir, diffDir].forEach((d) =>
    fs.mkdirSync(d, { recursive: true })
  );
}

const cfg = loadTargets();
ensureDirs();

test.use({ headless: true, ignoreHTTPSErrors: true });

cfg.routes.forEach((route) => {
  const routeName = route.name || route.path.replace(/\W+/g, "_");
  cfg.viewports.forEach((vp) => {
    const shotName = `${routeName}-${vp.name}.png`;
    test(`${routeName} [${vp.name}]`, async ({ page }) => {
      await page.setViewportSize({ width: vp.width, height: vp.height });
      await page.goto(cfg.baseURL + route.path, { waitUntil: "networkidle" });
      if (route.waitFor) {
        await page.waitForSelector(route.waitFor, { timeout: 10000 });
      } else {
        await page.waitForTimeout(route.waitMs || 500);
      }

      const currentPath = path.join(currentDir, shotName);
      await page.screenshot({ path: currentPath, fullPage: true });

      const baselinePath = path.join(baselineDir, shotName);
      if (!fs.existsSync(baselinePath)) {
        fs.copyFileSync(currentPath, baselinePath);
        test.info().annotations.push({
          type: "baseline",
          description: `Created baseline ${shotName}`,
        });
        return;
      }

      const baseline = PNG.sync.read(fs.readFileSync(baselinePath));
      const current = PNG.sync.read(fs.readFileSync(currentPath));
      if (
        baseline.width !== current.width ||
        baseline.height !== current.height
      ) {
        throw new Error(
          `Dimension mismatch for ${shotName}: baseline ${baseline.width}x${baseline.height}, current ${current.width}x${current.height}`
        );
      }
      const diff = new PNG({ width: baseline.width, height: baseline.height });
      const diffCount = pixelmatch(
        baseline.data,
        current.data,
        diff.data,
        baseline.width,
        baseline.height,
        { threshold: 0.1 }
      );
      if (diffCount > 0) {
        const diffPath = path.join(diffDir, shotName);
        fs.writeFileSync(diffPath, PNG.sync.write(diff));
      }
      expect(diffCount).toBeLessThanOrEqual(threshold);
    });
  });
});
