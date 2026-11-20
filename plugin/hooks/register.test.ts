import type { On, SessionUsage } from 'claude-code'
import type { Engine } from 'claude-code/testing'
import { expect, mock, test } from 'claude-code/testing'

const band = {
  plugin: 'ailine', component: 'AbovePrompt', surface: 'terminal',
  props: {
    hasSurvey: false, isCollapsed: false, isWorking: false,
    maxRows: 5, bodyColumns: 120, scroll: { offset: 0, bodyRows: 5 }, view: {},
  },
} as const

async function row($: Engine) {
  const ui = await $.ui.mount(band)
  const text = (await ui.find({ type: 'Text', text: /./ }))?.text
  await ui.unmount()
  return text
}

function session(on: On) {
  const clock = mock.clock(on)
  const inputs: { argv: readonly string[]; stdin?: string }[] = []
  const rows: (string | undefined)[] = []
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  on('session.end', (_, e) => ({ sessionId: e.sessionId }))
  on('session.cwd', () => ({ value: '/workspace' }))
  on('session.model', () => ({ value: 'claude-opus-5-5' }))
  on('session.info', () => ({ value: { transcriptPath: '/workspace/session.jsonl' } }))
  const state = {
    usage: {
      startedAt: 0,
      context: { window: 1000000, percent: 37 },
      rateLimits: [
        { kind: 'five_hour', percentUsed: 23, resetsAt: '2026-11-21T00:00:00Z' },
        { kind: 'seven_day', percentUsed: 42, resetsAt: '2026-11-25T00:00:00Z' },
        { kind: 'spend_limit', percentUsed: 120 },
      ],
      cost: { usd: 1.25 },
    } as SessionUsage,
    output: {
      exitCode: 0, stdout: '\x1b[32m🧠 37%\x1b[0m\n', stderr: '',
      isStdoutTruncated: false, isStderrTruncated: false,
    },
    throws: false,
    delay: 0,
  }
  on('session.usage', () => ({ value: state.usage }))
  on('process.run', async (_, e) => {
    inputs.push({ argv: e.argv, stdin: e.init?.stdin })
    if (state.delay) await clock.sleep(state.delay)
    if (state.throws) throw new Error('private diagnostic')
    return { value: state.output }
  })
  on('ui.status', (_, e) => {
    rows.push(e.text)
    return { value: undefined }
  })
  return { clock, inputs, rows, state }
}

const start = { cwd: '/workspace', surface: 'terminal', isInteractive: true } as const
const end = { reason: 'prompt_input_exit', sessionId: 'test', resume: { id: 'test' } } as const

test('maps available session data to the binary without inventing missing fields', async ($, on) => {
  const { inputs } = session(on)
  await $.session.start(start)
  expect(inputs.length).toBe(1)
  expect(inputs[0]!.argv).toEqual(['ailine', '--hyperlinks=false'])
  expect(JSON.parse(inputs[0]!.stdin!)).toEqual({
    model: { id: 'claude-opus-5-5', display_name: 'claude-opus-5-5' },
    cwd: '/workspace',
    transcript_path: '/workspace/session.jsonl',
    context_window: { used_percentage: 37 },
    cost: { total_cost_usd: 1.25 },
    rate_limits: {
      five_hour: { used_percentage: 23, resets_at: 1795219200 },
      seven_day: { used_percentage: 42, resets_at: 1795564800 },
    },
  })
  expect(await row($)).toBe('🧠 37%')
})

test('refreshes while idle and keeps refreshing after clear and resume', async ($, on) => {
  const { clock, inputs, state } = session(on)
  await $.session.start(start)
  await clock.advance(5000)
  expect(inputs.length).toBe(2)
  state.usage.context.percent = 50
  await $.session.end({ ...end, reason: 'clear' })
  await clock.advance(5000)
  expect(inputs.length).toBe(3)
  expect(JSON.parse(inputs[2]!.stdin!).context_window.used_percentage).toBe(50)
  await $.session.end({ ...end, reason: 'resume' })
  await clock.advance(5000)
  expect(inputs.length).toBe(4)
})

test('does not start a child in noninteractive sessions', async ($, on) => {
  const { clock, inputs } = session(on)
  await $.session.start({ ...start, surface: null, isInteractive: false })
  await clock.advance(5000)
  expect(inputs).toEqual([])
  expect(await row($)).toBeUndefined()
})

test('accepts an executable path without shell parsing', { options: { binary: '/opt/ailine tools/ailine' } }, async ($, on) => {
  const { inputs } = session(on)
  await $.session.start(start)
  expect(inputs[0]!.argv).toEqual(['/opt/ailine tools/ailine', '--hyperlinks=false'])
})

test('reports a failed executable instead of displaying partial output', async ($, on) => {
  const { state } = session(on)
  state.output.exitCode = 1
  state.output.stdout = 'partial'
  state.output.stderr = 'private diagnostic'
  await $.session.start(start)
  expect(await row($)).toBe('ailine: unavailable (check executable and config)')
})

test('omits absent context and cost and skips windows without valid reset times', async ($, on) => {
  const { inputs, state } = session(on)
  state.usage = {
    startedAt: 0, context: { window: 1000000 },
    rateLimits: [
      { kind: 'five_hour', percentUsed: 20 },
      { kind: 'seven_day', percentUsed: 30, resetsAt: 'invalid' },
    ],
  }
  await $.session.start(start)
  const payload = JSON.parse(inputs[0]!.stdin!)
  expect(payload.cost).toBeUndefined()
  expect(payload.context_window).toBeUndefined()
  expect(payload.rate_limits).toEqual({})
  expect(inputs[0]!.argv).toContain('--cost=false')
})

test('recovers from a rejected process call on the next refresh', async ($, on) => {
  const { clock, state } = session(on)
  state.throws = true
  await $.session.start(start)
  expect(await row($)).toBe('ailine: unavailable (check executable and config)')
  state.throws = false
  await clock.advance(5000)
  expect(await row($)).toBe('🧠 37%')
})

test('rejects truncated output', async ($, on) => {
  const { state } = session(on)
  state.output.isStdoutTruncated = true
  await $.session.start(start)
  expect(await row($)).toBe('ailine: unavailable (check executable and config)')
})

test('stops its timer and clears its row at exit', async ($, on) => {
  const { clock, inputs } = session(on)
  await $.session.start(start)
  await $.session.end(end)
  await clock.advance(10000)
  expect(inputs.length).toBe(1)
  expect(await row($)).toBeUndefined()
})

test('does not overlap slow children or restore a row after exit', async ($, on) => {
  const { clock, inputs, state } = session(on)
  await $.session.start(start)
  state.delay = 8000
  await clock.advance(10000)
  expect(inputs.length).toBe(2)
  await $.session.end(end)
  await clock.advance(8000)
  expect(inputs.length).toBe(2)
  expect(await row($)).toBeUndefined()
})

test('draws a native band rather than a warning notice on supported surfaces', async ($, on) => {
  const { rows } = session(on)
  await $.session.start(start)
  expect(rows).toEqual([])
  for (const surface of ['terminal', 'desktop', 'mobile'] as const) {
    const ui = await $.ui.mount({ ...band, surface })
    expect((await ui.find({ type: 'Text', text: '🧠 37%' }))?.text).toBe('🧠 37%')
    await ui.unmount()
  }
})
