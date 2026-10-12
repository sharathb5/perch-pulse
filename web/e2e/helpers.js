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
  pulseServicesEmpty: loadFixture('pulse.services.empty.json'),
  pulseServicesHistorical: loadFixture('pulse.services.historical.json'),
  pulseServicesOpen: loadFixture('pulse.services.open.json'),
  pulseServicesUnavailable: loadFixture('pulse.services.unavailable.json'),
  pulseIncidentsHistorical: loadFixture('pulse.incidents.historical.json'),
  pulseIncidentsOpen: loadFixture('pulse.incidents.open.json'),
  pulseIncidentDetail: loadFixture('pulse.incident.detail.json'),
  pulseChanges: loadFixture('pulse.changes.json'),
  pulseChangesEmpty: loadFixture('pulse.changes.empty.json'),
  pulseWorkflowsUnavailable: loadFixture('pulse.workflows.unavailable.json'),
  pulseWorkflowsAvailable: loadFixture('pulse.workflows.available.json'),
  pulseRunCreate: loadFixture('pulse.run.create.json'),
  pulseRunCompleted: loadFixture('pulse.run.completed.json'),
  pulseRunCompletedGraph: loadFixture('pulse.run.completed.graph.json'),
  pulseRunRunning: loadFixture('pulse.run.running.json'),
  pulseRunFailed: loadFixture('pulse.run.failed.json'),
  pulseLiveStatusDisabled: loadFixture('pulse.live.status.disabled.json'),
  pulseLiveStatusCollecting: loadFixture('pulse.live.status.collecting.json'),
  pulseLiveStatusWarming: loadFixture('pulse.live.status.warming.json'),
  pulseLiveServicesRegression: loadFixture('pulse.live.services.regression.json'),
  pulseLiveIncidentsActive: loadFixture('pulse.live.incidents.active.json'),
  pulseInvestigationCreate: loadFixture('pulse.investigation.create.json'),
}

/**
 * @param {import('@playwright/test').Page} page
 * @param {{
 *   graph?: object,
 *   status?: object,
 *   graphStatus?: number,
 *   statusStatus?: number,
 *   pulseServices?: object,
 *   pulseIncidents?: object,
 *   pulseIncidentDetail?: object,
 *   pulseChanges?: object,
 *   pulseStatus?: number,
 *   pulseWorkflows?: object,
 *   pulseRun?: object,
 *   pulseRunCreate?: object,
 *   pulseWorkflowsStatus?: number,
 *   pulseRunsStatus?: number,
 *   pulseLiveStatus?: object,
 *   pulseLiveServices?: object,
 *   pulseLiveIncidents?: object,
 *   pulseInvestigation?: object,
 *   pulseInvestigationStatus?: number,
 * }} opts
 */
export async function mockApis(page, opts = {}) {
  const graph = opts.graph ?? fixtures.graphOk
  const status = opts.status ?? fixtures.statusOk
  const graphStatus = opts.graphStatus ?? 200
  const statusStatus = opts.statusStatus ?? 200
  // Default empty Pulse success keeps existing screenshot baselines stable (no indicators).
  const pulseServices = opts.pulseServices ?? fixtures.pulseServicesEmpty
  const pulseIncidentsResolved =
    opts.pulseIncidents ??
    (opts.pulseServices === fixtures.pulseServicesHistorical
      ? fixtures.pulseIncidentsHistorical
      : opts.pulseServices === fixtures.pulseServicesOpen
        ? fixtures.pulseIncidentsOpen
        : loadEmptyIncidents())
  const pulseDetail = opts.pulseIncidentDetail ?? fixtures.pulseIncidentDetail
  const pulseChangesResolved =
    opts.pulseChanges ??
    (opts.pulseServices === fixtures.pulseServicesHistorical ||
    opts.pulseServices === fixtures.pulseServicesOpen
      ? fixtures.pulseChanges
      : fixtures.pulseChangesEmpty)
  const pulseStatus = opts.pulseStatus ?? 200
  const pulseWorkflows = opts.pulseWorkflows ?? fixtures.pulseWorkflowsUnavailable
  const pulseRun = opts.pulseRun ?? fixtures.pulseRunCompletedGraph
  const pulseRunCreate = opts.pulseRunCreate ?? fixtures.pulseRunCreate
  const pulseWorkflowsStatus = opts.pulseWorkflowsStatus ?? 200
  const pulseRunsStatus = opts.pulseRunsStatus ?? 200
  // Default disabled Live Pulse keeps prior screenshot baselines stable.
  const pulseLiveStatus = opts.pulseLiveStatus ?? fixtures.pulseLiveStatusDisabled
  const pulseLiveServices =
    opts.pulseLiveServices ??
    ({
      schema_version: 'pulse.api.live_services.v1',
      generated_at: pulseLiveStatus.generated_at,
      count: 0,
      services: [],
      status: pulseLiveStatus.status,
      limitations: [],
    })
  const pulseInvestigation = opts.pulseInvestigation ?? fixtures.pulseInvestigationCreate
  const pulseInvestigationStatus = opts.pulseInvestigationStatus ?? 200
  const pulseLiveIncidents =
    opts.pulseLiveIncidents ??
    ({
      schema_version: 'pulse.api.live_incidents.v1',
      generated_at: pulseLiveStatus.generated_at,
      count: 0,
      incidents: [],
      status: pulseLiveStatus.status,
      limitations: [],
    })

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

  await page.route('**/api/pulse/live/status**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(pulseLiveStatus),
    })
  })
  await page.route('**/api/pulse/live/services**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(pulseLiveServices),
    })
  })
  await page.route('**/api/pulse/live/incidents**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(pulseLiveIncidents),
    })
  })

  await page.route('**/api/pulse/services**', async (route) => {
    if (pulseStatus >= 400) {
      await route.fulfill({
        status: pulseStatus,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'pulse.api.error.v1',
          error: 'pulse services unavailable',
          code: 'unavailable',
        }),
      })
      return
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(pulseServices),
    })
  })

  await page.route('**/api/pulse/changes**', async (route) => {
    if (pulseStatus >= 400) {
      await route.fulfill({
        status: pulseStatus,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'pulse.api.error.v1',
          error: 'pulse changes unavailable',
          code: 'unavailable',
        }),
      })
      return
    }
    const path = new URL(route.request().url()).pathname
    const isDetail = /\/api\/pulse\/changes\/.+/.test(path)
    const body = isDetail
      ? {
          schema_version: 'pulse.api.change.v1',
          generated_at: '2026-10-10T12:00:00Z',
          data_sources: pulseChangesResolved.data_sources,
          change: pulseChangesResolved.changes?.[0] ?? null,
          graph_nodes: ['api'],
        }
      : pulseChangesResolved
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(body),
    })
  })

  await page.route('**/api/pulse/investigations**', async (route) => {
    const req = route.request()
    const path = new URL(req.url()).pathname
    if (pulseInvestigationStatus >= 400) {
      await route.fulfill({
        status: pulseInvestigationStatus,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'pulse.api.error.v1',
          error: 'investigation unavailable',
          code: 'unavailable',
        }),
      })
      return
    }
    if (req.method() === 'POST' && path.endsWith('/feedback')) {
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'pulse.api.investigation.feedback.create.v1',
          generated_at: '2026-10-11T12:00:00Z',
          feedback: {
            schema_version: 'pulse.investigation.feedback.v1',
            feedback_id: 'fb|test',
            investigation_id: pulseInvestigation.investigation.investigation_id,
            incident_id: pulseInvestigation.investigation.incident_id,
            timestamp: '2026-10-11T12:00:00Z',
            assessment: 'useful',
          },
          limitations: ['Feedback is a human assessment — not verified ground truth.'],
        }),
      })
      return
    }
    if (req.method() === 'POST') {
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify(pulseInvestigation),
      })
      return
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        schema_version: 'pulse.api.investigation.v1',
        generated_at: '2026-10-11T12:00:00Z',
        investigation: pulseInvestigation.investigation,
        limitations: pulseInvestigation.limitations,
      }),
    })
  })

  await page.route('**/api/pulse/incidents**', async (route) => {
    if (pulseStatus >= 400) {
      await route.fulfill({
        status: pulseStatus,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'pulse.api.error.v1',
          error: 'pulse incidents unavailable',
          code: 'unavailable',
        }),
      })
      return
    }
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/investigation-context')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'pulse.api.investigation_context.v1',
          generated_at: '2026-10-11T12:00:00Z',
          investigation: pulseInvestigation.investigation,
          agent_context: {
            schema_version: 'pulse.investigation.agent_context.v1',
            investigation_id: pulseInvestigation.investigation.investigation_id,
            incident_id: pulseInvestigation.investigation.incident_id,
            facts: ['fixture fact'],
            hypotheses: ['fixture hypothesis'],
            unknown: ['Root cause is UNKNOWN'],
            next_steps: ['Verify carefully'],
            diagnosed: false,
          },
          limitations: [],
        }),
      })
      return
    }
    // Detail: /api/pulse/incidents/{id...} — list is exactly /api/pulse/incidents
    const isDetail = /\/api\/pulse\/incidents\/.+/.test(path)
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(isDetail ? pulseDetail : pulseIncidentsResolved),
    })
  })

  await page.route('**/api/pulse/workflows**', async (route) => {
    if (pulseWorkflowsStatus >= 400) {
      await route.fulfill({
        status: pulseWorkflowsStatus,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'pulse.api.error.v1',
          error: 'workflows unavailable',
          code: 'unavailable',
        }),
      })
      return
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(pulseWorkflows),
    })
  })

  await page.route('**/api/pulse/runs**', async (route) => {
    const req = route.request()
    const path = new URL(req.url()).pathname
    if (req.method() === 'POST') {
      if (pulseRunsStatus >= 400) {
        await route.fulfill({
          status: pulseRunsStatus,
          contentType: 'application/json',
          body: JSON.stringify({
            schema_version: 'pulse.api.error.v1',
            error: 'could not start diagnostic',
            code: 'unavailable',
          }),
        })
        return
      }
      await route.fulfill({
        status: 202,
        contentType: 'application/json',
        body: JSON.stringify(pulseRunCreate),
      })
      return
    }
    // GET /api/pulse/runs/{id}
    if (pulseRunsStatus >= 400 || !/\/api\/pulse\/runs\/.+/.test(path)) {
      await route.fulfill({
        status: pulseRunsStatus >= 400 ? pulseRunsStatus : 404,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'pulse.api.error.v1',
          error: 'run not found',
          code: 'not_found',
        }),
      })
      return
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(pulseRun),
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

function loadEmptyIncidents() {
  return {
    schema_version: 'pulse.api.incidents.v1',
    generated_at: '2026-10-10T12:00:00Z',
    data_sources: {
      observations: 'process_memory_unavailable',
      incidents: 'file_store',
      changes: 'file_store',
    },
    count: 0,
    limit: 50,
    incidents: [],
  }
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
    mask: [
      page.locator('.react-flow__minimap'),
      page.locator('.react-flow__controls'),
      // Belt-and-suspenders if a wall-clock stamp ever leaks into the panel.
      page.getByTestId('pulse-refreshed-at'),
    ],
  }
}
