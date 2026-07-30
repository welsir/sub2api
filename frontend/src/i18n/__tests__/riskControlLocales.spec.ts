import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

describe('risk control locale copy', () => {
  it('describes worker runtime as audit and pre-block record processing', () => {
    expect(zh.admin.riskControl.workerStatusHint).toContain('前置拦截记录任务')
    expect(zh.admin.riskControl.workerStatusHint).not.toContain('异步观察任务')
    expect(en.admin.riskControl.workerStatusHint).toContain('pre-block record tasks')
    expect(en.admin.riskControl.workerStatusHint).not.toContain('observation tasks')
  })

  it('keeps pre-block audit key summary aware of async worker load', () => {
    expect(zh.admin.riskControl.preBlockAPIKeyLoadSummary).toContain('worker：{workerActive} / {workerTotal}')
    expect(en.admin.riskControl.preBlockAPIKeyLoadSummary).toContain('worker: {workerActive} / {workerTotal}')
  })

  it('does not describe pre-block audit key polling as bypassing the worker pool', () => {
    expect(zh.admin.riskControl.preBlockAPIKeyLoadHint).toBe('同步前置拦截直接轮询可用审核 Key。')
    expect(zh.admin.riskControl.preBlockAPIKeyLoadHint).not.toContain('Worker 池')
    expect(en.admin.riskControl.preBlockAPIKeyLoadHint).not.toContain('worker pool')
  })

  it('describes default-on moderation as exempt-only for current, future, and ungrouped requests', () => {
    expect(zh.admin.riskControl.groupScopeHint).toContain('除显式豁免外')
    expect(zh.admin.riskControl.groupScopeHint).toContain('新建')
    expect(zh.admin.riskControl.groupScopeHint).toContain('未分组')
    expect(en.admin.riskControl.groupScopeHint).toContain('explicitly exempted')
    expect(en.admin.riskControl.groupScopeHint).toContain('newly created')
    expect(en.admin.riskControl.groupScopeHint).toContain('ungrouped')
  })

  it('labels excluded groups and the active runtime scope explicitly', () => {
    expect(zh.admin.riskControl.excludedGroups).toBe('豁免分组')
    expect(zh.admin.riskControl.excludedGroupsHint).toContain('名称和 ID')
    expect(zh.admin.riskControl.runtimeDefaultOn).toContain('默认审核')
    expect(en.admin.riskControl.excludedGroups).toBe('Excluded groups')
    expect(en.admin.riskControl.excludedGroupsHint).toContain('name and ID')
    expect(en.admin.riskControl.runtimeDefaultOn).toContain('Default-on')
  })
})
