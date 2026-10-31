import fs from 'node:fs'
import path from 'node:path'
import type { FullResult, Reporter, TestCase, TestResult } from '@playwright/test/reporter'

// Lists every test that passed only on a retry (a flake) in test-results/flaky.json.
export default class FlakyReporter implements Reporter {
  private flaky: { title: string; project: string; retries: number }[] = []

  onTestEnd(test: TestCase, result: TestResult) {
    if (result.status === 'passed' && result.retry > 0) {
      this.flaky.push({ title: test.titlePath().slice(1).join(' > '), project: test.parent.project()?.name ?? '', retries: result.retry })
    }
  }

  onEnd(_result: FullResult) {
    const dir = path.resolve('test-results')
    fs.mkdirSync(dir, { recursive: true })
    fs.writeFileSync(path.join(dir, 'flaky.json'), JSON.stringify(this.flaky, null, 2) + '\n')
  }
}
