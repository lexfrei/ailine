import type { EngineInterface, Register, SessionUsage, Timer } from 'claude-code'

function payload(model: string, cwd: string, transcriptPath: string, usage: SessionUsage) {
  const rateLimits: Record<string, { used_percentage: number; resets_at: number }> = {}
  for (const window of usage.rateLimits) {
    if (window.kind !== 'five_hour' && window.kind !== 'seven_day') continue
    const reset = Date.parse(window.resetsAt ?? '')
    if (!Number.isFinite(reset) || !Number.isFinite(window.percentUsed)) continue
    rateLimits[window.kind] = { used_percentage: window.percentUsed, resets_at: reset / 1000 }
  }
  return {
    model: { id: model, display_name: model },
    cwd,
    transcript_path: transcriptPath,
    context_window: usage.context.percent === undefined ? undefined : { used_percentage: usage.context.percent },
    cost: usage.cost === undefined ? undefined : { total_cost_usd: usage.cost.usd },
    rate_limits: rateLimits,
  }
}

async function refresh($: EngineInterface, state: { binary: string; active: boolean; busy: boolean; text: string | undefined }) {
  if (!state.active || state.busy) return
  state.busy = true
  try {
    const [model, cwd, info, usage] = await Promise.all([
      $.session.model(), $.session.cwd(), $.session.info(), $.session.usage(),
    ])
    const argv = [state.binary, '--hyperlinks=false']
    if (usage.cost === undefined) argv.push('--cost=false')
    const result = await $.process.run(argv, {
      cwd,
      stdin: JSON.stringify(payload(model, cwd, info.transcriptPath, usage)),
      timeoutMs: 8000,
      env: { COLUMNS: '0' },
    })
    if (result.exitCode !== 0 || result.isStdoutTruncated) throw new Error('ailine failed')
    if (state.active) state.text = result.stdout.replace(/\x1b\[[0-9;]*m/g, '').trim() || undefined
  } catch {
    if (state.active) state.text = 'ailine: unavailable (check executable and config)'
  } finally {
    state.busy = false
    if (state.active) $.ui.invalidate('ui.render')
  }
}

export const register: Register = (on, options) => {
  const state = { binary: String(options.binary ?? 'ailine'), active: false, busy: false, text: undefined as string | undefined }
  let timer: Timer | undefined

  on('session.start', async ($, e, next) => {
    const result = await next(e)
    timer?.cancel()
    state.active = e.isInteractive
    if (state.active) {
      await refresh($, state)
      timer = $.clock.every(5000, () => { void refresh($, state) })
    }
    return result
  })

  on('ui.render', { component: 'AbovePrompt' }, ($, e, next) => {
    if (!state.text || e.props.hasSurvey) return next(e)
    const { Text } = $.ui.resolve(e)
    return <Text>{state.text}</Text>
  })

  on('session.end', ($, e, next) => {
    if (e.reason !== 'clear' && e.reason !== 'resume') {
      state.active = false
      timer?.cancel()
      state.text = undefined
      $.ui.invalidate('ui.render')
    }
    return next(e)
  })
}
