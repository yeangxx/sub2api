import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import ts from 'typescript'

const source = readFileSync(
  resolve(process.cwd(), 'src/components/account/CreateAccountModal.vue'),
  'utf8'
)

const scriptSource = source.slice(
  source.indexOf('>', source.indexOf('<script setup')) + 1,
  source.lastIndexOf('</script>')
)

const script = ts.createSourceFile(
  'CreateAccountModal.ts',
  scriptSource,
  ts.ScriptTarget.Latest,
  true,
  ts.ScriptKind.TS
)

const isAdminAccountCreateCall = (node: ts.Node): node is ts.CallExpression => {
  if (!ts.isCallExpression(node) || !ts.isPropertyAccessExpression(node.expression)) return false
  const create = node.expression
  if (create.name.text !== 'create' || !ts.isPropertyAccessExpression(create.expression)) return false
  const accounts = create.expression
  return accounts.name.text === 'accounts' && ts.isIdentifier(accounts.expression) && accounts.expression.text === 'adminAPI'
}

const containsRoutingHelper = (node: ts.Node): boolean => {
  let found = false
  const visit = (child: ts.Node) => {
    if (
      ts.isCallExpression(child) &&
      ts.isIdentifier(child.expression) &&
      child.expression.text === 'withAccountRoutingCreateFields'
    ) {
      found = true
      return
    }
    ts.forEachChild(child, visit)
  }
  visit(node)
  return found
}

const accountCreateCalls: ts.CallExpression[] = []
const collectAccountCreateCalls = (node: ts.Node) => {
  if (isAdminAccountCreateCall(node)) accountCreateCalls.push(node)
  ts.forEachChild(node, collectAccountCreateCalls)
}
collectAccountCreateCalls(script)

describe('CreateAccountModal Grok account types', () => {
  it('routes every account create call through the routing field payload helper', () => {
    expect(accountCreateCalls).toHaveLength(6)
    accountCreateCalls.forEach((call) => {
      expect(call.arguments[0]).toBeDefined()
      expect(containsRoutingHelper(call.arguments[0]!)).toBe(true)
    })
    expect(source).toContain('<AccountRoutingFields')
    expect(source).toContain("form.price_book_id = null")
  })

  it('offers API-key setup alongside OAuth with the official xAI default', () => {
    expect(source).toContain('data-testid="grok-account-type-api-key"')
    expect(source).toContain("@click=\"accountCategory = 'apikey'\"")
    expect(source).toContain("newPlatform === 'grok'")
    expect(source).toContain("? 'https://api.x.ai/v1'")
    expect(source).toContain("form.platform === 'grok'")
    expect(source).toContain("? 'xai-...'")
  })
})
