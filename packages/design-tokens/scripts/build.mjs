// 将 contracts/design/tokens.json 生成为 Tailwind v4 的 @theme 块。
//
// 这是"设计契约 → CSS 变量"的唯一生成入口；theme.css 是生成物，禁止手改。
// 真相源在 contracts/design/tokens.json，本包只负责生成与分发。
// 支持两层结构：color.brand.500 → --color-brand-500。
import { readFileSync, writeFileSync } from 'node:fs'

const tokens = JSON.parse(readFileSync(new URL('../../../contracts/design/tokens.json', import.meta.url), 'utf8'))

const lines = ['/* 生成物：由 scripts/build.mjs 从 tokens.json 生成，禁止手改。 */', '@theme {']

for (const [name, value] of Object.entries(tokens.color ?? {})) {
  if (typeof value === 'string') {
    lines.push(`  --color-${name}: ${value};`)
    continue
  }
  for (const [step, v] of Object.entries(value)) {
    lines.push(`  --color-${name}-${step}: ${v};`)
  }
}

for (const [name, value] of Object.entries(tokens.radius ?? {})) {
  lines.push(`  --radius-${name}: ${value};`)
}

lines.push('}')

writeFileSync(new URL('../theme.css', import.meta.url), lines.join('\n') + '\n')
console.log('design-tokens: theme.css written')
