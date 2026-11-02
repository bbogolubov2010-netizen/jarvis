import { describe, expect, it } from 'vitest'
import { renderMarkdown } from './markdown'

describe('renderMarkdown', () => {
  it('returns an empty string for empty input', () => {
    expect(renderMarkdown('')).toBe('')
  })

  it('escapes raw HTML so the result is safe for v-html', () => {
    const html = renderMarkdown('<img src=x onerror="alert(1)">')
    expect(html).not.toContain('<img')
    expect(html).toContain('&lt;img')
    expect(html).toContain('&quot;')
  })

  it('renders paragraphs joined across wrapped lines', () => {
    expect(renderMarkdown('first line\nsecond line')).toBe('<p>first line second line</p>')
    expect(renderMarkdown('one\n\ntwo')).toBe('<p>one</p><p>two</p>')
  })

  it('renders inline bold, italic and code', () => {
    expect(renderMarkdown('**bold** and *italic*')).toBe('<p><strong>bold</strong> and <em>italic</em></p>')
    expect(renderMarkdown('use `npm test` now')).toBe('<p>use <code>npm test</code> now</p>')
  })

  it('does not apply inline formatting inside code spans', () => {
    const html = renderMarkdown('`**not bold**`')
    expect(html).toBe('<p><code>**not bold**</code></p>')
  })

  it('renders headings as h3 through h5', () => {
    expect(renderMarkdown('# one')).toBe('<h3>one</h3>')
    expect(renderMarkdown('## two')).toBe('<h4>two</h4>')
    expect(renderMarkdown('### three')).toBe('<h5>three</h5>')
  })

  it('renders unordered and ordered lists', () => {
    expect(renderMarkdown('- one\n- two')).toBe('<ul><li>one</li></ul><ul><li>two</li></ul>')
    expect(renderMarkdown('1. first\n2. second')).toBe('<ol><li>first</li></ol><ol><li>second</li></ol>')
  })

  it('renders quotes and horizontal rules', () => {
    expect(renderMarkdown('> quoted')).toBe('<p class="md-quote">quoted</p>')
    expect(renderMarkdown('---')).toBe('<hr>')
  })

  it('renders fenced code blocks with an escaped language', () => {
    const html = renderMarkdown('```go\nfmt.Println("<hi>")\n```')
    expect(html).toBe('<pre class="md-code"><code data-lang="go">fmt.Println(&quot;&lt;hi&gt;&quot;)</code></pre>')
  })

  it('renders fenced code blocks without a language', () => {
    expect(renderMarkdown('```\nplain\n```')).toBe('<pre class="md-code"><code>plain</code></pre>')
  })

  it('allows only http(s) links and escapes the label', () => {
    expect(renderMarkdown('[docs](https://example.com/docs)')).toBe(
      '<p><a href="https://example.com/docs" rel="noopener noreferrer">docs</a></p>'
    )
    const unsafe = renderMarkdown('[x](javascript:alert(1))')
    expect(unsafe).not.toContain('<a ')
  })
})
