// Minimal, dependency-free Markdown renderer for chat messages.
// All input is HTML-escaped before formatting, so the result is safe for v-html.

function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, (character) => {
    switch (character) {
      case '&': return '&amp;'
      case '<': return '&lt;'
      case '>': return '&gt;'
      case '"': return '&quot;'
      default: return '&#39;'
    }
  })
}

function renderInline(raw: string): string {
  let text = escapeHtml(raw)
  const codeSpans: string[] = []

  text = text.replace(/`([^`]+)`/g, (_match, code: string) => {
    codeSpans.push(`<code>${code}</code>`)
    return `\u0000${codeSpans.length - 1}\u0000`
  })

  text = text.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
  text = text.replace(/(^|[^*])\*([^*\n]+)\*/g, '$1<em>$2</em>')
  text = text.replace(
    /\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g,
    '<a href="$2" rel="noopener noreferrer">$1</a>'
  )
  text = text.replace(/\u0000(\d+)\u0000/g, (_match, index: string) => codeSpans[Number(index)] ?? '')

  return text
}

function renderTextBlock(source: string): string {
  const lines = source.split('\n')
  const output: string[] = []
  let paragraph: string[] = []

  const flushParagraph = () => {
    if (paragraph.length === 0) return
    output.push(`<p>${renderInline(paragraph.join(' '))}</p>`)
    paragraph = []
  }

  for (const rawLine of lines) {
    const line = rawLine.trim()
    if (line === '') {
      flushParagraph()
      continue
    }

    const heading = /^(#{1,3})\s+(.*)$/.exec(line)
    if (heading) {
      flushParagraph()
      const level = heading[1].length + 2
      output.push(`<h${level}>${renderInline(heading[2])}</h${level}>`)
      continue
    }

    if (/^([-*])\s+/.test(line)) {
      flushParagraph()
      output.push(`<ul><li>${renderInline(line.replace(/^([-*])\s+/, ''))}</li></ul>`)
      continue
    }

    if (/^\d+\.\s+/.test(line)) {
      flushParagraph()
      output.push(`<ol><li>${renderInline(line.replace(/^\d+\.\s+/, ''))}</li></ol>`)
      continue
    }

    if (line.startsWith('> ')) {
      flushParagraph()
      output.push(`<p class="md-quote">${renderInline(line.slice(2))}</p>`)
      continue
    }

    if (line === '---') {
      flushParagraph()
      output.push('<hr>')
      continue
    }

    paragraph.push(line)
  }

  flushParagraph()
  return output.join('')
}

export function renderMarkdown(source: string): string {
  if (!source) return ''

  const chunks = source.split(/```/)
  const html: string[] = []

  chunks.forEach((chunk, index) => {
    if (index % 2 === 0) {
      html.push(renderTextBlock(chunk))
      return
    }

    const newline = chunk.indexOf('\n')
    const language = newline >= 0 ? chunk.slice(0, newline).trim() : ''
    const code = newline >= 0 ? chunk.slice(newline + 1) : chunk
    const languageAttribute = language ? ` data-lang="${escapeHtml(language)}"` : ''
    html.push(
      `<pre class="md-code"><code${languageAttribute}>${escapeHtml(code.replace(/\n$/, ''))}</code></pre>`
    )
  })

  return html.join('')
}
