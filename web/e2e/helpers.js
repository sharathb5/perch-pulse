import { expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')

function loadFixture(name) {
  return JSON.parse(readFileSync(join(root, 'fixtures', name), 'utf8'))
}

export const fixtures = {
  graphOk: loadFixture('graph.ok.json'),
  statusOk: loadFixture('status.ok.json'),
  graphEmpty: loadFixture('graph.empty.json'),
  statusEmpty: loadFixture('status.empty.json'),
}

/**
 * @param {import('@playwright/test').Page} page
 * @param {{ graph?: object, status?: object, graphStatus?: number, statusStatus?: number }} opts
 */
export async function mockApis(page, opts = {}) {
  const graph = opts.graph ?? fixtures.graphOk
  const status = opts.status ?? fixtures.statusOk
  const graphStatus = opts.graphStatus ?? 200
  const statusStatus = opts.statusStatus ?? 200

  await page.route('**/api/graph**', async (route) => {
    if (graphStatus >= 400) {
      await route.fulfill({
        status: graphStatus,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'graph unavailable' }),
      })
      return
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(graph),
    })
  })

  await page.route('**/api/status**', async (route) => {
    if (statusStatus >= 400) {
      await route.fulfill({
        status: statusStatus,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'status unavailable' }),
      })
      return
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(status),
    })
  })

  await page.route('**/api/logs**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ source: 'none', setup_hint: '', stdout_lines: [] }),
    })
  })
  await page.route('**/api/credentials**', async (route) => {
    await route.fulfill({ status: 405, body: 'method not allowed in e2e' })
  })
}

/** Stabilize rendering before screenshots. */
export async function stabilize(page) {
  await page.addStyleTag({
    content: `
      *, *::before, *::after {
        animation: none !important;
        transition: none !important;
        caret-color: transparent !important;
      }
      html, body, button, input, select, textarea {
        font-family: Arial, Helvetica, sans-serif !important;
      }
      .react-flow__edgepath, .react-flow__connectionpath {
        stroke-dasharray: none !important;
        animation: none !important;
      }
    `,
  })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.waitForTimeout(200)
}

/**
 * @param {import('@playwright/test').Page} page
 */
export async function expectGraphReady(page) {
  await expect(page.getByTestId('perch-navbar')).toBeVisible()
  await expect(page.getByTestId('perch-graph')).toBeVisible()
}

/** Mask React Flow chrome that is easy to AA-flake across runs. */
export function screenshotOpts(page) {
  return {
    fullPage: true,
    mask: [page.locator('.react-flow__minimap'), page.locator('.react-flow__controls')],
  }
}
