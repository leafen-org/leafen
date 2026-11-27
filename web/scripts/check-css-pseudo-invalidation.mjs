#!/usr/bin/env node

// Rules that have Chrome work out again, at every move of the pointer or the
// focus anywhere in the page, all that the element whose state changed holds:
//
// Chrome keeps what a state's rules name after it in one set for the state —
// :hover's, :focus's — whatever element the rule ties the state to, and
// applies that set under every element whose state changes: under the cell,
// row, table, block and page the pointer leaves or enters, under the editor
// the focus leaves. A rule whose state sits before a descendant or child
// combinator and whose last compound has no class, id or attribute of its
// own (`*`, `:not(.x)`, a type, or an attribute every element may carry, as
// check-css-sibling-invalidation.mjs counts them) puts "everything", or
// every element of that type, into that set: one sidebar menu's
// `[data-workspace-order-row]:hover > .profileWorkspaceIcon > :not(.handle)`
// restyled the whole page as the pointer left a table for an opening select
// menu, `.ruleHandle:hover > span` every span and a comment column's
// `.reactionEntry:not(:hover, :focus-within) … > button:not(…)` every button
// (2026-11-22: 33, 15 and 9 ms of 70 at CPU ×2 — a table cell's menu
// painted 190–250 ms after its press). So this check refuses such a rule for
// the states the pointer and the focus move — :hover, :active, :focus,
// :focus-visible, :focus-within — even inside :not(), :is() or :where(). Let
// the element in that state carry the value as a custom property its
// children take (`.x:hover { --x-ground: … }`, `.x > span { background:
// var(--x-ground, none) }`), or name the last element by a class of its own.
//
// A state of a control alone (:disabled, :checked) changes on that control,
// whose few children it restyles, and is left to the rule's own judgement.

import { relative } from 'node:path'

import { bareEnd, compounds, list, selectors, styleSources, unwrapped, webRoot, withoutComments } from './css-selector-parts.mjs'

const movedStates = /:(?:hover|active|focus|focus-visible|focus-within)(?![\w-])/u

// A :has() argument's states are the :has() rule's own (check-css-sibling-invalidation.mjs).
const withoutHas = (compound) => {
  let out = compound
  for (let at = out.indexOf(':has('); at >= 0; at = out.indexOf(':has(')) {
    let depth = 0
    let end = at + 4
    for (; end < out.length; end++) {
      if (out[end] === '(') depth++
      else if (out[end] === ')' && --depth === 0) break
    }
    out = out.slice(0, at) + out.slice(end + 1)
  }
  return out
}

function stateSweeps(selector) {
  const found = []
  const { items, nested } = compounds(selector)
  const last = items.at(-1)?.text ?? ''
  const own = last.replace(/::[\w-]+(?:\([^)]*\))?$/u, '')
  const state = items.slice(0, -1).find(({ text }) => movedStates.test(withoutHas(text)))
  if (state && bareEnd(own)) found.push(`${state.text} … ${last}`)
  for (const inner of nested) found.push(...stateSweeps(inner))
  return found
}

const failures = []
for (const [path, text] of styleSources()) {
  const name = relative(webRoot, path)
  for (const { selector, line } of selectors(withoutComments(text))) {
    for (const complex of list(unwrapped(selector))) {
      for (const sweep of stateSweeps(complex)) failures.push(`${name}:${line}: ${complex} (${sweep})`)
    }
  }
}

if (failures.length) {
  console.error('Rules with a pointer or focus state before an element named by no class, id or attribute, restyling all under every element whose state changes:')
  for (const failure of failures) console.error(`  ${failure}`)
  console.error('Let the element in that state carry the value as a custom property its children take, or name the last element by a class of its own (check-css-pseudo-invalidation.mjs).')
  process.exit(1)
}
