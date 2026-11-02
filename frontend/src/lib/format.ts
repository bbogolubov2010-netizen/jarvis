export function errorText(error: unknown): string {
  if (error instanceof Error) return error.message
  return String(error)
}

export function greeting(date: Date = new Date()): string {
  const hour = date.getHours()
  if (hour < 12) return 'Good morning'
  if (hour < 18) return 'Good afternoon'
  return 'Good evening'
}

export function gameDuration(seconds: number): string {
  if (!seconds || seconds < 60) return 'less than a minute'
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return minutes + ' min'
  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60
  return rest ? hours + ' h ' + rest + ' min' : hours + ' h'
}

export function catalogUpdatedLabel(updatedAt: number): string {
  if (!updatedAt) return 'not scanned yet'
  return new Date(updatedAt).toLocaleString()
}
